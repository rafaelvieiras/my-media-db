package musicbrainz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// novoClientDeTeste builds a Client pointing at fake servers (httptest.Server), WITHOUT any real
// sleep from the rate limiter (interval=0) -- the tests of this package never hit the real network
// (MusicBrainz/Cover Art Archive), and the behavior of the rate limiter itself is covered in
// isolation by ratelimit_test.go.
func novoClientDeTeste(mbURL, caaURL string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    mbURL,
		caaBaseURL: caaURL,
		lim:        novoLimitador(0),
	}
}

// respostasMB is the router of the fake MusicBrainz server: each key is a simplified
// "PATH?ORDERED_QUERY" -- in practice we compare the path + presence/value of the "inc" param (the
// only ones that vary between the routes used by the client).
func servidorMBFake(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/artist", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("User-Agent = %q, esperava %q", got, userAgent)
		}
		if r.URL.Query().Get("query") == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"artists": [
				{"id": "artist-1", "name": "Test Artist", "disambiguation": "a test band"},
				{"id": "artist-2", "name": "No Disambiguation"}
			]
		}`))
	})

	mux.HandleFunc("/artist/artist-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// BuscarArtistaArvore requests "release-groups+annotation"; BuscarAlbumPorNome requests only
		// "release-groups" (without annotation, which it does not use) -- both receive the same list
		// of release-groups.
		if inc := r.URL.Query().Get("inc"); strings.Contains(inc, "release-groups") {
			_, _ = w.Write([]byte(`{
				"name": "Test Artist",
				"annotation": "",
				"release-groups": [
					{"id": "rg-1", "title": "Album One", "first-release-date": "1999-05-01"},
					{"id": "rg-2", "title": "Album Two", "first-release-date": "2001-01-01"}
				]
			}`))
			return
		}
		// BuscarDetalhe: without inc.
		_, _ = w.Write([]byte(`{"name": "Test Artist"}`))
	})

	mux.HandleFunc("/artist/does-not-exist", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	// TEXTUAL search of release-group (BuscarAlbumPorTitulo): it returns "Album One" (rg-1, of
	// artist-1) and a homonym of ANOTHER artist that does NOT contain the target track -- it proves
	// that the path by title chooses by evidence from the tracklist.
	mux.HandleFunc("/release-group", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("query"); !strings.HasPrefix(q, "releasegroup:") {
			t.Errorf("query = %q, quero prefixo releasegroup:", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"release-groups": [
				{"id": "rg-1", "title": "Album One", "first-release-date": "1999-05-01",
				 "artist-credit": [{"name": "Test Artist", "artist": {"id": "artist-1", "name": "Test Artist"}}]},
				{"id": "rg-9", "title": "Album One", "first-release-date": "2010-01-01",
				 "artist-credit": [{"name": "Other Artist", "artist": {"id": "artist-9", "name": "Other Artist"}}]}
			]
		}`))
	})

	mux.HandleFunc("/release-group/rg-9", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Album One",
			"first-release-date": "2010-01-01",
			"releases": [{"id": "rel-9", "status": "Official"}]
		}`))
	})

	mux.HandleFunc("/release/rel-9", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [{"position": 1, "tracks": [
				{"position": 1, "title": "Nada A Ver",
				 "recording": {"id": "rec-9", "title": "Nada A Ver"}}
			]}]
		}`))
	})

	mux.HandleFunc("/release-group/rg-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Album One",
			"first-release-date": "1999-05-01",
			"genres": [{"name": "rock", "count": 5}, {"name": "pop", "count": 2}],
			"releases": [{"id": "rel-1", "status": "Official"}]
		}`))
	})

	// /recording routes -- used by BuscarAlbumDaFaixa: "rec-1" resolves to the release-group
	// "rg-1" (through "rel-1", already served above); "rec-sem-rg" has a release WITHOUT a nested
	// release-group (incomplete data in the source).
	mux.HandleFunc("/recording/rec-1", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("inc"); got != "releases+release-groups" {
			t.Errorf("inc = %q, quero releases+release-groups", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"releases": [
				{"id": "rel-1", "status": "Official", "release-group": {"id": "rg-1"}}
			]
		}`))
	})
	mux.HandleFunc("/recording/rec-sem-rg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"releases": [{"id": "rel-x", "status": "Official"}]}`))
	})
	mux.HandleFunc("/recording/rec-inexistente-album", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	mux.HandleFunc("/release-group/rg-2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"genres": [{"name": "electronic", "count": 1}],
			"releases": [{"id": "rel-2", "status": "Official"}]
		}`))
	})

	mux.HandleFunc("/release-group/rg-empty", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"genres": [], "releases": []}`))
	})

	mux.HandleFunc("/release/rel-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [
				{
					"position": 1,
					"tracks": [
						{
							"position": 1,
							"title": "Track A",
							"artist-credit": [{"artist": {"id": "artist-1", "name": "Test Artist"}}],
							"recording": {
								"id": "rec-1",
								"title": "Track A",
								"isrcs": ["US1234567890"],
								"artist-credit": [{"artist": {"id": "artist-1", "name": "Test Artist"}}]
							}
						},
						{
							"position": 2,
							"title": "Track B",
							"artist-credit": [{"artist": {"id": "artist-1", "name": "Test Artist"}}],
							"recording": {
								"id": "rec-2",
								"title": "Track B",
								"isrcs": [],
								"artist-credit": [{"artist": {"id": "artist-1", "name": "Test Artist"}}]
							}
						}
					]
				}
			]
		}`))
	})

	// rel-2 has a track with an artist DIVERGING from the album artist (feat./collaboration), on 2
	// discs (it tests DiscNumber/DiscTotal).
	mux.HandleFunc("/release/rel-2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [
				{
					"position": 1,
					"tracks": [
						{
							"position": 1,
							"title": "Collab Track",
							"artist-credit": [{"artist": {"id": "guest-1", "name": "Guest Artist"}}],
							"recording": {
								"id": "rec-3",
								"title": "Collab Track",
								"isrcs": ["US9876543210"],
								"artist-credit": [{"artist": {"id": "guest-1", "name": "Guest Artist"}}]
							}
						}
					]
				},
				{
					"position": 2,
					"tracks": [
						{
							"position": 1,
							"title": "Disc2 Track",
							"artist-credit": [{"artist": {"id": "artist-1", "name": "Test Artist"}}],
							"recording": {
								"id": "rec-4",
								"title": "Disc2 Track",
								"isrcs": [],
								"artist-credit": [{"artist": {"id": "artist-1", "name": "Test Artist"}}]
							}
						}
					]
				}
			]
		}`))
	})

	return httptest.NewServer(mux)
}

func servidorCAAFake(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/release-group/rg-1", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("User-Agent (CAA) = %q, esperava %q", got, userAgent)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"images": [
				{"front": true, "back": false, "image": "https://coverartarchive.org/rg-1/front.jpg"},
				{"front": false, "back": true, "image": "https://coverartarchive.org/rg-1/back.jpg"},
				{"front": false, "back": false, "image": "https://coverartarchive.org/rg-1/booklet.jpg"}
			]
		}`))
	})

	mux.HandleFunc("/release-group/rg-sem-capa", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	return httptest.NewServer(mux)
}

func TestNome(t *testing.T) {
	c := New(nil, "")
	if c.Nome() != "musicbrainz" {
		t.Fatalf("Nome() = %q, esperava musicbrainz", c.Nome())
	}
}

func TestBuscar(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	resultados, err := c.Buscar(context.Background(), "Test Artist", "artista", "")
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if len(resultados) != 2 {
		t.Fatalf("len(resultados) = %d, esperava 2", len(resultados))
	}

	r0 := resultados[0]
	if r0.Source != "musicbrainz" || r0.SourceID != "artist-1" || r0.Type != "artista" || r0.Title != "Test Artist" {
		t.Fatalf("resultado[0] inesperado: %+v", r0)
	}
	if r0.Overview == nil || *r0.Overview != "a test band" {
		t.Fatalf("resultado[0].Overview = %v, esperava \"a test band\"", r0.Overview)
	}
	if r0.Saved {
		t.Fatalf("resultado[0].Saved deveria ser false")
	}

	r1 := resultados[1]
	if r1.Overview != nil {
		t.Fatalf("resultado[1].Overview = %v, esperava nil (sem disambiguation)", *r1.Overview)
	}
}

func TestBuscarTipoNaoArtistaEhNoop(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	resultados, err := c.Buscar(context.Background(), "qualquer coisa", "movie", "")
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if resultados != nil {
		t.Fatalf("Buscar(tipo=movie) = %v, esperava nil", resultados)
	}
}

func TestBuscarDetalheEMapearDetalhe(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	raw, err := c.BuscarDetalhe(context.Background(), "artist-1", "artista", nil)
	if err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}

	campos, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}
	if campos["title"] != "Test Artist" {
		t.Fatalf("campos[title] = %v, esperava Test Artist", campos["title"])
	}
}

func TestBuscarDetalheNaoEncontrado(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, err := c.BuscarDetalhe(context.Background(), "does-not-exist", "artista", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
}

// servidorDetalheMusicaFake serves only /release-group/{id} and /recording/{id} WITHOUT requiring any
// `inc` (unlike servidorMBFake, whose /recording/rec-1 validates inc=releases+release-groups for
// another consumer, BuscarAlbumDaFaixa) -- the path of BuscarDetalhe for album/track does not request
// any `inc`, only the title.
func servidorDetalheMusicaFake(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/release-group/rg-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title": "Album One", "first-release-date": "1999-05-01"}`))
	})
	mux.HandleFunc("/release-group/rg-inexistente", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/recording/rec-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title": "Track A"}`))
	})
	return httptest.NewServer(mux)
}

// TestBuscarDetalheAlbumEFaixaDespachaPorTipo covers the bug found in the production validation:
// BuscarDetalhe for tipo=album/faixa always called /artist/ (wrong sourceID -- for those two
// types the sourceID is the release-group/recording MBID, not the artist one) and hit 404/502,
// breaking "Download Metadata". It confirms the correct dispatch of the endpoint AND the
// "title"→"name" remap (remapParaNome) that lets MapearDetalhe work without changing the
// signature of the core provider.Provider interface.
func TestBuscarDetalheAlbumEFaixaDespachaPorTipo(t *testing.T) {
	mb := servidorDetalheMusicaFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	t.Run("album", func(t *testing.T) {
		raw, err := c.BuscarDetalhe(context.Background(), "rg-1", "album", nil)
		if err != nil {
			t.Fatalf("BuscarDetalhe: %v", err)
		}
		campos, err := c.MapearDetalhe(raw)
		if err != nil {
			t.Fatalf("MapearDetalhe: %v", err)
		}
		if campos["title"] != "Album One" {
			t.Fatalf("campos[title] = %v, esperava Album One", campos["title"])
		}
	})

	t.Run("faixa", func(t *testing.T) {
		raw, err := c.BuscarDetalhe(context.Background(), "rec-1", "faixa", nil)
		if err != nil {
			t.Fatalf("BuscarDetalhe: %v", err)
		}
		campos, err := c.MapearDetalhe(raw)
		if err != nil {
			t.Fatalf("MapearDetalhe: %v", err)
		}
		if campos["title"] != "Track A" {
			t.Fatalf("campos[title] = %v, esperava Track A", campos["title"])
		}
	})
}

func TestBuscarDetalheAlbumNaoEncontrado(t *testing.T) {
	mb := servidorDetalheMusicaFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, err := c.BuscarDetalhe(context.Background(), "rg-inexistente", "album", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
}

func TestBuscarArtistaArvore(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	arvore, err := c.BuscarArtistaArvore(context.Background(), "artist-1", nil)
	if err != nil {
		t.Fatalf("BuscarArtistaArvore: %v", err)
	}

	if arvore.Name != "Test Artist" || arvore.MBID != "artist-1" {
		t.Fatalf("identidade do artista inesperada: %+v", arvore)
	}
	if len(arvore.Albuns) != 2 {
		t.Fatalf("len(Albuns) = %d, esperava 2", len(arvore.Albuns))
	}

	album1 := arvore.Albuns[0]
	if album1.Name != "Album One" || album1.MBID != "rg-1" || album1.Year != "1999" || album1.Genre != "rock" {
		t.Fatalf("album1 inesperado: %+v", album1)
	}
	if len(album1.Faixas) != 2 {
		t.Fatalf("len(album1.Faixas) = %d, esperava 2", len(album1.Faixas))
	}
	f0 := album1.Faixas[0]
	if f0.Title != "Track A" || f0.MBID != "rec-1" || f0.TrackNumber != 1 || f0.DiscNumber != 1 || f0.DiscTotal != 1 {
		t.Fatalf("faixa 0 do album1 inesperada: %+v", f0)
	}
	if f0.ISRC != "US1234567890" {
		t.Fatalf("faixa 0 ISRC = %q, esperava US1234567890", f0.ISRC)
	}
	if f0.ArtistaCreditoNome != "Test Artist" || f0.ArtistaCreditoMBID != "artist-1" {
		t.Fatalf("faixa 0 artista-credito inesperado: %+v", f0)
	}
	f1 := album1.Faixas[1]
	if f1.ISRC != "" {
		t.Fatalf("faixa 1 ISRC = %q, esperava vazio (sem isrcs na fonte)", f1.ISRC)
	}

	// album2 (rg-2/rel-2): track 0 has a DIVERGING artist (feat.); track 1 is on disc 2 of 2
	// (DiscTotal=2).
	album2 := arvore.Albuns[1]
	if album2.Name != "Album Two" || album2.MBID != "rg-2" || album2.Genre != "electronic" {
		t.Fatalf("album2 inesperado: %+v", album2)
	}
	if len(album2.Faixas) != 2 {
		t.Fatalf("len(album2.Faixas) = %d, esperava 2", len(album2.Faixas))
	}
	collab := album2.Faixas[0]
	if collab.ArtistaCreditoMBID != "guest-1" || collab.ArtistaCreditoNome != "Guest Artist" {
		t.Fatalf("faixa colaborativa deveria ter artista divergente, teve: %+v", collab)
	}
	if collab.ArtistaCreditoMBID == arvore.MBID {
		t.Fatalf("faixa colaborativa nao deveria ter o MBID do artista raiz")
	}
	disc2 := album2.Faixas[1]
	if disc2.DiscNumber != 2 || disc2.DiscTotal != 2 {
		t.Fatalf("faixa do disco 2 inesperada: %+v", disc2)
	}
	if disc2.ArtistaCreditoMBID != arvore.MBID {
		t.Fatalf("faixa do disco 2 deveria ter o artista raiz, teve %+v", disc2)
	}
}

func TestBuscarArtistaArvoreNaoEncontrado(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, err := c.BuscarArtistaArvore(context.Background(), "does-not-exist", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
}

// TestBuscarArtistaArvoreAlbumPersistentementeIndisponivelDegradaSemAbortar covers the real finding of
// the production validation (Duran Duran, a real MBID): the public MusicBrainz server is unstable enough
// that, in a tree with dozens of albums, it is expected that the detail call of ONE specific
// release-group exhausts all the retry attempts. That must NOT abort the whole tree -- the problematic
// album must degrade to a shell without genre/tracks, and the other albums (before and after it) stay
// complete.
func TestBuscarArtistaArvoreAlbumPersistentementeIndisponivelDegradaSemAbortar(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/artist/artist-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "Test Artist",
			"release-groups": [
				{"id": "rg-1", "title": "Album One", "first-release-date": "1999-05-01"},
				{"id": "rg-2", "title": "Album Two (instavel)", "first-release-date": "2001-01-01"}
			]
		}`))
	})
	mux.HandleFunc("/release-group/rg-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"genres": [{"name": "rock", "count": 5}], "releases": []}`))
	})
	mux.HandleFunc("/release-group/rg-2", func(w http.ResponseWriter, r *http.Request) {
		// Always 503 -- it simulates the real instability of the public server. getMB will exhaust
		// tentativasMB attempts for THIS release-group specifically.
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := clientComBackoffFake(srv.URL)

	arvore, err := c.BuscarArtistaArvore(context.Background(), "artist-1", nil)
	if err != nil {
		t.Fatalf("BuscarArtistaArvore nao deveria falhar por causa de UM album instavel: %v", err)
	}
	if len(arvore.Albuns) != 2 {
		t.Fatalf("esperava 2 albuns (1 completo + 1 degradado), teve %d", len(arvore.Albuns))
	}
	completo, degradado := arvore.Albuns[0], arvore.Albuns[1]
	if completo.MBID != "rg-1" || completo.Genre != "rock" {
		t.Fatalf("album completo inesperado: %+v", completo)
	}
	if degradado.MBID != "rg-2" || degradado.Name != "Album Two (instavel)" {
		t.Fatalf("album degradado com identidade errada: %+v", degradado)
	}
	if degradado.Genre != "" || len(degradado.Faixas) != 0 {
		t.Fatalf("album degradado deveria vir sem genero/faixas, teve: %+v", degradado)
	}
}

// TestBuscarAlbumDaFaixa covers the PRECISE path: it resolves the release-group from the recording MBID
// "rec-1" (through "rel-1"/status Official) and assembles ONLY that album -- name/year come from the
// release-group ITSELF (rg.Title/rg.FirstReleaseDate), since the caller does not have them
// beforehand.
func TestBuscarAlbumDaFaixa(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album, achou, err := c.BuscarAlbumDaFaixa(context.Background(), "rec-1", nil)
	if err != nil {
		t.Fatalf("BuscarAlbumDaFaixa: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true")
	}
	if album.Name != "Album One" || album.MBID != "rg-1" || album.Year != "1999" || album.Genre != "rock" {
		t.Fatalf("album inesperado: %+v", album)
	}
	if len(album.Faixas) != 2 {
		t.Fatalf("len(Faixas) = %d, esperava 2 (rel-1)", len(album.Faixas))
	}
}

// TestBuscarAlbumDaFaixaSemReleaseGroup covers the incomplete data in the source (the recording has
// releases, but NONE with a nested release-group): ok=false, gracefully -- it is not an error.
func TestBuscarAlbumDaFaixaSemReleaseGroup(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, achou, err := c.BuscarAlbumDaFaixa(context.Background(), "rec-sem-rg", nil)
	if err != nil {
		t.Fatalf("BuscarAlbumDaFaixa: %v", err)
	}
	if achou {
		t.Fatal("achou = true, esperava false (sem release-group utilizavel)")
	}
}

// TestBuscarAlbumDaFaixaRecordingNaoEncontrado covers the 404: sentinel error
// provider.ErrNaoEncontrado (errors.Is), the same contract as the other methods.
func TestBuscarAlbumDaFaixaRecordingNaoEncontrado(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, _, err := c.BuscarAlbumDaFaixa(context.Background(), "rec-inexistente-album", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
}

// TestBuscarAlbumDaFaixaMbidVazio covers the input validation (without a round trip to the
// network): immediate error, never a GET with an empty path.
func TestBuscarAlbumDaFaixaMbidVazio(t *testing.T) {
	c := novoClientDeTeste("http://unused.invalid", "")
	if _, _, err := c.BuscarAlbumDaFaixa(context.Background(), "", nil); err == nil {
		t.Fatal("esperava erro (mbid de faixa vazio)")
	}
}

// TestBuscarAlbumPorNome covers the TEXTUAL path: it lists the release-groups of the artist
// "artist-1" (rg-1 "Album One", rg-2 "Album Two") and assembles ONLY the one that matches "Album
// One" -- rg-2 is never assembled (only listed).
func TestBuscarAlbumPorNome(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-1", "Album One", provider.AlvoFaixaAlbum{}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true")
	}
	if album.Name != "Album One" || album.MBID != "rg-1" || album.Year != "1999" || album.Genre != "rock" {
		t.Fatalf("album inesperado: %+v", album)
	}
	if len(album.Faixas) != 2 {
		t.Fatalf("len(Faixas) = %d, esperava 2 (rel-1)", len(album.Faixas))
	}
}

// TestBuscarAlbumPorNomeSemCandidato covers a name without a plausible correspondence (below the
// matching.LimiarAutoMatch threshold): ok=false, gracefully.
func TestBuscarAlbumPorNomeSemCandidato(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-1", "Totally Unrelated Name", provider.AlvoFaixaAlbum{}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if achou {
		t.Fatal("achou = true, esperava false (nome sem correspondencia plausivel)")
	}
}

// TestBuscarAlbumPorNomeArtistaNaoEncontrado covers the 404 of the artist.
func TestBuscarAlbumPorNomeArtistaNaoEncontrado(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, _, err := c.BuscarAlbumPorNome(context.Background(), "does-not-exist", "Album One", provider.AlvoFaixaAlbum{}, nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
}

func TestMontarAlbumReleaseGroupSemReleases(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album := c.montarAlbum(context.Background(), "rg-empty", "Album Vazio", "2020", provider.AlvoFaixaAlbum{})
	if album.Name != "Album Vazio" || album.MBID != "rg-empty" {
		t.Fatalf("album inesperado: %+v", album)
	}
	if len(album.Faixas) != 0 {
		t.Fatalf("esperava 0 faixas para release-group sem releases, teve %d", len(album.Faixas))
	}
}

// TestBuscarAlbumPorMbid covers the DIRECT path: the MBID already is the one of the release-group
// ("rg-1"), without a previous resolution step -- it assembles the album with its tracks, the same
// shape as BuscarAlbumDaFaixa/BuscarAlbumPorNome.
func TestBuscarAlbumPorMbid(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album, err := c.BuscarAlbumPorMbid(context.Background(), "rg-1", nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorMbid: %v", err)
	}
	if album.Name != "Album One" || album.MBID != "rg-1" || album.Year != "1999" || album.Genre != "rock" {
		t.Fatalf("album inesperado: %+v", album)
	}
	if len(album.Faixas) != 2 {
		t.Fatalf("len(Faixas) = %d, esperava 2 (rel-1)", len(album.Faixas))
	}
}

// TestBuscarAlbumPorMbidNaoEncontrado covers the 404: unlike montarAlbum (used only AFTER a previous
// resolution, which degrades silently), this method has NO previous resolution -- that is why it
// must propagate provider.ErrNaoEncontrado (errors.Is) instead of returning an empty shell
// indistinguishable from "found, without data".
func TestBuscarAlbumPorMbidNaoEncontrado(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, err := c.BuscarAlbumPorMbid(context.Background(), "rg-inexistente", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
}

// TestBuscarAlbumPorMbidVazio covers the input validation (without a round trip to the network).
func TestBuscarAlbumPorMbidVazio(t *testing.T) {
	c := novoClientDeTeste("http://unused.invalid", "")
	if _, err := c.BuscarAlbumPorMbid(context.Background(), "", nil); err == nil {
		t.Fatal("esperava erro (mbid de album vazio)")
	}
}

func TestBuscarArtes(t *testing.T) {
	caa := servidorCAAFake(t)
	defer caa.Close()
	c := novoClientDeTeste("", caa.URL)

	artes, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{Mbid: "rg-1"}, "album", nil)
	if err != nil {
		t.Fatalf("BuscarArtes: %v", err)
	}
	if len(artes) != 2 {
		t.Fatalf("len(artes) = %d, esperava 2 (front+back, booklet ignorado)", len(artes))
	}
	achouFront, achouBack := false, false
	for _, a := range artes {
		switch a.Kind {
		case "poster":
			achouFront = true
			if a.URL != "https://coverartarchive.org/rg-1/front.jpg" {
				t.Fatalf("URL da capa inesperada: %s", a.URL)
			}
		case "back":
			achouBack = true
		default:
			t.Fatalf("kind inesperado: %s", a.Kind)
		}
	}
	if !achouFront || !achouBack {
		t.Fatalf("esperava front(poster) e back mapeados, artes=%+v", artes)
	}
}

func TestBuscarArtesSemCapa(t *testing.T) {
	caa := servidorCAAFake(t)
	defer caa.Close()
	c := novoClientDeTeste("", caa.URL)

	artes, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{Mbid: "rg-sem-capa"}, "album", nil)
	if err != nil {
		t.Fatalf("BuscarArtes: %v", err)
	}
	if artes != nil {
		t.Fatalf("artes = %v, esperava nil (404 = graca)", artes)
	}
}

func TestBuscarArtesTipoNaoAlbumEhNoop(t *testing.T) {
	caa := servidorCAAFake(t)
	defer caa.Close()
	c := novoClientDeTeste("", caa.URL)

	artes, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{Mbid: "rg-1"}, "movie", nil)
	if err != nil {
		t.Fatalf("BuscarArtes: %v", err)
	}
	if artes != nil {
		t.Fatalf("artes = %v, esperava nil (tipo != album)", artes)
	}
}

func TestBuscarArtesSemMbidEhNoop(t *testing.T) {
	caa := servidorCAAFake(t)
	defer caa.Close()
	c := novoClientDeTeste("", caa.URL)

	artes, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{}, "album", nil)
	if err != nil {
		t.Fatalf("BuscarArtes: %v", err)
	}
	if artes != nil {
		t.Fatalf("artes = %v, esperava nil (sem Mbid)", artes)
	}
}

// TestImplementaInterfaces is a compile-time check (it runs nothing) that the Client satisfies the
// expected interfaces: core provider.Provider, provider.MusicaProvider (tree, legacy),
// provider.MusicaAlbumProvider (selective), provider.ArteProvider and
// provider.FaixaResolverProvider.
func TestImplementaInterfaces(t *testing.T) {
	var _ provider.Provider = (*Client)(nil)
	var _ provider.MusicaProvider = (*Client)(nil)
	var _ provider.MusicaAlbumProvider = (*Client)(nil)
	var _ provider.ArteProvider = (*Client)(nil)
	var _ provider.FaixaResolverProvider = (*Client)(nil)
}

// servidorRecordingFake is a hermetic httptest.Server dedicated to ResolverArtistaDaFaixa: it
// answers GET /recording/{mbid}?inc=artist-credits with a fixed artist-credit for
// "rec-com-artista", an EMPTY list of artist-credit for "rec-sem-artista" (incomplete data in the
// source) and 404 for any other id.
func servidorRecordingFake(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/recording/rec-com-artista", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("inc"); got != "artist-credits" {
			t.Errorf("inc = %q, quero artist-credits", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artist-credit":[{"artist":{"id":"artist-da-faixa","name":"Fake Band"}}]}`))
	})
	mux.HandleFunc("/recording/rec-sem-artista", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artist-credit":[]}`))
	})
	mux.HandleFunc("/recording/rec-inexistente", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	return httptest.NewServer(mux)
}

// TestResolverArtistaDaFaixa covers the happy path: a recording with 1 artist-credit returns the
// MBID of the artist.
func TestResolverArtistaDaFaixa(t *testing.T) {
	srv := servidorRecordingFake(t)
	defer srv.Close()
	c := novoClientDeTeste(srv.URL, "")

	got, err := c.ResolverArtistaDaFaixa(context.Background(), "rec-com-artista")
	if err != nil {
		t.Fatalf("ResolverArtistaDaFaixa: %v", err)
	}
	if got != "artist-da-faixa" {
		t.Errorf("artistaMBID = %q, quero %q", got, "artist-da-faixa")
	}
}

// TestResolverArtistaDaFaixaSemArtistCredit covers the incomplete data in the source (the recording
// exists but without ANY artist-credit): a generic error, never a silent empty MBID.
func TestResolverArtistaDaFaixaSemArtistCredit(t *testing.T) {
	srv := servidorRecordingFake(t)
	defer srv.Close()
	c := novoClientDeTeste(srv.URL, "")

	if _, err := c.ResolverArtistaDaFaixa(context.Background(), "rec-sem-artista"); err == nil {
		t.Fatal("ResolverArtistaDaFaixa deveria falhar (recording sem artist-credit)")
	}
}

// TestResolverArtistaDaFaixaNaoEncontrada covers the 404: sentinel error provider.ErrNaoEncontrado
// (errors.Is), the same contract as the other methods.
func TestResolverArtistaDaFaixaNaoEncontrada(t *testing.T) {
	srv := servidorRecordingFake(t)
	defer srv.Close()
	c := novoClientDeTeste(srv.URL, "")

	_, err := c.ResolverArtistaDaFaixa(context.Background(), "rec-inexistente")
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Errorf("err = %v, quero errors.Is(err, provider.ErrNaoEncontrado)", err)
	}
}

// servidorContadorMB creates an httptest.Server that counts the calls to `path` and returns, for
// the n-th call (1-based), `statusPorChamada[n-1]` -- calls beyond the size of the slice repeat the
// LAST status of the list (useful for "always 503"). A 200 status returns a minimal valid body for
// BuscarDetalhe/MapearDetalhe; any other status returns an empty body (getMB does not look at the
// error body). Used to exercise the retry of getMB deterministically, without depending on the real
// public API.
func servidorContadorMB(t *testing.T, path string, statusPorChamada []int) (*httptest.Server, *int32) {
	t.Helper()
	var chamadas int32
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&chamadas, 1)
		idx := int(n) - 1
		if idx >= len(statusPorChamada) {
			idx = len(statusPorChamada) - 1
		}
		status := statusPorChamada[idx]
		if status == http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name": "Retry Artist"}`))
			return
		}
		w.WriteHeader(status)
	})
	return httptest.NewServer(mux), &chamadas
}

// clientComBackoffFake builds a test Client (without a sleep of the rate limiter itself, as in
// novoClientDeTeste) and REPLACES the `dormir` of the shared limiter with a no-op: the retry loop of
// getMB uses `c.lim.dormirFn` for the backoff between attempts (500ms x attempt -- see the comment
// of backoffMB in musicbrainz.go), and without this injection the tests of attempts exhaustion would
// really sleep (500ms + 1000ms = 1.5s in the case 2 alone).
func clientComBackoffFake(mbURL string) *Client {
	c := novoClientDeTeste(mbURL, "")
	c.lim.dormir = func(time.Duration) {}
	return c
}

// TestGetMBRetrySucessoAposFalhasTransitorias covers the happy case of the retry (the public
// MusicBrainz API returns 503 "currently busy" with a real frequency of ~1 in 3 calls): the first 2
// attempts see 503, the 3rd (within tentativasMB=3) succeeds -- getMB must return the result of the
// attempt that worked, not an error.
func TestGetMBRetrySucessoAposFalhasTransitorias(t *testing.T) {
	srv, chamadas := servidorContadorMB(t, "/artist/retry-artist", []int{
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusOK,
	})
	defer srv.Close()
	c := clientComBackoffFake(srv.URL)

	raw, err := c.BuscarDetalhe(context.Background(), "retry-artist", "artista", nil)
	if err != nil {
		t.Fatalf("BuscarDetalhe: %v", err)
	}
	campos, err := c.MapearDetalhe(raw)
	if err != nil {
		t.Fatalf("MapearDetalhe: %v", err)
	}
	if campos["title"] != "Retry Artist" {
		t.Fatalf("campos[title] = %v, esperava Retry Artist (resultado da 3a tentativa)", campos["title"])
	}
	if got := atomic.LoadInt32(chamadas); got != 3 {
		t.Fatalf("chamadas ao servidor = %d, esperava 3 (2 falhas + 1 sucesso)", got)
	}
}

// TestGetMBRetryEsgotaTentativas covers the case of a persistent 503: getMB must try exactly
// tentativasMB times (never more, never less) and return an error -- it must not hang nor enter an
// infinite loop.
func TestGetMBRetryEsgotaTentativas(t *testing.T) {
	srv, chamadas := servidorContadorMB(t, "/artist/sempre-ocupado", []int{http.StatusServiceUnavailable})
	defer srv.Close()
	c := clientComBackoffFake(srv.URL)

	_, err := c.BuscarDetalhe(context.Background(), "sempre-ocupado", "artista", nil)
	if err == nil {
		t.Fatal("BuscarDetalhe deveria falhar (503 em todas as tentativas)")
	}
	if errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err nao deveria ser ErrNaoEncontrado (503 nao e 404): %v", err)
	}
	if !strings.Contains(err.Error(), "esgotou") {
		t.Fatalf("err = %v, esperava mencionar esgotamento das tentativas", err)
	}
	if got := atomic.LoadInt32(chamadas); got != tentativasMB {
		t.Fatalf("chamadas ao servidor = %d, esperava %d (tentativasMB)", got, tentativasMB)
	}
}

// TestGetMB404NaoTentaNovamente confirms that a 404 (a resource that really does not exist, not a
// server instability) makes getMB return provider.ErrNaoEncontrado on the FIRST attempt, without
// retry -- transitorioMB never considers a 404 transient.
func TestGetMB404NaoTentaNovamente(t *testing.T) {
	srv, chamadas := servidorContadorMB(t, "/artist/inexistente-de-verdade", []int{http.StatusNotFound})
	defer srv.Close()
	c := clientComBackoffFake(srv.URL)

	_, err := c.BuscarDetalhe(context.Background(), "inexistente-de-verdade", "artista", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
	if got := atomic.LoadInt32(chamadas); got != 1 {
		t.Fatalf("chamadas ao servidor = %d, esperava 1 (404 nao tenta de novo)", got)
	}
}

// TestGetMBStatusGenericoNaoTentaNovamente confirms that a status != 200/404/502/503/504 (e.g. 500)
// is a generic error WITHOUT retry -- only the 3 statuses documented as instability of the public
// server (502/503/504) trigger the retry; anything else is treated as definitive.
func TestGetMBStatusGenericoNaoTentaNovamente(t *testing.T) {
	srv, chamadas := servidorContadorMB(t, "/artist/erro-generico", []int{http.StatusInternalServerError})
	defer srv.Close()
	c := clientComBackoffFake(srv.URL)

	_, err := c.BuscarDetalhe(context.Background(), "erro-generico", "artista", nil)
	if err == nil {
		t.Fatal("BuscarDetalhe deveria falhar (500)")
	}
	if errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err nao deveria ser ErrNaoEncontrado (500 nao e 404): %v", err)
	}
	if strings.Contains(err.Error(), "esgotou") {
		t.Fatalf("err = %v, nao deveria mencionar esgotamento de tentativas (500 nao e transitorio)", err)
	}
	if got := atomic.LoadInt32(chamadas); got != 1 {
		t.Fatalf("chamadas ao servidor = %d, esperava 1 (500 nao tenta de novo)", got)
	}
}

// servidorContadorCAA is the equivalent of servidorContadorMB for the Cover Art Archive -- it returns
// a valid images body on the OK status, an empty body on the others (the content of the error does
// not matter for getCAA).
func servidorContadorCAA(t *testing.T, path string, statusPorChamada []int) (*httptest.Server, *int32) {
	t.Helper()
	var chamadas int32
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&chamadas, 1)
		idx := int(n) - 1
		if idx >= len(statusPorChamada) {
			idx = len(statusPorChamada) - 1
		}
		status := statusPorChamada[idx]
		if status == http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"images": [{"front": true, "back": false, "image": "https://x/front.jpg"}]}`))
			return
		}
		w.WriteHeader(status)
	})
	return httptest.NewServer(mux), &chamadas
}

// clientComBackoffFakeCAA is clientComBackoffFake, but pointing caaBaseURL (not baseURL) at the fake
// server -- getCAA uses c.caaBaseURL, a host separate from getMB.
func clientComBackoffFakeCAA(caaURL string) *Client {
	c := novoClientDeTeste("", caaURL)
	c.lim.dormir = func(time.Duration) {}
	return c
}

// TestGetCAARetrySucessoAposFalhasTransitorias covers the real finding of the production validation:
// the redirect of the Cover Art Archive to archive.org (a distributed CDN, dnNNNNNN.ca.archive.org)
// gave a timeout/error in a good part of the calls of a real 25-album import -- unlike the "busy" 503
// of MusicBrainz, getCAA tries again on ANY error (not only 502/503/504), because the CAA does not
// always fail with an HTTP status (a network timeout has no status at all).
func TestGetCAARetrySucessoAposFalhasTransitorias(t *testing.T) {
	srv, chamadas := servidorContadorCAA(t, "/release-group/rg-retry", []int{
		http.StatusInternalServerError, http.StatusInternalServerError, http.StatusOK,
	})
	defer srv.Close()
	c := clientComBackoffFakeCAA(srv.URL)

	artes, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{Mbid: "rg-retry"}, "album", nil)
	if err != nil {
		t.Fatalf("BuscarArtes: %v", err)
	}
	if len(artes) != 1 || artes[0].Kind != "poster" {
		t.Fatalf("artes inesperadas apos retry: %+v", artes)
	}
	if got := atomic.LoadInt32(chamadas); got != 3 {
		t.Fatalf("chamadas ao servidor CAA = %d, esperava 3 (2 falhas + 1 sucesso)", got)
	}
}

// TestGetCAARetryEsgotaTentativas confirms that getCAA does not try forever: it exhausts tentativasMB
// and returns an error (BuscarArtes propagates it; the caller already treats that as best-effort
// enrichment, see internal/enrich).
func TestGetCAARetryEsgotaTentativas(t *testing.T) {
	srv, chamadas := servidorContadorCAA(t, "/release-group/rg-sempre-500", []int{http.StatusInternalServerError})
	defer srv.Close()
	c := clientComBackoffFakeCAA(srv.URL)

	_, err := c.BuscarArtes(context.Background(), provider.ExternalIDs{Mbid: "rg-sempre-500"}, "album", nil)
	if err == nil {
		t.Fatal("BuscarArtes deveria falhar apos esgotar as tentativas")
	}
	if got := atomic.LoadInt32(chamadas); got != tentativasMB {
		t.Fatalf("chamadas ao servidor CAA = %d, esperava %d (tentativasMB)", got, tentativasMB)
	}
}

// servidorFreestylerFake assembles the REAL scenario of Bomfunk MC's "Freestyler" (found while
// validating a real production catalog): the release-group "rg-freestyler" has 2 releases --
// "rel-maxi" (status "Official", a maxi-single with only remixed versions, WITHOUT the pure
// "Freestyler") and "rel-full" (status other than Official, but with the exact "Freestyler" track).
// It also returns `chamadasFaixas`, which counts the calls to the /release/{id} endpoints (never
// /release-group/{id}) -- used by the tests that prove the absence of a cost regression on the path
// without alvoFaixa.
func servidorFreestylerFake(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var chamadasFaixas int32
	mux := http.NewServeMux()

	mux.HandleFunc("/artist/artist-freestyler", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "Bomfunk MC's",
			"release-groups": [
				{"id": "rg-freestyler", "title": "Freestyler", "first-release-date": "2000-01-01"}
			]
		}`))
	})

	mux.HandleFunc("/release-group/rg-freestyler", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Official listed FIRST in the JSON on purpose -- ordenarReleasesPorPrioridade does not depend on
		// the input order, only on the status; and escolherRelease (zero-value path) would find it
		// anyway.
		_, _ = w.Write([]byte(`{
			"title": "Freestyler",
			"first-release-date": "2000-01-01",
			"genres": [],
			"releases": [
				{"id": "rel-maxi", "status": "Official"},
				{"id": "rel-full", "status": "Bootleg"}
			]
		}`))
	})

	mux.HandleFunc("/release/rel-maxi", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&chamadasFaixas, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [{
				"position": 1,
				"tracks": [
					{"position": 1, "title": "Freestyler (radio edit)", "artist-credit": [],
					 "recording": {"id": "rec-radio", "title": "Freestyler (radio edit)", "isrcs": [], "artist-credit": []}}
				]
			}]
		}`))
	})

	mux.HandleFunc("/release/rel-full", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&chamadasFaixas, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [{
				"position": 1,
				"tracks": [
					{"position": 5, "title": "Freestyler", "artist-credit": [],
					 "recording": {"id": "rec-full-freestyler", "title": "Freestyler", "isrcs": [], "artist-credit": []}}
				]
			}]
		}`))
	})

	return httptest.NewServer(mux), &chamadasFaixas
}

// TestBuscarFaixasComAlvoTentaOutraReleaseQuandoOfficialNaoTemAFaixa is the main acceptance
// criterion of this feature: the "Official" release (rel-maxi) does NOT have the pure "Freestyler"
// track (only remixed versions -- 89.5 of similarity, below the threshold of 92), but "rel-full" (the
// same release-group) has it -- BuscarAlbumPorNome with `alvoFaixa` filled in must bring the tracks of
// rel-full, not the ones of rel-maxi.
func TestBuscarFaixasComAlvoTentaOutraReleaseQuandoOfficialNaoTemAFaixa(t *testing.T) {
	mb, _ := servidorFreestylerFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-freestyler", "Freestyler",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Freestyler"}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true")
	}

	achouFaixa := false
	for _, fx := range album.Faixas {
		if fx.Title == "Freestyler" {
			achouFaixa = true
		}
	}
	if !achouFaixa {
		t.Fatalf("faixa 'Freestyler' nao encontrada entre as faixas devolvidas -- deveria ter tentado rel-full (a release Official nao a tem): %+v", album.Faixas)
	}
}

// TestBuscarFaixasComAlvoZeroValueMantemUmaSoChamada confirms that the path without alvoFaixa (zero
// value) preserves the exact HISTORICAL behavior: only 1 HTTP call to the release tracks endpoint,
// even with the same release-group having another release that would contain "Freestyler" -- nobody
// asked to search, so it does not search (zero cost regression).
func TestBuscarFaixasComAlvoZeroValueMantemUmaSoChamada(t *testing.T) {
	mb, chamadas := servidorFreestylerFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-freestyler", "Freestyler", provider.AlvoFaixaAlbum{}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true")
	}
	if got := atomic.LoadInt32(chamadas); got != 1 {
		t.Fatalf("chamadas a /release/{id} = %d, esperava 1 (sem alvoFaixa nao deve regredir custo)", got)
	}
}

// servidorTetoFake assembles a release-group with 17 releases (more than maxReleasesTentadosPorAlvo=15),
// NONE of them containing the target track -- it proves that buscarFaixasComAlvo respects the ceiling
// instead of iterating the whole release-group.
func servidorTetoFake(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var chamadas int32
	mux := http.NewServeMux()

	mux.HandleFunc("/artist/artist-teto", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "Artista Teto",
			"release-groups": [
				{"id": "rg-teto", "title": "Album Teto", "first-release-date": "2005-01-01"}
			]
		}`))
	})

	const totalReleases = 17
	releaseIDs := make([]string, 0, totalReleases)
	for i := 1; i <= totalReleases; i++ {
		releaseIDs = append(releaseIDs, fmt.Sprintf("rel-t%d", i))
	}

	var releasesJSON strings.Builder
	for i, id := range releaseIDs {
		if i > 0 {
			releasesJSON.WriteString(",")
		}
		status := "Bootleg"
		if i == 0 {
			status = "Official"
		}
		fmt.Fprintf(&releasesJSON, `{"id": "%s", "status": "%s"}`, id, status)
	}

	mux.HandleFunc("/release-group/rg-teto", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Album Teto",
			"first-release-date": "2005-01-01",
			"genres": [],
			"releases": [` + releasesJSON.String() + `]
		}`))
	})

	for _, id := range releaseIDs {
		id := id
		mux.HandleFunc("/release/"+id, func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&chamadas, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"media": [{"position": 1, "tracks": [
					{"position": 1, "title": "Faixa Qualquer", "artist-credit": [],
					 "recording": {"id": "rec-` + id + `", "title": "Faixa Qualquer", "isrcs": [], "artist-credit": []}}
				]}]
			}`))
		})
	}

	return httptest.NewServer(mux), &chamadas
}

// TestBuscarFaixasComAlvoRespeitaTeto confirms the ceiling maxReleasesTentadosPorAlvo: with 17
// releases in the release-group and NONE containing the target track, the provider tries at most
// maxReleasesTentadosPorAlvo (never the 17) before degrading.
func TestBuscarFaixasComAlvoRespeitaTeto(t *testing.T) {
	mb, chamadas := servidorTetoFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-teto", "Album Teto",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Musica Que Nao Existe Em Lugar Nenhum"}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true (o release-group bate por TITULO -- quem decide se a FAIXA foi achada e enrich.escolherFaixaAlvo, nao este provider)")
	}
	if got := atomic.LoadInt32(chamadas); got != maxReleasesTentadosPorAlvo {
		t.Fatalf("chamadas a /release/{id} = %d, esperava exatamente %d (teto respeitado, release-group tinha 17 releases)", got, maxReleasesTentadosPorAlvo)
	}
}

// TestBuscarFaixasComAlvoSemFalhaDeRedeNaoFazSegundaPassada confirms the acceptance criterion of NOT
// spending an extra call for nothing: in the Freestyler scenario (servidorFreestylerFake, no release
// fails on the network -- rel-maxi answers 200 without matching the target, rel-full answers 200 and
// matches), the 2nd pass never happens because `falhadas` stays empty -- only 2 HTTP calls to
// /release/{id} in total (one per release, none repeated).
func TestBuscarFaixasComAlvoSemFalhaDeRedeNaoFazSegundaPassada(t *testing.T) {
	mb, chamadasFaixas := servidorFreestylerFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-freestyler", "Freestyler",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Freestyler"}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true")
	}
	achouFaixa := false
	for _, fx := range album.Faixas {
		if fx.Title == "Freestyler" {
			achouFaixa = true
		}
	}
	if !achouFaixa {
		t.Fatalf("faixa 'Freestyler' nao encontrada: %+v", album.Faixas)
	}
	if got := atomic.LoadInt32(chamadasFaixas); got != 2 {
		t.Fatalf("chamadas a /release/{id} = %d, esperava exatamente 2 (1 por release, sem 2a passada -- nenhuma falhou de rede)", got)
	}
}

// servidorRedeFake builds a release-group ("rg-rede") with 2 releases: "rel-official" (status
// "Official", WITHOUT the target track -- it answers 200 always, first try) and "rel-target" (status
// "Bootleg", WITH the target track -- its HTTP status depends on `statusPorChamada`, indexed by the
// NUMBER of the call specifically to /release/rel-target, 1-based; calls beyond the size of the slice
// repeat the last item). That allows simulating, on the SAME release, "failure in the first N calls
// (exhausting tentativasMB in the 1st pass) and success in a later call (the one of the 2nd pass)" --
// the real scenario of Bomfunk MC's/Freestyler. It also returns the call counter to
// /release/rel-target, so the tests can confirm exactly how many physical attempts happened.
func servidorRedeFake(t *testing.T, statusPorChamadaAlvo []int) (*httptest.Server, *int32) {
	t.Helper()
	var chamadasAlvo int32
	mux := http.NewServeMux()

	mux.HandleFunc("/artist/artist-rede", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "Artista Rede",
			"release-groups": [
				{"id": "rg-rede", "title": "Album Rede", "first-release-date": "2000-01-01"}
			]
		}`))
	})

	mux.HandleFunc("/release-group/rg-rede", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Album Rede",
			"first-release-date": "2000-01-01",
			"genres": [],
			"releases": [
				{"id": "rel-official", "status": "Official"},
				{"id": "rel-target", "status": "Bootleg"}
			]
		}`))
	})

	mux.HandleFunc("/release/rel-official", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [{"position": 1, "tracks": [
				{"position": 1, "title": "Faixa Sem Relacao", "artist-credit": [],
				 "recording": {"id": "rec-sem-relacao", "title": "Faixa Sem Relacao", "isrcs": [], "artist-credit": []}}
			]}]
		}`))
	})

	mux.HandleFunc("/release/rel-target", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&chamadasAlvo, 1)
		idx := int(n) - 1
		if idx >= len(statusPorChamadaAlvo) {
			idx = len(statusPorChamadaAlvo) - 1
		}
		status := statusPorChamadaAlvo[idx]
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [{"position": 1, "tracks": [
				{"position": 5, "title": "Freestyler", "artist-credit": [],
				 "recording": {"id": "rec-full-freestyler", "title": "Freestyler", "isrcs": [], "artist-credit": []}}
			]}]
		}`))
	})

	return httptest.NewServer(mux), &chamadasAlvo
}

// TestBuscarFaixasComAlvoSegundaPassadaResgataReleaseAposFalhaTransitoria is the main acceptance
// criterion of the second-pass behavior: "rel-target" (the release that contains the target track)
// exhausts the tentativasMB=3 attempts of the 1st pass (503 on all of them), but answers 200 on the
// 1st call of the 2nd pass -- exactly the real scenario (Bomfunk MC's/Freestyler) that motivated it.
// Before the correction, that release was discarded forever on the 1st failure and the final result
// would NEVER match the target, even with the right release existing and answering shortly after.
func TestBuscarFaixasComAlvoSegundaPassadaResgataReleaseAposFalhaTransitoria(t *testing.T) {
	mb, chamadasAlvo := servidorRedeFake(t, []int{
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable, // exhausts the 1st pass
		http.StatusOK, // success on the 1st attempt of the 2nd pass
	})
	defer mb.Close()
	c := clientComBackoffFake(mb.URL)

	album, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-rede", "Album Rede",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Freestyler"}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true")
	}
	achouFaixa := false
	for _, fx := range album.Faixas {
		if fx.Title == "Freestyler" {
			achouFaixa = true
		}
	}
	if !achouFaixa {
		t.Fatalf("faixa 'Freestyler' nao encontrada -- a 2a passada deveria ter resgatado rel-target: %+v", album.Faixas)
	}
	if got := atomic.LoadInt32(chamadasAlvo); got != 4 {
		t.Fatalf("chamadas a /release/rel-target = %d, esperava 4 (3 falhas esgotando a 1a passada + 1 sucesso na 2a)", got)
	}
}

// TestBuscarFaixasComAlvoFalhaPersistenteNasDuasPassadasDegradaSemPanico covers the worst case:
// "rel-target" fails in ALL the attempts of BOTH passes (too persistent a 503 -- rare, but it must not
// hang nor enter an infinite loop). The final result must degrade exactly as today: it uses the last
// release that answered successfully (rel-official), without trying a 3rd pass.
func TestBuscarFaixasComAlvoFalhaPersistenteNasDuasPassadasDegradaSemPanico(t *testing.T) {
	mb, chamadasAlvo := servidorRedeFake(t, []int{http.StatusServiceUnavailable})
	defer mb.Close()
	c := clientComBackoffFake(mb.URL)

	album, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-rede", "Album Rede",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Freestyler"}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true (o release-group bate por titulo)")
	}
	if len(album.Faixas) != 1 || album.Faixas[0].Title != "Faixa Sem Relacao" {
		t.Fatalf("faixas = %+v, esperava degradar para as de rel-official (unica release que respondeu com sucesso)", album.Faixas)
	}
	// tentativasMB=3 per pass, 2 passes (the 1st with all the ids, the 2nd only with rel-target, which
	// is the only one that failed on the network) -- never a 3rd.
	if got := atomic.LoadInt32(chamadasAlvo); got != int32(2*tentativasMB) {
		t.Fatalf("chamadas a /release/rel-target = %d, esperava exatamente %d (tentativasMB x 2 passadas, sem 3a passada)", got, 2*tentativasMB)
	}
}

// servidorRecordingNaoOfficialFake covers the gap of the PRECISE path (BuscarAlbumDaFaixa): the
// recording "rec-y-precisa" belongs to the release "rel-y-bootleg", which is NOT the "Official" one of
// the release-group "rg-y" (that one is "rel-y-official", without the track) -- it proves that, by
// forwarding AlvoFaixaAlbum{RecordingMBID}, the provider tries the REAL release of the recording
// instead of getting stuck on the wrong "Official" one.
func servidorRecordingNaoOfficialFake(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/recording/rec-y-precisa", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"releases": [
				{"id": "rel-y-bootleg", "status": "Bootleg", "release-group": {"id": "rg-y"}}
			]
		}`))
	})

	mux.HandleFunc("/release-group/rg-y", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Album Y",
			"first-release-date": "2010-01-01",
			"genres": [],
			"releases": [
				{"id": "rel-y-official", "status": "Official"},
				{"id": "rel-y-bootleg", "status": "Bootleg"}
			]
		}`))
	})

	mux.HandleFunc("/release/rel-y-official", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [{"position": 1, "tracks": [
				{"position": 1, "title": "Outra Faixa", "artist-credit": [],
				 "recording": {"id": "rec-y-outra", "title": "Outra Faixa", "isrcs": [], "artist-credit": []}}
			]}]
		}`))
	})

	mux.HandleFunc("/release/rel-y-bootleg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"media": [{"position": 1, "tracks": [
				{"position": 1, "title": "A Faixa Certa", "artist-credit": [],
				 "recording": {"id": "rec-y-precisa", "title": "A Faixa Certa", "isrcs": [], "artist-credit": []}}
			]}]
		}`))
	})

	return httptest.NewServer(mux)
}

// TestBuscarAlbumDaFaixaTentaReleaseQueContemORecordingQuandoNaoEhOfficial covers the 2nd
// acceptance criterion: the PRECISE path (BuscarAlbumDaFaixa) now finds the track even when it lives
// in a release that is NOT the "Official" one of the release-group -- before this correction,
// montarAlbum (without alvoFaixa) would always choose rel-y-official through escolherRelease, and the
// track would never be found.
func TestBuscarAlbumDaFaixaTentaReleaseQueContemORecordingQuandoNaoEhOfficial(t *testing.T) {
	mb := servidorRecordingNaoOfficialFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album, achou, err := c.BuscarAlbumDaFaixa(context.Background(), "rec-y-precisa", nil)
	if err != nil {
		t.Fatalf("BuscarAlbumDaFaixa: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true")
	}

	achouFaixa := false
	for _, fx := range album.Faixas {
		if fx.MBID == "rec-y-precisa" {
			achouFaixa = true
		}
	}
	if !achouFaixa {
		t.Fatalf("faixa com MBID 'rec-y-precisa' nao encontrada -- rel-y-official nao a contem, deveria ter tentado rel-y-bootleg (a release REAL do recording): %+v", album.Faixas)
	}
}

// TestReleaseContemFaixaAlvoGateDeDuracaoRejeitaDivergencia covers the duration gate of round 2: the
// release has a track whose TITLE matches enough to pass the threshold (the same text as the target),
// but the duration diverges much more than the tolerance
// (matching.ToleranciaDuracaoSegundos, 3s) -- the gate must REJECT (false) even with the title
// matching, because it is an additional safety lock, never more lenient than the title criterion
// alone.
func TestReleaseContemFaixaAlvoGateDeDuracaoRejeitaDivergencia(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Song Title", MBID: "rec-divergente", TrackNumber: 1, DuracaoMs: 200_000},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Song Title", DuracaoMs: 180_000} // diff = 20s >> 3s

	if releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = true, esperava false (titulo bate mas duracao diverge bem alem da tolerancia)")
	}
}

// TestReleaseContemFaixaAlvoGateDeDuracaoPuladoQuandoDesconhecida confirms that the duration gate is
// SKIPPED (without regression) when either side does not report a known duration (0) -- the same
// scenario of diverging title/duration as the previous test, but with AlvoFaixaAlbum.DuracaoMs == 0:
// the result must be identical to the behavior from before this change (accepted by title alone).
func TestReleaseContemFaixaAlvoGateDeDuracaoPuladoQuandoDesconhecida(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Song Title", MBID: "rec-divergente", TrackNumber: 1, DuracaoMs: 200_000},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Song Title", DuracaoMs: 0} // local file duration unknown

	if !releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = false, esperava true (duracao desconhecida de um dos lados deve pular o gate, sem regressao)")
	}
}

// TestReleaseContemFaixaAlvoDesempataPorDuracaoQuandoTituloEmpata covers the real case of the
// Freestyler scenario: the release has TWO tracks with the EXACT title of the target (a tie on the
// highest score) -- one with a duration compatible with the target, the other not. The duration must
// break the tie towards the compatible track, without rejecting the whole release.
func TestReleaseContemFaixaAlvoDesempataPorDuracaoQuandoTituloEmpata(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Freestyler", MBID: "rec-errada", TrackNumber: 1, DuracaoMs: 247_000},
		{Title: "Freestyler (Skillsters remix)", MBID: "rec-remix", TrackNumber: 2, DuracaoMs: 183_000},
		{Title: "Freestyler", MBID: "rec-certa", TrackNumber: 5, DuracaoMs: 306_000},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Freestyler", DuracaoMs: 306_364} // diff = 364ms << 3s

	if !releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = false, esperava true (duracao deveria desempatar as duas faixas 'Freestyler' para a compativel)")
	}
}

// TestReleaseContemFaixaAlvoNaoDesempataQuandoDuracaoDoAlvoDesconhecida covers the same title tie as
// the previous test, but with the duration of the target unknown (0) -- without a signal to decide
// between the two "Freestyler" tracks, it must reject (safe behavior, without regression).
func TestReleaseContemFaixaAlvoNaoDesempataQuandoDuracaoDoAlvoDesconhecida(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Freestyler", MBID: "rec-errada", TrackNumber: 1, DuracaoMs: 247_000},
		{Title: "Freestyler", MBID: "rec-certa", TrackNumber: 5, DuracaoMs: 306_000},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Freestyler", DuracaoMs: 0}

	if releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = true, esperava false (duracao do alvo desconhecida nao pode desempatar titulo duplicado)")
	}
}

// TestReleaseContemFaixaAlvoNaoDesempataQuandoAmbasCompativeis covers the title tie with BOTH tracks
// inside the duration tolerance -- it stays ambiguous (a tie on the duration as well), it never
// chooses arbitrarily.
func TestReleaseContemFaixaAlvoNaoDesempataQuandoAmbasCompativeis(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Freestyler", MBID: "rec-a", TrackNumber: 1, DuracaoMs: 306_100},
		{Title: "Freestyler", MBID: "rec-b", TrackNumber: 5, DuracaoMs: 306_900},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Freestyler", DuracaoMs: 306_500}

	if releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = true, esperava false (ambas as faixas empatadas ficam dentro da tolerancia, continua ambiguo)")
	}
}

// TestBuscarAlbumPorTitulo covers the COMPILATION/SOUNDTRACK path: it resolves the album by the title
// (among homonyms of different artists), confirms the target track in the tracklist and returns the
// artist-credit of the source.
func TestBuscarAlbumPorTitulo(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	// "Album One (Deluxe Edition)" is the name the TAG brings; the canonical release-group is "Album
	// One" -- the normalization is what makes this path match.
	ac, achou, err := c.BuscarAlbumPorTitulo(context.Background(), "Album One (Deluxe Edition)", provider.AlvoFaixaAlbum{FaixaTitulo: "Track A"}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorTitulo: %v", err)
	}
	if !achou {
		t.Fatal("achou = false, esperava true")
	}
	if ac.Album.MBID != "rg-1" || len(ac.Album.Faixas) != 2 {
		t.Fatalf("album inesperado: %+v", ac.Album)
	}
	if ac.ArtistaMBID != "artist-1" || ac.ArtistaNome != "Test Artist" {
		t.Fatalf("artist-credit = (%q,%q), quero (artist-1, Test Artist)", ac.ArtistaMBID, ac.ArtistaNome)
	}
}

// TestBuscarAlbumPorTituloSemFaixaNaTracklist covers the EVIDENCE requirement: a release-group with
// the right title but WITHOUT the target track is not accepted (the homonym candidate rg-9 is
// assembled and discarded, and the result is ok=false).
func TestBuscarAlbumPorTituloSemFaixaNaTracklist(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	if _, achou, err := c.BuscarAlbumPorTitulo(context.Background(), "Album One", provider.AlvoFaixaAlbum{FaixaTitulo: "Musica Que Nao Existe"}, nil); err != nil {
		t.Fatalf("BuscarAlbumPorTitulo: %v", err)
	} else if achou {
		t.Fatal("achou = true, esperava false (nenhuma release contém a faixa-alvo)")
	}
}

// TestBuscarAlbumPorNomeNormalizaEdicao covers the normalization on the path by ARTIST: the tag
// brings "(Deluxe Edition)" and the canonical release-group does not.
func TestBuscarAlbumPorNomeNormalizaEdicao(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album, achou, err := c.BuscarAlbumPorNome(context.Background(), "artist-1", "Album One (Deluxe Edition)", provider.AlvoFaixaAlbum{}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorNome: %v", err)
	}
	if !achou || album.MBID != "rg-1" {
		t.Fatalf("album = %+v (achou=%v), quero rg-1", album, achou)
	}
}

// TestBuscarAlbumPorTituloEscopaPorArtista covers a production finding: the search by title without
// the artist returned homonyms of OTHER artists and the right album stayed out of the top (a real
// case: "Rio (Collector's Edition)" brought ten release-groups called "Collector's Edition"). The
// clause becomes `releasegroup:"<normalized>" AND artistname:"<album-artist>"`.
func TestBuscarAlbumPorTituloEscopaPorArtista(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		queries = append(queries, q)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(q, "artistname:") {
			_, _ = w.Write([]byte(`{"release-groups": [
				{"id": "rg-rio", "title": "Rio", "artist-credit": [{"name": "Duran Duran", "artist": {"id": "artist-dd", "name": "Duran Duran"}}]}
			]}`))
			return
		}
		_, _ = w.Write([]byte(`{"release-groups": []}`))
	}))
	defer srv.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/release-group/rg-rio", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title": "Rio", "releases": [{"id": "rel-rio", "status": "Official"}]}`))
	})
	_ = mux

	c := novoClientDeTeste(srv.URL, "")
	ac, achou, err := c.BuscarAlbumPorTitulo(context.Background(), "Rio (Collector's Edition)",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Lonely in Your Nightmare", FaixaDisco: 1, FaixaNumero: 3, ArtistaNome: "Duran Duran"}, nil)
	if err != nil {
		t.Fatalf("BuscarAlbumPorTitulo: %v", err)
	}
	if len(queries) == 0 {
		t.Fatal("nenhuma busca foi feita")
	}
	primeira := queries[0]
	if !strings.Contains(primeira, `releasegroup:"Rio"`) {
		t.Fatalf("query = %q, quero a frase normalizada releasegroup:\"Rio\" (a tag trazia \"(Collector's Edition)\")", primeira)
	}
	if !strings.Contains(primeira, `AND artistname:"Duran Duran"`) {
		t.Fatalf("query = %q, quero a clausula artistname escopando o album-artista", primeira)
	}
	_ = achou
	_ = ac
}

// TestBuscarAlbumPorTituloSemArtistaEmCompilacao confirms that a compilation is NOT scoped by
// artist: the generic credit ("Various Artists") identifies nothing and the title is the only signal
// (the path created for soundtracks).
func TestBuscarAlbumPorTituloSemArtistaEmCompilacao(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"release-groups": [
			{"id": "rg-tw", "title": "Twilight: Original Motion Picture Soundtrack", "artist-credit": [{"name": "Various Artists", "artist": {"id": "artist-va", "name": "Various Artists"}}]}
		]}`))
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "")
	// The tracklist is not the target here: the test only looks at the query sent.
	_, _, _ = c.BuscarAlbumPorTitulo(context.Background(), "Twilight Original Motion Picture Soundtrack (International Special Edition)",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Decode", ArtistaNome: "Разные исполнители"}, nil)
	if len(queries) == 0 {
		t.Fatal("nenhuma busca foi feita")
	}
	if strings.Contains(queries[0], "artistname:") {
		t.Fatalf("query = %q, nao quero clausula de artista para compilacao", queries[0])
	}
}

// TestBuscarAlbumPorTituloCaiParaBuscaSemArtista covers the degradation: when the search scoped by
// artist returns nothing (the credit of the source differs from the tag), the search repeated without
// the artist still resolves the album -- without an error and without losing the case.
func TestBuscarAlbumPorTituloCaiParaBuscaSemArtista(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		queries = append(queries, q)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(q, "artistname:") {
			_, _ = w.Write([]byte(`{"release-groups": []}`))
			return
		}
		_, _ = w.Write([]byte(`{"release-groups": [
			{"id": "rg-1", "title": "The Hurting", "artist-credit": [{"name": "Tears for Fears", "artist": {"id": "artist-tff", "name": "Tears for Fears"}}]}
		]}`))
	}))
	defer srv.Close()

	c := novoClientDeTeste(srv.URL, "")
	if _, _, err := c.BuscarAlbumPorTitulo(context.Background(), "The Hurting (Deluxe Edition)",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Mad World", ArtistaNome: "Tears for Fears"}, nil); err != nil {
		t.Fatalf("BuscarAlbumPorTitulo: %v", err)
	}
	if len(queries) < 2 {
		t.Fatalf("queries = %v, quero a 2a tentativa sem artista", queries)
	}
	if strings.Contains(queries[1], "artistname:") {
		t.Fatalf("2a query = %q, quero SEM artista", queries[1])
	}
}

// TestReleaseContemFaixaAlvoDesempataPorDisco covers the new tiebreak: the same track appears on disc
// 1 and on disc 2 of a reissue, with identical titles; the position (disc, track) of the file chooses
// the right one.
func TestReleaseContemFaixaAlvoDesempataPorDisco(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Pale Shelter", MBID: "rec-d1", TrackNumber: 2, DiscNumber: 1, DiscTotal: 2, DuracaoMs: 261_000},
		{Title: "Pale Shelter", MBID: "rec-d2", TrackNumber: 1, DiscNumber: 2, DiscTotal: 2, DuracaoMs: 261_000},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Pale Shelter", FaixaDisco: 2, FaixaNumero: 1}
	if !releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = false, esperava true (a posicao disco 2 / faixa 1 existe)")
	}
	// Without the position, the tie stays unresolved: it rejects (it never applies a "similar
	// one").
	if releaseContemFaixaAlvo(faixas, provider.AlvoFaixaAlbum{FaixaTitulo: "Pale Shelter"}) {
		t.Fatal("releaseContemFaixaAlvo = true sem posicao, esperava false (empate ambiguo)")
	}
}
