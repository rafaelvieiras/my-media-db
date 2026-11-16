package server

// Tests of POST /entities/{id}/materialize-missing-media (Manage menu >
// "Download Missing Artwork"): it proves that the handler (1) forces
// a NEW query to the registered ArteProvider (updating/writing the
// media_external inventory(source='fanarttv')) and (2) materializes in the real
// folder via the SAME MaterializarMidiasFaltantes of the job -- same fake provider
// pattern of internal/enrich/artes_test.go, same allowlist/assetsDir pattern of
// artwork_missing_test.go/midias_faltantes_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// fakeArteProviderServer implements the NO-OP core provider.Provider (Nome
// "fanarttv") PLUS the provider.ArteProvider capability, returning canned
// artwork -- it mirrors fakeArteProvider of internal/enrich/artes_test.go (not
// exported from there, so it needs its own copy here).
type fakeArteProviderServer struct {
	artes    []provider.Arte
	chamadas int
}

func (f *fakeArteProviderServer) Nome() string { return "fanarttv" }

func (f *fakeArteProviderServer) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (f *fakeArteProviderServer) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (f *fakeArteProviderServer) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (f *fakeArteProviderServer) BuscarArtes(_ context.Context, _ provider.ExternalIDs, _ string, _ []string) ([]provider.Arte, error) {
	f.chamadas++
	return f.artes, nil
}

// setupMaterializeMissingMediaServer starts a Server with a temporary assetsDir,
// the fakeArteProviderServer registered (it turns on s.enrich) and the proxy allowlist
// restricted to the informed host.
func setupMaterializeMissingMediaServer(t *testing.T, hostPermitido string, arte *fakeArteProviderServer) (http.Handler, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "materialize-missing.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database, WithAssetsDir(dir), WithSearchProvider(arte))
	s.hostsImagem = map[string]bool{hostPermitido: true}
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// semearEntityComTmdb creates an entity with external_ids_json={"tmdb":sourceID}
// -- the anchor that AtualizarInventarioArtes/idsExternosDe needs to cross
// with the ArteProvider (without any external id, BuscarArtes is not even called).
func semearEntityComTmdb(t *testing.T, d *db.DB, id, tipo, titulo, sourceID string) {
	t.Helper()
	extIDs, err := json.Marshal(map[string]string{"tmdb": sourceID})
	if err != nil {
		t.Fatalf("marshal external_ids: %v", err)
	}
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipo, Title: titulo, ExternalIdsJson: nsOf(string(extIDs)),
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// decodeMaterializeMissingMediaResult decodes the endpoint response.
func decodeMaterializeMissingMediaResult(t *testing.T, body []byte) materializeMissingMediaResultDTO {
	t.Helper()
	var out materializeMissingMediaResultDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar materializeMissingMediaResultDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestMaterializeMissingMediaAtualizaInventarioEMaterializa covers the central
// criterion: the entity's fanart.tv inventory is written by a NEW query to the
// ArteProvider (it never reuses a pre-existing inventory only passively) AND the
// resulting winning artwork is really materialized in the real library folder, together
// with the .nfo.
func TestMaterializeMissingMediaAtualizaInventarioEMaterializa(t *testing.T) {
	srv := servidorImagem(t, nil)
	arte := &fakeArteProviderServer{artes: []provider.Arte{
		{Kind: kindPoster, URL: srv.URL + "/img", Likes: 5},
	}}
	h, d := setupMaterializeMissingMediaServer(t, hostDe(t, srv.URL), arte)
	semearEntityComTmdb(t, d, "e1", "movie", "Matrix", "603")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/materialize-missing-media", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if arte.chamadas != 1 {
		t.Errorf("BuscarArtes chamado %d vezes, quero 1 (nova consulta forcada)", arte.chamadas)
	}

	resultado := decodeMaterializeMissingMediaResult(t, rec.Body.Bytes())
	if len(resultado.ArtesBaixadas) != 1 || resultado.ArtesBaixadas[0] != kindPoster {
		t.Errorf("artes_baixadas = %v, quero [poster]", resultado.ArtesBaixadas)
	}
	if !resultado.NfoGravado {
		t.Error("nfo_gravado = false, quero true (entity sem nfo_path ainda)")
	}

	// The fanart.tv inventory was written (media_external source=fanarttv).
	row, err := escrita(t, d).ObterMediaExternalPorFonte(context.Background(), sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: "e1", Source: "fanarttv",
	})
	if err != nil {
		t.Fatalf("inventario fanarttv ausente: %v", err)
	}
	if row.PayloadJson.String == "" {
		t.Error("payload do inventario fanarttv vazio")
	}

	// The artwork was really materialized in the real folder (poster.png -- content-type
	// of the test image, servidorImagem serves PNG).
	posterPath := filepath.Join(libDir, "poster.png")
	if _, err := os.Stat(posterPath); err != nil {
		t.Errorf("poster nao materializado em disco (%s): %v", posterPath, err)
	}
	nfoPath := filepath.Join(libDir, "Matrix.1999.nfo")
	if _, err := os.Stat(nfoPath); err != nil {
		t.Errorf("nfo nao gravado em disco (%s): %v", nfoPath, err)
	}
}

// TestMaterializeMissingMediaSemEnrichDegradaESoMaterializa covers the
// silent degradation (AGENTS.md): without ANY external search provider
// registered, s.enrich stays nil -- the handler skips the inventory update step
// (without 5xx) and goes straight to MaterializarMidiasFaltantes over
// whatever inventory already exists (here, none -- so nothing to materialize,
// but the .nfo is still written, best-effort as always).
func TestMaterializeMissingMediaSemEnrichDegradaESoMaterializa(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "sem-enrich.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database, WithAssetsDir(dir)) // without WithSearchProvider: s.enrich stays nil
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	semearEntity(t, database, "e1", "movie", "Sem Provider")
	libDir := libDirComVideo(t, database, "e1", "mf1", "Sem.Provider.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/materialize-missing-media", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeMaterializeMissingMediaResult(t, rec.Body.Bytes())
	if len(resultado.ArtesBaixadas) != 0 {
		t.Errorf("artes_baixadas = %v, quero [] (sem inventario, nada a materializar)", resultado.ArtesBaixadas)
	}
	if !resultado.NfoGravado {
		t.Error("nfo_gravado = false, quero true (nfo nao depende de arte)")
	}
	if _, err := os.Stat(filepath.Join(libDir, "Sem.Provider.nfo")); err != nil {
		t.Errorf("nfo nao gravado em disco: %v", err)
	}
}

// TestMaterializeMissingMediaEntidadeInexistente: 404 for a nonexistent entity.
func TestMaterializeMissingMediaEntidadeInexistente(t *testing.T) {
	arte := &fakeArteProviderServer{}
	h, _ := setupMaterializeMissingMediaServer(t, "example.com", arte)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/materialize-missing-media", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}
