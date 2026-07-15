package server

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo implementa o ENRIQUECIMENTO EXTERNO RETROATIVO (briefing 0056),
// extensão do job saude_biblioteca (0053/0054): varre entities `cataloged`
// (ou wanted/unreleased) com confiança 70 do scanner (título+ano parseados do
// arquivo, SEM id externo embutido) -- o complemento EXATO de ListarRevisao
// restrito a itens locais puros com arquivo no disco. Esses itens NUNCA
// passam por nenhum provider externo (não caem na fila de Revisão, então o
// auto-match do 0050/0054 nunca os alcança) -- ficam "invisíveis" para
// sempre, com título/ano bonitinhos mas sem pôster/sinopse/elenco. Este
// arquivo fecha esse gap: busca local-first e depois externo por lote
// pequeno e throttled; aplica sozinho com confiança alta (mesmo limiar do
// 0050), sinaliza para a fila de Revisão quando achou algo plausível mas não
// confiável o bastante, ou só registra a tentativa (throttle) quando não
// achou nada -- reaproveitando INTEGRALMENTE o motor de busca/pontuação e a
// sequência de aplicação externa (aplicarCandidatoExterno, extraído de
// tentarAutoMatch em automatch.go) já existentes.

const (
	// limiteEnriquecimentoRetroativoPorExecucao é o teto de itens VERIFICADOS
	// (não só listados) por execução do job -- throttling de custo/rate-limit
	// do provider externo numa biblioteca que pode ter um backlog grande na
	// primeira vez que este briefing roda.
	limiteEnriquecimentoRetroativoPorExecucao = 15

	// intervaloReTentativaRetroativa é o intervalo mínimo entre duas
	// tentativas do MESMO item -- não re-bate no provider por um item que já
	// foi verificado recentemente e não achou nada (ou não bateu o limiar).
	intervaloReTentativaRetroativa = 30 * 24 * time.Hour

	// limiarSinalizacaoRetroativa é o piso de plausibilidade para gravar o
	// item na fila de Revisão (via AtualizarConfidencePorEntity): abaixo
	// disso, o candidato é ruído (títulos parecidos por coincidência) e não
	// vale interromper o dono -- só a tentativa é registrada (throttle).
	// Decisão NOVA deste briefing (ver §6 do briefing 0056): o 0050 só
	// precisava de UM corte (aplicar ou não, o item já estava visível de
	// qualquer jeito); aqui "não aplicar" sozinho deixaria o item invisível
	// para sempre, daí o piso adicional só para a decisão de "vale sinalizar".
	limiarSinalizacaoRetroativa = 50.0
)

// carimboRetroativoFormato é o layout Go do timestamp gravado por
// AtualizarTentativaMatchExterno (strftime('%Y-%m-%dT%H:%M:%fZ', 'now') no
// SQL -- 3 dígitos de milissegundo), usado para calcular o corte `desde` do
// throttle em Go. Mesmo layout usado por scanner.carimboFormato/
// health_system.go (ComputedAt), sem precisão de nanossegundos (%f do
// SQLite trunca em milissegundos).
const carimboRetroativoFormato = "2006-01-02T15:04:05.000Z"

// EnriquecerRetroativo varre o lote de itens elegíveis (locais puros, nunca
// verificados contra provider nenhum, respeitando o throttle por item) e
// tenta enriquecer cada um -- aplicando sozinho, sinalizando para Revisão ou
// só registrando a tentativa. Satisfaz libraryhealth.Matcher (chamado pelo
// job saude_biblioteca, briefing 0056, DEPOIS do auto-match em lote
// existente 0054). err é SÓ erro real de banco na LISTAGEM; falha num item
// individual é absorvida (best-effort, loga e segue).
func (s *Server) EnriquecerRetroativo(ctx context.Context) (aplicados, sinalizados int, err error) {
	// Sem provider configurado (ou sem assetsDir -- mesma guarda de
	// tentarAutoMatch), NENHUMA tentativa real pode acontecer: devolve sem
	// tocar o banco, para não carimbar last_external_match_attempt_at de
	// itens que não foram de fato verificados -- eles mantêm prioridade
	// MÁXIMA (nunca tentado) assim que um provider voltar a existir, em vez
	// de esperar 30 dias de throttle por um período em que não havia
	// provider nenhum (ver §6 do briefing 0056).
	if s.enrich == nil {
		return 0, 0, nil
	}

	desde := time.Now().Add(-intervaloReTentativaRetroativa).UTC().Format(carimboRetroativoFormato)
	rows, err := s.leitura().ListarElegiveisEnriquecimentoRetroativo(ctx, sqlcgen.ListarElegiveisEnriquecimentoRetroativoParams{
		Limiar: sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true},
		Desde:  sql.NullString{String: desde, Valid: true},
		Limite: limiteEnriquecimentoRetroativoPorExecucao,
	})
	if err != nil {
		return 0, 0, err
	}

	w := sqlcgen.New(s.db.Write)
	for _, row := range rows {
		ent, err := s.leitura().ObterEntity(ctx, row.ID)
		if err != nil {
			// Sumiu entre a listagem e o processamento (purga concorrente) --
			// pula, não derruba o lote (mesmo padrão de autoMatchLote).
			continue
		}

		resultado, tentarErr := s.tentarEnriquecerOuSinalizar(ctx, ent)
		if tentarErr != nil {
			log.Printf("saude_biblioteca: enriquecimento retroativo falhou para %s (best-effort, ignorado): %v", ent.ID, tentarErr)
		}

		// Carimba a tentativa SEMPRE ao final (mesmo se tentarErr != nil): o
		// item foi de fato verificado nesta execução -- best-effort, erro
		// aqui só loga, não aborta o item nem o lote (briefing 0056 §4).
		if werr := w.AtualizarTentativaMatchExterno(ctx, ent.ID); werr != nil {
			log.Printf("saude_biblioteca: gravar tentativa de enriquecimento retroativo falhou para %s: %v", ent.ID, werr)
		}

		switch resultado {
		case "applied":
			aplicados++
		case "flagged":
			sinalizados++
		}
	}
	return aplicados, sinalizados, nil
}

// tentarEnriquecerOuSinalizar tenta enriquecer UMA entity elegível: deriva
// título/ano do arquivo, busca local-first e depois externo, SEM o corte de
// LimiarAutoMatch embutido nas variantes de busca (precisa do MELHOR score
// mesmo abaixo do limiar, para decidir "sinalizar" -- por isso NÃO reusa
// melhorCandidatoLocal/melhorCandidatoExterno de automatch.go, que já
// descartam candidato fraco antes de devolver). resultado é
// "applied"/"flagged"/"none" (só para log/teste, nunca vira DTO de API). err
// é SÓ erro real de banco/infra.
func (s *Server) tentarEnriquecerOuSinalizar(ctx context.Context, ent sqlcgen.Entity) (resultado string, err error) {
	titulo, ano, ok, err := s.tituloAnoDoArquivo(ctx, ent.ID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "none", nil
	}

	// 1) Local-first (mesma ordem de tentarAutoMatch): nunca duplica entity
	// já catalogada.
	candidatosLocais, err := s.buscarLocal(ctx, titulo, ent.Type, "", "")
	if err != nil {
		return "", err
	}
	melhorLocal, achouLocal := s.melhorCandidatoLocalSemCorte(ctx, candidatosLocais, ent.ID, titulo, ano)
	if achouLocal && melhorLocal.Score >= LimiarAutoMatch {
		if err := s.fundirEntities(ctx, melhorLocal.ID, ent.ID); err != nil {
			return "", err
		}
		return "applied", nil
	}

	// 2) Externo: só quando local não bateu o limiar de aplicação.
	melhorExternoScore := -1.0
	achouExterno := false
	if s.enrich != nil {
		externos, disponivel := s.buscarExterno(ctx, titulo, ent.Type, "")
		if disponivel {
			cand, score, achou := melhorCandidatoExternoSemCorte(externos, titulo, ano)
			if achou {
				melhorExternoScore, achouExterno = score, true
				if score >= LimiarAutoMatch {
					aplicado, err := s.aplicarCandidatoExterno(ctx, ent, cand, score)
					if err != nil {
						return "", err
					}
					if aplicado.Resolved {
						return "applied", nil
					}
					// aplicarCandidatoExterno degradou (colisão com
					// investimento do dono na outra entity, ou a fonte
					// falhou ao entregar o detalhe): não há como aplicar às
					// cegas -- cai para a decisão de sinalizar abaixo, com o
					// mesmo score já calculado (o candidato continua
					// plausível, só não pôde ser aplicado sozinho).
				}
			}
		}
	}

	// 3) Sem candidato forte em nenhuma fonte (ou aplicação externa
	// degradada acima): decide sinalizar pelo MAIOR score entre local e
	// externo, já calculados.
	melhorGeral := -1.0
	if achouLocal {
		melhorGeral = melhorLocal.Score
	}
	if achouExterno && melhorExternoScore > melhorGeral {
		melhorGeral = melhorExternoScore
	}
	if melhorGeral < limiarSinalizacaoRetroativa {
		return "none", nil
	}

	// Teto do valor gravado: sem ele, um candidato pontuado entre
	// limiarBaixaConfianca e LimiarAutoMatch (achou algo bom, mas não bom o
	// bastante para os 92 do auto-match) seria gravado com um
	// match_confidence que NÃO bate `< limiar` -- o item continuaria
	// invisível, o oposto do pretendido (ver §6 do briefing 0056).
	valorGravado := melhorGeral
	if valorGravado > limiarBaixaConfianca-1 {
		valorGravado = limiarBaixaConfianca - 1
	}
	if err := sqlcgen.New(s.db.Write).AtualizarConfidencePorEntity(ctx, sqlcgen.AtualizarConfidencePorEntityParams{
		MatchConfidence: sql.NullFloat64{Float64: valorGravado, Valid: true},
		EntityID:        ent.ID,
	}); err != nil {
		return "", err
	}
	return "flagged", nil
}

// melhorCandidatoLocalSemCorte espelha melhorCandidatoLocal (automatch.go),
// mas SEM o corte por LimiarAutoMatch -- devolve o candidato de MAIOR score
// mesmo abaixo do limiar (o enriquecimento retroativo precisa do valor para
// decidir "sinalizar"). Candidato com ano divergente em mais de 1 ano
// (matching.Pontuar descartado=true) nunca entra no ranking, igual ao
// caminho com corte.
func (s *Server) melhorCandidatoLocalSemCorte(ctx context.Context, candidatos []entityCardDTO, selfID, titulo string, ano int) (candidatoPontuado, bool) {
	var melhor candidatoPontuado
	achou := false
	for _, c := range candidatos {
		if c.ID == selfID {
			continue
		}
		anoCand := s.anoDaEntity(ctx, c.ID)
		pontos, descartado := matching.Pontuar(titulo, ano, c.Title, anoCand)
		if descartado {
			continue
		}
		if !achou || pontos > melhor.Score {
			melhor = candidatoPontuado{ID: c.ID, Title: c.Title, Score: pontos}
			if anoCand > 0 {
				a := anoCand
				melhor.Year = &a
			}
			achou = true
		}
	}
	return melhor, achou
}

// melhorCandidatoExternoSemCorte espelha melhorCandidatoExterno
// (automatch.go), mas SEM o corte por LimiarAutoMatch -- mesmo racional de
// melhorCandidatoLocalSemCorte, para o ramo externo.
func melhorCandidatoExternoSemCorte(candidatos []provider.ResultadoExterno, titulo string, ano int) (provider.ResultadoExterno, float64, bool) {
	var melhor provider.ResultadoExterno
	melhorScore := -1.0
	achou := false
	for _, c := range candidatos {
		anoCand := anoDeStringPtr(c.Year)
		pontos, descartado := matching.Pontuar(titulo, ano, c.Title, anoCand)
		if descartado {
			continue
		}
		if pontos > melhorScore {
			melhor, melhorScore, achou = c, pontos, true
		}
	}
	return melhor, melhorScore, achou
}
