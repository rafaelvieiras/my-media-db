package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider/tmdb"
)

// filmeRicoJSON e um payload de /movie/603 com os blocos do detalhe rico (0022),
// traduzido em en (US) e pt (BR), com classificacao BR=14 e US=R.
const filmeRicoJSON = `{
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
  "translations": {"id": 603, "translations": [
    {"iso_3166_1": "US", "iso_639_1": "en", "data": {"title": "The Matrix", "overview": "Neo learns the truth.", "tagline": "Welcome to the Real World."}},
    {"iso_3166_1": "BR", "iso_639_1": "pt", "data": {"title": "Matrix", "overview": "Neo descobre a verdade.", "tagline": "Bem-vindo ao mundo real."}}
  ]},
  "videos": {"results": [{"name": "Trailer", "key": "abc123", "site": "YouTube", "type": "Trailer", "official": true}]},
  "keywords": {"keywords": [{"id": 1, "name": "dystopia"}]},
  "release_dates": {"results": [
    {"iso_3166_1": "BR", "release_dates": [{"certification": "14"}]},
    {"iso_3166_1": "US", "release_dates": [{"certification": "R"}]}
  ]},
  "external_ids": {"imdb_id": "tt0133093"},
  "similar": {"results": [{"id": 604, "title": "The Matrix Reloaded", "release_date": "2003-05-15", "poster_path": "/rel.jpg"}]}
}`

var generosFilmeSrv = map[string]string{
	"pt-BR": `{"genres":[{"id":28,"name":"Acao"}]}`,
	"en":    `{"genres":[{"id":28,"name":"Action"}]}`,
}

// servidorTMDBRico sobe um httptest que serve /movie/603 (rico), /genre/movie/list e
// simula 404 em /movie/000 e 500 em /movie/500. Conta as idas a /movie/603 (cache).
func servidorTMDBRico(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/603":
			if hits != nil {
				atomic.AddInt32(hits, 1)
			}
			_, _ = w.Write([]byte(filmeRicoJSON))
		case "/movie/000":
			http.Error(w, "not found", http.StatusNotFound)
		case "/movie/500":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/genre/movie/list":
			_, _ = w.Write([]byte(generosFilmeSrv[r.URL.Query().Get("language")]))
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupExternalServer sobe um Server com banco migrado e o provider TMDB real
// apontado ao httptest rico.
func setupExternalServer(t *testing.T, hits *int32) (http.Handler, *db.DB) {
	t.Helper()
	srv := servidorTMDBRico(t, hits)
	prov := tmdb.New(tmdb.Credencial{Token: "tok"}, srv.Client(), srv.URL)
	return setupSearchServer(t, WithSearchProvider(prov))
}

// decodeDetalheExterno decodifica a resposta do detalhe externo.
func decodeDetalheExterno(t *testing.T, body []byte) detalheExternoDTO {
	t.Helper()
	var out detalheExternoDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar detalheExternoDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// obterDetalheExterno faz o GET (esperando 200) e devolve o DTO decodificado.
func obterDetalheExterno(t *testing.T, h http.Handler, alvo string) detalheExternoDTO {
	t.Helper()
	rec := do(t, h, http.MethodGet, alvo, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeDetalheExterno(t, rec.Body.Bytes())
}

// TestExternalDetailCamposPtBR cobre os campos ESCALARES resolvidos em pt-BR
// (traduziveis + neutros de valor unico + proveniencia de idioma).
func TestExternalDetailCamposPtBR(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	d := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")

	eq(t, "title", d.Title, "Matrix")
	eq(t, "lang", d.Lang, "pt-BR")
	eqPtr(t, "original_title", d.OriginalTitle, "The Matrix")
	eqPtr(t, "tagline", d.Tagline, "Bem-vindo ao mundo real.")
	eqPtr(t, "description", d.Description, "Neo descobre a verdade.")
	eqPtr(t, "year", d.Year, "1999")
	eqPtr(t, "certification", d.Certification, "14") // regiao BR do pt-BR
	eqPtr(t, "poster_url", d.PosterURL, "https://image.tmdb.org/t/p/w342/matrix.jpg")
	eqPtr(t, "backdrop_url", d.BackdropURL, "https://image.tmdb.org/t/p/w780/bg.jpg")
	if d.Runtime == nil || *d.Runtime != 136 {
		t.Errorf("runtime = %v, quero 136", d.Runtime)
	}
	eq(t, "resolved_languages[title]", d.ResolvedLanguages["title"], "pt-BR")
}

// TestExternalDetailBlocosPtBR cobre os BLOCOS ricos (listas/objetos) em pt-BR:
// generos/keywords traduzidos, videos, colecao, similares e links.
func TestExternalDetailBlocosPtBR(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	d := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=pt-BR")

	eq(t, "genero", umItem(t, "genres", d.Genres), "Acao")
	eq(t, "keyword", umItem(t, "keywords", d.Keywords), "dystopia")
	if umItem(t, "videos", d.Videos).URL == nil {
		t.Error("video sem url")
	}
	if d.Collection == nil {
		t.Fatal("collection nil")
	}
	eq(t, "collection.source_id", d.Collection.SourceID, "2344")
	sim := umItem(t, "similar", d.Similar)
	eq(t, "similar.source_id", sim.SourceID, "604")
	eq(t, "similar.type", sim.Type, "movie")
	eq(t, "imdb link", d.ExternalLinks["imdb"], "https://www.imdb.com/title/tt0133093")
	eq(t, "tmdb link", d.ExternalLinks["tmdb"], "https://www.themoviedb.org/movie/603")
}

// umItem exige que a lista tenha exatamente um item e o devolve (enxuga os asserts
// de bloco de item unico, mantendo baixa a complexidade do teste).
func umItem[T any](t *testing.T, nome string, s []T) T {
	t.Helper()
	if len(s) != 1 {
		var zero T
		t.Fatalf("%s: len = %d, quero 1", nome, len(s))
		return zero
	}
	return s[0]
}

func TestExternalDetailResolveEn(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	d := obterDetalheExterno(t, h, "/api/v1/external/tmdb/movie/603?lang=en")
	eq(t, "title", d.Title, "The Matrix")
	eqPtr(t, "certification", d.Certification, "R") // en sem regiao -> fallback US
	if len(d.Genres) != 1 || d.Genres[0] != "Action" {
		t.Errorf("genres = %v, quero [Action]", d.Genres)
	}
}

// TestExternalDetailCacheNaoRebate: a 2a ida ao mesmo item nao bate na fonte.
func TestExternalDetailCacheNaoRebate(t *testing.T) {
	var hits int32
	h, _ := setupExternalServer(t, &hits)
	for i := 0; i < 2; i++ {
		rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET #%d status = %d", i, rec.Code)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("idas a /movie/603 = %d, quero 1 (cache)", got)
	}
}

// TestExternalDetailNaoCriaEntity: ver o externo NUNCA cataloga.
func TestExternalDetailNaoCriaEntity(t *testing.T) {
	h, d := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	todas, err := sqlcgen.New(d.Read).ListarEntities(context.Background(), sqlcgen.ListarEntitiesParams{Limit: 100, Offset: 0})
	if err != nil {
		t.Fatalf("ListarEntities: %v", err)
	}
	if len(todas) != 0 {
		t.Errorf("entities = %d, quero 0 (ver != catalogar)", len(todas))
	}
}

func TestExternalDetail404(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/000?lang=pt-BR", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

func TestExternalDetail502(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/500?lang=pt-BR", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "external_source_error")
}

func TestExternalDetailTypeInvalido(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/person/1?lang=pt-BR", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
}

func TestExternalDetailLangInvalido(t *testing.T) {
	h, _ := setupExternalServer(t, nil)
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=xx", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_query")
}

func TestExternalDetail503SemProvider(t *testing.T) {
	h, _ := setupSearchServer(t) // sem provider
	rec := do(t, h, http.MethodGet, "/api/v1/external/tmdb/movie/603?lang=pt-BR", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "external_source_unavailable")
}
