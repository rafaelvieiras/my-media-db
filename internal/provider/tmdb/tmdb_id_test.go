package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// detalheTVResposta is a /tv/{id} payload: same field names as a /search/multi hit, but
// WITHOUT media_type (the route already says the type).
const detalheTVResposta = `{
  "id": 39340,
  "name": "Duas Garotas em Apuros",
  "original_name": "2 Broke Girls",
  "overview": "Duas garotas dividem um apartamento no Brooklyn.",
  "poster_path": "/broken.jpg",
  "first_air_date": "2011-09-19"
}`

// detalheFilmeResposta is a /movie/{id} payload.
const detalheFilmeResposta = `{
  "id": 603,
  "title": "Matrix",
  "original_title": "The Matrix",
  "overview": "Neo descobre a verdade.",
  "poster_path": "/matrix.jpg",
  "release_date": "1999-03-31"
}`

// findResposta is a /find/{id} payload with one series and one film.
const findResposta = `{
  "tv_results": [{
    "id": 39340,
    "name": "Duas Garotas em Apuros",
    "original_name": "2 Broke Girls",
    "first_air_date": "2011-09-19",
    "poster_path": "/broken.jpg"
  }],
  "movie_results": [{
    "id": 603,
    "title": "Matrix",
    "original_title": "The Matrix",
    "release_date": "1999-03-31"
  }],
  "person_results": [{"id": 1, "name": "Alguém"}]
}`

// servidorID serves the routes used by the ID door and records the paths requested, so a
// test can prove which routes ran (and that a 404 type does not abort the other one).
func servidorID(t *testing.T, caminhos *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caminhos != nil {
			*caminhos = append(*caminhos, r.URL.Path+"?"+r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tv/39340":
			_, _ = w.Write([]byte(detalheTVResposta))
		case "/movie/603":
			_, _ = w.Write([]byte(detalheFilmeResposta))
		case "/find/248741", "/find/tt1845307":
			_, _ = w.Write([]byte(findResposta))
		default:
			http.Error(w, "não encontrado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestResolverIDTMDBProvaOsDoisTipos proves that a bare numeric TMDB id is looked up in
// BOTH types: TMDB ids are not unique across types, so showing two rows beats silently
// picking the wrong one.
func TestResolverIDTMDBProvaOsDoisTipos(t *testing.T) {
	var caminhos []string
	srv := servidorID(t, &caminhos)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	res, err := c.ResolverID(context.Background(), provider.ReferenciaDeID{Fonte: "tmdb", ID: "39340"}, "pt-BR")
	if err != nil {
		t.Fatalf("ResolverID: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("resultados = %d, quero 1 (tv existe, movie 39340 não)", len(res))
	}
	eqS(t, "tipo", res[0].Type, "tv")
	eqS(t, "id", res[0].SourceID, "39340")
	eqS(t, "fonte", res[0].Source, "tmdb")
	eqS(t, "título", res[0].Title, "Duas Garotas em Apuros")
	eqS(t, "título original", res[0].TituloOriginal, "2 Broke Girls")
	eqSPtr(t, "ano", res[0].Year, "2011")
	eqSPtr(t, "pôster", res[0].PosterURL, "https://image.tmdb.org/t/p/w342/broken.jpg")
	if len(caminhos) != 2 {
		t.Fatalf("rotas chamadas = %v, quero as duas (tv e movie)", caminhos)
	}
}

// TestResolverIDTMDBComTipoNaReferencia proves the URL/filter type SHORT-CIRCUITS the other
// route: a themoviedb.org URL already knows it is a series.
func TestResolverIDTMDBComTipoNaReferencia(t *testing.T) {
	var caminhos []string
	srv := servidorID(t, &caminhos)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	res, err := c.ResolverID(context.Background(), provider.ReferenciaDeID{Fonte: "tmdb", ID: "39340", Tipo: "tv"}, "pt-BR")
	if err != nil {
		t.Fatalf("ResolverID: %v", err)
	}
	if len(res) != 1 || res[0].Type != "tv" {
		t.Fatalf("resultados = %+v, quero 1 série", res)
	}
	if len(caminhos) != 1 {
		t.Fatalf("rotas chamadas = %v, quero só /tv/39340", caminhos)
	}
}

// TestResolverIDFilme covers the movie route mapping (title/release_date).
func TestResolverIDFilme(t *testing.T) {
	srv := servidorID(t, nil)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	res, err := c.ResolverID(context.Background(), provider.ReferenciaDeID{Fonte: "tmdb", ID: "603", Tipo: "movie"}, "pt-BR")
	if err != nil {
		t.Fatalf("ResolverID: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("resultados = %d, quero 1", len(res))
	}
	eqS(t, "tipo", res[0].Type, "movie")
	eqS(t, "título", res[0].Title, "Matrix")
	eqSPtr(t, "ano", res[0].Year, "1999")
}

// TestResolverIDFonteDesconhecida proves an id from a source this client does not serve
// resolves to nothing instead of guessing.
func TestResolverIDFonteDesconhecida(t *testing.T) {
	srv := servidorID(t, nil)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	res, err := c.ResolverID(context.Background(), provider.ReferenciaDeID{Fonte: "letterboxd", ID: "1"}, "pt-BR")
	if err != nil || res != nil {
		t.Fatalf("res = %+v, err = %v; quero (nil, nil)", res, err)
	}
}

// TestResolverIDFindTVDB proves the TVDB id crosses through /find and that the person
// result is dropped while the series and the film come back.
func TestResolverIDFindTVDB(t *testing.T) {
	var caminhos []string
	srv := servidorID(t, &caminhos)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	res, err := c.ResolverID(context.Background(), provider.ReferenciaDeID{Fonte: "tvdb", ID: "248741"}, "pt-BR")
	if err != nil {
		t.Fatalf("ResolverID: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("resultados = %d, quero 2 (série + filme, pessoa descartada)", len(res))
	}
	eqS(t, "primeiro tipo", res[0].Type, "tv")
	eqS(t, "primeiro id", res[0].SourceID, "39340")
	eqS(t, "segundo tipo", res[1].Type, "movie")
	if len(caminhos) != 1 || !strings.Contains(caminhos[0], "/find/248741?") || !strings.Contains(caminhos[0], "external_source=tvdb_id") {
		t.Fatalf("rotas chamadas = %v", caminhos)
	}
}

// TestResolverIDFindIMDBComFiltroDeTipo proves the type filter from the input applies to
// the /find payload (a search inside the "series" tab must not offer a film).
func TestResolverIDFindIMDBComFiltroDeTipo(t *testing.T) {
	srv := servidorID(t, nil)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	res, err := c.ResolverID(context.Background(), provider.ReferenciaDeID{Fonte: "imdb", ID: "tt1845307", Tipo: "tv"}, "en")
	if err != nil {
		t.Fatalf("ResolverID: %v", err)
	}
	if len(res) != 1 || res[0].Type != "tv" {
		t.Fatalf("resultados = %+v, quero só a série", res)
	}
}

// TestResolverIDNaoEncontradoDegrada proves a 404 from the source becomes ZERO results
// (a wrong id is "found nothing", not a failure of the search).
func TestResolverIDNaoEncontradoDegrada(t *testing.T) {
	srv := servidorID(t, nil)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	res, err := c.ResolverID(context.Background(), provider.ReferenciaDeID{Fonte: "tmdb", ID: "999999999"}, "pt-BR")
	if err != nil {
		t.Fatalf("ResolverID devolveu erro: %v (quero degradar para vazio)", err)
	}
	if len(res) != 0 {
		t.Fatalf("resultados = %+v, quero vazio", res)
	}
}

// TestResolverIDErroDeRedePropaga proves a real failure (not a 404) still propagates, so
// the handler can degrade the whole external search instead of pretending "not found".
func TestResolverIDErroDeRedePropaga(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	_, err := c.ResolverID(context.Background(), provider.ReferenciaDeID{Fonte: "tmdb", ID: "39340"}, "pt-BR")
	if err == nil {
		t.Fatal("quero erro propagado em 500")
	}
	if errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatal("500 não é ErrNaoEncontrado")
	}
}
