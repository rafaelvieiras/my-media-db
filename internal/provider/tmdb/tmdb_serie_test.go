package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Compile-time assertion: the TMDB Client now satisfies the SerieProvider capability --
// the series tree is no longer exclusive to TheTVDB.
var _ provider.SerieProvider = (*Client)(nil)

// detalheTVArvore is the /tv/{id} that lists the seasons (seasons[]) of the tree,
// including season 0 (specials) -- the original language is "en".
const detalheTVArvore = `{
  "id": 456,
  "original_language": "en",
  "name": "The Simpsons",
  "seasons": [
    {"season_number": 0, "name": "Specials", "air_date": "1988-04-19", "poster_path": "/sp.jpg"},
    {"season_number": 1, "name": "Season 1", "air_date": "1989-12-17", "poster_path": "/s1.jpg"}
  ]
}`

// season1EN/season1PT are /tv/456/season/1 in the en (base) and pt-BR (translation)
// languages: the same episode.id, with localized name/overview.
const season1EN = `{
  "name": "Season 1",
  "air_date": "1989-12-17",
  "poster_path": "/s1.jpg",
  "episodes": [
    {"id": 1001, "season_number": 1, "episode_number": 1, "name": "Simpsons Roasting", "overview": "Xmas.", "air_date": "1989-12-17", "still_path": "/e1.jpg", "runtime": 23},
    {"id": 1002, "season_number": 1, "episode_number": 2, "name": "Bart the Genius", "overview": "Test.", "air_date": "1990-01-14", "still_path": "/e2.jpg", "runtime": 24}
  ]
}`

const season1PT = `{
  "name": "Temporada 1",
  "air_date": "1989-12-17",
  "episodes": [
    {"id": 1001, "season_number": 1, "episode_number": 1, "name": "Um Natal Muito Louco", "overview": "Natal."},
    {"id": 1002, "season_number": 1, "episode_number": 2, "name": "Bart, o Genio", "overview": "Prova."}
  ]
}`

const season0EN = `{
  "name": "Specials",
  "episodes": [
    {"id": 900, "season_number": 0, "episode_number": 1, "name": "Good Night", "overview": "Short.", "air_date": "1987-04-19"}
  ]
}`

// servidorArvoreTMDB answers /tv/456 and /tv/456/season/{0,1} according to ?language=.
func servidorArvoreTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		lang := r.URL.Query().Get("language")
		switch {
		case r.URL.Path == "/tv/456":
			_, _ = w.Write([]byte(detalheTVArvore))
		case r.URL.Path == "/tv/456/season/0":
			_, _ = w.Write([]byte(season0EN))
		case r.URL.Path == "/tv/456/season/1" && strings.HasPrefix(lang, "pt"):
			_, _ = w.Write([]byte(season1PT))
		case r.URL.Path == "/tv/456/season/1":
			_, _ = w.Write([]byte(season1EN))
		default:
			http.Error(w, "path inesperado: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBuscarSerieArvoreTMDB: the tree comes from TMDB (provider independence) with the
// "official" order, 2 seasons (0 and 1), episodes numbered/anchored on the id, still and
// i18n (en base + pt-BR translation merged by episode.id).
func TestBuscarSerieArvoreTMDB(t *testing.T) {
	srv := servidorArvoreTMDB(t)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	arvore, err := c.BuscarSerieArvore(context.Background(), "456", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("BuscarSerieArvore: %v", err)
	}
	if len(arvore.Temporadas) != 2 {
		t.Fatalf("temporadas = %d, quero 2 (0 e 1)", len(arvore.Temporadas))
	}
	assertOrdensArvore(t, arvore)

	t1 := temporadaPorNumero(t, arvore, 1)
	if len(t1.Episodios) != 2 {
		t.Fatalf("episodios da T1 = %d, quero 2", len(t1.Episodios))
	}
	assertTemporada1(t, t1)
	assertEpisodio1Ident(t, t1.Episodios[0])
	assertEpisodio1I18n(t, t1.Episodios[0])
}

// assertOrdensArvore checks the default order and the set of orders of the tree.
func assertOrdensArvore(t *testing.T, arvore provider.SerieArvore) {
	t.Helper()
	if arvore.DefaultSeasonType != "official" || len(arvore.SeasonTypes) != 1 {
		t.Errorf("ordens = default %q types %v, quero official/[official]", arvore.DefaultSeasonType, arvore.SeasonTypes)
	}
}

// assertTemporada1 checks the poster, the year and the localized names of season 1.
func assertTemporada1(t *testing.T, t1 provider.TemporadaExterna) {
	t.Helper()
	if t1.PosterURL == "" || !strings.Contains(t1.PosterURL, "/s1.jpg") {
		t.Errorf("poster da T1 = %q, quero conter /s1.jpg", t1.PosterURL)
	}
	if t1.Year != "1989" {
		t.Errorf("ano da T1 = %q, quero 1989", t1.Year)
	}
	if t1.Name["pt-BR"] != "Temporada 1" || t1.Name["en"] != "Season 1" {
		t.Errorf("nome da T1 por idioma = %v, quero pt-BR/en localizados", t1.Name)
	}
}

// assertEpisodio1Ident checks the identity (id/external), numbering and still of ep1.
func assertEpisodio1Ident(t *testing.T, ep provider.EpisodioExterno) {
	t.Helper()
	if ep.SourceID != "1001" || ep.ExternalIDs["tmdb"] != "1001" {
		t.Errorf("ep1 id = %q ext = %v, quero 1001", ep.SourceID, ep.ExternalIDs)
	}
	if n := ep.Numbering["official"]; n.Season != 1 || n.Number != 1 {
		t.Errorf("numbering official ep1 = %v, quero {1,1}", n)
	}
	if !strings.Contains(ep.StillURL, "/e1.jpg") {
		t.Errorf("still ep1 = %q, quero conter /e1.jpg", ep.StillURL)
	}
}

// assertEpisodio1I18n checks the runtime and the localized fields (title/overview) of ep1.
func assertEpisodio1I18n(t *testing.T, ep provider.EpisodioExterno) {
	t.Helper()
	if ep.Runtime == nil || *ep.Runtime != 23 {
		t.Errorf("runtime ep1 = %v, quero 23", ep.Runtime)
	}
	if ep.Title["en"] != "Simpsons Roasting" || ep.Title["pt-BR"] != "Um Natal Muito Louco" || ep.Title["pt"] != "Um Natal Muito Louco" {
		t.Errorf("title ep1 por idioma = %v, quero en + pt-BR + pt", ep.Title)
	}
	if ep.Overview["en"] != "Xmas." || ep.Overview["pt-BR"] != "Natal." {
		t.Errorf("overview ep1 por idioma = %v, quero en + pt-BR", ep.Overview)
	}
}

// TestBuscarSerieArvoreTMDBSourceVazio: an empty sourceID fails without touching the network.
func TestBuscarSerieArvoreTMDBSourceVazio(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "http://nao-usar.invalid")
	if _, err := c.BuscarSerieArvore(context.Background(), "", []string{"en"}); err == nil {
		t.Fatal("quero erro com sourceID vazio")
	}
}

// temporadaPorNumero finds the TemporadaExterna of a season_number (it fails if absent).
func temporadaPorNumero(t *testing.T, a provider.SerieArvore, n int) provider.TemporadaExterna {
	t.Helper()
	for _, temp := range a.Temporadas {
		if temp.SeasonNumber == n {
			return temp
		}
	}
	t.Fatalf("temporada %d ausente na arvore", n)
	return provider.TemporadaExterna{}
}
