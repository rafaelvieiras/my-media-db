package server

// materialize_missing_media.go implements POST
// /api/v1/entities/{id}/materialize-missing-media (Manage menu > "Baixar Artes Faltantes"):
// an action ALWAYS visible (it does not depend on catalog_completeness) that first forces a
// NEW query to the registered ArteProvider(s) (today: fanart.tv) to refresh the entity's
// artwork inventory (media_external(source='fanarttv')) and then reuses WITHOUT CHANGING the
// same materialization of the periodic midias_faltantes job
// (MaterializarMidiasFaltantes, midias_faltantes.go) to write into the library's real folder
// the artwork currently WINNING for each kind still without a local_path, and the .nfo when
// it is missing.
//
// The inventory refresh is BEST-EFFORT (silent degradation, AGENTS.md §6): without s.enrich
// configured (no external source with a credential) or without a match in the artwork
// provider(s), the step is skipped/logged and the materialization runs the same way over
// whatever inventory already exists — it never turns the whole request into an error.

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// materializeMissingMediaEntity handles POST
// /api/v1/entities/{id}/materialize-missing-media. 404 if the entity does not exist; 200
// always after that (MaterializeMissingMediaResult, the same shape
// MaterializarMidiasFaltantes already returns) — each sub-step is best-effort, never a 5xx
// just because "the artwork provider found nothing new".
func (s *Server) materializeMissingMediaEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	// (1) New query to the registered ArteProvider(s) to refresh the entity's artwork
	// inventory — best-effort: without s.enrich (no external source configured) or without
	// capability/match in the provider, it just goes on to materialize over whatever inventory
	// already exists (silent degradation, AGENTS.md §6).
	if s.enrich != nil {
		langs, err := s.idiomasPermitidos(ctx, w)
		if err != nil {
			return
		}
		if err := s.enrich.AtualizarInventarioArtes(ctx, id, ent.Type, langs); err != nil {
			log.Printf("materialize-missing-media: atualizar inventario de artes da entity %q falhou (segue com o inventario atual): %v", id, err)
		}
	}

	// (2) The SAME materialization of the midias_faltantes job, without changing its logic: it
	// writes into the real folder the artwork currently winning for each kind still without a
	// local_path, and the .nfo when it is missing.
	artesBaixadas, nfoGravado, err := s.MaterializarMidiasFaltantes(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	writeJSON(w, http.StatusOK, materializeMissingMediaResultDTO{
		ArtesBaixadas: artesBaixadasOuVazio(artesBaixadas),
		NfoGravado:    nfoGravado,
	})
}

// materializeMissingMediaResultDTO is the response of POST
// /entities/{id}/materialize-missing-media (MaterializeMissingMediaResult in the contract,
// openapi.yaml) — it mirrors the return of MaterializarMidiasFaltantes.
type materializeMissingMediaResultDTO struct {
	ArtesBaixadas []string `json:"artes_baixadas"`
	NfoGravado    bool     `json:"nfo_gravado"`
}

// artesBaixadasOuVazio returns a slice that is NEVER nil (it serializes `[]`, not `null`),
// the same pattern as downloadMissingArtworkResultDTO.
func artesBaixadasOuVazio(artes []string) []string {
	if artes == nil {
		return []string{}
	}
	return artes
}
