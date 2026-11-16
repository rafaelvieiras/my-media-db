package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
)

// filmeRicoJSON is a /movie/603 payload with the rich detail blocks,
// translated into en (US) and pt (BR), with BR classification 14 and US R.
const filmeRicoJSON = `{
  "id": 603,
  "original_language": "en",
  "title": "The Matrix",
  "original_title": "The Matrix",
  "overview": "Neo learns the truth.",
  "tagline": "Welcome to the Real World.",
  "release_date": "1999-03-31",
  "runtime": 136,
  "vote_average": 8.2,
  "poster_path": "/matrix.jpg",
  "backdrop_path": "/bg.jpg",
  "genres": [{"id": 28, "name": "Action"}],
  "belongs_to_collection": {"id": 2344, "name": "The Matrix Collection", "poster_path": "/col.jpg"},
  "translations": {"id": 603, "translations": [
    {"iso_3166_1": "US", "iso_639_1": "en", "data": {"title": "The Matrix", "overview": "Neo learns the truth.", "tagline": "Welcome to the Real World."}},
    {"iso_3166_1": "BR", "iso_639_1": "pt", "data": {"title": "Matrix", "overview": "Neo descobre a verdade.", "tagline": "Bem-vindo ao mundo real."}}
  ]},
  "videos": {"results": [{"name": "Trailer", "key": "abc123", "site": "YouTube", "type": "Trailer", "official": true}]},
  "keywords": {"keywords": [{"id": 1, "name": "dystopia"}]},
  "release_dates": {"results": [
    {"iso_3166_1": "BR", "release_dates": [{"certification": "14"}]},
    {"iso_3166_1": "US", "release_dates": [{"certification": "R"}]}
  ]},
  "external_ids": {"imdb_id": "tt0133093"},
  "recommendations": {"results": [{"id": 604, "title": "The Matrix Reloaded", "release_date": "2003-05-15", "poster_path": "/rel.jpg", "vote_count": 20}]},
  "credits": {
    "cast": [{"id": 6384, "name": "Keanu Reeves", "character": "Neo", "order": 0, "profile_path": "/keanu.jpg"}],
    "crew": [{"id": 9339, "name": "Lana Wachowski", "department": "Directing", "job": "Director", "profile_path": "/lw.jpg"}]
  },
  "production_companies": [{"id": 174, "name": "Warner Bros.", "logo_path": "/wb.png"}]
}`

var generosFilmeSrv = map[string]string{
	"pt-BR": `{"genres":[{"id":28,"name":"Acao"}]}`,
	"en":    `{"genres":[{"id":28,"name":"Action"}]}`,
}

// servidorTMDBRico starts an httptest that serves /movie/603 (rich),
// /genre/movie/list and simulates 404 on /movie/000 and 500 on /movie/500. It counts the
// hits to /movie/603 (cache).
func servidorTMDBRico(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/603":
			if hits != nil {
				atomic.AddInt32(hits, 1)
			}
			_, _ = w.Write([]byte(filmeRicoJSON))
		case "/movie/000":
			http.Error(w, "not found", http.StatusNotFound)
		case "/movie/500":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/genre/movie/list":
			_, _ = w.Write([]byte(generosFilmeSrv[r.URL.Query().Get("language")]))
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupExternalServer starts a Server with a migrated database and the real TMDB
// provider pointed at the rich httptest.
func setupExternalServer(t *testing.T, hits *int32) (http.Handler, *db.DB) {
	t.Helper()
	srv := servidorTMDBRico(t, hits)
	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	return setupSearchServer(t, WithSearchProvider(prov))
}

// setupExternalServerComProxy is setupExternalServer with assetsDir configured — to
// prove that profile_url/logo_url of the credits come out ALREADY REWRITTEN to the
// local proxy route (the same conversion as the poster/backdrop, which is only observable
// with assetsDir present).
func setupExternalServerComProxy(t *testing.T) http.Handler {
	t.Helper()
	srv := servidorTMDBRico(t, nil)
	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	h, _ := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))
	return h
}

// decodeDetalheExterno decodes the external detail response.
func decodeDetalheExterno(t *testing.T, body []byte) detalheExternoDTO {
	t.Helper()
	var out detalheExternoDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar detalheExternoDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// obterDetalheExterno performs the GET (expecting 200) and returns the decoded DTO.
func obterDetalheExterno(t *testing.T, h http.Handler, alvo string) detalheExternoDTO {
	t.Helper()
	rec := do(t, h, http.MethodGet, alvo, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeDetalheExterno(t, rec.Body.Bytes())
}

// TestExternalDetailCamposPtBR covers the SCALAR fields resolved in pt-BR
// (translatables + value-unique neutrals + language provenance).
func TestExternalDetailCamposPtBR(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	d := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")

	eq(t, "title", d.Title, "Matrix")
	eq(t, "lang", d.Lang, "pt-BR")
	eqPtr(t, "original_title", d.OriginalTitle, "The Matrix")
	eqPtr(t, "tagline", d.Tagline, "Bem-vindo ao mundo real.")
	eqPtr(t, "description", d.Description, "Neo descobre a verdade.")
	eqPtr(t, "year", d.Year, "1999")
	eqPtr(t, "certification", d.Certification, "14") // BR region of pt-BR
	eqPtr(t, "poster_url", d.PosterURL, "https://image.tmdb.org/t/p/w342/matrix.jpg")
	eqPtr(t, "backdrop_url", d.BackdropURL, "https://image.tmdb.org/t/p/w780/bg.jpg")
	if d.Runtime == nil || *d.Runtime != 136 {
		t.Errorf("runtime = %v, quero 136", d.Runtime)
	}
	eq(t, "resolved_languages[title]", d.ResolvedLanguages["title"], "pt-BR")
}

// TestExternalDetailBlocosPtBR covers the rich BLOCKS (lists/objects) in pt-BR:
// translated genres/keywords, videos, collection, similar items and links.
func TestExternalDetailBlocosPtBR(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	d := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")

	eq(t, "genero", umItem(t, "genres", d.Genres), "Acao")
	eq(t, "keyword", umItem(t, "keywords", d.Keywords), "dystopia")
	if umItem(t, "videos", d.Videos).URL == nil {
		t.Error("video sem url")
	}
	if d.Collection == nil {
		t.Fatal("collection nil")
	}
	eq(t, "collection.source_id", d.Collection.SourceID, "2344")
	sim := umItem(t, "similar", d.Similar)
	eq(t, "similar.source_id", sim.SourceID, "604")
	eq(t, "similar.type", sim.Type, "movie")
	eq(t, "imdb link", d.ExternalLinks["imdb"], "https://www.imdb.com/title/tt0133093")
	eq(t, "tmdb link", d.ExternalLinks["tmdb"], "https://www.themoviedb.org/movie/603")
}

// TestExternalDetailCreditos covers the CAST/CREW/COMPANIES: the shape
// of the provider canonical through the DTO, without proxy (assetsDir absent → direct
// URL).
func TestExternalDetailCreditos(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	d := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")

	c := umItem(t, "cast", d.Cast)
	eq(t, "cast.source_id", c.SourceID, "6384")
	eq(t, "cast.name", c.Name, "Keanu Reeves")
	eq(t, "cast.character", c.Character, "Neo")
	if c.Order != 0 {
		t.Errorf("cast.order = %d, quero 0", c.Order)
	}
	eqPtr(t, "cast.profile_url", c.ProfileURL, "https://image.tmdb.org/t/p/w185/keanu.jpg")
	if c.EpisodeCount != nil {
		t.Errorf("cast.episode_count = %v, quero nil (filme)", *c.EpisodeCount)
	}

	cr := umItem(t, "crew", d.Crew)
	eq(t, "crew.source_id", cr.SourceID, "9339")
	eq(t, "crew.department", cr.Department, "Directing")
	eq(t, "crew.job", cr.Job, "Director")

	comp := umItem(t, "companies", d.Companies)
	eq(t, "company.source_id", comp.SourceID, "174")
	eq(t, "company.name", comp.Name, "Warner Bros.")
	eqPtr(t, "company.logo_url", comp.LogoURL, "https://image.tmdb.org/t/p/w185/wb.png")
}

// TestExternalDetailCreditosProxyados proves that, with assetsDir configured, the
// profile_url/logo_url of the credits come out ALREADY REWRITTEN to the local proxy route
// (/api/v1/images?src=...) — the same conversion as the poster/backdrop.
func TestExternalDetailCreditosProxyados(t *testing.T) {
	h := setupExternalServerComProxy(t)
	d := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")

	c := umItem(t, "cast", d.Cast)
	if c.ProfileURL == nil || !strings.HasPrefix(*c.ProfileURL, rotaProxyImagem+"?src=") {
		t.Errorf("cast.profile_url = %v, quero rota do proxy local", c.ProfileURL)
	}
	comp := umItem(t, "companies", d.Companies)
	if comp.LogoURL == nil || !strings.HasPrefix(*comp.LogoURL, rotaProxyImagem+"?src=") {
		t.Errorf("company.logo_url = %v, quero rota do proxy local", comp.LogoURL)
	}
}

// umItem requires the list to have exactly one item and returns it (it trims the
// single-item block asserts, keeping the test complexity low).
func umItem[T any](t *testing.T, nome string, s []T) T {
	t.Helper()
	if len(s) != 1 {
		var zero T
		t.Fatalf("%s: len = %d, quero 1", nome, len(s))
		return zero
	}
	return s[0]
}

func TestExternalDetailResolveEn(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	d := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=en")
	eq(t, "title", d.Title, "The Matrix")
	eqPtr(t, "certification", d.Certification, "R") // en without region -> US fallback
	if len(d.Genres) != 1 || d.Genres[0] != "Action" {
		t.Errorf("genres = %v, quero [Action]", d.Genres)
	}
}

// TestExternalDetailCacheNaoRebate: the 2nd hit on the same item does not reach the
// source.
func TestExternalDetailCacheNaoRebate(t *testing.T) {
	var hits int32
	h, _ := setupExternalServer(t, &hits)
	for i := 0; i < 2; i++ {
		rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET #%d status = %d", i, rec.Code)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("idas a /movie/603 = %d, quero 1 (cache)", got)
	}
}

// TestExternalDetailNaoCriaEntity: viewing the external one NEVER catalogs.
func TestExternalDetailNaoCriaEntity(t *testing.T) {
	h, d := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	todas, err := sqlcgen.New(d.Read).ListarEntities(context.Background(), sqlcgen.ListarEntitiesParams{Limit: 100, Offset: 0})
	if err != nil {
		t.Fatalf("ListarEntities: %v", err)
	}
	if len(todas) != 0 {
		t.Errorf("entities = %d, quero 0 (ver != catalogar)", len(todas))
	}
}

func TestExternalDetail404(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/000?lang=pt-BR", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

func TestExternalDetail502(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/500?lang=pt-BR", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "external_source_error")
}

func TestExternalDetailTypeInvalido(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/person/1?lang=pt-BR", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
}

func TestExternalDetailLangInvalido(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=xx", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_query")
}

func TestExternalDetail503SemProvider(t *testing.T) {
	h, _ := setupSearchServer(t) // no provider
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}
