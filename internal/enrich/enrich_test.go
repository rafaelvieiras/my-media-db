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

// fakeProvider implementa provider.Provider sem rede real: BuscarDetalhe devolve um
// raw fixo (ou erro); MapearDetalhe devolve campos canonicos fixos (com o
// poster_url apontando para um httptest de imagem). Nome()="tmdb" para casar o
// find-or-create por external_id_tmdb.
type fakeProvider struct {
	raw      json.RawMessage
	campos   map[string]any
	erro     error
	chamadas int
	langs    []string // últimos idiomas recebidos por BuscarDetalhe (0020)
}

func (f *fakeProvider) Nome() string { return "tmdb" }

func (f *fakeProvider) Buscar(context.Context, string, string) ([]provider.ResultadoExterno, error) {
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

// setupDB sobe um banco temporario migrado.
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

// servidorImagem serve um binario de imagem fake com o content-type dado.
func servidorImagem(t *testing.T, contentType string, corpo []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(corpo)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// novoServico monta o Servico com um assetsDir temporario e o provider fake.
func novoServico(t *testing.T, database *db.DB, prov provider.Provider) (*Servico, string) {
	t.Helper()
	dir := t.TempDir()
	return New(database, prov, dir), dir
}

func TestEnriquecerGravaExternalIDsEPoster(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	imgBytes := []byte("BYTES-DA-IMAGEM-FAKE")
	img := servidorImagem(t, "image/jpeg", imgBytes)

	// Entity pobre pre-existente (ex.: draft do scanner).
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "arquivo.mkv"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	raw := json.RawMessage(`{"title":"Filme X","overview":"sinopse"}`)
	fake := &fakeProvider{raw: raw, campos: map[string]any{"title": "Filme X", "poster_url": img.URL + "/p.jpg"}}
	svc, dir := novoServico(t, database, fake)

	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"pt-BR", "en"}); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	// media_external guarda o RAW.
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
	// external_ids atualizado (identidade MD4).
	ent, _ := r.ObterEntity(ctx, "e1")
	if ent.ExternalIDTmdb.String != "603" {
		t.Errorf("external_id_tmdb = %q, quero 603 (external_ids=%q)", ent.ExternalIDTmdb.String, ent.ExternalIdsJson.String)
	}
	// asset poster origin=external com binario em disco.
	verificarPosterExterno(t, r, "e1", dir, imgBytes)
}

// verificarPosterExterno confere que existe um asset poster origin=external, sob o
// assetsDir, e que seu binario em disco bate com o esperado.
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
	// Override do dono e arte origin=user pre-existentes: NAO podem ser tocados.
	if _, err := w.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{EntityID: "e1", OverrideJson: nsValid(`{"title":"Titulo do Dono"}`)}); err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}
	if _, err := w.CriarAsset(ctx, sqlcgen.CriarAssetParams{ID: "au", EntityID: "e1", Kind: "poster", Origin: "user", PathOrUri: "/arte/do/dono.jpg"}); err != nil {
		t.Fatalf("CriarAsset user: %v", err)
	}

	fake := &fakeProvider{raw: json.RawMessage(`{"title":"X"}`), campos: map[string]any{"title": "X", "poster_url": img.URL + "/p.jpg"}}
	svc, _ := novoServico(t, database, fake)

	for i := 0; i < 2; i++ {
		if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"pt-BR", "en"}); err != nil {
			t.Fatalf("Enriquecer #%d: %v", i, err)
		}
	}

	r := sqlcgen.New(database.Read)
	verificarNaoDestrutivo(t, r, "e1")
	// Dedup: exatamente 1 asset origin=external (re-enrich reusa, nao duplica).
	if n := contarAssetsExternos(t, r, "e1"); n != 1 {
		t.Errorf("assets origin=external = %d, quero 1 (dedup)", n)
	}
}

// verificarNaoDestrutivo confere que o override do dono e a arte origin=user
// seguem intactos apos o (re-)enriquecimento.
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

// contarAssetsExternos conta os assets origin=external de uma entity.
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

// TestEnriquecerGravaOriginalLanguage prova que o enrich grava a ancora do fallback
// (entity.original_language) a partir do campo do detalhe (0020), passa os idiomas
// permitidos ao provider, e que re-enriquecer NAO destroi o override do dono.
func TestEnriquecerGravaOriginalLanguage(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("img"))
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "t"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	// Override do dono pre-existente: re-enrich nao pode toca-lo.
	if _, err := w.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{EntityID: "e1", OverrideJson: nsValid(`{"title":"Dono"}`)}); err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}

	fake := &fakeProvider{
		raw:    json.RawMessage(`{"detail":{},"tags_by_lang":{}}`),
		campos: map[string]any{"title": "X", "original_language": "ja", "poster_url": img.URL + "/p.jpg"},
	}
	svc, _ := novoServico(t, database, fake)

	permitidos := []string{"pt-BR", "en", "ja"}
	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", permitidos); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	ent, _ := r.ObterEntity(ctx, "e1")
	if ent.OriginalLanguage.String != "ja" {
		t.Errorf("original_language = %q, quero 'ja' (ancora do fallback)", ent.OriginalLanguage.String)
	}
	// Os idiomas permitidos foram repassados ao provider.
	eqLista(t, "langs repassados", fake.langs, permitidos)

	// Re-enrich: override do dono intacto.
	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", permitidos); err != nil {
		t.Fatalf("re-Enriquecer: %v", err)
	}
	ov, err := r.ObterMediaOverride(ctx, "e1")
	if err != nil || ov.OverrideJson.String != `{"title":"Dono"}` {
		t.Errorf("override do dono alterado/perdido: %q (err=%v)", ov.OverrideJson.String, err)
	}
}

// eqLista compara duas listas de string em ordem (helper de teste).
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
	// Servidor de imagem que sempre falha (500).
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

	// Enrich NAO quebra por causa da imagem.
	if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"pt-BR", "en"}); err != nil {
		t.Fatalf("Enriquecer nao deveria falhar por imagem: %v", err)
	}
	r := sqlcgen.New(database.Read)
	if _, err := r.ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{EntityID: "e1", Source: "tmdb"}); err != nil {
		t.Errorf("media_external deveria existir mesmo sem poster: %v", err)
	}
	// Sem asset externo (imagem falhou).
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

	// Segundo import com o mesmo id externo: reusa (dedup MD4), nao cria outra.
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
