package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Tests for POST /entities/{id}/download-missing-artwork:
// downloads the best artwork available for the `artwork:<kind>` kinds that show up
// in catalog_completeness.missing (recalculated live). No real network: the
// download points at an allowlisted httptest server, the same pattern as
// artwork_select_test.go.

// setupDownloadMissingServer starts a Server with a temporary assetsDir and the
// proxy allowlist restricted to the given host, returning the handler and the
// *db.DB so the tests can seed entity/config/inventory.
func setupDownloadMissingServer(t *testing.T, hostPermitido string) (http.Handler, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "download-missing.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database, WithAssetsDir(dir))
	s.hostsImagem = map[string]bool{hostPermitido: true}
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// configurarCompletudeArtwork PUTs a completeness config that only requires the
// given artwork kinds (movie), without the other fields -- to isolate the artwork
// download test from any other missing field.
func configurarCompletudeArtwork(t *testing.T, h http.Handler, kinds ...string) {
	t.Helper()
	corpo := catalogCompletenessSettings{"movie": {Artwork: kinds}}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT catalog-completeness status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// semearInventarioFanart writes the fanart.tv inventory envelope (same format as
// artwork_test.go) for the entity, with the given kinds/candidates.
func semearInventarioFanart(t *testing.T, d *db.DB, entityID string, inv map[string][]provider.ArteInventario) {
	t.Helper()
	envelope, err := json.Marshal(map[string]any{provider.ChaveEnvelopeInventarioArtes: inv})
	if err != nil {
		t.Fatalf("marshal inventario: %v", err)
	}
	semearMediaExternal(t, d, "me-fanart-"+entityID, entityID, "fanarttv", string(envelope))
}

// decodeDownloadMissingResult decodes the endpoint response.
func decodeDownloadMissingResult(t *testing.T, body []byte) downloadMissingArtworkResultDTO {
	t.Helper()
	var out downloadMissingArtworkResultDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar downloadMissingArtworkResultDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestDownloadMissingArtworkBaixaMultiplosKinds covers criterion §5: with a fanart.tv
// inventory available for BOTH missing kinds (poster, fanart), it downloads both and
// materializes two new origin=external assets.
func TestDownloadMissingArtworkBaixaMultiplosKinds(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster, kindFanart)
	semearEntity(t, d, "e1", "movie", "Duna")
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		kindPoster: {{URL: srv.URL + "/img", Likes: 5}},
		kindFanart: {{URL: srv.URL + "/img", Likes: 5}},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 2 || !contains(resultado.Downloaded, kindPoster) || !contains(resultado.Downloaded, kindFanart) {
		t.Errorf("downloaded = %v, quero [poster fanart] (em qualquer ordem)", resultado.Downloaded)
	}
	if len(resultado.Unavailable) != 0 {
		t.Errorf("unavailable = %v, quero vazio", resultado.Unavailable)
	}

	assets, err := escrita(t, d).ListarAssetsPorEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("assets criados = %d, quero 2 (poster + fanart materializados)", len(assets))
	}
	for _, a := range assets {
		if a.Origin != originExternal {
			t.Errorf("asset %s origin = %q, quero external", a.Kind, a.Origin)
		}
	}
}

// TestDownloadMissingArtworkKindSemOpcao covers criterion §5: a missing kind with NO
// option in the providers falls into `unavailable`, without an error; the other kind
// (with an option) still downloads normally.
func TestDownloadMissingArtworkKindSemOpcao(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster, kindClearart)
	semearEntity(t, d, "e1", "movie", "Filme Obscuro")
	// Only poster has a candidate in the inventory -- clearart does not show up
	// anywhere (neither inventory nor external layer).
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		kindPoster: {{URL: srv.URL + "/img", Likes: 1}},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 1 || resultado.Downloaded[0] != kindPoster {
		t.Errorf("downloaded = %v, quero [poster]", resultado.Downloaded)
	}
	if len(resultado.Unavailable) != 1 || resultado.Unavailable[0] != kindClearart {
		t.Errorf("unavailable = %v, quero [clearart]", resultado.Unavailable)
	}
}

// TestDownloadMissingArtworkItemCompletoNaoTocaNada covers criterion §5: an
// already complete item (all required kinds already have a local asset) returns both
// lists empty, without downloading anything.
func TestDownloadMissingArtworkItemCompletoNaoTocaNada(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster)
	semearEntity(t, d, "e1", "movie", "Ja Completo")
	semearAsset(t, d, "a1", "e1", kindPoster, "/x/poster.jpg")
	// The inventory exists (a candidate is available), but it should not be touched --
	// the item is already complete before reaching the download loop.
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		kindPoster: {{URL: srv.URL + "/img", Likes: 1}},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 0 || len(resultado.Unavailable) != 0 {
		t.Errorf("resultado = %+v, quero as duas listas vazias (item já completo)", resultado)
	}
	assets, err := escrita(t, d).ListarAssetsPorEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	if len(assets) != 1 {
		t.Errorf("assets = %d, quero 1 (o pré-existente, nada novo baixado)", len(assets))
	}
}

// TestDownloadMissingArtworkDraftNaoTocaNada covers criterion §5: a draft item
// (without an evaluated catalog_completeness) returns both lists empty, without
// touching anything.
func TestDownloadMissingArtworkDraftNaoTocaNada(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster)
	semearEntity(t, d, "e1", "movie", "Rascunho")
	definirStatus(t, d, "e1", "draft")
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		kindPoster: {{URL: srv.URL + "/img", Likes: 1}},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 0 || len(resultado.Unavailable) != 0 {
		t.Errorf("resultado = %+v, quero as duas listas vazias (draft, sem completude avaliada)", resultado)
	}
}

// TestDownloadMissingArtworkFalhaTentaProxima covers criterion §5: the candidate with
// the HIGHEST likes fails to download (500 from the httptest) -- the kind still
// downloads through the NEXT candidate, best-effort, before falling into unavailable.
func TestDownloadMissingArtworkFalhaTentaProxima(t *testing.T) {
	srv := servidorImagem(t, nil)
	h, d := setupDownloadMissingServer(t, hostDe(t, srv.URL))
	configurarCompletudeArtwork(t, h, kindPoster)
	semearEntity(t, d, "e1", "movie", "Com Candidata Ruim")
	semearInventarioFanart(t, d, "e1", map[string][]provider.ArteInventario{
		// The candidate with the HIGHER likes (10) fails (500); the one with fewer
		// likes (5) works -- it proves the order tries the higher-likes one first AND
		// that a failure does not give up on the kind.
		kindPoster: {
			{URL: srv.URL + "/erro", Likes: 10},
			{URL: srv.URL + "/img", Likes: 5},
		},
	})

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeDownloadMissingResult(t, rec.Body.Bytes())
	if len(resultado.Downloaded) != 1 || resultado.Downloaded[0] != kindPoster {
		t.Errorf("downloaded = %v, quero [poster] (a 2a candidata deveria ter salvado o kind)", resultado.Downloaded)
	}
	if len(resultado.Unavailable) != 0 {
		t.Errorf("unavailable = %v, quero vazio", resultado.Unavailable)
	}
	assets, err := escrita(t, d).ListarAssetsPorEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	if len(assets) != 1 {
		t.Errorf("assets criados = %d, quero exatamente 1 (só a candidata que funcionou)", len(assets))
	}
}

// TestDownloadMissingArtworkEntidadeInexistente: 404 for an unknown entity.
func TestDownloadMissingArtworkEntidadeInexistente(t *testing.T) {
	h, _ := setupDownloadMissingServer(t, "example.com")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/download-missing-artwork", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestDownloadMissingArtworkSemAssetsDir: 503 when the server has no assetsDir
// configured (the same guard as uploadAsset/selectArtwork).
func TestDownloadMissingArtworkSemAssetsDir(t *testing.T) {
	h, d := setupEntitiesServer(t) // without WithAssetsDir
	semearEntity(t, d, "e1", "movie", "Sem AssetsDir")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/download-missing-artwork", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// contains tells whether `slice` contains `alvo` (simple helper for order-free
// assertions on the downloaded/unavailable lists).
func contains(slice []string, alvo string) bool {
	for _, v := range slice {
		if v == alvo {
			return true
		}
	}
	return false
}
