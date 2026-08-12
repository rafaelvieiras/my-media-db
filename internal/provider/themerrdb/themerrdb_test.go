package themerrdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// servidorThemerrdb responde só as rotas conhecidas com fixtures fixas e
// devolve 404 para qualquer outra (mesmo comportamento real da ThemerrDB
// para um id sem entrada curada).
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

// TestBuscarTemaMusicalFilmeCaiParaImdb cobre a regra "tenta tmdb, senão
// imdb": tmdb sem match (404) mas imdb com match -> devolve o candidato do
// imdb, sem erro.
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

// TestBuscarTemaMusicalSemMatchDevolveGraca cobre o critério de aceite §5:
// id desconhecido -> (_, false, nil), NUNCA erro.
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

// TestBuscarTemaMusicalSerieSemTmdbDevolveGraca cobre a regra "série só via
// TMDB": sem id tmdb (só imdb, que a ThemerrDB não indexa pra série), a lista
// de rotas fica vazia -> sem match, sem erro, sem bater na rede.
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

// TestBuscarTemaMusicalSemIDUtilizavel cobre filme sem nenhum id conhecido.
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

// TestBuscarTemaMusicalStatusInesperadoPropaga cobre erro real (5xx) da
// fonte: propaga, não é graça.
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
