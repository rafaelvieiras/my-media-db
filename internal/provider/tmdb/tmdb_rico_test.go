package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// detalheFilmeRico is a /movie/{id} payload with the blocks of the RICH DETAIL:
// tagline (top + per language in the translations), backdrop, score, runtime,
// collection, videos, keywords, rating by region (release_dates), external_ids and
// recommendations (switched from `similar`, see tmdb.go).
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
  "recommendations": {"results": [
    {"id": 604, "title": "The Matrix Reloaded", "release_date": "2003-05-15", "poster_path": "/rel.jpg", "genre_ids": [28], "vote_average": 7.1, "vote_count": 5000}
  ]}
}`

// servidorRico answers /movie/603 with the rich payload and the genre lists; it captures
// the LAST request to /movie/603 (for the append_to_response test).
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

// TestBuscarDetalheAppendRico guarantees that append_to_response requests the rich blocks
// (videos/keywords/release_dates/content_ratings/external_ids/recommendations) in a
// single round trip. `recommendations` replaces `similar`.
func TestBuscarDetalheAppendRico(t *testing.T) {
	var req http.Request
	srv := servidorRico(t, &req)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	if _, err := c.BuscarDetalhe(context.Background(), "603", "movie", []string{"pt-BR", "en"}); err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	ap := req.URL.Query().Get("append_to_response")
	for _, bloco := range []string{"translations", "videos", "keywords", "release_dates", "content_ratings", "external_ids", "recommendations"} {
		if !strings.Contains(ap, bloco) {
			t.Errorf("append_to_response %q nao contem %q", ap, bloco)
		}
	}
}

// mapearRico does BuscarDetalhe + MapearDetalhe of the rich movie and returns the canonical.
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

// TestMapearDetalheRicoTraduziveis covers the TRANSLATABLE rich fields: tagline (map by
// language) and keywords (under the original language, not localized by TMDB).
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

// TestMapearDetalheRicoNeutros covers the NEUTRAL rich fields: runtime, score, backdrop,
// rating by region, videos, collection and links. The SIMILAR ones are no longer covered
// here: MapearDetalhe/enriquecerNeutrosRico no longer fills in out["similar"] -- that
// responsibility migrated to the Similares method (provider.SimilarProvider), covered by
// the TestSimilares* tests further below.
func TestMapearDetalheRicoNeutros(t *testing.T) {
	campos := mapearRico(t)

	if r, _ := campos["runtime"].(int); r != 136 {
		t.Errorf("runtime = %v, quero 136", campos["runtime"])
	}
	if va, _ := campos["vote_average"].(float64); va != 8.2 {
		t.Errorf("vote_average = %v, quero 8.2", campos["vote_average"])
	}
	eqCampo(t, campos, "backdrop_url", "https://image.tmdb.org/t/p/w780/bg.jpg")

	// Rating by region: BR from release_dates, US takes the 1st non-empty one (R).
	eqMapa(t, campos, "certification_by_region", "BR", "14")
	eqMapa(t, campos, "certification_by_region", "US", "R")

	// Videos: only the ones with a key; the YouTube URL is assembled.
	vids, ok := campos["videos"].([]any)
	if !ok || len(vids) != 1 {
		t.Fatalf("videos = %v, quero 1 item (com chave)", campos["videos"])
	}
	eqMapaItem(t, vids[0], "url", "https://www.youtube.com/watch?v=abc123")

	// Collection.
	col, _ := campos["collection"].(map[string]any)
	eqMapaItem(t, col, "source_id", "2344")
	eqMapaItem(t, col, "name", "The Matrix Collection")

	if _, ok := campos["similar"]; ok {
		t.Errorf("campos[\"similar\"] presente = %v, MapearDetalhe nao deve mais preencher essa chave (0091)", campos["similar"])
	}

	// External links (imdb from external_ids).
	links, _ := campos["external_links"].(map[string]any)
	eqMapaItem(t, links, "imdb", "https://www.imdb.com/title/tt0133093")
}

// eqMapa checks campos[field][key] == want (nested map in the canonical).
func eqMapa(t *testing.T, campos map[string]any, campo, chave, want string) {
	t.Helper()
	m, ok := campos[campo].(map[string]any)
	if !ok {
		t.Fatalf("%s nao e mapa: %T", campo, campos[campo])
	}
	eqMapaItem(t, m, chave, want)
}

// eqMapaItem checks m[key] == want in a dynamic map[string]any (test any).
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

// TestBuscarDetalheNaoEncontrado proves that a 404 from the source becomes the sentinel
// ErrNaoEncontrado (the handler translates it into 404, distinct from the 502 of a real
// failure).
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

// asLista converts a dynamic value ([]string) into a slice -- a local test helper.
func asLista(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	return nil
}

// TestTvdbIDDoDetalheSerieDoEnvelope: the external_ids append of a series already brings
// tvdb_id for free; TvdbIDDoDetalheSerie extracts it from the AUGMENTED envelope (the same
// raw returned by BuscarDetalhe/PayloadFresco), without an extra round trip to the
// network.
func TestTvdbIDDoDetalheSerieDoEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		lang := r.URL.Query().Get("language")
		switch r.URL.Path {
		case "/tv/1396":
			_, _ = w.Write([]byte(`{
			  "id": 1396,
			  "original_language": "en",
			  "name": "Breaking Bad",
			  "external_ids": {"imdb_id": "tt0903747", "tvdb_id": "81189"},
			  "translations": {"id": 1396, "translations": []}
			}`))
		case "/genre/tv/list":
			_, _ = w.Write([]byte(generosSerie[lang]))
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	raw, err := c.BuscarDetalhe(context.Background(), "1396", "tv", []string{"en"})
	if err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	id, ok := c.TvdbIDDoDetalheSerie(raw)
	if !ok || id != "81189" {
		t.Errorf("TvdbIDDoDetalheSerie = (%q,%v), quero (81189,true)", id, ok)
	}
}

// TestTvdbIDDoDetalheSeriePayloadLegado proves that the method also accepts the raw detail
// WITHOUT an envelope (legacy payload, without {detail,tags_by_lang}) -- it does not
// assume that PayloadFresco always returns the augmented envelope.
func TestTvdbIDDoDetalheSeriePayloadLegado(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	raw := json.RawMessage(`{"id":1396,"external_ids":{"tvdb_id":"81189"}}`)
	id, ok := c.TvdbIDDoDetalheSerie(raw)
	if !ok || id != "81189" {
		t.Errorf("payload legado: got (%q,%v), quero (81189,true)", id, ok)
	}
}

// TestTvdbIDDoDetalheSerieSemMatch covers the silent degradation: without a tvdb_id in
// the external_ids, or with an unreadable payload, ok=false -- never a panic/propagated
// error.
func TestTvdbIDDoDetalheSerieSemMatch(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	if id, ok := c.TvdbIDDoDetalheSerie(json.RawMessage(`{"detail":{"external_ids":{"imdb_id":"tt1"}}}`)); ok || id != "" {
		t.Errorf("sem tvdb_id: got (%q,%v), quero (\"\",false)", id, ok)
	}
	if id, ok := c.TvdbIDDoDetalheSerie(json.RawMessage(`nao e json`)); ok || id != "" {
		t.Errorf("payload ilegivel: got (%q,%v), quero (\"\",false)", id, ok)
	}
}

// TestTvdbIDDoDetalheSerieNumerico proves that a numeric tvdb_id (TMDB is inconsistent
// between titles: sometimes a string, sometimes a JSON number) is accepted and
// normalized to a string -- before the fix, the Unmarshal failed and the method returned
// ("",false) even with the tvdb_id present.
func TestTvdbIDDoDetalheSerieNumerico(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	raw := json.RawMessage(`{"detail":{"external_ids":{"imdb_id":"tt0229888","tvdb_id":212601}}}`)
	id, ok := c.TvdbIDDoDetalheSerie(raw)
	if !ok || id != "212601" {
		t.Errorf("tvdb_id numerico: got (%q,%v), quero (212601,true)", id, ok)
	}
}

// TestSimilaresFiltraAdultoEVotoBaixo: an adult item and an item with a vote_count below
// the threshold (votoMinimoSimilar=20) never appear in the list of similars, even with a
// high genre overlap and score; only the valid item (non-adult, vote_count at the
// threshold) survives.
func TestSimilaresFiltraAdultoEVotoBaixo(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	raw := json.RawMessage(`{
		"id": 603, "title": "The Matrix", "genres": [{"id": 28, "name": "Action"}],
		"recommendations": {"results": [
			{"id": 1, "title": "Adulto", "adult": true, "genre_ids": [28], "vote_average": 9, "vote_count": 1000},
			{"id": 2, "title": "PoucosVotos", "adult": false, "genre_ids": [28], "vote_average": 9, "vote_count": 19},
			{"id": 3, "title": "Valido", "adult": false, "genre_ids": [28], "vote_average": 7, "vote_count": 20}
		]}
	}`)
	sim, err := c.Similares(raw)
	if err != nil {
		t.Fatalf("Similares: %v", err)
	}
	if len(sim) != 1 || sim[0].SourceID != "3" {
		t.Fatalf("Similares = %+v, quero so o item 3 (valido)", sim)
	}
}

// TestSimilaresOrdenaPorSobreposicaoDeGenero: between two items with the SAME vote_count
// above the threshold, the one with more genres in common with the ORIGIN item
// (d.Genres) appears first -- even with a lower score (vote_average).
func TestSimilaresOrdenaPorSobreposicaoDeGenero(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	raw := json.RawMessage(`{
		"id": 603, "title": "The Matrix",
		"genres": [{"id": 28, "name": "Action"}, {"id": 878, "name": "Sci-Fi"}],
		"recommendations": {"results": [
			{"id": 10, "title": "UmGenero", "genre_ids": [28], "vote_average": 9, "vote_count": 100},
			{"id": 20, "title": "DoisGeneros", "genre_ids": [28, 878], "vote_average": 5, "vote_count": 100}
		]}
	}`)
	sim, err := c.Similares(raw)
	if err != nil {
		t.Fatalf("Similares: %v", err)
	}
	if len(sim) != 2 || sim[0].SourceID != "20" || sim[1].SourceID != "10" {
		t.Fatalf("Similares = %+v, quero [20,10] (mais sobreposicao de genero primeiro)", sim)
	}
}

// TestSimilaresDesempataPorNota: on an EQUAL genre overlap, the score (vote_average) breaks the tie -- highest first.
func TestSimilaresDesempataPorNota(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	raw := json.RawMessage(`{
		"id": 603, "title": "The Matrix", "genres": [{"id": 28, "name": "Action"}],
		"recommendations": {"results": [
			{"id": 30, "title": "NotaBaixa", "genre_ids": [28], "vote_average": 5, "vote_count": 100},
			{"id": 40, "title": "NotaAlta", "genre_ids": [28], "vote_average": 9, "vote_count": 100}
		]}
	}`)
	sim, err := c.Similares(raw)
	if err != nil {
		t.Fatalf("Similares: %v", err)
	}
	if len(sim) != 2 || sim[0].SourceID != "40" || sim[1].SourceID != "30" {
		t.Fatalf("Similares = %+v, quero [40,30] (nota maior primeiro em empate de genero)", sim)
	}
}

// TestSimilaresEnvelopeEPayloadLegado proves that Similares accepts both the LEGACY
// payload (raw detail without envelope) and the AUGMENTED envelope {detail,tags_by_lang}
// -- via detalheBrutoDoRaw, the same detection that MapearDetalhe/TvdbIDDoDetalheSerie
// already use (no duplicated parsing).
func TestSimilaresEnvelopeEPayloadLegado(t *testing.T) {
	const detalhe = `{"id": 603, "title": "The Matrix", "genres": [{"id": 28, "name": "Action"}], "recommendations": {"results": [{"id": 50, "title": "Recomendado", "genre_ids": [28], "vote_average": 7, "vote_count": 100}]}}`
	c := New(Credencial{Token: "tok"}, nil, "")

	simLegado, err := c.Similares(json.RawMessage(detalhe))
	if err != nil {
		t.Fatalf("Similares (legado): %v", err)
	}
	if len(simLegado) != 1 || simLegado[0].SourceID != "50" {
		t.Fatalf("Similares (legado) = %+v, quero 1 item (50)", simLegado)
	}

	envelope := json.RawMessage(`{"detail": ` + detalhe + `, "tags_by_lang": {}}`)
	simEnvelope, err := c.Similares(envelope)
	if err != nil {
		t.Fatalf("Similares (envelope): %v", err)
	}
	if len(simEnvelope) != 1 || simEnvelope[0].SourceID != "50" {
		t.Fatalf("Similares (envelope) = %+v, quero 1 item (50)", simEnvelope)
	}
}
