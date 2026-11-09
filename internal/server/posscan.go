package server

// posscan.go implementa o PÓS-SCAN por biblioteca (briefing 0135, ADR-0031):
// o gancho que o scanner chama ao fim do scan de UMA fonte, depois de todos os
// lotes commitados. Ele existe porque o pipeline de match roda UMA única vez por
// arquivo: o que falhou na hora (rede, árvore do artista que ainda não existia,
// import de série que chegou depois dos episódios) ficava esperando o cron do job
// saude_biblioteca -- 15 itens por tipo, throttle de 15 dias.
//
// Aqui NÃO há lógica de matching nova: cada item reusa o caminho já existente
// (scanner.ReconciliarFaixaDraft / scanner.ReconciliarSerieDraft /
// Server.TentarAutoMatchFilme). O que este arquivo acrescenta é (1) o recorte por
// library_source, (2) a ausência do throttle de dias (é o retry do scan que o
// dono acabou de pedir) e (3) a materialização opcional de quem passou a ter
// identidade, quando o toggle de auto-download está ligado.
//
// Best-effort do começo ao fim: erro de banco na LISTAGEM propaga (o scanner só
// loga); erro num item individual é absorvido e logado -- um item ruim nunca
// aborta a reconciliação dos demais.

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// posScanBudget é o teto de TEMPO de parede do pós-scan de UM scan (briefing
// 0138). Cada tentativa de faixa custa chamadas externas a 1 req/s; sem teto, um
// scan com 200 faixas em rascunho bloqueou a fila de scans por 45+ min (medido
// em produção). Estourou: para onde está e deixa o resto para o próximo scan --
// as queries já ordenam por "nunca tentado primeiro", então a fila avança.
const posScanBudget = 4 * time.Minute

// esgotou diz se o orçamento de tempo do pós-scan acabou (ver posScanBudget).
func esgotou(limite time.Time) bool { return time.Now().After(limite) }

// ProcessarPosScan satisfaz scanner.PosScan (briefing 0135): reconcilía os
// rascunhos e retenta os pendentes DA FONTE `sourceID`. Devolve erro só em falha
// real de LEITURA (banco/config); o resto é best-effort.
func (s *Server) ProcessarPosScan(ctx context.Context, sourceID string) error {
	// Sem scanner ou sem provider/importador não há o que reconciliar: os dois
	// reconcilers e o auto-match degradam sozinhos, mas evitamos a varredura de
	// banco inteira nesse caso.
	if s.scanner == nil || sourceID == "" {
		return nil
	}
	langCfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return err
	}

	limite := time.Now().Add(posScanBudget)

	faixas, err := s.leitura().ListarFaixasDraftOrfasPorSource(ctx, sqlcgen.ListarFaixasDraftOrfasPorSourceParams{
		SourceID:     sql.NullString{String: sourceID, Valid: true},
		HorasMinimas: scanner.PosScanHorasMinimas,
		Limite:       scanner.CapReconciliacaoFaixas,
	})
	if err != nil {
		return err
	}
	albumMaterializar := make(map[string]bool)
	faixasOk := 0
	for _, row := range faixas {
		if ctx.Err() != nil || esgotou(limite) {
			break
		}
		if !s.reconciliarECarimbar(ctx, row.ID, langCfg.Allowed, "faixa", s.scanner.ReconciliarFaixaDraft) {
			continue
		}
		faixasOk++
		// A capa mora no ÁLBUM (ADR-0027) -- materializa o pai da faixa
		// recém-ligada, uma vez por álbum nesta passada.
		for _, albumID := range s.albumsDaEntity(ctx, row.ID) {
			if albumMaterializar[albumID] {
				continue
			}
			albumMaterializar[albumID] = true
			if merr := s.MaterializarSeAutoDownloadLigado(ctx, albumID); merr != nil {
				log.Printf("pos-scan: materialização do álbum %q falhou (best-effort, ignorado): %v", albumID, merr)
			}
		}
	}

	series, err := s.leitura().ListarSeriesDraftPorSource(ctx, sqlcgen.ListarSeriesDraftPorSourceParams{
		SourceID:     sql.NullString{String: sourceID, Valid: true},
		HorasMinimas: scanner.PosScanHorasMinimas,
		Limite:       scanner.CapReconciliacaoSeries,
	})
	if err != nil {
		return err
	}
	seriesOk := 0
	for _, row := range series {
		if ctx.Err() != nil || esgotou(limite) {
			break
		}
		if s.reconciliarECarimbar(ctx, row.ID, langCfg.Allowed, "série", s.scanner.ReconciliarSerieDraft) {
			seriesOk++
		}
	}

	filmes, err := s.leitura().ListarFilmesPendentesPorSource(ctx, sqlcgen.ListarFilmesPendentesPorSourceParams{
		SourceID: sql.NullString{String: sourceID, Valid: true},
		Limite:   scanner.CapAutoMatchPendentes,
	})
	if err != nil {
		return err
	}
	filmesOk := 0
	for _, row := range filmes {
		if ctx.Err() != nil || esgotou(limite) {
			break
		}
		if s.autoMatchPendentePosScan(ctx, row.ID) {
			filmesOk++
		}
	}

	// Purga do estoque (briefing 0138): os rascunhos que perderam o arquivo para
	// a entity definitiva ficavam para sempre na fila de revisão como "sem
	// arquivo". Idempotente -- 0 quando não há nada a limpar.
	purgados := int64(0)
	if n, err := sqlcgen.New(s.db.Write).ApagarRascunhosVazios(ctx); err != nil {
		log.Printf("pos-scan: purga de rascunhos vazios falhou (best-effort, ignorado): %v", err)
	} else {
		purgados = n
	}

	if faixasOk+seriesOk+filmesOk > 0 || purgados > 0 {
		log.Printf("pos-scan: biblioteca %q -- %d faixa(s), %d série(s), %d filme(s) resolvidos depois do scan, %d rascunho(s) vazio(s) purgado(s)",
			sourceID, faixasOk, seriesOk, filmesOk, purgados)
	}
	return nil
}

// autoMatchPendentePosScan tenta casar+enriquecer UM filme pendente e, casando,
// materializa as artes quando o toggle está ligado -- mesmo par que o scan usa
// para item novo (dispararAutoMatchFilme + MaterializarSeAutoDownloadLigado),
// agora para o que ficou para trás.
func (s *Server) autoMatchPendentePosScan(ctx context.Context, entityID string) bool {
	matched, err := s.TentarAutoMatchFilme(ctx, entityID)
	if err != nil {
		log.Printf("pos-scan: auto-match do filme %q falhou (best-effort, ignorado): %v", entityID, err)
		return false
	}
	if !matched {
		return false
	}
	if merr := s.MaterializarSeAutoDownloadLigado(ctx, entityID); merr != nil {
		log.Printf("pos-scan: materialização do filme %q falhou (best-effort, ignorado): %v", entityID, merr)
	}
	return true
}

// albumsDaEntity devolve os pais (álbuns) de uma faixa -- leitura simples da
// árvore de relations; vazio quando não há vínculo ou o banco falha (a
// materialização é opcional, nunca vale abortar por isso).
func (s *Server) albumsDaEntity(ctx context.Context, entityID string) []string {
	rels, err := s.leitura().ListarRelacoesPais(ctx, entityID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	ids := make([]string, 0, len(rels))
	for _, rel := range rels {
		if rel.ParentID != "" {
			ids = append(ids, rel.ParentID)
		}
	}
	return ids
}
