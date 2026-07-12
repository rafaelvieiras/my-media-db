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

// Testes da AÇÃO de selecionar/baixar uma arte do inventario (briefing 0048):
// POST /entities/{id}/artwork/select. Cobrem o anti-SSRF (host fora da allowlist),
// o kind invalido e o caminho feliz (baixa, cria asset origin='external' com a url no
// meta, marca preferida e e idempotente). Sem rede real: o download aponta a um
// httptest allowlistado.

// setupSelectServer sobe um Server com banco migrado, assetsDir temporario e a allowlist
// do proxy RESTRITA ao host informado (mais o padrao). Devolve o handler, o *db.DB e o
// dir de assets. O httpImagem default (timeout curto) baixa do httptest sem rede externa.
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
	// Substitui a allowlist por uma restrita ao httptest (ASSIGN, nao mutacao do mapa
	// global hostsImagemPadrao).
	s.hostsImagem = map[string]bool{hostPermitido: true}
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database, dir
}

// decodeAssetDTO decodifica a resposta do select (assetDTO).
func decodeAssetDTO(t *testing.T, body []byte) assetDTO {
	t.Helper()
	var a assetDTO
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("decodificar assetDTO: %v (corpo=%s)", err, body)
	}
	return a
}

// TestSelectArtworkHostForaDaAllowlist: o host da url e do usuario; um host fora da
// allowlist do proxy e barrado com 422 (anti-SSRF), sem tocar a rede.
func TestSelectArtworkHostForaDaAllowlist(t *testing.T) {
	h, d, _ := setupSelectServer(t, "image.tmdb.org") // allowlist NAO inclui o host abaixo
	semearEntity(t, d, "e1", "movie", "Matrix")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/artwork/select", map[string]any{
		"kind": "poster", "url": "https://evil.example.com/x.png", "prefer": true,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestSelectArtworkKindInvalido: kind fora do conjunto (kindsValidos) → 422.
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

// TestSelectArtworkEntidadeInexistente: 404 para entity que nao existe.
func TestSelectArtworkEntidadeInexistente(t *testing.T) {
	h, _, _ := setupSelectServer(t, "image.tmdb.org")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/artwork/select", map[string]any{
		"kind": "poster", "url": "https://image.tmdb.org/t/p/w342/p.jpg",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestSelectArtworkBaixaCriaExternalEPrefere: url allowlistada baixa o binario, cria um
// asset origin='external' com a url no meta e (prefer=true) o marca preferido; um 2o
// select da MESMA url e idempotente (nao duplica, reusa o asset).
func TestSelectArtworkBaixaCriaExternalEPrefere(t *testing.T) {
	srv := servidorImagem(t, nil) // /img → 200 image/png
	h, d, dir := setupSelectServer(t, hostDe(t, srv.URL))
	semearEntity(t, d, "e1", "movie", "Matrix")

	urlArte := srv.URL + "/img"
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/artwork/select", map[string]any{
		"kind": "poster", "url": urlArte, "prefer": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	a := decodeAssetDTO(t, rec.Body.Bytes())
	if a.Origin != originExternal || a.Kind != "poster" || !a.Preferred {
		t.Fatalf("asset = %+v, quero origin=external kind=poster preferred=true", a)
	}

	// O binario foi materializado no disco sob {assetsDir}/e1/external-*.png.
	entradas, err := os.ReadDir(filepath.Join(dir, "e1"))
	if err != nil {
		t.Fatalf("ler dir da entity: %v", err)
	}
	if len(entradas) != 1 {
		t.Fatalf("arquivos da entity = %d, quero 1 (o binario baixado)", len(entradas))
	}

	// A url de origem ficou no meta_json (base da idempotencia).
	asset, err := escrita(t, d).ObterAsset(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("ObterAsset: %v", err)
	}
	if metaSourceURL(asset.MetaJson) != urlArte {
		t.Errorf("meta.source_url = %q, quero %q", metaSourceURL(asset.MetaJson), urlArte)
	}

	// Idempotente: 2o select da mesma url reusa o MESMO asset (sem duplicar).
	rec2 := do(t, h, http.MethodPost, "/api/v1/entities/e1/artwork/select", map[string]any{
		"kind": "poster", "url": urlArte, "prefer": false,
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("2o select status = %d, quero 200 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	if a2 := decodeAssetDTO(t, rec2.Body.Bytes()); a2.ID != a.ID {
		t.Errorf("2o select criou asset novo %q, quero reusar %q (idempotente)", a2.ID, a.ID)
	}
	assets, err := escrita(t, d).ListarAssetsPorEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	if len(assets) != 1 {
		t.Errorf("assets da entity = %d, quero 1 (idempotente, sem duplicar)", len(assets))
	}
}
