package server

import (
	"context"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Este arquivo implementa o REPROCESSAMENTO DE ARTES FALTANTES (briefing
// 0118), complemento do enriquecimento retroativo (0056/0116/0117): uma
// entity que JÁ tem external_ids (identidade resolvida, direto no import ou
// via retroativo) pode mesmo assim nunca ter ganhado o inventário de arte de
// nenhum ArteProvider (fanart.tv etc, briefing 0048) -- o caso real que
// motivou este briefing: o provider foi configurado (chave de API cadastrada)
// DEPOIS que a maioria dos artistas/filmes/séries já tinha sido importada, e
// não existia nenhum mecanismo automático para voltar e tentar de novo (só o
// botão manual "Baixar Artes Faltantes" por item, briefing 0088). Este passo
// fecha esse gap em lote, com o MESMO throttle/dias do enriquecimento
// retroativo.

// limiteReprocessamentoArtesPorExecucao é o teto de itens verificados por
// execução do job -- mesmo raciocínio/valor de
// limiteEnriquecimentoRetroativoPorExecucao (retroativo.go): throttling de
// custo/rate-limit do(s) ArteProvider(s) numa biblioteca com backlog grande na
// primeira vez que este briefing roda.
const limiteReprocessamentoArtesPorExecucao = 15

// ReprocessarArtesFaltantes varre o lote de entities JÁ IDENTIFICADAS (tipos de
// topo, com posse, respeitando o throttle por item) cujo inventário de arte
// nunca foi verificado e tenta buscar de novo em todo ArteProvider registrado
// (AtualizarInventarioArtes -- best-effort, nunca falha por si só: sem
// ArteProvider, sem id utilizável ou sem match, degrada em silêncio). Satisfaz
// libraryhealth.Matcher (chamado pelo job saude_biblioteca, DEPOIS do
// enriquecimento retroativo). err é SÓ erro real de banco na LISTAGEM; falha
// num item individual é absorvida (best-effort, loga e segue).
func (s *Server) ReprocessarArtesFaltantes(ctx context.Context) (processados int, err error) {
	// Sem enrich configurado, AtualizarInventarioArtes nem existiria para
	// chamar -- mesma guarda de EnriquecerRetroativo, evita carimbar
	// last_external_match_attempt_at de itens nunca de fato verificados.
	if s.enrich == nil {
		return 0, nil
	}

	cfgRetry, err := s.obterRetroactiveEnrichmentSettings(ctx)
	if err != nil {
		return 0, err
	}
	langCfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := s.leitura().ListarElegiveisReprocessamentoArtes(ctx, sqlcgen.ListarElegiveisReprocessamentoArtesParams{
		DiasGlobal: int64(cfgRetry.RetryDays),
		Limite:     limiteReprocessamentoArtesPorExecucao,
	})
	if err != nil {
		return 0, err
	}

	for _, row := range rows {
		if err := s.enrich.AtualizarInventarioArtes(ctx, row.ID, row.Type, langCfg.Allowed); err != nil {
			log.Printf("saude_biblioteca: reprocessamento de artes falhou para %s (best-effort, ignorado): %v", row.ID, err)
		}
		// Carimba SEMPRE (mesmo padrão de EnriquecerRetroativo/
		// carimbarTentativaExterna): o item foi de fato verificado nesta
		// execução, mesmo sem nenhum ArteProvider ter achado nada -- senão o
		// próximo lote tentaria de novo no dia seguinte, sem respeitar o
		// throttle.
		if werr := s.carimbarTentativaExterna(ctx, row.ID); werr != nil {
			log.Printf("saude_biblioteca: gravar tentativa de reprocessamento de artes falhou para %s: %v", row.ID, werr)
		}
		processados++
	}
	return processados, nil
}
