package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// Tests of the 3 features of manual items and external ids: (A) internal-aware similars
// and external search (internal_id), (B) editing external_ids, (C) creating a manual
// local item.

// decodeResolvida decodes a resolved entity response (POST/PUT that return the same
// shape as the detail).
func decodeResolvida(t *testing.T, body []byte) resolver.Entidade {
	t.Helper()
	var out resolver.Entidade
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar resolver.Entidade: %v (corpo=%s)", err, body)
	}
	return out
}

// ── Feature A: internal-first in similars and search ─────────────────────────

// TestSearchExternoInternalId: an external result ALREADY imported (by tmdb id) gets the
// internal_id pointing at the entity; one that is not imported stays nil.
func TestSearchExternoInternalId(t *testing.T) {
	fake := &fakeProvider{resultados: []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "603", Type: "movie", Title: "The Matrix"},
		{Source: "tmdb", SourceID: "999", Type: "movie", Title: "Nao Importado"},
	}}
	h, d := setupSearchServer(t, WithSearchProvider(fake))
	semearFilmeTmdb(t, d, "ent-603", "603", "Matrix")

	rec := do(t, h, http.MethodGet, "/api/v1/search?q=matrix", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	out := decodeSearch(t, rec.Body.Bytes())
	byID := map[string]externalResultDTO{}
	for _, e := range out.External {
		byID[e.SourceID] = e
	}
	if e := byID["603"]; e.InternalID == nil || *e.InternalID != "ent-603" {
		t.Errorf("external 603 internal_id = %v, quero ent-603", e.InternalID)
	}
	if e := byID["999"]; e.InternalID != nil {
		t.Errorf("external 999 internal_id = %v, quero nil (nao importado)", e.InternalID)
	}
}

// TestExternalDetailSimilarInternalId: the similars band of the external detail (movie
// 603 has the similar 604) annotates the internal_id when the similar is already an
// entity; nil otherwise.
func TestExternalDetailSimilarInternalId(t *testing.T) {
	h, d := setupExternalServer(t, nil)
	semearFilmeTmdb(t, d, "ent-604", "604", "Reloaded")
	dto := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")
	sim := umItem(t, "similar", dto.Similar)
	if sim.SourceID != "604" {
		t.Fatalf("similar.source_id = %q, quero 604", sim.SourceID)
	}
	if sim.InternalID == nil || *sim.InternalID != "ent-604" {
		t.Errorf("similar.internal_id = %v, quero ent-604", sim.InternalID)
	}
}

// TestExternalDetailSimilarSemImport: without the imported similar, the internal_id comes out
// nil.
func TestExternalDetailSimilarSemImport(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	dto := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")
	sim := umItem(t, "similar", dto.Similar)
	if sim.InternalID != nil {
		t.Errorf("similar.internal_id = %v, quero nil", sim.InternalID)
	}
}

// ── Feature B: editing external_ids ──────────────────────────────────────────

// TestUpdateExternalIdsGrava: the PUT writes the map; the generated column
// external_id_tmdb is re-derived (ObterEntityPorTmdb starts finding the entity).
func TestUpdateExternalIdsGrava(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Item Local")

	rec := do(t, h, http.MethodPut, "/api/v1/entities/"+id+"/external-ids",
		map[string]any{"external_ids": map[string]string{"tmdb": "603", "imdb": "tt0133093"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeResolvida(t, rec.Body.Bytes())
	if got.ExternalIDs["tmdb"] != "603" || got.ExternalIDs["imdb"] != "tt0133093" {
		t.Errorf("external_ids resolvidos = %v, quero tmdb=603 imdb=tt0133093", got.ExternalIDs)
	}
	ent, err := escrita(t, d).ObterEntityPorTmdb(context.Background(), nsOf("603"))
	if err != nil || ent.ID != id {
		t.Errorf("coluna gerada nao re-derivou: ObterEntityPorTmdb(603) = (%v, %v), quero %s", ent.ID, err, id)
	}
}

// TestUpdateExternalIdsRemovePorVazio: an empty value REMOVES that source (it does not
// write an empty id); the others remain.
func TestUpdateExternalIdsRemovePorVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Item Local")
	do(t, h, http.MethodPut, "/api/v1/entities/"+id+"/external-ids",
		map[string]any{"external_ids": map[string]string{"tmdb": "603", "imdb": "tt1"}})

	rec := do(t, h, http.MethodPut, "/api/v1/entities/"+id+"/external-ids",
		map[string]any{"external_ids": map[string]string{"tmdb": "603", "imdb": ""}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeResolvida(t, rec.Body.Bytes())
	if _, existe := got.ExternalIDs["imdb"]; existe {
		t.Errorf("imdb ainda presente (%v), quero removido pelo valor vazio", got.ExternalIDs)
	}
	if got.ExternalIDs["tmdb"] != "603" {
		t.Errorf("tmdb = %q, quero 603 (preservado)", got.ExternalIDs["tmdb"])
	}
}

// TestUpdateExternalIds404: entity that does not exist.
func TestUpdateExternalIds404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/entities/nao-existe/external-ids",
		map[string]any{"external_ids": map[string]string{"tmdb": "1"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quero 404", rec.Code)
	}
}

// TestUpdateExternalIdsChaveVazia: empty source (key) → 422.
func TestUpdateExternalIdsChaveVazia(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Item Local")
	rec := do(t, h, http.MethodPut, "/api/v1/entities/"+id+"/external-ids",
		map[string]any{"external_ids": map[string]string{"": "x"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, quero 422 (chave vazia)", rec.Code)
	}
}

// TestUpdateExternalIdsCorpoInvalido: malformed JSON → 400.
func TestUpdateExternalIdsCorpoInvalido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Item Local")
	rec := do(t, h, http.MethodPut, "/api/v1/entities/"+id+"/external-ids", []byte("{invalido"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quero 400", rec.Code)
	}
}

// ── Feature C: creating a manual local item ──────────────────────────────────

// TestCreateItemCria: POST /entities creates the movie entity with the base fields and
// answers 201 with the resolved entity (release_date persisted).
func TestCreateItemCria(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities",
		map[string]any{"type": "movie", "title": "Meu Filme", "release_date": "1999-03-31"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeResolvida(t, rec.Body.Bytes())
	if got.Type != "movie" || got.Title != "Meu Filme" {
		t.Errorf("item = {type:%q title:%q}, quero {movie, Meu Filme}", got.Type, got.Title)
	}
	if got.ReleaseDate == nil || *got.ReleaseDate != "1999-03-31" {
		t.Errorf("release_date = %v, quero 1999-03-31", got.ReleaseDate)
	}
	if len(got.ExternalIDs) != 0 {
		t.Errorf("external_ids = %v, quero vazio (item local sem fonte)", got.ExternalIDs)
	}
}

// TestCreateItemTipoInvalido: type outside movie|tv → 422 (a collection uses
// /collections).
func TestCreateItemTipoInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	for _, tipo := range []string{"documentary", "collection", ""} {
		rec := do(t, h, http.MethodPost, "/api/v1/entities", map[string]any{"type": tipo, "title": "X"})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("type=%q: status = %d, quero 422", tipo, rec.Code)
		}
	}
}

// TestCreateItemTituloVazio: empty title (or whitespace only) → 422.
func TestCreateItemTituloVazio(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities", map[string]any{"type": "tv", "title": "   "})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, quero 422 (titulo vazio)", rec.Code)
	}
}

// TestCreateItemCorpoInvalido: malformed JSON → 400.
func TestCreateItemCorpoInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities", []byte("{"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quero 400", rec.Code)
	}
}
