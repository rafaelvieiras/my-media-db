package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// respostaFixa is a /search/multi payload with a movie, a series and a person (the last
// one must be discarded by the media_type filter).
const respostaFixa = `{
  "results": [
    {
      "id": 603,
      "media_type": "movie",
      "title": "The Matrix",
      "overview": "Neo descobre a verdade.",
      "poster_path": "/matrix.jpg",
      "release_date": "1999-03-31"
    },
    {
      "id": 1396,
      "media_type": "tv",
      "name": "Breaking Bad",
      "overview": "Um professor vira quimico do crime.",
      "poster_path": "/bb.jpg",
      "first_air_date": "2008-01-20"
    },
    {
      "id": 999,
      "media_type": "person",
      "name": "Keanu Reeves"
    }
  ]
}`

// servidorFixo starts an httptest.Server that answers /search/multi with the fixed
// payload and captures the last request received (for auth asserts).
func servidorFixo(t *testing.T, capturar *http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capturar != nil {
			*capturar = *r
		}
		if r.URL.Path != "/search/multi" {
			http.Error(w, "path inesperado", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respostaFixa))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// eqS compares a string; eqSPtr compares a *string with an expected value (nil fails).
// They keep the tests linear (low cyclomatic complexity).
func eqS(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, quero %q", name, got, want)
	}
}

func eqSPtr(t *testing.T, name string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, quero %q", name, want)
		return
	}
	eqS(t, name, *got, want)
}

func TestBuscarMapeiaEFiltra(t *testing.T) {
	srv := servidorFixo(t, nil)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	res, err := c.Buscar(context.Background(), "matrix", "", "")
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	// person discarded -> 2 results (movie + tv).
	if len(res) != 2 {
		t.Fatalf("len(res) = %d, quero 2 (person descartado)", len(res))
	}

	filme := res[0]
	eqS(t, "filme.source", filme.Source, "tmdb")
	eqS(t, "filme.source_id", filme.SourceID, "603")
	eqS(t, "filme.type", filme.Type, "movie")
	eqS(t, "filme.title", filme.Title, "The Matrix")
	eqSPtr(t, "filme.year", filme.Year, "1999")
	eqSPtr(t, "filme.poster_url", filme.PosterURL, "https://image.tmdb.org/t/p/w342/matrix.jpg")
	eqSPtr(t, "filme.overview", filme.Overview, "Neo descobre a verdade.")
	if filme.Saved {
		t.Error("saved = true, quero false na busca")
	}

	serie := res[1]
	eqS(t, "serie.source_id", serie.SourceID, "1396")
	eqS(t, "serie.type", serie.Type, "tv")
	eqS(t, "serie.title", serie.Title, "Breaking Bad")
	eqSPtr(t, "serie.year", serie.Year, "2008")
}

func TestBuscarFiltraPorTipo(t *testing.T) {
	srv := servidorFixo(t, nil)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	res, err := c.Buscar(context.Background(), "matrix", "movie", "")
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if len(res) != 1 || res[0].Type != "movie" {
		t.Fatalf("tipo=movie: quero 1 filme, veio %+v", res)
	}
}

// TestBuscarEnviaLanguage covers the language in the search (regression): with lang, the
// request to /search/multi carries language=<lang>; without lang, the parameter is not
// sent (the source falls back to the default). Before, the search ignored the active
// language of the UI.
func TestBuscarEnviaLanguage(t *testing.T) {
	var comLang, semLang http.Request
	srv1 := servidorFixo(t, &comLang)
	if _, err := New(Credencial{Token: "t"}, srv1.Client(), srv1.URL).
		Buscar(context.Background(), "x", "", "pt-BR"); err != nil {
		t.Fatalf("Buscar(pt-BR): %v", err)
	}
	if got := comLang.URL.Query().Get("language"); got != "pt-BR" {
		t.Errorf("language = %q, quero pt-BR", got)
	}

	srv2 := servidorFixo(t, &semLang)
	if _, err := New(Credencial{Token: "t"}, srv2.Client(), srv2.URL).
		Buscar(context.Background(), "x", "", ""); err != nil {
		t.Fatalf("Buscar(sem lang): %v", err)
	}
	if _, tem := semLang.URL.Query()["language"]; tem {
		t.Error("language presente sem lang pedido, quero ausente")
	}
}

func TestAuthTokenUsaBearer(t *testing.T) {
	var capturada http.Request
	srv := servidorFixo(t, &capturada)
	c := New(Credencial{Token: "tok-v4"}, srv.Client(), srv.URL)

	if _, err := c.Buscar(context.Background(), "x", "", ""); err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if got := capturada.Header.Get("Authorization"); got != "Bearer tok-v4" {
		t.Errorf("Authorization = %q, quero 'Bearer tok-v4'", got)
	}
	if ak := capturada.URL.Query().Get("api_key"); ak != "" {
		t.Errorf("api_key = %q, quero vazio quando ha token", ak)
	}
}

func TestAuthApiKeyUsaQuery(t *testing.T) {
	var capturada http.Request
	srv := servidorFixo(t, &capturada)
	c := New(Credencial{APIKey: "key-v3"}, srv.Client(), srv.URL)

	if _, err := c.Buscar(context.Background(), "x", "", ""); err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if got := capturada.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, quero vazio quando so ha api_key", got)
	}
	if ak := capturada.URL.Query().Get("api_key"); ak != "key-v3" {
		t.Errorf("api_key = %q, quero 'key-v3'", ak)
	}
}

func TestBuscarStatusErroPropaga(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	if _, err := c.Buscar(context.Background(), "x", "", ""); err == nil {
		t.Fatal("quero erro em status != 200")
	}
}

// detalheFilme and detalheSerie are raw payloads of /movie/{id} and /tv/{id} with
// append_to_response=translations. The movie uses title/release_date + data.title in the
// translations; the series uses name/first_air_date + data.name. original_language is the
// anchor of the fallback. genres brings ids (the localized names come from the per-language
// lists). Translations: en (iso_3166_1=US) and pt (iso_3166_1=BR).
const detalheFilme = `{
  "id": 603,
  "original_language": "en",
  "title": "The Matrix",
  "original_title": "The Matrix",
  "overview": "Neo learns the truth.",
  "release_date": "1999-03-31",
  "poster_path": "/matrix.jpg",
  "genres": [{"id": 28, "name": "Action"}, {"id": 878, "name": "Science Fiction"}],
  "translations": {
    "id": 603,
    "translations": [
      {"iso_3166_1": "US", "iso_639_1": "en", "data": {"title": "The Matrix", "overview": "Neo learns the truth."}},
      {"iso_3166_1": "BR", "iso_639_1": "pt", "data": {"title": "Matrix", "overview": "Neo descobre a verdade."}}
    ]
  }
}`

const detalheSerie = `{
  "id": 1396,
  "original_language": "en",
  "name": "Breaking Bad",
  "original_name": "Breaking Bad",
  "overview": "A teacher turns to crime.",
  "first_air_date": "2008-01-20",
  "poster_path": "/bb.jpg",
  "genres": [{"id": 18, "name": "Drama"}],
  "translations": {
    "id": 1396,
    "translations": [
      {"iso_3166_1": "US", "iso_639_1": "en", "data": {"name": "Breaking Bad", "overview": "A teacher turns to crime."}},
      {"iso_3166_1": "BR", "iso_639_1": "pt", "data": {"name": "Breaking Bad", "overview": "Um professor vira quimico do crime."}}
    ]
  }
}`

// generosFilme and generosSerie are the localized genre lists (GET
// /genre/{movie|tv}/list?language=), per language. The ids match the ones of the detail.
var generosFilme = map[string]string{
	"pt-BR": `{"genres":[{"id":28,"name":"Acao"},{"id":878,"name":"Ficcao cientifica"}]}`,
	"en":    `{"genres":[{"id":28,"name":"Action"},{"id":878,"name":"Science Fiction"}]}`,
}

var generosSerie = map[string]string{
	"pt-BR": `{"genres":[{"id":18,"name":"Drama"}]}`,
	"en":    `{"genres":[{"id":18,"name":"Drama"}]}`,
}

// servidorDetalhe starts an httptest.Server that answers /movie/{id}, /tv/{id} (with the
// translations) and /genre/{movie|tv}/list?language= (the localized lists). It counts the
// calls per path (for the genre cache test).
func servidorDetalhe(t *testing.T, chamadas map[string]int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if chamadas != nil {
			chamadas[r.URL.Path+"?language="+r.URL.Query().Get("language")]++
		}
		w.Header().Set("Content-Type", "application/json")
		lang := r.URL.Query().Get("language")
		switch r.URL.Path {
		case "/movie/603":
			_, _ = w.Write([]byte(detalheFilme))
		case "/tv/1396":
			_, _ = w.Write([]byte(detalheSerie))
		case "/genre/movie/list":
			_, _ = w.Write([]byte(generosFilme[lang]))
		case "/genre/tv/list":
			_, _ = w.Write([]byte(generosSerie[lang]))
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBuscarDetalheEnvelopeFilme(t *testing.T) {
	srv := servidorDetalhe(t, nil)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	raw, err := c.BuscarDetalhe(context.Background(), "603", "movie", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	// The augmented payload has detail + tags_by_lang (localized genres).
	var env envelopeTMDB
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("payload aumentado invalido: %v", err)
	}
	if len(env.Detail) == 0 {
		t.Fatal("envelope sem detail")
	}
	eqLista(t, "tags_by_lang[pt-BR]", env.TagsByLang["pt-BR"], []string{"Acao", "Ficcao cientifica"})
	eqLista(t, "tags_by_lang[en]", env.TagsByLang["en"], []string{"Action", "Science Fiction"})

	campos, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}
	// title/description per language.
	eqIdioma(t, campos, "title", "pt-BR", "Matrix")
	eqIdioma(t, campos, "title", "en", "The Matrix")
	eqIdioma(t, campos, "description", "pt-BR", "Neo descobre a verdade.")
	eqIdioma(t, campos, "description", "en", "Neo learns the truth.")
	// tags per language.
	eqIdiomaLista(t, campos, "tags", "pt-BR", []string{"Acao", "Ficcao cientifica"})
	eqIdiomaLista(t, campos, "tags", "en", []string{"Action", "Science Fiction"})
	// Neutral scalars + anchor.
	eqCampo(t, campos, "original_title", "The Matrix")
	eqCampo(t, campos, "release_date", "1999-03-31")
	eqCampo(t, campos, "poster_url", "https://image.tmdb.org/t/p/w342/matrix.jpg")
	eqCampo(t, campos, "original_language", "en")
}

func TestBuscarDetalheEnvelopeSerie(t *testing.T) {
	srv := servidorDetalhe(t, nil)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	raw, err := c.BuscarDetalhe(context.Background(), "1396", "tv", []string{"pt-BR", "en"})
	if err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	campos, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}
	// Series: data.name -> title; first_air_date -> release_date.
	eqIdioma(t, campos, "title", "en", "Breaking Bad")
	eqIdioma(t, campos, "description", "pt-BR", "Um professor vira quimico do crime.")
	eqIdiomaLista(t, campos, "tags", "pt-BR", []string{"Drama"})
	eqCampo(t, campos, "release_date", "2008-01-20")
	eqCampo(t, campos, "poster_url", "https://image.tmdb.org/t/p/w342/bb.jpg")
	eqCampo(t, campos, "original_language", "en")
}

// TestMapearDetalheEnvelopeSerieComTvdbIdNumerico reproduces the real production bug:
// TMDB sometimes returns external_ids.tvdb_id as a JSON NUMBER (not a string). Before the
// fix, that brought down the Unmarshal of the WHOLE detail -- not only the tvdb_id, but
// title/description/tags vanished as well. Here we confirm that the complete mapping keeps
// working with a numeric tvdb_id, with the same base payload already used by
// TestBuscarDetalheEnvelopeSerie (detalheSerie).
func TestMapearDetalheEnvelopeSerieComTvdbIdNumerico(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	detalheComTvdbNumerico := strings.Replace(
		detalheSerie, `"poster_path": "/bb.jpg",`,
		`"poster_path": "/bb.jpg", "external_ids": {"imdb_id": "tt0903747", "tvdb_id": 81189},`,
		1,
	)
	raw := json.RawMessage(`{"detail": ` + detalheComTvdbNumerico + `, "tags_by_lang": {"pt-BR": ["Drama"]}}`)

	campos, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe com tvdb_id numerico: %v", err)
	}
	eqIdioma(t, campos, "title", "en", "Breaking Bad")
	eqIdioma(t, campos, "description", "pt-BR", "Um professor vira quimico do crime.")
	eqCampo(t, campos, "release_date", "2008-01-20")
	links, _ := campos["external_links"].(map[string]any)
	if links["imdb"] != "https://www.imdb.com/title/tt0903747" {
		t.Errorf("external_links[imdb] = %v, quero o link do imdb_id", links["imdb"])
	}
}

// TestListaGenerosCache proves that the genre lists per language are cached: two
// BuscarDetalhe calls (same type) hit /genre/movie/list ONCE per language, not twice.
func TestListaGenerosCache(t *testing.T) {
	chamadas := map[string]int{}
	srv := servidorDetalhe(t, chamadas)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	for i := 0; i < 2; i++ {
		if _, err := c.BuscarDetalhe(context.Background(), "603", "movie", []string{"pt-BR", "en"}); err != nil {
			t.Fatalf("BuscarDetalhe #%d: %v", i, err)
		}
	}
	if n := chamadas["/genre/movie/list?language=pt-BR"]; n != 1 {
		t.Errorf("/genre/movie/list?language=pt-BR chamado %d vezes, quero 1 (cache)", n)
	}
	if n := chamadas["/genre/movie/list?language=en"]; n != 1 {
		t.Errorf("/genre/movie/list?language=en chamado %d vezes, quero 1 (cache)", n)
	}
	// The detail is NOT cached (2 calls); only the genre lists are.
	if n := chamadas["/movie/603?language=pt-BR"]; n != 2 {
		t.Errorf("/movie/603 chamado %d vezes, quero 2 (detalhe nao e cacheado)", n)
	}
}

// TestBuscarTitulosAlternativos covers the provider.TituloAlternativoProvider capability:
// it extracts title (movie) / name (series) from translations.translations[].data.
func TestBuscarTitulosAlternativos(t *testing.T) {
	srv := servidorDetalhe(t, nil)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	titulosFilme, err := c.BuscarTitulosAlternativos(context.Background(), "movie", "603")
	if err != nil {
		t.Fatalf("BuscarTitulosAlternativos(movie): %v", err)
	}
	eqLista(t, "titulos alternativos (filme)", titulosFilme, []string{"The Matrix", "Matrix"})

	titulosSerie, err := c.BuscarTitulosAlternativos(context.Background(), "tv", "1396")
	if err != nil {
		t.Fatalf("BuscarTitulosAlternativos(tv): %v", err)
	}
	if len(titulosSerie) == 0 {
		t.Error("titulos alternativos (serie) vazio, quero ao menos um item das translations")
	}
}

// TestBuscarTitulosAlternativosUsaAppendMaisLeve proves that the call does NOT request
// the heavy appends of BuscarDetalhe (credits/keywords/similar/etc) -- only
// translations.
func TestBuscarTitulosAlternativosUsaAppendMaisLeve(t *testing.T) {
	var appendRecebido string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appendRecebido = r.URL.Query().Get("append_to_response")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(detalheFilme))
	}))
	t.Cleanup(srv.Close)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	if _, err := c.BuscarTitulosAlternativos(context.Background(), "movie", "603"); err != nil {
		t.Fatalf("BuscarTitulosAlternativos: %v", err)
	}
	if appendRecebido != "translations" {
		t.Errorf("append_to_response = %q, quero \"translations\" (chamada mais leve que BuscarDetalhe)", appendRecebido)
	}
}

func TestBuscarTitulosAlternativosTipoInvalido(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "http://exemplo")
	if _, err := c.BuscarTitulosAlternativos(context.Background(), "person", "1"); err == nil {
		t.Fatal("quero erro para tipo != movie|tv")
	}
	if _, err := c.BuscarTitulosAlternativos(context.Background(), "movie", ""); err == nil {
		t.Fatal("quero erro para sourceID vazio")
	}
}

func TestBuscarDetalheTipoInvalido(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "http://exemplo")
	if _, err := c.BuscarDetalhe(context.Background(), "1", "person", nil); err == nil {
		t.Fatal("quero erro para tipo != movie|tv")
	}
	if _, err := c.BuscarDetalhe(context.Background(), "", "movie", nil); err == nil {
		t.Fatal("quero erro para sourceID vazio")
	}
}

// TestMapearDetalheLegado proves COMPATIBILITY: an OLD payload (raw detail, without
// envelope) maps to SCALARS; the resolver treats them as belonging to the main language.
func TestMapearDetalheLegado(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	legado := []byte(`{
      "title": "The Matrix",
      "original_title": "The Matrix",
      "overview": "Neo descobre a verdade.",
      "release_date": "1999-03-31",
      "poster_path": "/matrix.jpg",
      "genres": [{"id": 28, "name": "Acao"}, {"id": 878, "name": "Ficcao"}]
    }`)
	campos, err := c.MapearDetalhe(legado)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}
	// Scalars (not map[language]value).
	eqCampo(t, campos, "title", "The Matrix")
	eqCampo(t, campos, "description", "Neo descobre a verdade.")
	eqCampo(t, campos, "release_date", "1999-03-31")
	eqCampo(t, campos, "poster_url", "https://image.tmdb.org/t/p/w342/matrix.jpg")
	tags, _ := campos["tags"].([]string)
	if len(tags) != 2 || tags[0] != "Acao" || tags[1] != "Ficcao" {
		t.Errorf("tags = %v, quero [Acao Ficcao] (escalar legado)", campos["tags"])
	}
}

// MapearDetalhe only includes keys that the source brought: a minimal payload (legacy)
// must not produce poster_url/tags.
func TestMapearDetalheOmiteAusentes(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "")
	campos, err := c.MapearDetalhe([]byte(`{"title":"So Titulo"}`))
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}
	eqCampo(t, campos, "title", "So Titulo")
	if _, ok := campos["poster_url"]; ok {
		t.Error("poster_url nao deveria existir sem poster_path")
	}
	if _, ok := campos["tags"]; ok {
		t.Error("tags nao deveria existir sem genres")
	}
}

// eqCampo checks a scalar string field of the canonical map.
func eqCampo(t *testing.T, m map[string]any, chave, want string) {
	t.Helper()
	got, ok := m[chave].(string)
	if !ok {
		t.Errorf("%s ausente ou nao-string (m=%v)", chave, m)
		return
	}
	if got != want {
		t.Errorf("%s = %q, quero %q", chave, got, want)
	}
}

// eqIdioma checks m[key][language] == want (translatable scalar field).
func eqIdioma(t *testing.T, m map[string]any, chave, idioma, want string) {
	t.Helper()
	porIdioma, ok := m[chave].(map[string]any)
	if !ok {
		t.Errorf("%s nao e map[idioma]valor (m=%v)", chave, m[chave])
		return
	}
	got, ok := porIdioma[idioma].(string)
	if !ok {
		t.Errorf("%s[%s] ausente ou nao-string (=%v)", chave, idioma, porIdioma)
		return
	}
	if got != want {
		t.Errorf("%s[%s] = %q, quero %q", chave, idioma, got, want)
	}
}

// eqIdiomaLista checks m[key][language] == want (translatable list field).
func eqIdiomaLista(t *testing.T, m map[string]any, chave, idioma string, want []string) {
	t.Helper()
	porIdioma, ok := m[chave].(map[string]any)
	if !ok {
		t.Errorf("%s nao e map[idioma]lista (m=%v)", chave, m[chave])
		return
	}
	got, _ := porIdioma[idioma].([]string)
	eqLista(t, chave+"["+idioma+"]", got, want)
}

// eqLista compares two string lists in order.
func eqLista(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, quero %v", name, got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s = %v, quero %v", name, got, want)
			return
		}
	}
}

func TestConfigurada(t *testing.T) {
	if (Credencial{}).Configurada() {
		t.Error("credencial vazia nao deveria estar configurada")
	}
	if !(Credencial{Token: "t"}).Configurada() {
		t.Error("token deveria configurar")
	}
	if !(Credencial{APIKey: "k"}).Configurada() {
		t.Error("api_key deveria configurar")
	}
}
