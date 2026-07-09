package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// respostaFixa é um payload de /search/multi com um filme, uma serie e uma
// pessoa (esta ultima deve ser descartada pelo filtro de media_type).
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

// servidorFixo sobe um httptest.Server que responde /search/multi com o payload
// fixo e captura a ultima requisicao recebida (para asserts de auth).
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

// eqS compara uma string; eqSPtr compara um *string com um valor esperado (nil
// falha). Mantêm os testes lineares (baixa complexidade ciclomática).
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

	res, err := c.Buscar(context.Background(), "matrix", "")
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	// person descartado -> 2 resultados (movie + tv).
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

	res, err := c.Buscar(context.Background(), "matrix", "movie")
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if len(res) != 1 || res[0].Type != "movie" {
		t.Fatalf("tipo=movie: quero 1 filme, veio %+v", res)
	}
}

func TestAuthTokenUsaBearer(t *testing.T) {
	var capturada http.Request
	srv := servidorFixo(t, &capturada)
	c := New(Credencial{Token: "tok-v4"}, srv.Client(), srv.URL)

	if _, err := c.Buscar(context.Background(), "x", ""); err != nil {
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

	if _, err := c.Buscar(context.Background(), "x", ""); err != nil {
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

	if _, err := c.Buscar(context.Background(), "x", ""); err == nil {
		t.Fatal("quero erro em status != 200")
	}
}

// detalheFilme e detalheSerie sao payloads crus de /movie/{id} e /tv/{id} com
// append_to_response=translations (0020). Filme usa title/release_date + data.title
// nas traducoes; serie usa name/first_air_date + data.name. original_language e a
// ancora do fallback. genres traz ids (os nomes localizados vem das listas por
// idioma). Traducoes: en (iso_3166_1=US) e pt (iso_3166_1=BR).
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

// generosFilme e generosSerie sao as listas de genero localizadas (GET
// /genre/{movie|tv}/list?language=), por idioma. Os ids casam os do detalhe.
var generosFilme = map[string]string{
	"pt-BR": `{"genres":[{"id":28,"name":"Acao"},{"id":878,"name":"Ficcao cientifica"}]}`,
	"en":    `{"genres":[{"id":28,"name":"Action"},{"id":878,"name":"Science Fiction"}]}`,
}

var generosSerie = map[string]string{
	"pt-BR": `{"genres":[{"id":18,"name":"Drama"}]}`,
	"en":    `{"genres":[{"id":18,"name":"Drama"}]}`,
}

// servidorDetalhe sobe um httptest.Server que responde /movie/{id}, /tv/{id} (com
// as traducoes) e /genre/{movie|tv}/list?language= (as listas localizadas). Conta
// as chamadas por path (para o teste de cache de generos).
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
	// O payload aumentado tem detail + tags_by_lang (generos localizados).
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
	// title/description por idioma.
	eqIdioma(t, campos, "title", "pt-BR", "Matrix")
	eqIdioma(t, campos, "title", "en", "The Matrix")
	eqIdioma(t, campos, "description", "pt-BR", "Neo descobre a verdade.")
	eqIdioma(t, campos, "description", "en", "Neo learns the truth.")
	// tags por idioma.
	eqIdiomaLista(t, campos, "tags", "pt-BR", []string{"Acao", "Ficcao cientifica"})
	eqIdiomaLista(t, campos, "tags", "en", []string{"Action", "Science Fiction"})
	// Neutros escalares + ancora.
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
	// Serie: data.name -> title; first_air_date -> release_date.
	eqIdioma(t, campos, "title", "en", "Breaking Bad")
	eqIdioma(t, campos, "description", "pt-BR", "Um professor vira quimico do crime.")
	eqIdiomaLista(t, campos, "tags", "pt-BR", []string{"Drama"})
	eqCampo(t, campos, "release_date", "2008-01-20")
	eqCampo(t, campos, "poster_url", "https://image.tmdb.org/t/p/w342/bb.jpg")
	eqCampo(t, campos, "original_language", "en")
}

// TestListaGenerosCache prova que as listas de genero por idioma sao cacheadas: dois
// BuscarDetalhe (mesmo tipo) batem UMA vez por idioma em /genre/movie/list, nao duas.
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
	// O detalhe NAO e cacheado (2 chamadas); so as listas de genero sao.
	if n := chamadas["/movie/603?language=pt-BR"]; n != 2 {
		t.Errorf("/movie/603 chamado %d vezes, quero 2 (detalhe nao e cacheado)", n)
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

// TestMapearDetalheLegado prova a COMPATIBILIDADE: um payload ANTIGO (detalhe cru do
// 0015, sem envelope) mapeia para ESCALARES; o resolver os trata como do principal.
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
	// Escalares (nao map[idioma]valor).
	eqCampo(t, campos, "title", "The Matrix")
	eqCampo(t, campos, "description", "Neo descobre a verdade.")
	eqCampo(t, campos, "release_date", "1999-03-31")
	eqCampo(t, campos, "poster_url", "https://image.tmdb.org/t/p/w342/matrix.jpg")
	tags, _ := campos["tags"].([]string)
	if len(tags) != 2 || tags[0] != "Acao" || tags[1] != "Ficcao" {
		t.Errorf("tags = %v, quero [Acao Ficcao] (escalar legado)", campos["tags"])
	}
}

// MapearDetalhe so inclui chaves que a fonte trouxe: um payload minimo (legado) nao
// deve gerar poster_url/tags.
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

// eqCampo confere um campo string escalar do mapa canonico.
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

// eqIdioma confere m[chave][idioma] == want (campo traduzivel escalar).
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

// eqIdiomaLista confere m[chave][idioma] == want (campo-lista traduzivel).
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

// eqLista compara duas listas de string em ordem.
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
