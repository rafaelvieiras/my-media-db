package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// This file implements the BATCH PATH of catalog completeness:
// RecalcularCompletudeCatalogacao is called by the `saude_biblioteca` job
// (internal/job/libraryhealth) at the end of each run, and recalculates the persisted
// CACHE (entity_catalog_completeness) that the listing/`/members` read — see the long
// note in completeness.go on why the listing does NOT compute live per item (the
// detail page computes, this file does not).

// paginaRecalculoCompletude is the page size used when sweeping ALL the entities
// (ListarEntities/LIMIT+OFFSET): large enough for few round trips to the database,
// small enough not to hold a giant page in memory at once. It is an INTERNAL pagination
// step of the job, which is why it does not reuse limitMaximo (entities.go) — that one
// is an HTTP response ceiling, this one is just the sweeper step.
const paginaRecalculoCompletude = 200

// RecalcularCompletudeCatalogacao sweeps ALL NON-DRAFT entities in pages (same internal
// pagination pattern as the package) and writes the outcome of the catalog completeness
// evaluation into entity_catalog_completeness. It satisfies
// libraryhealth.CompletenessRecalculator (injected in server.WithLibraryHealthJob) so
// the `saude_biblioteca` job can call this step at the end of each run.
//
// An error on ONE item (e.g. sql.ErrNoRows on an entity purged between the listing and
// the processing) does NOT abort the others — best-effort, the same spirit as
// escanearTodas/autoMatchLote: it only logs and moves on to the next one. Only a
// LISTING error (the page itself failing) interrupts the sweep — at that point there is
// no way to know what else exists to evaluate.
func (s *Server) RecalcularCompletudeCatalogacao(ctx context.Context, registro execqueue.Registro) (avaliados, incompletos int, err error) {
	registro = orNullRegistry(registro)
	cfg, err := s.obterCatalogCompletenessSettings(ctx)
	if err != nil {
		return 0, 0, err
	}
	w := sqlcgen.New(s.db.Write)
	// The denominator of the bar: the sweep walks ListarEntities, i.e. the whole table
	// (drafts included, skipped after being counted). Measured once, before the first
	// page -- 34 thousand rows of a read-only COUNT are cheaper than a bar that lies.
	total, err := s.leitura().ContarEntities(ctx)
	if err != nil {
		return 0, 0, err
	}

	var offset, posicao int64
	for {
		ents, err := s.leitura().ListarEntities(ctx, sqlcgen.ListarEntitiesParams{
			Limit: paginaRecalculoCompletude, Offset: offset,
		})
		if err != nil {
			return avaliados, incompletos, err
		}
		if len(ents) == 0 {
			break
		}
		for _, ent := range ents {
			// 34.153 items in production: the counter is the only narration that fits
			// (the Progresso throttle turns this into one write every 500 ms).
			// Atual is the POSITION in the sweep, not the number of items evaluated:
			// a draft is walked and skipped, and the bar must still move past it.
			posicao++
			registro.Progresso(execqueue.Progresso{Item: ent.Title, Atual: int(posicao), Total: int(total)})
			if ehDraft(ent.Status) {
				continue
			}
			resultado, itemErr := s.avaliarERecalcularUmItem(ctx, w, cfg, ent)
			if itemErr != nil {
				log.Printf("saude_biblioteca: recalculo de completude de catalogacao falhou para %s (best-effort, ignorado): %v", ent.ID, itemErr)
				continue
			}
			avaliados++
			if !resultado.Complete {
				incompletos++
			}
		}
		if len(ents) < paginaRecalculoCompletude {
			break
		}
		offset += paginaRecalculoCompletude
	}
	return avaliados, incompletos, nil
}

// avaliarERecalcularUmItem evaluates ONE non-draft entity against the `cfg` ruler and
// writes the result (UPSERT) into entity_catalog_completeness.
//
// When the type of `ent` is not in `cfg`, it writes {true,[]} STRAIGHT AWAY, without
// calling the resolver — it avoids the resolution cost (files/assets/override/
// external/ancestors/ownership) for types with no ruler at all. Otherwise it calls
// s.resolverEntity with an empty language (a neutral base — completeness depends on the
// PRESENCE of the field, not on which language filled it) and reuses
// resolvida.CatalogCompleteness, which resolverEntity ALREADY computes internally
// (anotarCompletudeCatalogacao, entities.go, the same path as the detail page) — it does
// not duplicate the rich-field extraction nor the assets read here.
func (s *Server) avaliarERecalcularUmItem(ctx context.Context, w *sqlcgen.Queries, cfg catalogCompletenessSettings, ent sqlcgen.Entity) (resultadoCompletude, error) {
	var resultado resultadoCompletude
	if _, achou := cfg[ent.Type]; !achou {
		resultado = resultadoCompletude{Complete: true, Missing: []string{}}
	} else {
		resolvida, ok, err := s.resolverEntity(ctx, ent.ID, "", true)
		if err != nil {
			return resultadoCompletude{}, err
		}
		if !ok {
			return resultadoCompletude{}, sql.ErrNoRows
		}
		if resolvida.CatalogCompleteness == nil {
			// It should not happen for an item already filtered as non-draft
			// (anotarCompletudeCatalogacao only leaves nil for a draft) — degrade
			// with an explicit error instead of writing a misleading zero-value.
			return resultadoCompletude{}, errors.New("resolverEntity não calculou catalog_completeness para item não-draft")
		}
		resultado = *resolvida.CatalogCompleteness
	}

	missingJSON, err := json.Marshal(resultado.Missing)
	if err != nil {
		return resultadoCompletude{}, err
	}
	var complete int64
	if resultado.Complete {
		complete = 1
	}
	if err := w.UpsertEntityCatalogCompleteness(ctx, sqlcgen.UpsertEntityCatalogCompletenessParams{
		EntityID:    ent.ID,
		Complete:    complete,
		MissingJson: string(missingJSON),
	}); err != nil {
		return resultadoCompletude{}, err
	}
	return resultado, nil
}
