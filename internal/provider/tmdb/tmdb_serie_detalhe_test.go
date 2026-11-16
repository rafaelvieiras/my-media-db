package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// detalheSerieRico is a /tv/{id} payload with the rich SERIES blocks: status
// (situation), genres, tagline (top + per language), backdrop, content_ratings (series
// rating), keywords (results), recommendations (name/first_air_date, switched from
// `similar`, see tmdb.go) and external_ids.
const detalheSerieRico = `{
  "id": 456,
  "original_language": "en",
  "name": "The Simpsons",
  "original_name": "The Simpsons",
  "overview": "A familia de Springfield.",
  "tagline": "The whole family in one show.",
  "status": "Returning Series",
  "first_air_date": "1989-12-17",
  "episode_run_time": [22],
  "vote_average": 8.3,
  "poster_path": "/simpsons.jpg",
  "backdrop_path": "/simpsons_bg.jpg",
  "genres": [{"id": 16, "name": "Animation"}, {"id": 35, "name": "Comedy"}],
  "translations": {
    "id": 456,
    "translations": [
      {"iso_3166_1": "US", "iso_639_1": "en", "data": {"name": "The Simpsons", "overview": "Springfield family.", "tagline": "The whole family in one show."}},
      {"iso_3166_1": "BR", "iso_639_1": "pt", "data": {"name": "Os Simpsons", "overview": "A familia de Springfield.", "tagline": "A familia toda num programa so."}}
    ]
  },
  "keywords": {"results": [{"id": 1, "name": "satire"}, {"id": 2, "name": "family"}]},
  "content_ratings": {"results": [
    {"iso_3166_1": "BR", "rating": "12"},
    {"iso_3166_1": "US", "rating": "TV-PG"}
  ]},
  "external_ids": {"imdb_id": "tt0096697"},
  "recommendations": {"results": [
    {"id": 1434, "name": "Family Guy", "first_air_date": "1999-01-31", "poster_path": "/fg.jpg", "genre_ids": [16, 35], "vote_average": 7.5, "vote_count": 5000}
  ]}
}`

// generosSerieRico are the SERIES genre lists localized per language (/genre/tv/list),
// for the genres->tags mapping per language.
var generosSerieRico = map[string]string{
	"pt-BR": `{"genres": [{"id": 16, "name": "Animacao"}, {"id": 35, "name": "Comedia"}]}`,
	"en":    `{"genres": [{"id": 16, "name": "Animation"}, {"id": 35, "name": "Comedy"}]}`,
	"":      `{"genres": [{"id": 16, "name": "Animation"}, {"id": 35, "name": "Comedy"}]}`,
}

// servidorSerieRico answers /find (tvdb->tmdb crossing), /tv/456 (rich detail) and
// /genre/tv/list (localized genres). capturarTV stores the LAST request to /tv/456.
func servidorSerieRico(t *testing.T, capturarTV, capturarFind *http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		lang := r.URL.Query().Get("language")
		switch r.URL.Path {
		case "/find/71663":
			if capturarFind != nil {
				*capturarFind = *r
			}
			_, _ = w.Write([]byte(`{"tv_results": [{"id": 456}], "movie_results": []}`))
		case "/tv/456":
			if capturarTV != nil {
				*capturarTV = *r
			}
			_, _ = w.Write([]byte(detalheSerieRico))
		case "/genre/tv/list":
			_, _ = w.Write([]byte(generosSerieRico[lang]))
		default:
			http.Error(w, "path inesperado: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBuscarSerieDetalheRicoAppend guarantees the /find?external_source=tvdb_id crossing
// and the append_to_response of the rich series blocks in a single round trip to /tv.
func TestBuscarSerieDetalheRicoAppend(t *testing.T) {
	var reqTV, reqFind http.Request
	srv := servidorSerieRico(t, &reqTV, &reqFind)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	if _, ok, err := c.BuscarSerieDetalheRico(context.Background(), "71663", []string{"pt-BR", "en"}); err != nil || !ok {
		t.Fatalf("BuscarSerieDetalheRico: ok=%v err=%v", ok, err)
	}
	if src := reqFind.URL.Query().Get("external_source"); src != "tvdb_id" {
		t.Errorf("find external_source = %q, quero tvdb_id", src)
	}
	ap := reqTV.URL.Query().Get("append_to_response")
	for _, bloco := range []string{"content_ratings", "keywords", "translations", "recommendations", "external_ids"} {
		if !strings.Contains(ap, bloco) {
			t.Errorf("append_to_response %q nao contem %q", ap, bloco)
		}
	}
}

// mapearSerieRicoTeste performs the crossing + mapping and returns the rich canonical.
func mapearSerieRicoTeste(t *testing.T) map[string]any {
	t.Helper()
	srv := servidorSerieRico(t, nil, nil)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)
	campos, ok, err := c.BuscarSerieDetalheRico(context.Background(), "71663", []string{"pt-BR", "en"})
	if err != nil || !ok {
		t.Fatalf("BuscarSerieDetalheRico: ok=%v err=%v", ok, err)
	}
	return campos
}

// TestSerieRicoTraduziveis covers the TRANSLATABLE rich fields of the contract: tagline
// (per language), keywords and tags (genres localized per language).
func TestSerieRicoTraduziveis(t *testing.T) {
	campos := mapearSerieRicoTeste(t)

	eqIdioma(t, campos, "tagline", "pt-BR", "A familia toda num programa so.")
	eqIdioma(t, campos, "tagline", "en", "The whole family in one show.")

	kw, _ := campos["keywords"].(map[string]any)
	eqLista(t, "keywords[en]", asLista(kw["en"]), []string{"satire", "family"})

	eqIdiomaLista(t, campos, "tags", "pt-BR", []string{"Animacao", "Comedia"})
	eqIdiomaLista(t, campos, "tags", "en", []string{"Animation", "Comedy"})
}

// TestSerieRicoNeutros covers the NEUTRAL rich fields of the contract: rating by
// region, backdrop_url and airing_status (raw) -- and the NON-inclusion of
// title/overview/original_language/poster (which would clobber the TVDB tree). The
// SIMILAR ones are no longer covered here: the rich canonical mapping of a series
// (mapearSerieRicoCanonico -> enriquecerCanonicoRico -> enriquecerNeutrosRico) no longer
// fills in campos["similar"] -- that responsibility migrated to the Similares method
// (provider.SimilarProvider), covered by the TestSimilares* tests in
// tmdb_rico_test.go.
func TestSerieRicoNeutros(t *testing.T) {
	campos := mapearSerieRicoTeste(t)

	cert, _ := campos["certification_by_region"].(map[string]any)
	if cert["BR"] != "12" || cert["US"] != "TV-PG" {
		t.Errorf("certification_by_region = %v, quero BR=12 US=TV-PG", cert)
	}

	if _, ok := campos["similar"]; ok {
		t.Errorf("campos[\"similar\"] presente = %v, mapeamento rico de serie nao deve mais preencher essa chave (0091)", campos["similar"])
	}

	if bd, _ := campos["backdrop_url"].(string); !strings.Contains(bd, "/simpsons_bg.jpg") {
		t.Errorf("backdrop_url = %q, quero conter /simpsons_bg.jpg", bd)
	}
	if campos["airing_status"] != "Returning Series" {
		t.Errorf("airing_status = %v, quero Returning Series (cru)", campos["airing_status"])
	}
	exigirSemChavesTMDB(t, campos, "title", "description", "original_language", "poster_url", "release_date")
}

// exigirSemChavesTMDB fails if the map carries any of the given keys.
func exigirSemChavesTMDB(t *testing.T, m map[string]any, chaves ...string) {
	t.Helper()
	for _, k := range chaves {
		if _, existe := m[k]; existe {
			t.Errorf("canonico rico NAO deveria carregar %q (clobbaria o TVDB)", k)
		}
	}
}

// TestSerieRicoSemMatch: /find without tv_results returns (nil,false,nil) -- gracefully
// (TVDB-only), never an error.
func TestSerieRicoSemMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/find/999" {
			_, _ = w.Write([]byte(`{"tv_results": [], "movie_results": []}`))
			return
		}
		http.Error(w, "nao deveria buscar /tv sem match", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	campos, ok, err := c.BuscarSerieDetalheRico(context.Background(), "999", []string{"en"})
	if err != nil || ok || campos != nil {
		t.Fatalf("sem match: quero (nil,false,nil), tive (%v,%v,%v)", campos, ok, err)
	}
}
