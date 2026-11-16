package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
)

// Tests for the ACTION of selecting/downloading one inventory artwork:
// POST /entities/{id}/artwork/select. They cover the anti-SSRF guard (host outside the
// allowlist), the invalid kind and the happy path (downloads, creates an
// origin='external' asset with the url in the meta, marks it preferred and is
// idempotent). No real network: the download points at an allowlisted httptest server.

// setupSelectServer starts a Server with a migrated database, a temporary assetsDir and
// the proxy allowlist RESTRICTED to the given host (plus the default). Returns the
// handler, the *db.DB and the assets dir. The default httpImagem (short timeout)
// downloads from the httptest server without external network.
func setupSelectServer(t *testing.T, hostPermitido string) (http.Handler, *db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "select.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database, WithAssetsDir(dir))
	// Replaces the allowlist with one restricted to the httptest server (ASSIGN, not a
	// mutation of the global hostsImagemPadrao map).
	s.hostsImagem = map[string]bool{hostPermitido: true}
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database, dir
}

// decodeAssetDTO decodes the select response (assetDTO).
func decodeAssetDTO(t *testing.T, body []byte) assetDTO {
	t.Helper()
	var a assetDTO
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("decodificar assetDTO: %v (corpo=%s)", err, body)
	}
	return a
}

// TestSelectArtworkHostForaDaAllowlist: the url host comes from the user; a host outside
// the proxy allowlist is rejected with 422 (anti-SSRF), without touching the network.
func TestSelectArtworkHostForaDaAllowlist(t *testing.T) {
	h, d, _ := setupSelectServer(t, "image.tmdb.org") // the allowlist does NOT include the host below
	semearEntity(t, d, "e1", "movie", "Matrix")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/artwork/select", map[string]any{
		"kind": "poster", "url": "https://evil.example.com/x.png", "prefer": true,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestSelectArtworkKindInvalido: kind outside the set (kindsValidos) → 422.
func TestSelectArtworkKindInvalido(t *testing.T) {
	h, d, _ := setupSelectServer(t, "image.tmdb.org")
	semearEntity(t, d, "e1", "movie", "Matrix")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/artwork/select", map[string]any{
		"kind": "bogus", "url": "https://image.tmdb.org/t/p/w342/p.jpg",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestSelectArtworkEntidadeInexistente: 404 for an entity that does not exist.
func TestSelectArtworkEntidadeInexistente(t *testing.T) {
	h, _, _ := setupSelectServer(t, "image.tmdb.org")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/artwork/select", map[string]any{
		"kind": "poster", "url": "https://image.tmdb.org/t/p/w342/p.jpg",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestSelectArtworkBaixaCriaExternalEPrefere: an allowlisted url downloads the binary,
// creates an origin='external' asset with the url in the meta and (prefer=true) marks it
// preferred; a 2nd select of the SAME url is idempotent (does not duplicate, reuses the
// asset).
func TestSelectArtworkBaixaCriaExternalEPrefere(t *testing.T) {
	srv := servidorImagem(t, nil) // /img → 200 image/png
	h, d, dir := setupSelectServer(t, hostDe(t, srv.URL))
	semearEntity(t, d, "e1", "movie", "Matrix")

	urlArte := srv.URL + "/img"
	a := selecionarArte(t, h, urlArte, true)
	if a.Origin != originExternal || a.Kind != "poster" || !a.Preferred {
		t.Fatalf("asset = %+v, quero origin=external kind=poster preferred=true", a)
	}

	assertBinarioNoDisco(t, dir)
	assertMetaSourceURL(t, d, a.ID, urlArte)
	assertSelectIdempotente(t, h, d, urlArte, a.ID)
}

// selecionarArte POSTs /artwork/select for the given url, requires 200 and returns the
// assetDTO.
func selecionarArte(t *testing.T, h http.Handler, urlArte string, prefer bool) assetDTO {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/artwork/select", map[string]any{
		"kind": "poster", "url": urlArte, "prefer": prefer,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeAssetDTO(t, rec.Body.Bytes())
}

// assertBinarioNoDisco checks that the downloaded binary was materialized under
// {assetsDir}/e1/external-*.png (exactly one file).
func assertBinarioNoDisco(t *testing.T, dir string) {
	t.Helper()
	entradas, err := os.ReadDir(filepath.Join(dir, "e1"))
	if err != nil {
		t.Fatalf("ler dir da entity: %v", err)
	}
	if len(entradas) != 1 {
		t.Fatalf("arquivos da entity = %d, quero 1 (o binario baixado)", len(entradas))
	}
}

// assertMetaSourceURL checks that the source url ended up in meta_json (the basis of the
// idempotency).
func assertMetaSourceURL(t *testing.T, d *db.DB, assetID, urlArte string) {
	t.Helper()
	asset, err := escrita(t, d).ObterAsset(context.Background(), assetID)
	if err != nil {
		t.Fatalf("ObterAsset: %v", err)
	}
	if metaSourceURL(asset.MetaJson) != urlArte {
		t.Errorf("meta.source_url = %q, quero %q", metaSourceURL(asset.MetaJson), urlArte)
	}
}

// assertSelectIdempotente proves that a 2nd select of the SAME url reuses the same asset
// (without duplicating).
func assertSelectIdempotente(t *testing.T, h http.Handler, d *db.DB, urlArte, assetID string) {
	t.Helper()
	if a2 := selecionarArte(t, h, urlArte, false); a2.ID != assetID {
		t.Errorf("2o select criou asset novo %q, quero reusar %q (idempotente)", a2.ID, assetID)
	}
	assets, err := escrita(t, d).ListarAssetsPorEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	if len(assets) != 1 {
		t.Errorf("assets da entity = %d, quero 1 (idempotente, sem duplicar)", len(assets))
	}
}
