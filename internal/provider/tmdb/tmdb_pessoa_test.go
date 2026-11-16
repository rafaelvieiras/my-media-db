package tmdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// combinedCreditsFixoJSON is a /person/6384?append_to_response=combined_credits
// payload with: a CAST movie credit (603, "The Matrix", with poster), a CAST tv credit
// WITHOUT poster (1396, "Breaking Bad"), and the work 604 ("The Matrix Reloaded") with
// TWO CREW credits (Writer + Director, to prove the joining of jobs) plus a CREW credit
// of the SAME work 603 (to prove that cast wins).
const combinedCreditsFixoJSON = `{
  "id": 6384,
  "combined_credits": {
    "cast": [
      {"id": 603, "media_type": "movie", "title": "The Matrix", "character": "Neo", "release_date": "1999-03-31", "poster_path": "/matrix.jpg"},
      {"id": 1396, "media_type": "tv", "name": "Breaking Bad", "character": "Cameo", "first_air_date": "2008-01-20", "poster_path": ""}
    ],
    "crew": [
      {"id": 604, "media_type": "movie", "title": "The Matrix Reloaded", "job": "Writer", "release_date": "2003-05-15", "poster_path": "/reloaded.jpg"},
      {"id": 603, "media_type": "movie", "title": "The Matrix", "job": "Producer", "release_date": "1999-03-31", "poster_path": "/matrix.jpg"},
      {"id": 604, "media_type": "movie", "title": "The Matrix Reloaded", "job": "Director", "release_date": "2003-05-15", "poster_path": "/reloaded.jpg"}
    ]
  }
}`

// servidorPessoa starts an httptest.Server that serves /person/6384 with the fixed
// payload and 404 on /person/000 (nonexistent person).
func servidorPessoa(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/person/6384":
			_, _ = w.Write([]byte(combinedCreditsFixoJSON))
		case "/person/000":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "path inesperado", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBuscarFilmografiaMapeiaDedupOrdena proves the mapping
// (source/source_id/type/title/year/poster_url/kind/character/job), the dedup by work
// (603 appears in cast AND crew -> cast wins, without job), the joining of distinct jobs
// of the SAME crew-only work (604: "Writer · Director") and the DESCENDING year ordering
// (2008, 2003, 1999).
func TestBuscarFilmografiaMapeiaDedupOrdena(t *testing.T) {
	srv := servidorPessoa(t)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	res, err := c.BuscarFilmografia(context.Background(), "6384", "")
	if err != nil {
		t.Fatalf("BuscarFilmografia: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("len(res) = %d, quero 3 (603 dedup, 604, 1396) — res=%+v", len(res), res)
	}

	serie := res[0] // 1396, 2008, cast, without poster
	eqS(t, "serie.source", serie.Source, "tmdb")
	eqS(t, "serie.source_id", serie.SourceID, "1396")
	eqS(t, "serie.type", serie.Type, "tv")
	eqS(t, "serie.title", serie.Title, "Breaking Bad")
	eqSPtr(t, "serie.year", serie.Year, "2008")
	eqS(t, "serie.kind", serie.Kind, "cast")
	eqSPtr(t, "serie.character", serie.Character, "Cameo")
	if serie.PosterURL != nil {
		t.Errorf("serie.poster_url = %v, quero nil (sem poster_path)", *serie.PosterURL)
	}
	if serie.Job != nil {
		t.Errorf("serie.job = %v, quero nil (kind=cast)", *serie.Job)
	}

	reloaded := res[1] // 604, 2003, crew, two jobs joined
	eqS(t, "reloaded.source_id", reloaded.SourceID, "604")
	eqS(t, "reloaded.type", reloaded.Type, "movie")
	eqSPtr(t, "reloaded.year", reloaded.Year, "2003")
	eqS(t, "reloaded.kind", reloaded.Kind, "crew")
	eqSPtr(t, "reloaded.job", reloaded.Job, "Writer · Director")
	eqSPtr(t, "reloaded.poster_url", reloaded.PosterURL, "https://image.tmdb.org/t/p/w185/reloaded.jpg")
	if reloaded.Character != nil {
		t.Errorf("reloaded.character = %v, quero nil (kind=crew)", *reloaded.Character)
	}

	matrix := res[2] // 603, 1999, cast WINS over the crew of the same work
	eqS(t, "matrix.source_id", matrix.SourceID, "603")
	eqS(t, "matrix.kind", matrix.Kind, "cast")
	eqSPtr(t, "matrix.character", matrix.Character, "Neo")
	eqSPtr(t, "matrix.poster_url", matrix.PosterURL, "https://image.tmdb.org/t/p/w185/matrix.jpg")
	if matrix.Job != nil {
		t.Errorf("matrix.job = %v, quero nil (cast venceu o credito de crew da mesma obra)", *matrix.Job)
	}
}

// TestBuscarFilmografiaNaoEncontrada proves that a 404 from TMDB becomes
// provider.ErrNaoEncontrado (the handler treats it by degrading to
// filmography_external=[]).
func TestBuscarFilmografiaNaoEncontrada(t *testing.T) {
	srv := servidorPessoa(t)
	c := New(Credencial{Token: "tok"}, srv.Client(), srv.URL)

	_, err := c.BuscarFilmografia(context.Background(), "000", "")
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, quero provider.ErrNaoEncontrado", err)
	}
}

// TestMontarFilmografiaExternaCorta40 proves the CEILING: 45 works of distinct
// (descending) years mapped as cast-only return only the 40 most RECENT ones, in the
// right order. It calls montarFilmografiaExterna directly (a PURE function) to avoid
// assembling a giant httptest payload.
func TestMontarFilmografiaExternaCorta40(t *testing.T) {
	const n = 45
	cast := make([]creditoCombinadoTMDB, 0, n)
	for i := 0; i < n; i++ {
		ano := 2020 - i // descending years: 2020, 2019, ..., 1976
		cast = append(cast, creditoCombinadoTMDB{
			ID:          int64(1000 + i),
			MediaType:   "movie",
			Title:       fmt.Sprintf("Obra %d", i),
			ReleaseDate: fmt.Sprintf("%d-01-01", ano),
			Character:   "Personagem",
		})
	}

	out := montarFilmografiaExterna(combinedCreditsTMDB{Cast: cast})
	if len(out) != tetoFilmografiaExterna {
		t.Fatalf("len(out) = %d, quero %d (teto)", len(out), tetoFilmografiaExterna)
	}
	eqSPtr(t, "out[0].year (mais recente)", out[0].Year, "2020")
	eqSPtr(t, "out[39].year (ultimo dentro do teto)", out[tetoFilmografiaExterna-1].Year, "1981")
}

// TestChaveObraExternaDescartaMediaTypeDesconhecido proves that a media_type outside
// movie|tv (e.g. eventual noise of "person" not foreseen for a credit) is silently
// discarded by the dedup, never appearing in the result.
func TestChaveObraExternaDescartaMediaTypeDesconhecido(t *testing.T) {
	out := montarFilmografiaExterna(combinedCreditsTMDB{
		Cast: []creditoCombinadoTMDB{
			{ID: 1, MediaType: "person", Name: "Ruido"},
			{ID: 2, MediaType: "movie", Title: "Valido", ReleaseDate: "2020-01-01"},
		},
	})
	if len(out) != 1 || out[0].SourceID != "2" {
		t.Fatalf("out = %+v, quero so o credito valido (id=2)", out)
	}
}
