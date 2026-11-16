package themerrdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// servidorThemerrdb answers only the known routes with fixed fixtures and
// returns 404 for any other one (the same real behavior of ThemerrDB for an id
// without a curated entry).
func servidorThemerrdb(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/movies/tmdb/603.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tmdb_id":"603","youtube_theme_url":"https://www.youtube.com/watch?v=matrix-theme"}`))
		case "/movies/imdb/tt0110912.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"imdb_id":"tt0110912","youtube_theme_url":"https://www.youtube.com/watch?v=pulpfiction-theme"}`))
		case "/tv_shows/tmdb/456.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tmdb_id":"456","youtube_theme_url":"https://www.youtube.com/watch?v=simpsons-theme"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestBuscarTemaMusicalFilmeComMatchTmdb(t *testing.T) {
	srv := servidorThemerrdb(t)
	defer srv.Close()
	c := New(nil, srv.URL)

	tema, ok, err := c.BuscarTemaMusical(context.Background(), provider.ExternalIDs{Tmdb: "603"}, "movie")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !ok {
		t.Fatal("esperava match, veio ok=false")
	}
	if tema.URL != "https://www.youtube.com/watch?v=matrix-theme" {
		t.Errorf("URL = %q, inesperada", tema.URL)
	}
}

// TestBuscarTemaMusicalFilmeCaiParaImdb covers the rule "try tmdb, otherwise
// imdb": tmdb without a match (404) but imdb with a match -> it returns the
// imdb candidate, without an error.
func TestBuscarTemaMusicalFilmeCaiParaImdb(t *testing.T) {
	srv := servidorThemerrdb(t)
	defer srv.Close()
	c := New(nil, srv.URL)

	tema, ok, err := c.BuscarTemaMusical(context.Background(), provider.ExternalIDs{Tmdb: "999", Imdb: "tt0110912"}, "movie")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !ok {
		t.Fatal("esperava match via imdb, veio ok=false")
	}
	if tema.URL != "https://www.youtube.com/watch?v=pulpfiction-theme" {
		t.Errorf("URL = %q, inesperada", tema.URL)
	}
}

// TestBuscarTemaMusicalSemMatchDevolveGraca covers the acceptance criterion:
// unknown id -> (_, false, nil), NEVER an error.
func TestBuscarTemaMusicalSemMatchDevolveGraca(t *testing.T) {
	srv := servidorThemerrdb(t)
	defer srv.Close()
	c := New(nil, srv.URL)

	tema, ok, err := c.BuscarTemaMusical(context.Background(), provider.ExternalIDs{Tmdb: "999999"}, "movie")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if ok {
		t.Fatalf("esperava sem match, veio %+v", tema)
	}
}

func TestBuscarTemaMusicalSerieComMatch(t *testing.T) {
	srv := servidorThemerrdb(t)
	defer srv.Close()
	c := New(nil, srv.URL)

	tema, ok, err := c.BuscarTemaMusical(context.Background(), provider.ExternalIDs{Tmdb: "456"}, "tv")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !ok {
		t.Fatal("esperava match, veio ok=false")
	}
	if tema.URL != "https://www.youtube.com/watch?v=simpsons-theme" {
		t.Errorf("URL = %q, inesperada", tema.URL)
	}
}

// TestBuscarTemaMusicalSerieSemTmdbDevolveGraca covers the rule "series only
// through TMDB": without a tmdb id (only imdb, which ThemerrDB does not index for
// series), the route list stays empty -> no match, no error, without hitting the
// network.
func TestBuscarTemaMusicalSerieSemTmdbDevolveGraca(t *testing.T) {
	c := New(nil, "http://nao-deveria-ser-chamado.invalid")

	tema, ok, err := c.BuscarTemaMusical(context.Background(), provider.ExternalIDs{Imdb: "tt0110912"}, "tv")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if ok {
		t.Fatalf("esperava sem match, veio %+v", tema)
	}
}

// TestBuscarTemaMusicalSemIDUtilizavel covers a movie without any known id.
func TestBuscarTemaMusicalSemIDUtilizavel(t *testing.T) {
	c := New(nil, "http://nao-deveria-ser-chamado.invalid")

	_, ok, err := c.BuscarTemaMusical(context.Background(), provider.ExternalIDs{}, "movie")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if ok {
		t.Fatal("esperava sem match, veio ok=true")
	}
}

// TestBuscarTemaMusicalStatusInesperadoPropaga covers a real error (5xx) from
// the source: it propagates, it is not graceful.
func TestBuscarTemaMusicalStatusInesperadoPropaga(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(nil, srv.URL)

	_, _, err := c.BuscarTemaMusical(context.Background(), provider.ExternalIDs{Tmdb: "603"}, "movie")
	if err == nil {
		t.Fatal("esperava erro propagado para status 500, veio nil")
	}
}

func TestNomeECoreNoOp(t *testing.T) {
	c := New(nil, "")
	if c.Nome() != "themerrdb" {
		t.Errorf("Nome() = %q, quero themerrdb", c.Nome())
	}
	if res, err := c.Buscar(context.Background(), "x", "", ""); err != nil || res != nil {
		t.Errorf("Buscar deveria ser no-op vazio, veio (%v, %v)", res, err)
	}
	if raw, err := c.BuscarDetalhe(context.Background(), "1", "movie", nil); err != nil || string(raw) != "{}" {
		t.Errorf("BuscarDetalhe deveria ser no-op {}, veio (%s, %v)", raw, err)
	}
	if m, err := c.MapearDetalhe(nil); err != nil || len(m) != 0 {
		t.Errorf("MapearDetalhe deveria ser no-op vazio, veio (%v, %v)", m, err)
	}
}
