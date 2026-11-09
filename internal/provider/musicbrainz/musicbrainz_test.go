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
		// BuscarArtistaArvore pede "release-groups+annotation"; BuscarAlbumPorNome
		// (briefing 0115) pede só "release-groups" (sem annotation, que não usa) --
		// os dois recebem a mesma lista de release-groups.
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
		// BuscarDetalhe: sem inc.
		_, _ = w.Write([]byte(`{"name": "Test Artist"}`))
	})

	mux.HandleFunc("/artist/does-not-exist", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	// busca TEXTUAL de release-group (briefing 0136, BuscarAlbumPorTitulo): devolve
	// "Album One" (rg-1, do artist-1) e um homônimo de OUTRO artista que NÃO contém a
	// faixa-alvo -- prova que o caminho por título escolhe por evidência da tracklist.
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

	// rotas de /recording -- usadas por BuscarAlbumDaFaixa (briefing 0115): "rec-1"
	// resolve para o release-group "rg-1" (via "rel-1", já servido acima); "rec-sem-rg"
	// tem uma release SEM release-group aninhado (dado incompleto na fonte).
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

// servidorDetalheMusicaFake serve só /release-group/{id} e /recording/{id} SEM exigir
// nenhum `inc` (diferente de servidorMBFake, cujo /recording/rec-1 valida
// inc=releases+release-groups para outro consumidor, BuscarAlbumDaFaixa) -- o caminho
// de BuscarDetalhe pra album/faixa não pede `inc` nenhum, só o título.
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

// TestBuscarDetalheAlbumEFaixaDespachaPorTipo cobre o bug achado na validação em
// produção de 20/08/2026: BuscarDetalhe pra tipo=album/faixa chamava sempre /artist/
// (sourceID errado -- pra esses dois tipos o sourceID é MBID de release-group/recording,
// não de artista) e batia em 404/502, quebrando "Baixar Metadados". Confirma o dispatch
// correto de endpoint E o remap "title"→"name" (remapParaNome) que deixa MapearDetalhe
// funcionar sem mudar a assinatura da interface core provider.Provider.
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

// TestBuscarAlbumDaFaixa cobre o caminho PRECISO (briefing 0115): resolve o
// release-group a partir do MBID de recording "rec-1" (via "rel-1"/status
// Official) e monta SÓ esse álbum -- nome/ano vêm do PRÓPRIO release-group
// (rg.Title/rg.FirstReleaseDate), já que o chamador não os tem de antemão.
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

// TestBuscarAlbumDaFaixaSemReleaseGroup cobre o dado incompleto na fonte
// (recording tem releases, mas NENHUMA com release-group aninhado): ok=false,
// graça -- não é erro.
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

// TestBuscarAlbumDaFaixaRecordingNaoEncontrado cobre o 404: erro-sentinela
// provider.ErrNaoEncontrado (errors.Is), mesmo contrato dos demais métodos.
func TestBuscarAlbumDaFaixaRecordingNaoEncontrado(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, _, err := c.BuscarAlbumDaFaixa(context.Background(), "rec-inexistente-album", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
}

// TestBuscarAlbumDaFaixaMbidVazio cobre a validação de entrada (sem ida à
// rede): erro imediato, nunca um GET com path vazio.
func TestBuscarAlbumDaFaixaMbidVazio(t *testing.T) {
	c := novoClientDeTeste("http://unused.invalid", "")
	if _, _, err := c.BuscarAlbumDaFaixa(context.Background(), "", nil); err == nil {
		t.Fatal("esperava erro (mbid de faixa vazio)")
	}
}

// TestBuscarAlbumPorNome cobre o caminho TEXTUAL (briefing 0115): lista os
// release-groups do artista "artist-1" (rg-1 "Album One", rg-2 "Album Two") e
// monta SÓ o que bate com "Album One" -- rg-2 nunca é montado (só listado).
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

// TestBuscarAlbumPorNomeSemCandidato cobre nome sem correspondência
// plausível (abaixo do limiar matching.LimiarAutoMatch): ok=false, graça.
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

// TestBuscarAlbumPorNomeArtistaNaoEncontrado cobre o 404 do artista.
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

// TestBuscarAlbumPorMbid cobre o caminho DIRETO (briefing 0115 Parte 2): o MBID já
// é o do release-group ("rg-1"), sem passo de resolução prévio -- monta o álbum
// com suas faixas, mesmo shape de BuscarAlbumDaFaixa/BuscarAlbumPorNome.
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

// TestBuscarAlbumPorMbidNaoEncontrado cobre o 404: ao contrário de montarAlbum (só
// usado DEPOIS de uma resolução prévia, que degrada em silêncio), este método NÃO
// tem resolução prévia -- por isso precisa propagar provider.ErrNaoEncontrado
// (errors.Is) em vez de devolver um shell vazio indistinguível de "achou, sem dado".
func TestBuscarAlbumPorMbidNaoEncontrado(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	_, err := c.BuscarAlbumPorMbid(context.Background(), "rg-inexistente", nil)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("err = %v, esperava provider.ErrNaoEncontrado", err)
	}
}

// TestBuscarAlbumPorMbidVazio cobre a validação de entrada (sem ida à rede).
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

// TestImplementaInterfaces é uma checagem em tempo de compilacao (nao roda nada) de que o
// Client satisfaz as interfaces esperadas: core provider.Provider, provider.MusicaProvider
// (arvore, legado), provider.MusicaAlbumProvider (seletiva, briefing 0115),
// provider.ArteProvider e provider.FaixaResolverProvider.
func TestImplementaInterfaces(t *testing.T) {
	var _ provider.Provider = (*Client)(nil)
	var _ provider.MusicaProvider = (*Client)(nil)
	var _ provider.MusicaAlbumProvider = (*Client)(nil)
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

// servidorContadorCAA é o equivalente de servidorContadorMB para o Cover Art Archive --
// devolve um corpo de imagens válido no status OK, corpo vazio nos demais (o conteúdo do
// erro não importa para getCAA).
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

// clientComBackoffFakeCAA é clientComBackoffFake, mas apontando caaBaseURL (não baseURL)
// para o servidor fake -- getCAA usa c.caaBaseURL, um host separado de getMB.
func clientComBackoffFakeCAA(caaURL string) *Client {
	c := novoClientDeTeste("", caaURL)
	c.lim.dormir = func(time.Duration) {}
	return c
}

// TestGetCAARetrySucessoAposFalhasTransitorias cobre o achado real da validação em
// produção: o redirect do Cover Art Archive pra archive.org (CDN distribuída,
// dnNNNNNN.ca.archive.org) deu timeout/erro em boa parte das chamadas de uma importação
// real de 25 álbuns -- diferente do 503 "busy" do MusicBrainz, getCAA tenta de novo em
// QUALQUER erro (não só 502/503/504), porque a CAA nem sempre falha com um status HTTP
// (timeout de rede não tem status nenhum).
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

// TestGetCAARetryEsgotaTentativas confirma que getCAA não tenta pra sempre: esgota
// tentativasMB e devolve erro (BuscarArtes propaga; quem chama já trata isso como
// enriquecimento best-effort, ver internal/enrich).
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

// servidorFreestylerFake monta o cenário REAL do briefing 0121 (Bomfunk MC's
// "Freestyler", achado ao validar o 0120 em produção): o release-group
// "rg-freestyler" tem 2 releases -- "rel-maxi" (status "Official", um
// maxi-single só com versões remixadas, SEM "Freestyler" puro) e "rel-full"
// (status diferente de Official, mas com a faixa "Freestyler" exata). Devolve
// também `chamadasFaixas`, que conta as chamadas aos endpoints /release/{id}
// (nunca /release-group/{id}) -- usado pelos testes que provam ausência de
// regressão de custo no caminho sem alvoFaixa.
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
		// Official listada PRIMEIRO no JSON de propósito -- ordenarReleasesPorPrioridade
		// não depende da ordem de entrada, só do status; e escolherRelease (caminho
		// zero-value) já a acharia de qualquer jeito.
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

// TestBuscarFaixasComAlvoTentaOutraReleaseQuandoOfficialNaoTemAFaixa é o critério de
// aceite principal do briefing 0121: a release "Official" (rel-maxi) NÃO tem a
// faixa "Freestyler" pura (só versões remixadas -- 89.5 de similaridade, abaixo do
// limiar de 92), mas "rel-full" (mesmo release-group) tem -- BuscarAlbumPorNome com
// `alvoFaixa` preenchido deve trazer as faixas de rel-full, não as de rel-maxi.
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

// TestBuscarFaixasComAlvoZeroValueMantemUmaSoChamada confirma que o caminho sem
// alvoFaixa (zero value) preserva o comportamento HISTÓRICO exato: só 1 chamada
// HTTP ao endpoint de faixas de release, mesmo release-group tendo outra release
// que conteria "Freestyler" -- ninguém pediu pra procurar, então não procura (zero
// regressão de custo).
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

// servidorTetoFake monta um release-group com 17 releases (mais que
// maxReleasesTentadosPorAlvo=15), NENHUMA contendo a faixa-alvo -- prova que
// buscarFaixasComAlvo respeita o teto em vez de iterar o release-group inteiro.
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

// TestBuscarFaixasComAlvoRespeitaTeto confirma o teto maxReleasesTentadosPorAlvo:
// com 17 releases no release-group e NENHUMA contendo a faixa-alvo, o provider tenta
// no máximo maxReleasesTentadosPorAlvo (nunca as 17) antes de degradar.
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

// TestBuscarFaixasComAlvoSemFalhaDeRedeNaoFazSegundaPassada confirma o critério de
// aceite do briefing 0123 de NAO gastar chamada extra à toa: no cenário do 0121
// (servidorFreestylerFake, nenhuma release falha de rede -- rel-maxi responde 200 sem
// bater o alvo, rel-full responde 200 e bate), a 2a passada nunca acontece porque
// `falhadas` fica vazia -- só 2 chamadas HTTP a /release/{id} no total (uma por
// release, nenhuma repetida).
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

// servidorRedeFake monta um release-group ("rg-rede") com 2 releases: "rel-official"
// (status "Official", SEM a faixa-alvo -- responde 200 sempre, de primeira) e
// "rel-target" (status "Bootleg", COM a faixa-alvo -- seu status HTTP depende de
// `statusPorChamada`, indexado pelo NÚMERO da chamada especificamente a
// /release/rel-target, 1-based; chamadas além do tamanho do slice repetem o último
// item). Isso permite simular, na MESMA release, "falha nas N primeiras chamadas
// (esgotando tentativasMB na 1a passada) e sucesso numa chamada posterior (a da 2a
// passada)" -- o cenário real do briefing 0123 (Bomfunk MC's/Freestyler). Devolve
// também o contador de chamadas a /release/rel-target, para os testes confirmarem
// exatamente quantas tentativas físicas aconteceram.
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

// TestBuscarFaixasComAlvoSegundaPassadaResgataReleaseAposFalhaTransitoria é o critério
// de aceite principal do briefing 0123: "rel-target" (a release que contém a
// faixa-alvo) esgota as tentativasMB=3 tentativas da 1a passada (503 em todas), mas
// responde 200 na 1a chamada da 2a passada -- exatamente o cenário real (Bomfunk
// MC's/Freestyler) que motivou o briefing. Antes da correção, essa release era
// descartada para sempre na 1a falha e o resultado final NUNCA bateria o alvo,
// mesmo a release certa existindo e respondendo pouco depois.
func TestBuscarFaixasComAlvoSegundaPassadaResgataReleaseAposFalhaTransitoria(t *testing.T) {
	mb, chamadasAlvo := servidorRedeFake(t, []int{
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable, // esgota a 1a passada
		http.StatusOK, // sucesso logo na 1a tentativa da 2a passada
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

// TestBuscarFaixasComAlvoFalhaPersistenteNasDuasPassadasDegradaSemPanico cobre o pior
// caso: "rel-target" falha em TODAS as tentativas de AMBAS as passadas (503
// persistente demais -- raro, mas o briefing exige não travar nem entrar em loop
// infinito). O resultado final deve degradar exatamente como hoje: usa a última
// release que respondeu com sucesso (rel-official), sem tentar uma 3a passada.
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
	// tentativasMB=3 por passada, 2 passadas (1a com todos os ids, 2a só com
	// rel-target, que e' o unico que falhou de rede) -- nunca uma 3a.
	if got := atomic.LoadInt32(chamadasAlvo); got != int32(2*tentativasMB) {
		t.Fatalf("chamadas a /release/rel-target = %d, esperava exatamente %d (tentativasMB x 2 passadas, sem 3a passada)", got, 2*tentativasMB)
	}
}

// servidorRecordingNaoOfficialFake cobre o gap do caminho PRECISO (BuscarAlbumDaFaixa):
// o recording "rec-y-precisa" pertence à release "rel-y-bootleg", que NÃO é a
// "Official" do release-group "rg-y" (essa é "rel-y-official", sem a faixa) --
// prova que, ao repassar AlvoFaixaAlbum{RecordingMBID}, o provider tenta a release
// REAL do recording em vez de travar na "Official" errada.
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

// TestBuscarAlbumDaFaixaTentaReleaseQueContemORecordingQuandoNaoEhOfficial cobre o
// 2º critério de aceite do briefing 0121: o caminho PRECISO (BuscarAlbumDaFaixa)
// agora acha a faixa mesmo quando ela mora numa release que NÃO é a "Official" do
// release-group -- antes desta correção, montarAlbum (sem alvoFaixa) escolheria
// sempre rel-y-official via escolherRelease, e a faixa nunca seria encontrada.
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

// TestReleaseContemFaixaAlvoGateDeDuracaoRejeitaDivergencia cobre o gate de duração
// da rodada 2 (briefing 0121 §8): a release tem uma faixa cujo TÍTULO bate o
// suficiente para passar do limiar (mesmo texto do alvo), mas a duração diverge
// muito mais do que a tolerância (matching.ToleranciaDuracaoSegundos, 3s) -- o gate
// deve REJEITAR (false) mesmo com o título batendo, porque é uma trava de segurança
// adicional, nunca mais permissiva do que o critério de título sozinho.
func TestReleaseContemFaixaAlvoGateDeDuracaoRejeitaDivergencia(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Song Title", MBID: "rec-divergente", TrackNumber: 1, DuracaoMs: 200_000},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Song Title", DuracaoMs: 180_000} // diff = 20s >> 3s

	if releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = true, esperava false (titulo bate mas duracao diverge bem alem da tolerancia)")
	}
}

// TestReleaseContemFaixaAlvoGateDeDuracaoPuladoQuandoDesconhecida confirma que o
// gate de duração é PULADO (sem regressão) quando qualquer um dos lados não informa
// duração conhecida (0) -- mesmo cenário de título/duração divergente do teste
// anterior, mas com AlvoFaixaAlbum.DuracaoMs == 0: o resultado deve ser idêntico ao
// comportamento de antes desta mudança (aceita só por título).
func TestReleaseContemFaixaAlvoGateDeDuracaoPuladoQuandoDesconhecida(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Song Title", MBID: "rec-divergente", TrackNumber: 1, DuracaoMs: 200_000},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Song Title", DuracaoMs: 0} // duracao do arquivo local desconhecida

	if !releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = false, esperava true (duracao desconhecida de um dos lados deve pular o gate, sem regressao)")
	}
}

// TestReleaseContemFaixaAlvoDesempataPorDuracaoQuandoTituloEmpata cobre o caso
// real do briefing 0124 (Bomfunk MC's/Freestyler): a release tem DUAS faixas com
// o título EXATO do alvo (empate no maior score) -- uma com duração compatível
// com o alvo, outra não. A duração deve desempatar para a faixa compatível, sem
// rejeitar a release inteira.
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

// TestReleaseContemFaixaAlvoNaoDesempataQuandoDuracaoDoAlvoDesconhecida cobre o
// mesmo empate de título do teste anterior, mas com a duração do alvo
// desconhecida (0) -- sem sinal para decidir entre as duas faixas "Freestyler",
// deve rejeitar (comportamento seguro, sem regressao).
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

// TestReleaseContemFaixaAlvoNaoDesempataQuandoAmbasCompativeis cobre o empate de
// título com AMBAS as faixas dentro da tolerância de duração -- continua
// ambíguo (empate também na duração), nunca escolhe arbitrariamente.
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

// TestBuscarAlbumPorTitulo cobre o caminho de COMPILAÇÃO/TRILHA (briefing 0136):
// resolve o álbum pelo título (dentre homônimos de artistas diferentes), confirma a
// faixa-alvo na tracklist e devolve o artist-credit da fonte.
func TestBuscarAlbumPorTitulo(t *testing.T) {
	mb := servidorMBFake(t)
	defer mb.Close()
	c := novoClientDeTeste(mb.URL, "")

	// "Album One (Deluxe Edition)" é o nome que a TAG traz; o release-group canônico
	// é "Album One" -- a normalização do briefing 0136 é o que faz esse caminho casar.
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

// TestBuscarAlbumPorTituloSemFaixaNaTracklist cobre a exigência de EVIDÊNCIA: um
// release-group com o título certo mas SEM a faixa-alvo não é aceito (o candidato
// homônimo rg-9 é montado e descartado, e o resultado é ok=false).
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

// TestBuscarAlbumPorNomeNormalizaEdicao cobre a normalização do briefing 0136 no
// caminho por ARTISTA: a tag traz "(Deluxe Edition)" e o release-group canônico não.
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

// TestBuscarAlbumPorTituloEscopaPorArtista cobre o achado de produção do briefing
// 0138: a busca por título sem artista devolvia homônimos de OUTROS artistas e o
// álbum certo ficava fora do topo (caso real: "Rio (Collector's Edition)" trazia dez
// release-groups chamados "Collector's Edition"). A cláusula vira
// `releasegroup:"<normalizado>" AND artistname:"<álbum-artista>"`.
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

// TestBuscarAlbumPorTituloSemArtistaEmCompilacao confirma que compilação NÃO
// escopa por artista: o credit genérico ("Various Artists") não identifica nada e o
// título é o único sinal (o caminho que o briefing 0136 criou para trilha).
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
	// A tracklist não é o alvo aqui: o teste só olha a query enviada.
	_, _, _ = c.BuscarAlbumPorTitulo(context.Background(), "Twilight Original Motion Picture Soundtrack (International Special Edition)",
		provider.AlvoFaixaAlbum{FaixaTitulo: "Decode", ArtistaNome: "Разные исполнители"}, nil)
	if len(queries) == 0 {
		t.Fatal("nenhuma busca foi feita")
	}
	if strings.Contains(queries[0], "artistname:") {
		t.Fatalf("query = %q, nao quero clausula de artista para compilacao", queries[0])
	}
}

// TestBuscarAlbumPorTituloCaiParaBuscaSemArtista cobre a degradação: quando a busca
// escopada por artista não devolve nada (o credit da fonte difere da tag), a busca
// repetida sem o artista ainda resolve o álbum -- sem erro e sem perder o caso.
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

// TestReleaseContemFaixaAlvoDesempataPorDisco cobre o desempate novo (briefing
// 0138): a mesma faixa aparece no disco 1 e no disco 2 de uma reedição, com títulos
// idênticos; a posição (disco, faixa) do arquivo escolhe a certa.
func TestReleaseContemFaixaAlvoDesempataPorDisco(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Pale Shelter", MBID: "rec-d1", TrackNumber: 2, DiscNumber: 1, DiscTotal: 2, DuracaoMs: 261_000},
		{Title: "Pale Shelter", MBID: "rec-d2", TrackNumber: 1, DiscNumber: 2, DiscTotal: 2, DuracaoMs: 261_000},
	}
	alvo := provider.AlvoFaixaAlbum{FaixaTitulo: "Pale Shelter", FaixaDisco: 2, FaixaNumero: 1}
	if !releaseContemFaixaAlvo(faixas, alvo) {
		t.Fatal("releaseContemFaixaAlvo = false, esperava true (a posicao disco 2 / faixa 1 existe)")
	}
	// Sem a posição, o empate continua sem solução: rejeita (nunca aplica um
	// "parecido").
	if releaseContemFaixaAlvo(faixas, provider.AlvoFaixaAlbum{FaixaTitulo: "Pale Shelter"}) {
		t.Fatal("releaseContemFaixaAlvo = true sem posicao, esperava false (empate ambiguo)")
	}
}
