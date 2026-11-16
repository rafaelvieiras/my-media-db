package fanarttv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// respostaFilme is a /movies/{id} response with several types: poster,
// background (fanart, textless), logos (hd + normal), clearart (hd + movieart),
// disc (bluray, with disc/disc_type), banner and thumb. Numbers as STRING (as
// fanart.tv serializes them).
const respostaFilme = `{
  "name": "The Matrix",
  "tmdb_id": "603",
  "imdb_id": "tt0133093",
  "movieposter": [
    {"id": "1", "url": "https://assets.fanart.tv/movieposter/en.jpg", "lang": "en", "likes": "10"},
    {"id": "2", "url": "https://assets.fanart.tv/movieposter/pt.jpg", "lang": "pt", "likes": "3"}
  ],
  "moviebackground": [
    {"id": "3", "url": "https://assets.fanart.tv/moviebg/textless.jpg", "lang": "", "likes": "7"}
  ],
  "hdmovielogo": [
    {"id": "4", "url": "https://assets.fanart.tv/hdlogo/en.png", "lang": "en", "likes": "5"}
  ],
  "movielogo": [
    {"id": "5", "url": "https://assets.fanart.tv/logo/en.png", "lang": "en", "likes": "1"}
  ],
  "hdmovieclearart": [
    {"id": "6", "url": "https://assets.fanart.tv/hdclearart/en.png", "lang": "en", "likes": "2"}
  ],
  "movieart": [
    {"id": "7", "url": "https://assets.fanart.tv/movieart/en.png", "lang": "en", "likes": "4"}
  ],
  "moviedisc": [
    {"id": "8", "url": "https://assets.fanart.tv/disc/bluray.png", "lang": "en", "likes": "6", "disc": "1", "disc_type": "bluray"}
  ],
  "moviebanner": [
    {"id": "9", "url": "https://assets.fanart.tv/banner/en.jpg", "lang": "en", "likes": "0"}
  ],
  "moviethumb": [
    {"id": "10", "url": "https://assets.fanart.tv/thumb/en.jpg", "lang": "en", "likes": "0"}
  ]
}`

// respostaTV is a /tv/{id} response with show background, poster, logo,
// characterart and SEASON arts (seasonposter with season "1" and "all").
const respostaTV = `{
  "name": "The Simpsons",
  "thetvdb_id": "71663",
  "tvposter": [
    {"id": "1", "url": "https://assets.fanart.tv/tvposter/en.jpg", "lang": "en", "likes": "9"}
  ],
  "showbackground": [
    {"id": "2", "url": "https://assets.fanart.tv/showbg/textless.jpg", "lang": "", "likes": "8"}
  ],
  "clearlogo": [
    {"id": "3", "url": "https://assets.fanart.tv/clearlogo/en.png", "lang": "en", "likes": "5"}
  ],
  "characterart": [
    {"id": "4", "url": "https://assets.fanart.tv/character/homer.png", "lang": "", "likes": "3"}
  ],
  "seasonposter": [
    {"id": "5", "url": "https://assets.fanart.tv/seasonposter/s1.jpg", "lang": "en", "likes": "2", "season": "1"},
    {"id": "6", "url": "https://assets.fanart.tv/seasonposter/all.jpg", "lang": "en", "likes": "1", "season": "all"}
  ]
}`

// servidorFanart answers /movies/{id} and /tv/{id} with the fixtures, requires
// the api_key in the query and returns 404 for an unknown id. capturado stores
// the last URL.
func servidorFanart(t *testing.T, capturado *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capturado != nil {
			*capturado = r.URL.String()
		}
		if r.URL.Query().Get("api_key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movies/603":
			_, _ = w.Write([]byte(respostaFilme))
		case "/tv/71663":
			_, _ = w.Write([]byte(respostaTV))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNomeECore(t *testing.T) {
	c := New(Credencial{APIKey: "k"}, nil, "")
	if c.Nome() != "fanarttv" {
		t.Errorf("Nome = %q, quero fanarttv", c.Nome())
	}
	// Buscar/BuscarDetalhe are no-op (fanart.tv is not a search source).
	if res, err := c.Buscar(context.Background(), "matrix", "movie", "en"); err != nil || res != nil {
		t.Errorf("Buscar no-op = (%v,%v), quero (nil,nil)", res, err)
	}
	campos, err := c.MapearDetalhe(nil)
	if err != nil || len(campos) != 0 {
		t.Errorf("MapearDetalhe no-op = (%v,%v), quero (mapa vazio,nil)", campos, err)
	}
}

func TestBuscarArtesFilme(t *testing.T) {
	var capturado string
	srv := servidorFanart(t, &capturado)
	c := New(Credencial{APIKey: "minha-chave"}, srv.Client(), srv.URL)

	artes, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{Tmdb: "603"}, "movie", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("BuscarArtes: %v", err)
	}
	porKind := indexarPorKind(artes)

	assertContagemPorKindFilme(t, porKind)
	assertDetalhesArteFilme(t, porKind)

	// a api_key foi enviada.
	if capturado == "" || !contemAPIKey(capturado) {
		t.Errorf("URL sem api_key: %q", capturado)
	}
}

// assertContagemPorKindFilme checks the number of arts per kind of the movie
// fixture.
func assertContagemPorKindFilme(t *testing.T, porKind map[string][]provider.Arte) {
	t.Helper()
	// poster: 2 (en + pt), preservando lang e likes.
	if len(porKind["poster"]) != 2 {
		t.Fatalf("poster = %d, quero 2", len(porKind["poster"]))
	}
	// logo: hd + normal = 2.
	if len(porKind["logo"]) != 2 {
		t.Errorf("logo = %d, quero 2 (hd + normal)", len(porKind["logo"]))
	}
	// clearart: hdclearart + movieart = 2.
	if len(porKind["clearart"]) != 2 {
		t.Errorf("clearart = %d, quero 2", len(porKind["clearart"]))
	}
	// banner e thumb.
	if len(porKind["banner"]) != 1 || len(porKind["thumb"]) != 1 {
		t.Errorf("banner=%d thumb=%d, quero 1 cada", len(porKind["banner"]), len(porKind["thumb"]))
	}
}

// assertDetalhesArteFilme checks the details of the arts (textless fanart, disc
// parsed, likes converted from string) of the movie fixture.
func assertDetalhesArteFilme(t *testing.T, porKind map[string][]provider.Arte) {
	t.Helper()
	// fanart: textless (lang vazio).
	if fa := porKind["fanart"]; len(fa) != 1 || fa[0].Lang != "" {
		t.Errorf("fanart = %+v, quero 1 textless", fa)
	}
	// disc: com disc/disc_type parseados.
	disc := porKind["disc"]
	if len(disc) != 1 || disc[0].Disc != 1 || disc[0].DiscType != "bluray" {
		t.Errorf("disc = %+v, quero 1 com disc=1 disc_type=bluray", disc)
	}
	// likes convertidos de string.
	for _, a := range porKind["poster"] {
		if a.Lang == "en" && a.Likes != 10 {
			t.Errorf("poster en likes = %d, quero 10", a.Likes)
		}
	}
}

func TestBuscarArtesFilmePreferTmdbSenaoImdb(t *testing.T) {
	var capturado string
	srv := servidorFanart(t, &capturado)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	// Without TMDB, it falls back to IMDB in the path.
	if _, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{Imdb: "tt0133093"}, "movie", nil); err != nil {
		t.Fatalf("BuscarArtes imdb: %v", err)
	}
	if capturado == "" || !contains(capturado, "/movies/tt0133093") {
		t.Errorf("path = %q, quero /movies/tt0133093 (fallback imdb)", capturado)
	}
}

func TestBuscarArtesTVComTemporada(t *testing.T) {
	srv := servidorFanart(t, nil)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)

	artes, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{Tvdb: "71663"}, "tv", nil)
	if err != nil {
		t.Fatalf("BuscarArtes tv: %v", err)
	}
	porKind := indexarPorKind(artes)
	if len(porKind["poster"]) != 3 { // tvposter + 2 seasonposter (s1 + all)
		t.Errorf("poster tv = %d, quero 3 (tvposter + 2 seasonposter)", len(porKind["poster"]))
	}
	if len(porKind["characterart"]) != 1 {
		t.Errorf("characterart = %d, quero 1", len(porKind["characterart"]))
	}
	// as artes de temporada carregam o season.
	var s1, sall int
	for _, a := range porKind["poster"] {
		switch a.Season {
		case "1":
			s1++
		case "all":
			sall++
		}
	}
	if s1 != 1 || sall != 1 {
		t.Errorf("season posters: s1=%d all=%d, quero 1 cada", s1, sall)
	}
}

func TestBuscarArtesSemIdOuSemMatch(t *testing.T) {
	srv := servidorFanart(t, nil)
	c := New(Credencial{APIKey: "k"}, srv.Client(), srv.URL)
	ctx := context.Background()

	// tv sem id TVDB: (nil,nil).
	if artes, err := c.BuscarArtes(ctx, provider.ExternalIDs{Tmdb: "603"}, "tv", nil); err != nil || artes != nil {
		t.Errorf("tv sem tvdb = (%v,%v), quero (nil,nil)", artes, err)
	}
	// movie with an unknown id: 404 -> (nil,nil), it degrades silently.
	if artes, err := c.BuscarArtes(ctx, provider.ExternalIDs{Tmdb: "999"}, "movie", nil); err != nil || artes != nil {
		t.Errorf("movie 404 = (%v,%v), quero (nil,nil)", artes, err)
	}
}

// indexarPorKind groups the arts by kind for the assertions.
func indexarPorKind(artes []provider.Arte) map[string][]provider.Arte {
	out := map[string][]provider.Arte{}
	for _, a := range artes {
		out[a.Kind] = append(out[a.Kind], a)
	}
	return out
}

func contemAPIKey(s string) bool { return contains(s, "api_key=") }

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
