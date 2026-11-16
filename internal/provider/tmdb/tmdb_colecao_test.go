package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// colecaoJSON is a /collection/2344 payload with two parts (one without a title,
// which must be discarded) to exercise the mapping and the discard.
const colecaoJSON = `{
  "id": 2344,
  "name": "Colecao Matrix",
  "overview": "A saga.",
  "poster_path": "/col.jpg",
  "backdrop_path": "/colbg.jpg",
  "parts": [
    {"id": 603, "media_type": "movie", "title": "The Matrix", "overview": "Neo.", "poster_path": "/m1.jpg", "release_date": "1999-03-31"},
    {"id": 604, "media_type": "movie", "title": "The Matrix Reloaded", "poster_path": "/m2.jpg", "release_date": "2003-05-15"},
    {"id": 999, "media_type": "movie", "title": "", "poster_path": "/x.jpg"}
  ]
}`

// servidorColecao answers /collection/2344 (rich), 404 on /collection/000 and captures
// the requested language when `capturar` != nil.
func servidorColecao(t *testing.T, capturar *http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/collection/2344":
			if capturar != nil {
				*capturar = *r
			}
			_, _ = w.Write([]byte(colecaoJSON))
		case "/collection/000":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// buscarColecaoTeste searches /collection/2344 in the pt-BR language and returns the
// canonical, capturing the request (to check the language). It slims down the two
// mapping tests.
func buscarColecaoTeste(t *testing.T, req *http.Request) provider.Colecao {
	t.Helper()
	srv := servidorColecao(t, req)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)
	col, err := c.BuscarColecao(context.Background(), "2344", "pt-BR")
	if err != nil {
		t.Fatalf("BuscarColecao: %v", err)
	}
	return col
}

func TestBuscarColecaoCabecalho(t *testing.T) {
	var req http.Request
	col := buscarColecaoTeste(t, &req)

	if q := req.URL.Query().Get("language"); q != "pt-BR" {
		t.Errorf("language = %q, quero pt-BR", q)
	}
	if col.Source != "tmdb" || col.SourceID != "2344" {
		t.Errorf("source/id = %q/%q, quero tmdb/2344", col.Source, col.SourceID)
	}
	if col.Name != "Colecao Matrix" {
		t.Errorf("name = %q, quero Colecao Matrix", col.Name)
	}
	eqSPtr(t, "overview", col.Overview, "A saga.")
	eqSPtr(t, "poster_url", col.PosterURL, imagemBaseURL+"/col.jpg")
	eqSPtr(t, "backdrop_url", col.BackdropURL, backdropBaseURL+"/colbg.jpg")
}

func TestBuscarColecaoPartes(t *testing.T) {
	col := buscarColecaoTeste(t, nil)

	if len(col.Parts) != 2 { // the part without a title is discarded
		t.Fatalf("parts = %d, quero 2", len(col.Parts))
	}
	p0 := col.Parts[0]
	if p0.SourceID != "603" || p0.Type != "movie" || p0.Title != "The Matrix" {
		t.Errorf("parte[0] inesperada: %+v", p0)
	}
	eqSPtr(t, "parte[0].year", p0.Year, "1999")
	eqSPtr(t, "parte[0].poster_url", p0.PosterURL, imagemBaseURL+"/m1.jpg")
}

func TestBuscarColecaoNaoEncontrada(t *testing.T) {
	srv := servidorColecao(t, nil)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	_, err := c.BuscarColecao(context.Background(), "000", "pt-BR")
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

func TestBuscarColecaoIDVazio(t *testing.T) {
	c := New(Credencial{Token: "tok"}, nil, "http://exemplo.invalid")
	if _, err := c.BuscarColecao(context.Background(), "", "pt-BR"); err == nil {
		t.Fatal("quero erro para colecaoID vazio")
	}
}

// TestClientCumpreColecaoProvider guarantees at compile time that *Client satisfies
// the optional capability provider.ColecaoProvider (the handler type-asserts it).
func TestClientCumpreColecaoProvider(t *testing.T) {
	var _ provider.ColecaoProvider = (*Client)(nil)
}
