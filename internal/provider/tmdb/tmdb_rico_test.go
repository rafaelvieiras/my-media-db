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

// detalheFilmeRico e um payload de /movie/{id} com os blocos do DETALHE RICO (0022):
// tagline (top + por idioma nas translations), backdrop, nota, runtime, coleção,
// vídeos, keywords, classificação por região (release_dates), external_ids e similar.
const detalheFilmeRico = `{
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
  "translations": {
    "id": 603,
    "translations": [
      {"iso_3166_1": "US", "iso_639_1": "en", "data": {"title": "The Matrix", "overview": "Neo learns the truth.", "tagline": "Welcome to the Real World."}},
      {"iso_3166_1": "BR", "iso_639_1": "pt", "data": {"title": "Matrix", "overview": "Neo descobre a verdade.", "tagline": "Bem-vindo ao mundo real."}}
    ]
  },
  "videos": {"results": [
    {"name": "Trailer", "key": "abc123", "site": "YouTube", "type": "Trailer", "official": true, "iso_639_1": "en"},
    {"name": "SemChave", "key": "", "site": "YouTube", "type": "Clip", "official": false}
  ]},
  "keywords": {"keywords": [{"id": 1, "name": "dystopia"}, {"id": 2, "name": "hacker"}]},
  "release_dates": {"results": [
    {"iso_3166_1": "BR", "release_dates": [{"certification": "14"}]},
    {"iso_3166_1": "US", "release_dates": [{"certification": ""}, {"certification": "R"}]}
  ]},
  "external_ids": {"imdb_id": "tt0133093"},
  "similar": {"results": [
    {"id": 604, "title": "The Matrix Reloaded", "release_date": "2003-05-15", "poster_path": "/rel.jpg"}
  ]}
}`

// servidorRico responde /movie/603 com o payload rico e as listas de genero; captura
// a ULTIMA requisicao a /movie/603 (para o teste do append_to_response).
func servidorRico(t *testing.T, capturar *http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		lang := r.URL.Query().Get("language")
		switch r.URL.Path {
		case "/movie/603":
			if capturar != nil {
				*capturar = *r
			}
			_, _ = w.Write([]byte(detalheFilmeRico))
		case "/genre/movie/list":
			_, _ = w.Write([]byte(generosFilme[lang]))
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBuscarDetalheAppendRico garante que o append_to_response pede os blocos ricos
// (videos/keywords/release_dates/content_ratings/external_ids/similar) numa so ida.
func TestBuscarDetalheAppendRico(t *testing.T) {
	var req http.Request
	srv := servidorRico(t, &req)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	if _, err := c.BuscarDetalhe(context.Background(), "603", "movie", []string{"pt-BR", "en"}); err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	ap := req.URL.Query().Get("append_to_response")
	for _, bloco := range []string{"translations", "videos", "keywords", "release_dates", "content_ratings", "external_ids", "similar"} {
		if !strings.Contains(ap, bloco) {
			t.Errorf("append_to_response %q nao contem %q", ap, bloco)
		}
	}
}

// mapearRico faz BuscarDetalhe + MapearDetalhe do filme rico e devolve o canonico.
func mapearRico(t *testing.T) map[string]any {
	t.Helper()
	srv := servidorRico(t, nil)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)
	raw, err := c.BuscarDetalhe(context.Background(), "603", "movie", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	campos, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}
	return campos
}

// TestMapearDetalheRicoTraduziveis cobre os campos ricos TRADUZIVEIS: tagline (map por
// idioma) e keywords (sob o idioma original, nao localizadas pelo TMDB).
func TestMapearDetalheRicoTraduziveis(t *testing.T) {
	campos := mapearRico(t)
	eqIdioma(t, campos, "tagline", "pt-BR", "Bem-vindo ao mundo real.")
	eqIdioma(t, campos, "tagline", "en", "Welcome to the Real World.")

	kw, ok := campos["keywords"].(map[string]any)
	if !ok {
		t.Fatalf("keywords nao e map por idioma: %T", campos["keywords"])
	}
	eqLista(t, "keywords[en]", asLista(kw["en"]), []string{"dystopia", "hacker"})
}

// TestMapearDetalheRicoNeutros cobre os campos ricos NEUTROS: runtime, nota, backdrop,
// classificacao por regiao, videos, colecao, similares e links.
func TestMapearDetalheRicoNeutros(t *testing.T) {
	campos := mapearRico(t)

	if r, _ := campos["runtime"].(int); r != 136 {
		t.Errorf("runtime = %v, quero 136", campos["runtime"])
	}
	if va, _ := campos["vote_average"].(float64); va != 8.2 {
		t.Errorf("vote_average = %v, quero 8.2", campos["vote_average"])
	}
	eqCampo(t, campos, "backdrop_url", "https://image.tmdb.org/t/p/w780/bg.jpg")

	// Classificacao por regiao: BR do release_dates, US pega a 1a nao-vazia (R).
	eqMapa(t, campos, "certification_by_region", "BR", "14")
	eqMapa(t, campos, "certification_by_region", "US", "R")

	// Videos: so o que tem chave; URL do YouTube montada.
	vids, ok := campos["videos"].([]any)
	if !ok || len(vids) != 1 {
		t.Fatalf("videos = %v, quero 1 item (com chave)", campos["videos"])
	}
	eqMapaItem(t, vids[0], "url", "https://www.youtube.com/watch?v=abc123")

	// Colecao.
	col, _ := campos["collection"].(map[string]any)
	eqMapaItem(t, col, "source_id", "2344")
	eqMapaItem(t, col, "name", "The Matrix Collection")

	// Similares herdam o tipo do pai (movie) e trazem ano do release_date.
	sim, ok := campos["similar"].([]any)
	if !ok || len(sim) != 1 {
		t.Fatalf("similar = %v, quero 1", campos["similar"])
	}
	eqMapaItem(t, sim[0], "source_id", "604")
	eqMapaItem(t, sim[0], "type", "movie")
	eqMapaItem(t, sim[0], "year", "2003")

	// Links externos (imdb do external_ids).
	links, _ := campos["external_links"].(map[string]any)
	eqMapaItem(t, links, "imdb", "https://www.imdb.com/title/tt0133093")
}

// eqMapa checa campos[campo][chave] == want (mapa aninhado no canonico).
func eqMapa(t *testing.T, campos map[string]any, campo, chave, want string) {
	t.Helper()
	m, ok := campos[campo].(map[string]any)
	if !ok {
		t.Fatalf("%s nao e mapa: %T", campo, campos[campo])
	}
	eqMapaItem(t, m, chave, want)
}

// eqMapaItem checa m[chave] == want num map[string]any dinamico (any de teste).
func eqMapaItem(t *testing.T, m any, chave, want string) {
	t.Helper()
	obj, ok := m.(map[string]any)
	if !ok {
		t.Fatalf("valor nao e map[string]any: %T", m)
	}
	if got, _ := obj[chave].(string); got != want {
		t.Errorf("[%s] = %q, quero %q", chave, got, want)
	}
}

// TestBuscarDetalheNaoEncontrado prova que um 404 da fonte vira o sentinela
// ErrNaoEncontrado (o handler o traduz em 404, distinto do 502 de falha real).
func TestBuscarDetalheNaoEncontrado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nao encontrado", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	_, err := c.BuscarDetalhe(context.Background(), "999999", "movie", []string{"pt-BR"})
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

// asLista converte um valor dinamico ([]string) numa fatia — helper local do teste.
func asLista(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	return nil
}
