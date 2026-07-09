package tmdb

import (
	"context"
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
