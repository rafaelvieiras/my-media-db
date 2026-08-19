package musicbrainz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// novoClientDeTeste monta um Client apontando para servidores fake (httptest.Server),
// SEM nenhum sleep real do rate limiter (intervalo=0) — os testes deste pacote nunca
// batem na rede real (MusicBrainz/Cover Art Archive), e o comportamento do rate limiter
// em si é coberto isoladamente por ratelimit_test.go.
func novoClientDeTeste(mbURL, caaURL string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    mbURL,
		caaBaseURL: caaURL,
		lim:        novoLimitador(0),
	}
}

// respostasMB é o roteador do servidor fake do MusicBrainz: cada chave é "PATH?QUERY_ORDENADA"
// simplificado — na prática comparamos path + presença/valor do param "inc" (os únicos que
// variam entre as rotas usadas pelo client).
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
		if r.URL.Query().Get("inc") == "release-groups+annotation" {
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
		// BuscarDetalhe: sem inc.
		_, _ = w.Write([]byte(`{"name": "Test Artist"}`))
	})

	mux.HandleFunc("/artist/does-not-exist", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	mux.HandleFunc("/release-group/rg-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"genres": [{"name": "rock", "count": 5}, {"name": "pop", "count": 2}],
			"releases": [{"id": "rel-1", "status": "Official"}]
		}`))
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

	// rel-2 tem uma faixa com artista DIVERGENTE do artista do album (feat./colaboracao),
	// em 2 discos (testa DiscNumber/DiscTotal).
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

	// album2 (rg-2/rel-2): faixa 0 tem artista DIVERGENTE (feat.); faixa 1 esta no disco 2
	// de 2 (DiscTotal=2).
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

// TestBuscarArtistaArvoreAlbumPersistentementeIndisponivelDegradaSemAbortar cobre o achado
// real da validação em produção (Duran Duran, MBID real): o servidor público do
// MusicBrainz é instável o bastante que, numa árvore com dezenas de álbuns, é esperado que
// a chamada de detalhe de UM release-group específico esgote todas as tentativas de retry.
// Isso NÃO pode abortar a árvore inteira -- o álbum problemático deve degradar pra um
// shell sem gênero/faixas, e os demais álbuns (antes e depois dele) continuam completos.
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
		// Sempre 503 -- simula a instabilidade real do servidor público. getMB vai
		// esgotar tentativasMB tentativas para ESTE release-group especificamente.
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

func TestMontarAlbumReleaseGroupSemReleases(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	album := c.montarAlbum(context.Background(), "rg-empty", "Album Vazio", "2020")
	if album.Name != "Album Vazio" || album.MBID != "rg-empty" {
		t.Fatalf("album inesperado: %+v", album)
	}
	if len(album.Faixas) != 0 {
		t.Fatalf("esperava 0 faixas para release-group sem releases, teve %d", len(album.Faixas))
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

// TestImplementaInterfaces é uma checagem em tempo de compilacao (nao roda nada) de que o
// Client satisfaz as 3 interfaces esperadas: core provider.Provider, provider.MusicaProvider
// e provider.ArteProvider.
func TestImplementaInterfaces(t *testing.T) {
	var _ provider.Provider = (*Client)(nil)
	var _ provider.MusicaProvider = (*Client)(nil)
	var _ provider.ArteProvider = (*Client)(nil)
	var _ provider.FaixaResolverProvider = (*Client)(nil)
}

// servidorRecordingFake é um httptest.Server hermético dedicado a
// ResolverArtistaDaFaixa (briefing 0107): responde GET /recording/{mbid}?inc=
// artist-credits com um artist-credit fixo para "rec-com-artista", uma lista
// VAZIA de artist-credit para "rec-sem-artista" (dado incompleto na fonte) e
// 404 para qualquer outro id.
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

// TestResolverArtistaDaFaixa cobre o caminho feliz: recording com 1
// artist-credit devolve o MBID do artista.
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

// TestResolverArtistaDaFaixaSemArtistCredit cobre o dado incompleto na fonte
// (recording existe mas sem NENHUM artist-credit): erro genérico, nunca um
// MBID vazio silencioso.
func TestResolverArtistaDaFaixaSemArtistCredit(t *testing.T) {
	srv := servidorRecordingFake(t)
	defer srv.Close()
	c := novoClientDeTeste(srv.URL, "")

	if _, err := c.ResolverArtistaDaFaixa(context.Background(), "rec-sem-artista"); err == nil {
		t.Fatal("ResolverArtistaDaFaixa deveria falhar (recording sem artist-credit)")
	}
}

// TestResolverArtistaDaFaixaNaoEncontrada cobre o 404: erro-sentinela
// provider.ErrNaoEncontrado (errors.Is), mesmo contrato dos demais métodos.
func TestResolverArtistaDaFaixaNaoEncontrada(t *testing.T) {
	srv := servidorRecordingFake(t)
	defer srv.Close()
	c := novoClientDeTeste(srv.URL, "")

	_, err := c.ResolverArtistaDaFaixa(context.Background(), "rec-inexistente")
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Errorf("err = %v, quero errors.Is(err, provider.ErrNaoEncontrado)", err)
	}
}

// servidorContadorMB cria um httptest.Server que conta as chamadas a `path` e devolve, para
// a n-ésima chamada (1-based), `statusPorChamada[n-1]` -- chamadas além do tamanho do slice
// repetem o ÚLTIMO status da lista (útil para "sempre 503"). Um status 200 devolve um corpo
// mínimo válido para BuscarDetalhe/MapearDetalhe; qualquer outro status devolve corpo vazio
// (getMB não olha o corpo de erro). Usado para exercitar o retry de getMB de forma
// determinística, sem depender da API pública de verdade.
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

// clientComBackoffFake monta um Client de teste (sem sleep do rate-limiter em si, como
// novoClientDeTeste) e SUBSTITUI o `dormir` do limitador compartilhado por um no-op: o loop
// de retry de getMB usa `c.lim.dormirFn` para o backoff entre tentativas (500ms x tentativa
// -- ver comentário de backoffMB em musicbrainz.go), e sem essa injeção os testes de
// esgotamento de tentativas dormiriam de verdade (500ms + 1000ms = 1.5s só no caso 2).
func clientComBackoffFake(mbURL string) *Client {
	c := novoClientDeTeste(mbURL, "")
	c.lim.dormir = func(time.Duration) {}
	return c
}

// TestGetMBRetrySucessoAposFalhasTransitorias cobre o caso feliz do retry (briefing: API
// pública do MusicBrainz devolve 503 "currently busy" com frequência real ~1 em 3
// chamadas): as 2 primeiras tentativas veem 503, a 3ª (dentro de tentativasMB=3) tem
// sucesso -- getMB deve devolver o resultado da tentativa que deu certo, não um erro.
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

// TestGetMBRetryEsgotaTentativas cobre o caso de 503 persistente: getMB deve tentar
// exatamente tentativasMB vezes (nunca mais, nunca menos) e devolver erro -- não travar
// nem entrar em loop infinito.
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

// TestGetMB404NaoTentaNovamente confirma que um 404 (recurso realmente inexistente, não
// instabilidade do servidor) faz getMB devolver provider.ErrNaoEncontrado na PRIMEIRA
// tentativa, sem retry -- transitorioMB nunca considera 404 transitório.
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

// TestGetMBStatusGenericoNaoTentaNovamente confirma que um status != 200/404/502/503/504
// (ex.: 500) é erro genérico SEM retry -- só os 3 status documentados como instabilidade do
// servidor público (502/503/504) disparam o retry; qualquer outra coisa é tratada como
// definitiva.
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
