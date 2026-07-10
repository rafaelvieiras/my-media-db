package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// Testes das 3 features do briefing 0027: (A) similares e busca externa interno-aware
// (internal_id), (B) edição de external_ids, (C) criação de item local manual.

// decodeResolvida decodifica uma resposta de entity resolvida (POST/PUT que devolvem
// o mesmo shape do detalhe).
func decodeResolvida(t *testing.T, body []byte) resolver.Entidade {
	t.Helper()
	var out resolver.Entidade
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar resolver.Entidade: %v (corpo=%s)", err, body)
	}
	return out
}

// ── Feature A: interno-first em similares e busca ────────────────────────────

// TestSearchExternoInternalId: um resultado externo JÁ importado (por id tmdb) ganha o
// internal_id apontando para a entity; um não importado fica com nil.
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

// TestExternalDetailSimilarInternalId: a banda de similares do detalhe externo (movie
// 603 tem o similar 604) anota o internal_id quando o similar já é entity; nil se não.
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

// TestExternalDetailSimilarSemImport: sem o similar importado, o internal_id sai nil.
func TestExternalDetailSimilarSemImport(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	dto := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")
	sim := umItem(t, "similar", dto.Similar)
	if sim.InternalID != nil {
		t.Errorf("similar.internal_id = %v, quero nil", sim.InternalID)
	}
}

// ── Feature B: editar external_ids ───────────────────────────────────────────

// TestUpdateExternalIdsGrava: PUT grava o mapa; a coluna gerada external_id_tmdb
// re-deriva (ObterEntityPorTmdb passa a achar a entity).
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

// TestUpdateExternalIdsRemovePorVazio: um valor vazio REMOVE aquela fonte (não grava id
// vazio); as demais permanecem.
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

// TestUpdateExternalIds404: entity inexistente.
func TestUpdateExternalIds404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/entities/nao-existe/external-ids",
		map[string]any{"external_ids": map[string]string{"tmdb": "1"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quero 404", rec.Code)
	}
}

// TestUpdateExternalIdsChaveVazia: source (chave) vazio → 422.
func TestUpdateExternalIdsChaveVazia(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Item Local")
	rec := do(t, h, http.MethodPut, "/api/v1/entities/"+id+"/external-ids",
		map[string]any{"external_ids": map[string]string{"": "x"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, quero 422 (chave vazia)", rec.Code)
	}
}

// TestUpdateExternalIdsCorpoInvalido: JSON malformado → 400.
func TestUpdateExternalIdsCorpoInvalido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Item Local")
	rec := do(t, h, http.MethodPut, "/api/v1/entities/"+id+"/external-ids", []byte("{invalido"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quero 400", rec.Code)
	}
}

// ── Feature C: criar item local manual ───────────────────────────────────────

// TestCreateItemCria: POST /entities cria a entity movie com os campos base e responde
// 201 com a entity resolvida (release_date persistido).
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

// TestCreateItemTipoInvalido: type fora de movie|tv → 422 (coleção usa /collections).
func TestCreateItemTipoInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	for _, tipo := range []string{"documentary", "collection", ""} {
		rec := do(t, h, http.MethodPost, "/api/v1/entities", map[string]any{"type": tipo, "title": "X"})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("type=%q: status = %d, quero 422", tipo, rec.Code)
		}
	}
}

// TestCreateItemTituloVazio: título vazio (ou só espaços) → 422.
func TestCreateItemTituloVazio(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities", map[string]any{"type": "tv", "title": "   "})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, quero 422 (titulo vazio)", rec.Code)
	}
}

// TestCreateItemCorpoInvalido: JSON malformado → 400.
func TestCreateItemCorpoInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities", []byte("{"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quero 400", rec.Code)
	}
}
