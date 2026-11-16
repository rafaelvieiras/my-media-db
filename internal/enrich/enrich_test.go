package enrich

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// fakeProvider implements provider.Provider without a real network: BuscarDetalhe returns a
// fixed raw (or an error); MapearDetalhe returns fixed canonical fields (with
// poster_url pointing at an image httptest). Nome()="tmdb" to match the
// find-or-create by external_id_tmdb.
type fakeProvider struct {
	raw      json.RawMessage
	campos   map[string]any
	erro     error
	chamadas int
	langs    []string // last languages received by BuscarDetalhe
}

func (f *fakeProvider) Nome() string { return "tmdb" }

func (f *fakeProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (f *fakeProvider) BuscarDetalhe(_ context.Context, _, _ string, langs []string) (json.RawMessage, error) {
	f.chamadas++
	f.langs = langs
	if f.erro != nil {
		return nil, f.erro
	}
	return f.raw, nil
}

func (f *fakeProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return f.campos, nil
}

// setupDB starts a temporary migrated database.
func setupDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "enrich.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

// servidorImagem serves a fake image binary with the given content-type.
func servidorImagem(t *testing.T, contentType string, corpo []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(corpo)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// novoServico builds the Servico with a temporary assetsDir and the fake provider.
func novoServico(t *testing.T, database *db.DB, prov provider.Provider) (*Servico, string) {
	t.Helper()
	dir := t.TempDir()
	return New(database, provider.NovaRegistry(prov), dir), dir
}

func TestEnriquecerGravaExternalIDsEPoster(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	imgBytes := []byte("BYTES-DA-IMAGEM-FAKE")
	img := servidorImagem(t, "image/jpeg", imgBytes)

	// Pre-existing poor entity (e.g.: draft from the scanner).
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "arquivo.mkv"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	raw := json.RawMessage(`{"title":"Filme X","overview":"sinopse"}`)
	fake := &fakeProvider{raw: raw, campos: map[string]any{"title": "Filme X", "poster_url": img.URL + "/p.jpg"}}
	svc, dir := novoServico(t, database, fake)

	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"pt-BR", "en"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	// media_external stores the RAW.
	ext, err := r.ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{EntityID: "e1", Source: "tmdb"})
	if err != nil {
		t.Fatalf("ObterMediaExternalPorFonte: %v", err)
	}
	if ext.PayloadJson.String != string(raw) {
		t.Errorf("payload_json = %q, quero o raw %q", ext.PayloadJson.String, raw)
	}
	if ext.SourceID.String != "603" {
		t.Errorf("source_id = %q, quero 603", ext.SourceID.String)
	}
	// external_ids updated (MD4 identity).
	ent, _ := r.ObterEntity(ctx, "e1")
	if ent.ExternalIDTmdb.String != "603" {
		t.Errorf("external_id_tmdb = %q, quero 603 (external_ids=%q)", ent.ExternalIDTmdb.String, ent.ExternalIdsJson.String)
	}
	// poster asset with origin=external and the binary on disk.
	verificarPosterExterno(t, r, "e1", dir, imgBytes)
}

// TestEnriquecerCacheiaBackdrop checks that the enrich caches the BACKGROUND (backdrop)
// as a fanart asset with origin=external, like the poster (parity of the rich screen).
func TestEnriquecerCacheiaBackdrop(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	poster := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))
	backdrop := servidorImagem(t, "image/jpeg", []byte("BACKDROP-BYTES"))

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "arquivo.mkv"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	fake := &fakeProvider{raw: json.RawMessage(`{"title":"X"}`), campos: map[string]any{
		"title":        "X",
		"poster_url":   poster.URL + "/p.jpg",
		"backdrop_url": backdrop.URL + "/b.jpg",
	}}
	svc, dir := novoServico(t, database, fake)
	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"pt-BR"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	// fanart asset with origin=external holding the backdrop binary on disk, under assetsDir.
	asset, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: "e1", Kind: "fanart", Origin: "external"})
	if err != nil {
		t.Fatalf("asset fanart ausente: %v", err)
	}
	if filepath.Dir(filepath.Dir(asset.PathOrUri)) != dir {
		t.Errorf("path do fanart = %q, quero sob assetsDir %q", asset.PathOrUri, dir)
	}
	gravado, err := os.ReadFile(asset.PathOrUri)
	if err != nil {
		t.Fatalf("ler binario do fanart: %v", err)
	}
	if string(gravado) != "BACKDROP-BYTES" {
		t.Errorf("binario do fanart diverge do baixado")
	}
	// Poster and fanart coexist (both kinds cached).
	if _, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: "e1", Kind: "poster", Origin: "external"}); err != nil {
		t.Errorf("poster deveria coexistir com o fanart: %v", err)
	}
}

// verificarPosterExterno checks that a poster asset with origin=external exists, under the
// assetsDir, and that its binary on disk matches the expected one.
func verificarPosterExterno(t *testing.T, r *sqlcgen.Queries, entityID, dir string, want []byte) {
	t.Helper()
	asset, err := r.ObterAssetPorEntityKindOrigin(context.Background(), sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: entityID, Kind: "poster", Origin: "external"})
	if err != nil {
		t.Fatalf("asset externo ausente: %v", err)
	}
	if filepath.Dir(filepath.Dir(asset.PathOrUri)) != dir {
		t.Errorf("path do asset = %q, quero sob assetsDir %q", asset.PathOrUri, dir)
	}
	gravado, err := os.ReadFile(asset.PathOrUri)
	if err != nil {
		t.Fatalf("ler binario do poster: %v", err)
	}
	if string(gravado) != string(want) {
		t.Errorf("binario do poster diverge do baixado")
	}
}

func TestReEnriquecerNaoDestruvivo(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("img"))
	w := sqlcgen.New(database.Write)

	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "t"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	// The owner's override and the origin=user artwork are pre-existing: they must NOT be touched.
	if _, err := w.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{EntityID: "e1", OverrideJson: nsValid(`{"title":"Titulo do Dono"}`)}); err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}
	if _, err := w.CriarAsset(ctx, sqlcgen.CriarAssetParams{ID: "au", EntityID: "e1", Kind: "poster", Origin: "user", PathOrUri: "/arte/do/dono.jpg"}); err != nil {
		t.Fatalf("CriarAsset user: %v", err)
	}

	fake := &fakeProvider{raw: json.RawMessage(`{"title":"X"}`), campos: map[string]any{"title": "X", "poster_url": img.URL + "/p.jpg"}}
	svc, _ := novoServico(t, database, fake)

	for i := 0; i < 2; i++ {
		if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"pt-BR", "en"}, false); err != nil {
			t.Fatalf("Enriquecer #%d: %v", i, err)
		}
	}

	r := sqlcgen.New(database.Read)
	verificarNaoDestrutivo(t, r, "e1")
	// Dedup: exactly 1 asset with origin=external (re-enrich reuses, does not duplicate).
	if n := contarAssetsExternos(t, r, "e1"); n != 1 {
		t.Errorf("assets origin=external = %d, quero 1 (dedup)", n)
	}
}

// verificarNaoDestrutivo checks that the owner's override and the origin=user artwork
// remain intact after the (re-)enrichment.
func verificarNaoDestrutivo(t *testing.T, r *sqlcgen.Queries, entityID string) {
	t.Helper()
	ctx := context.Background()
	ov, err := r.ObterMediaOverride(ctx, entityID)
	if err != nil || ov.OverrideJson.String != `{"title":"Titulo do Dono"}` {
		t.Errorf("override alterado/perdido: %q (err=%v)", ov.OverrideJson.String, err)
	}
	au, err := r.ObterAsset(ctx, "au")
	if err != nil || au.PathOrUri != "/arte/do/dono.jpg" || au.Origin != "user" {
		t.Errorf("asset origin=user alterado: %+v (err=%v)", au, err)
	}
}

// contarAssetsExternos counts the assets with origin=external of an entity.
func contarAssetsExternos(t *testing.T, r *sqlcgen.Queries, entityID string) int {
	t.Helper()
	assets, _ := r.ListarAssetsPorEntity(context.Background(), entityID)
	n := 0
	for _, a := range assets {
		if a.Origin == "external" {
			n++
		}
	}
	return n
}

// TestEnriquecerGravaOriginalLanguage proves that the enrich writes the fallback anchor
// (entity.original_language) from the detail field, passes the allowed languages
// to the provider, and that re-enriching does NOT destroy the owner's override.
func TestEnriquecerGravaOriginalLanguage(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("img"))
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "t"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	// Pre-existing owner override: re-enrich must not touch it.
	if _, err := w.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{EntityID: "e1", OverrideJson: nsValid(`{"title":"Dono"}`)}); err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}

	fake := &fakeProvider{
		raw:    json.RawMessage(`{"detail":{},"tags_by_lang":{}}`),
		campos: map[string]any{"title": "X", "original_language": "ja", "poster_url": img.URL + "/p.jpg"},
	}
	svc, _ := novoServico(t, database, fake)

	permitidos := []string{"pt-BR", "en", "ja"}
	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", permitidos, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	ent, _ := r.ObterEntity(ctx, "e1")
	if ent.OriginalLanguage.String != "ja" {
		t.Errorf("original_language = %q, quero 'ja' (ancora do fallback)", ent.OriginalLanguage.String)
	}
	// The allowed languages were forwarded to the provider.
	eqLista(t, "langs repassados", fake.langs, permitidos)

	// Re-enrich: the owner's override is intact.
	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", permitidos, false); err != nil {
		t.Fatalf("re-Enriquecer: %v", err)
	}
	ov, err := r.ObterMediaOverride(ctx, "e1")
	if err != nil || ov.OverrideJson.String != `{"title":"Dono"}` {
		t.Errorf("override do dono alterado/perdido: %q (err=%v)", ov.OverrideJson.String, err)
	}
}

// eqLista compares two string lists in order (test helper).
func eqLista(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, quero %v", name, got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s = %v, quero %v", name, got, want)
			return
		}
	}
}

func TestEnriquecerFalhaImagemDegrada(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	// Image server that always fails (500).
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(bad.Close)

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "t"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	fake := &fakeProvider{raw: json.RawMessage(`{"title":"X"}`), campos: map[string]any{"title": "X", "poster_url": bad.URL + "/p.jpg"}}
	svc, _ := novoServico(t, database, fake)

	// The enrich does NOT break because of the image.
	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"pt-BR", "en"}, false); err != nil {
		t.Fatalf("Enriquecer nao deveria falhar por imagem: %v", err)
	}
	r := sqlcgen.New(database.Read)
	if _, err := r.ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{EntityID: "e1", Source: "tmdb"}); err != nil {
		t.Errorf("media_external deveria existir mesmo sem poster: %v", err)
	}
	// No external asset (the image failed).
	if _, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: "e1", Kind: "poster", Origin: "external"}); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("nao deveria haver asset externo apos falha de imagem (err=%v)", err)
	}
}

func TestImportarCriaDepoisReusa(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/png", []byte("png"))
	fake := &fakeProvider{raw: json.RawMessage(`{"title":"Filme Novo"}`), campos: map[string]any{"title": "Filme Novo", "poster_url": img.URL + "/p.png"}}
	svc, _ := novoServico(t, database, fake)

	id1, criada1, err := svc.Importar(ctx, "tmdb", "777", "movie", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("Importar #1: %v", err)
	}
	if !criada1 {
		t.Error("Importar #1 deveria ter criado (criada=true)")
	}

	r := sqlcgen.New(database.Read)
	ent, _ := r.ObterEntity(ctx, id1)
	if ent.Title != "Filme Novo" {
		t.Errorf("titulo inicial = %q, quero 'Filme Novo' (do detalhe)", ent.Title)
	}
	if ent.Status != "cataloged" {
		t.Errorf("status = %q, quero cataloged (default)", ent.Status)
	}

	// Second import with the same external id: reuse (MD4 dedup), it does not create another.
	id2, criada2, err := svc.Importar(ctx, "tmdb", "777", "movie", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("Importar #2: %v", err)
	}
	if criada2 {
		t.Error("Importar #2 deveria reusar (criada=false)")
	}
	if id2 != id1 {
		t.Errorf("id2 = %q, quero o mesmo de id1 = %q", id2, id1)
	}
	todas, _ := r.ListarEntities(ctx, sqlcgen.ListarEntitiesParams{Limit: 100, Offset: 0})
	if len(todas) != 1 {
		t.Errorf("total de entities = %d, quero 1 (sem duplicar)", len(todas))
	}
}

// TestImportarTituloDeMapaPorIdioma covers the REGRESSION of the multi-language model:
// when the provider returns "title" as map[language]value (the multi-language envelope), the
// initial title written on the entity has to be the one of the PRIMARY language (langs[0]) --
// not the sourceID. Before, the `.(string)` assertion failed on the map and the entity was born
// with the numeric id as its title (the "number instead of the name" in the list).
func TestImportarTituloDeMapaPorIdioma(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProvider{
		raw: json.RawMessage(`{}`),
		campos: map[string]any{
			"title":          map[string]any{"pt-BR": "Duna", "en": "Dune"},
			"original_title": "Dune",
		},
	}
	svc, _ := novoServico(t, database, fake)

	id, _, err := svc.Importar(ctx, "tmdb", "438631", "movie", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}
	ent, _ := sqlcgen.New(database.Read).ObterEntity(ctx, id)
	if ent.Title != "Duna" {
		t.Errorf("titulo = %q, quero 'Duna' (principal do mapa, nao o sourceID)", ent.Title)
	}
}

// TestEnriquecerCuraTituloNumerico covers the AUTO-HEALING: an entity that ended up with
// the title == sourceID (an item imported BEFORE the fix) has its title recomputed from the
// detail when it is re-enriched. An already readable title is NOT touched.
func TestEnriquecerCuraTituloNumerico(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)
	// "Broken" entity: title == id of the source (the numeric fallback from before the fix).
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "438631"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	fake := &fakeProvider{
		raw:    json.RawMessage(`{}`),
		campos: map[string]any{"title": map[string]any{"pt-BR": "Duna", "en": "Dune"}},
	}
	svc, _ := novoServico(t, database, fake)

	if err := svc.Enriquecer(ctx, "e1", "tmdb", "438631", "movie", []string{"pt-BR", "en"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}
	ent, _ := sqlcgen.New(database.Read).ObterEntity(ctx, "e1")
	if ent.Title != "Duna" {
		t.Errorf("titulo apos cura = %q, quero 'Duna'", ent.Title)
	}
}

// TestEnriquecerNaoTocaTituloLegivel guarantees that the healing is STRICT: a title that is
// already readable (different from the sourceID) remains intact on re-enrichment.
func TestEnriquecerNaoTocaTituloLegivel(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "Meu Titulo Editado"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	fake := &fakeProvider{
		raw:    json.RawMessage(`{}`),
		campos: map[string]any{"title": map[string]any{"pt-BR": "Duna"}},
	}
	svc, _ := novoServico(t, database, fake)

	if err := svc.Enriquecer(ctx, "e1", "tmdb", "438631", "movie", []string{"pt-BR"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}
	ent, _ := sqlcgen.New(database.Read).ObterEntity(ctx, "e1")
	if ent.Title != "Meu Titulo Editado" {
		t.Errorf("titulo = %q, quero intacto 'Meu Titulo Editado'", ent.Title)
	}
}

// TestImportarTituloCaiNoSourceID guarantees the final fallback: with no title at all in the
// canonical map, the entity uses the sourceID (defensive behaviour preserved).
func TestImportarTituloCaiNoSourceID(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProvider{raw: json.RawMessage(`{}`), campos: map[string]any{}}
	svc, _ := novoServico(t, database, fake)

	id, _, err := svc.Importar(ctx, "tmdb", "999", "movie", []string{"pt-BR"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}
	ent, _ := sqlcgen.New(database.Read).ObterEntity(ctx, id)
	if ent.Title != "999" {
		t.Errorf("titulo = %q, quero '999' (fallback sourceID)", ent.Title)
	}
}
