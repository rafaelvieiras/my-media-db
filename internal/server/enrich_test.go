package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// detProvider is a fake provider for the enrich/import tests in the server:
// Nome()="tmdb" (it matches the find-or-create and the external composition),
// BuscarDetalhe returns a fixed raw payload and MapearDetalhe fixed canonical fields
// (with poster_url on an image httptest).
type detProvider struct {
	raw    json.RawMessage
	campos map[string]any
}

func (p *detProvider) Nome() string { return "tmdb" }

func (p *detProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (p *detProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return p.raw, nil
}

func (p *detProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return p.campos, nil
}

// setupEnrichServer starts a Server with a migrated database, a fake provider (tmdb) and
// a temporary assetsDir, plus an httptest server serving the poster binary. It returns
// everything the enrich/import tests need.
func setupEnrichServer(t *testing.T) (http.Handler, *db.DB, *detProvider, []byte, string) {
	t.Helper()
	imgBytes := []byte("POSTER-BYTES")
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imgBytes)
	}))
	t.Cleanup(img.Close)

	prov := &detProvider{
		raw: json.RawMessage(`{"title":"Filme Externo","overview":"sinopse externa"}`),
		campos: map[string]any{
			"title":       "Filme Externo",
			"description": "sinopse externa",
			"poster_url":  img.URL + "/p.png",
		},
	}
	assetsDir := t.TempDir()
	h, d := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(assetsDir))
	return h, d, prov, imgBytes, assetsDir
}

// decodeEntidade decodes the resolved entity from the body.
func decodeEntidade(t *testing.T, rec *httptest.ResponseRecorder) resolver.Entidade {
	t.Helper()
	var e resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decodificar Entidade: %v (corpo=%s)", err, rec.Body.String())
	}
	return e
}

func TestImportCriaEReusaSemDuplicar(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source": "tmdb", "source_id": "777", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import #1 status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)
	// The resolved response reflects the external detail (title and synopsis composed).
	eq(t, "title", ent.Title, "Filme Externo")
	if ent.Description == nil || *ent.Description != "sinopse externa" {
		t.Errorf("description = %v, quero 'sinopse externa' (composição externa)", ent.Description)
	}
	eq(t, "external_ids.tmdb", ent.ExternalIDs["tmdb"], "777")
	id1 := ent.ID

	// Second import with the same external id: it reuses (200), it does not duplicate.
	rec2 := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source": "tmdb", "source_id": "777", "type": "movie"})
	if rec2.Code != http.StatusOK {
		t.Fatalf("import #2 status = %d, quero 200 (reuso)", rec2.Code)
	}
	ent2 := decodeEntidade(t, rec2)
	if ent2.ID != id1 {
		t.Errorf("id do reuso = %q, quero %q", ent2.ID, id1)
	}
	todas, _ := escrita(t, d).ListarEntities(context.Background(), sqlcgen.ListarEntitiesParams{Limit: 100, Offset: 0})
	if len(todas) != 1 {
		t.Errorf("total de entities = %d, quero 1 (dedup por external_id)", len(todas))
	}
}

// TestImportMultiIdiomaResolvePorLang is the end-to-end of the multi-language model
// WITHOUT NETWORK: it wires the REAL TMDB provider to an httptest server serving the
// detail (with translations) and the localized genre lists; it imports and reads the
// entity by ?lang=. It proves criterion 1 (?lang=pt-BR serves pt-BR, ?lang=en serves en;
// translated genres) and that original_language is written. No poster_path in the detail
// -> no image download.
func TestImportMultiIdiomaResolvePorLang(t *testing.T) {
	const detalhe = `{
      "id": 900,
      "original_language": "en",
      "title": "The Matrix",
      "original_title": "The Matrix",
      "overview": "Neo learns the truth.",
      "release_date": "1999-03-31",
      "genres": [{"id": 878, "name": "Science Fiction"}],
      "translations": {"translations": [
        {"iso_3166_1": "US", "iso_639_1": "en", "data": {"title": "The Matrix", "overview": "Neo learns the truth."}},
        {"iso_3166_1": "BR", "iso_639_1": "pt", "data": {"title": "Matrix", "overview": "Neo descobre a verdade."}}
      ]}
    }`
	generos := map[string]string{
		"pt-BR": `{"genres":[{"id":878,"name":"Ficcao cientifica"}]}`,
		"en":    `{"genres":[{"id":878,"name":"Science Fiction"}]}`,
	}
	srv := servidorTMDBMultiIdioma(t, detalhe, generos)

	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	h, _ := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "900", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	id := decodeEntidade(t, rec).ID

	// ?lang=pt-BR serves Portuguese (translated title/synopsis/genres).
	assertEntidadePTBR(t, decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil)))
	// ?lang=en serves English.
	assertEntidadeEN(t, decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/"+id+"?lang=en", nil)))
}

// TestImportEntityCreditosParidadeComExterno proves the PARITY between the EXTERNAL
// detail (GET /external/...) and the IMPORTED entity (GET /entities/{id}): the same
// credits (cast/crew/production companies), in the SAME shape, with profile_url/logo_url
// ALREADY PROXIED on both sides (assetsDir configured). Split into helpers (one assert
// per block) to keep gocyclo low.
func TestImportEntityCreditosParidadeComExterno(t *testing.T) {
	srv := servidorTMDBRico(t, nil)
	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	h, _ := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))

	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	id := decodeEntidade(t, rec).ID

	ent := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/"+id+"?lang=pt-BR", nil))
	ext := decodeDetalheExterno(t, do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil).Body.Bytes())

	assertParidadeCast(t, ent, ext)
	assertParidadeCrew(t, ent, ext)
	assertParidadeCompanies(t, ent, ext)
}

// assertParidadeCast checks the cast: same shape and profile_url proxied on both sides
// (entity and external).
func assertParidadeCast(t *testing.T, ent resolver.Entidade, ext detalheExternoDTO) {
	t.Helper()
	if len(ent.Cast) != 1 || len(ext.Cast) != 1 {
		t.Fatalf("cast: entity=%d externo=%d, quero 1 nos dois", len(ent.Cast), len(ext.Cast))
	}
	e, x := ent.Cast[0], ext.Cast[0]
	if e.SourceID != x.SourceID || e.Name != x.Name || e.Character != x.Character || e.Order != x.Order {
		t.Errorf("cast diverge: entity=%+v externo=%+v", e, x)
	}
	assertProfileURLParidadeEProxy(t, "cast", e.ProfileURL, x.ProfileURL)
}

// assertProfileURLParidadeEProxy checks that the photo/logo URLs of the entity side and
// of the external side are EQUAL and that the entity one is already the local proxy route
// — extracted from the credits asserts only to keep gocyclo low.
func assertProfileURLParidadeEProxy(t *testing.T, rotulo string, deEntity, deExterno *string) {
	t.Helper()
	if deEntity == nil || deExterno == nil || *deEntity != *deExterno {
		t.Errorf("%s: url diverge entre entity=%v e externo=%v", rotulo, deEntity, deExterno)
		return
	}
	if !strings.HasPrefix(*deEntity, rotaProxyImagem+"?src=") {
		t.Errorf("%s: entity url = %q, quero rota do proxy local", rotulo, *deEntity)
	}
}

// assertParidadeCrew checks the crew: same shape on both sides.
func assertParidadeCrew(t *testing.T, ent resolver.Entidade, ext detalheExternoDTO) {
	t.Helper()
	if len(ent.Crew) != 1 || len(ext.Crew) != 1 || ent.Crew[0].Job != ext.Crew[0].Job {
		t.Errorf("crew diverge: entity=%+v externo=%+v", ent.Crew, ext.Crew)
	}
}

// assertParidadeCompanies checks the production companies: same shape and logo_url
// proxied.
func assertParidadeCompanies(t *testing.T, ent resolver.Entidade, ext detalheExternoDTO) {
	t.Helper()
	if len(ent.Companies) != 1 || len(ext.Companies) != 1 {
		t.Fatalf("companies: entity=%d externo=%d, quero 1 nos dois", len(ent.Companies), len(ext.Companies))
	}
	e, x := ent.Companies[0], ext.Companies[0]
	assertProfileURLParidadeEProxy(t, "company.logo_url", e.LogoURL, x.LogoURL)
}

// servidorTMDBMultiIdioma starts an httptest server that serves the movie detail and the
// genre lists localized by ?language=, like the TMDB API.
func servidorTMDBMultiIdioma(t *testing.T, detalhe string, generos map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/900":
			_, _ = w.Write([]byte(detalhe))
		case "/genre/movie/list":
			_, _ = w.Write([]byte(generos[r.URL.Query().Get("language")]))
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// assertEntidadePTBR checks the translated title/synopsis/genres and the original_language
// anchor.
func assertEntidadePTBR(t *testing.T, pt resolver.Entidade) {
	t.Helper()
	eq(t, "pt.title", pt.Title, "Matrix")
	if pt.Description == nil || *pt.Description != "Neo descobre a verdade." {
		t.Errorf("pt.description = %v, quero 'Neo descobre a verdade.'", pt.Description)
	}
	if len(pt.Tags) != 1 || pt.Tags[0] != "Ficcao cientifica" {
		t.Errorf("pt.tags = %v, quero [Ficcao cientifica]", pt.Tags)
	}
	if pt.OriginalLanguage == nil || *pt.OriginalLanguage != "en" {
		t.Errorf("original_language = %v, quero 'en' (ancora gravada)", pt.OriginalLanguage)
	}
}

// assertEntidadeEN checks the title and genres served in English.
func assertEntidadeEN(t *testing.T, en resolver.Entidade) {
	t.Helper()
	eq(t, "en.title", en.Title, "The Matrix")
	if len(en.Tags) != 1 || en.Tags[0] != "Science Fiction" {
		t.Errorf("en.tags = %v, quero [Science Fiction]", en.Tags)
	}
}

func TestEnrichEntityInexistente404(t *testing.T) {
	h, _, _, _, _ := setupEnrichServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/enrich", map[string]string{"source_id": "1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

func TestEnrichSemSourceID422(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Id Externo") // no external_ids
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/enrich", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestExternalIDDeMusicbrainzUsaChaveMbid: the MusicBrainz provider's Nome() is
// "musicbrainz", but the id itself is stored in external_ids under the key "mbid"
// (ADR-0027, all the rest of the code: internal/enrich/musica.go, external_ids.mbid in
// the frontend) -- found while validating "Baixar Metadados" without an explicit
// source_id on an already cataloged music artist/album/track: without the redirect in
// externalIDDe, the lookup by external_ids["musicbrainz"] never finds the id and the
// button always hits 422 "sem source_id", even with the source correctly resolved.
func TestExternalIDDeMusicbrainzUsaChaveMbid(t *testing.T) {
	ns := nsOf(`{"mbid":"artista-fake-1"}`)
	if got := externalIDDe(ns, "musicbrainz"); got != "artista-fake-1" {
		t.Errorf(`externalIDDe(musicbrainz) = %q, quero "artista-fake-1"`, got)
	}
	// Regression: the other sources keep looking at their own key (no redirect).
	nsTmdb := nsOf(`{"tmdb":"603"}`)
	if got := externalIDDe(nsTmdb, "tmdb"); got != "603" {
		t.Errorf(`externalIDDe(tmdb) = %q, quero "603"`, got)
	}
}

func TestImportValidacoes(t *testing.T) {
	h, _, _, _, _ := setupEnrichServer(t)

	// Malformed JSON body -> 400.
	rec := do(t, h, http.MethodPost, "/api/v1/import", []byte("{nao-json"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("corpo inválido: status = %d, quero 400", rec.Code)
	}
	// type outside movie|tv -> 422.
	rec = do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "1", "type": "person"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("type inválido: status = %d, quero 422", rec.Code)
	}
	// empty source_id -> 422.
	rec = do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"type": "movie"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("source_id vazio: status = %d, quero 422", rec.Code)
	}
}

func TestEnrichPosterServidoDoAssetsDir(t *testing.T) {
	h, _, _, imgBytes, assetsDir := setupEnrichServer(t)

	// Imports: creates the entity, caches the external poster under the assetsDir.
	rec := do(t, h, http.MethodPost, "/api/v1/import", map[string]string{"source_id": "777", "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ent := decodeEntidade(t, rec)

	// The external poster is servable through the guard (path under the trusted assetsDir).
	poster := do(t, h, http.MethodGet, "/api/v1/entities/"+ent.ID+"/assets/poster", nil)
	if poster.Code != http.StatusOK {
		t.Fatalf("GET poster status = %d, quero 200 (corpo=%s)", poster.Code, poster.Body.String())
	}
	if poster.Body.String() != string(imgBytes) {
		t.Errorf("binário servido diverge do cacheado")
	}
	// Sanity check: the file really lives under the assetsDir.
	if _, err := filepath.Rel(assetsDir, filepath.Join(assetsDir, ent.ID)); err != nil {
		t.Fatalf("layout de assets inesperado: %v", err)
	}
}

func TestPosterForaDosDirsConfiaveis403(t *testing.T) {
	h, d, _, _, _ := setupEnrichServer(t)
	semearEntity(t, d, "e1", "movie", "X")
	// Asset pointing outside any trusted dir (neither source nor assetsDir).
	if _, err := escrita(t, d).CriarAsset(context.Background(), sqlcgen.CriarAssetParams{
		ID: "a1", EntityID: "e1", Kind: "poster", Origin: "external", PathOrUri: "/etc/segredo.png",
	}); err != nil {
		t.Fatalf("CriarAsset: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/assets/poster", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, quero 403 (path fora dos dirs confiáveis)", rec.Code)
	}
	checkErrorShape(t, rec, "forbidden")
}
