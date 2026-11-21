package server

import (
	"context"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// This file implements the REPROCESSING OF MISSING ARTWORK, a complement to the
// retroactive enrichment: an entity that ALREADY has external_ids (resolved identity,
// straight at import or via retroactive) may still never have gained the artwork
// inventory of any ArteProvider (fanart.tv etc.) -- the real case that motivated this:
// the provider was configured (API key registered) AFTER most artists/movies/series had
// already been imported, and there was no automatic mechanism to go back and try again
// (only the manual "Download Missing Artwork" button per item). This step closes that
// gap in batch, with the SAME throttle/days of the retroactive enrichment.

// limiteReprocessamentoArtesPorExecucao is the ceiling of items checked per
// job run -- same reasoning/value as
// limiteEnriquecimentoRetroativoPorExecucao (retroativo.go): throttling of
// cost/rate-limit of the ArteProvider(s) in a library with a large backlog on
// the first time this runs.
const limiteReprocessamentoArtesPorExecucao = 15

// ReprocessarArtesFaltantes sweeps the batch of ALREADY IDENTIFIED entities (top
// types, with ownership, respecting the per-item throttle) whose artwork inventory
// has never been checked and tries to fetch it again across every registered ArteProvider
// (AtualizarInventarioArtes -- best-effort, it never fails by itself: without an
// ArteProvider, without a usable id or without a match, it degrades silently). It satisfies
// libraryhealth.Matcher (called by the saude_biblioteca job, AFTER the
// retroactive enrichment). err is ONLY a real database error in the LISTING; a failure
// in an individual item is absorbed (best-effort, it logs and moves on).
func (s *Server) ReprocessarArtesFaltantes(ctx context.Context, registro execqueue.Registro) (processados int, err error) {
	registro = orNullRegistry(registro)
	// Without enrich configured, AtualizarInventarioArtes would not even exist to
	// call -- same guard as EnriquecerRetroativo, it avoids stamping
	// last_external_match_attempt_at on items never actually checked.
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

	for i, row := range rows {
		// The owner watches ONE bar: "which item of this step" (ADR-0040). Without these
		// two lines the step had no counters at all, so the queue screen showed no bar and
		// kept the item of the previous step.
		registro.Progresso(execqueue.Progresso{Item: row.ID, Atual: i, Total: len(rows)})
		if err := s.enrich.AtualizarInventarioArtes(ctx, row.ID, row.Type, langCfg.Allowed); err != nil {
			log.Printf("saude_biblioteca: reprocessamento de artes falhou para %s (best-effort, ignorado): %v", row.ID, err)
		}
		// Always STAMPED (same pattern as EnriquecerRetroativo/
		// carimbarTentativaExterna): the item was actually checked in this
		// run, even if no ArteProvider found anything -- otherwise the
		// next batch would try again the next day, without respecting the
		// throttle.
		if werr := s.carimbarTentativaExterna(ctx, row.ID); werr != nil {
			log.Printf("saude_biblioteca: gravar tentativa de reprocessamento de artes falhou para %s: %v", row.ID, werr)
		}
		processados++
	}
	registro.Progresso(execqueue.Progresso{Atual: len(rows), Total: len(rows)})
	return processados, nil
}
