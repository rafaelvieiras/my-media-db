package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
)

// colecaoSrvJSON is the /collection/2344 payload used in the handler tests.
const colecaoSrvJSON = `{
  "id": 2344,
  "name": "Colecao Matrix",
  "overview": "A saga.",
  "poster_path": "/col.jpg",
  "backdrop_path": "/colbg.jpg",
  "parts": [
    {"id": 603, "media_type": "movie", "title": "The Matrix", "poster_path": "/m1.jpg", "release_date": "1999-03-31"},
    {"id": 604, "media_type": "movie", "title": "The Matrix Reloaded", "poster_path": "/m2.jpg", "release_date": "2003-05-15"}
  ]
}`

// servidorTMDBColecaoSrv starts an httptest that serves /collection/2344 (rich) and 404
// on /collection/000.
func servidorTMDBColecaoSrv(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/collection/2344":
			_, _ = w.Write([]byte(colecaoSrvJSON))
		case "/collection/000":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupColecaoServer starts a Server with the TMDB provider pointed at the collection
// httptest.
func setupColecaoServer(t *testing.T, opts ...Option) (http.Handler, *db.DB) {
	t.Helper()
	srv := servidorTMDBColecaoSrv(t)
	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	return setupSearchServer(t, append([]Option{WithSearchProvider(prov)}, opts...)...)
}

func decodeColecao(t *testing.T, body []byte) colecaoExternaDTO {
	t.Helper()
	var out colecaoExternaDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar colecaoExternaDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// obterColecao performs the GET (expecting 200) and returns the decoded DTO.
func obterColecao(t *testing.T, h http.Handler) colecaoExternaDTO {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/collection/2344?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeColecao(t, rec.Body.Bytes())
}

func TestExternalCollectionCabecalho(t *testing.T) {
	d := obterColecao(t, mustFirst(setupColecaoServer(t)))

	eq(t, "source", d.Source, "tmdb")
	eq(t, "source_id", d.SourceID, "2344")
	eq(t, "name", d.Name, "Colecao Matrix")
	// Without assetsDir: the hero artwork comes STRAIGHT from the source (proxy
	// degradation).
	eqPtr(t, "backdrop_url", d.BackdropURL, "https://image.tmdb.org/t/p/w780/colbg.jpg")
}

func TestExternalCollectionPartes(t *testing.T) {
	d := obterColecao(t, mustFirst(setupColecaoServer(t)))

	if len(d.Parts) != 2 {
		t.Fatalf("parts = %d, quero 2", len(d.Parts))
	}
	p := d.Parts[0]
	eq(t, "parte[0].source_id", p.SourceID, "603")
	eq(t, "parte[0].type", p.Type, "movie")
	eq(t, "parte[0].title", p.Title, "The Matrix")
	eqPtr(t, "parte[0].year", p.Year, "1999")
	eqPtr(t, "parte[0].poster_url", p.PosterURL, "https://image.tmdb.org/t/p/w342/m1.jpg")
}

// mustFirst discards the *db.DB of the setup when the test only needs the handler.
func mustFirst(h http.Handler, _ *db.DB) http.Handler { return h }

// TestExternalCollectionProxyReescreveHero: with assetsDir, the hero poster/backdrop
// become the local proxy route; the part thumbnails stay direct.
func TestExternalCollectionProxyReescreveHero(t *testing.T) {
	h, _ := setupColecaoServer(t, WithAssetsDir(t.TempDir()))
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/collection/2344?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	d := decodeColecao(t, rec.Body.Bytes())
	if d.BackdropURL == nil || !strings.HasPrefix(*d.BackdropURL, "/api/v1/images?src=") {
		t.Errorf("backdrop_url = %v, quero rota de proxy", d.BackdropURL)
	}
	if d.PosterURL == nil || !strings.HasPrefix(*d.PosterURL, "/api/v1/images?src=") {
		t.Errorf("poster_url = %v, quero rota de proxy", d.PosterURL)
	}
	if len(d.Parts) == 0 || d.Parts[0].PosterURL == nil ||
		!strings.HasPrefix(*d.Parts[0].PosterURL, "https://image.tmdb.org/") {
		t.Errorf("miniatura da parte deveria seguir direta: %v", d.Parts[0].PosterURL)
	}
}

func TestExternalCollection404(t *testing.T) {
	h, _ := setupColecaoServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/collection/000?lang=pt-BR", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

func TestExternalCollection503SemProvider(t *testing.T) {
	h, _ := setupSearchServer(t) // no provider => no collection capability
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/collection/2344?lang=pt-BR", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}
