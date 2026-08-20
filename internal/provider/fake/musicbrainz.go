// Musicbrainz é um provider DETERMINÍSTICO de fixtures pra vertical de MÚSICA
// (briefing 0115), companheiro de `Provider` (que serve a fixture de
// filme/série sob Nome()="tmdb"): implementa só o que o import de artista
// (0106, provider.MusicaProvider) e a tela externa de "faixas faltando" do
// álbum (0115 Parte 2, provider.MusicaAlbumProvider) exigem, SEM rede/segredo.
// Registrado sob Nome()="musicbrainz" — nome real da fonte, mesma convenção
// de `Provider` usar "tmdb" (as colunas geradas `external_id_mbid`
// deduplicam certo, interno-first funciona igual à fonte real).
//
// Ligado no binário por MYMEDIADB_FAKE_PROVIDER=1 (cmd/mymediadb,
// opcoesBusca) ao lado do fake de filme/série — usado pelos testes E2E de
// AMBAS as camadas: Camada 1 (internal/e2e) injeta direto no harness; Camada
// 2 (Playwright) o alcança via o binário real.
package fake

import (
	"context"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Ids fixos da discografia fake: um artista, um álbum com 2 faixas OFICIAIS
// no release — mas a ÁRVORE do artista (BuscarArtistaArvore, o que POST
// /import type=artista traz) só inclui a 1ª, de propósito: prova o caminho
// de "faixa faltando do álbum" sem precisar de um scan real (o harness E2E
// não sobe o scanner).
const (
	MusicbrainzArtistaID = "artista-fake-1"
	MusicbrainzAlbumID   = "album-fake-1"
	MusicbrainzFaixa1ID  = "faixa-fake-1"
	MusicbrainzFaixa2ID  = "faixa-fake-2"
)

// Asserção de interface: Musicbrainz é um Provider core + as duas
// capacidades de música (árvore completa + álbum seletivo).
var (
	_ provider.Provider            = (*Musicbrainz)(nil)
	_ provider.MusicaProvider      = (*Musicbrainz)(nil)
	_ provider.MusicaAlbumProvider = (*Musicbrainz)(nil)
)

// Musicbrainz é o fake — imutável, sem estado mutável, seguro para
// concorrência (mesmo espírito de Provider).
type Musicbrainz struct{}

// NewMusicbrainz constrói o fake de música. Sem parâmetros: a fixture não
// serve imagem (Cover Art Archive fica fora deste fake, best-effort no
// enrich real).
func NewMusicbrainz() *Musicbrainz { return &Musicbrainz{} }

func (m *Musicbrainz) Nome() string { return "musicbrainz" }

// Buscar/BuscarDetalhe/MapearDetalhe não são exercitados pelo import de
// música (que sempre resolve por id explícito, nunca por busca textual/
// enrich de detalhe genérico) — stubs inertes, só para satisfazer o core
// provider.Provider.
func (m *Musicbrainz) Buscar(_ context.Context, _, _, _ string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (m *Musicbrainz) BuscarDetalhe(_ context.Context, _, _ string, _ []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (m *Musicbrainz) MapearDetalhe(_ json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// BuscarArtistaArvore (provider.MusicaProvider): a árvore que POST /import
// type=artista importa por inteiro — só a faixa 1 (ver comentário do
// pacote).
func (m *Musicbrainz) BuscarArtistaArvore(_ context.Context, _ string, _ []string) (provider.ArtistaArvore, error) {
	return provider.ArtistaArvore{
		Name: "Artista Fake",
		MBID: MusicbrainzArtistaID,
		Albuns: []provider.AlbumExterno{
			{
				Name:  "Album Fake",
				MBID:  MusicbrainzAlbumID,
				Year:  "2000",
				Genre: "rock",
				Faixas: []provider.FaixaExterna{
					{Title: "Faixa Um", MBID: MusicbrainzFaixa1ID, TrackNumber: 1, DiscNumber: 1, DiscTotal: 1},
				},
			},
		},
	}, nil
}

// BuscarAlbumDaFaixa/BuscarAlbumPorNome (provider.MusicaAlbumProvider,
// caminhos por faixa/nome do import ESCOPADO do scanner) não são
// exercitados pelos testes E2E (nenhuma das duas camadas sobe o scanner
// real) — stubs sem match (graça).
func (m *Musicbrainz) BuscarAlbumDaFaixa(_ context.Context, _ string, _ []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (m *Musicbrainz) BuscarAlbumPorNome(_ context.Context, _, _ string, _ []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

// BuscarAlbumPorMbid (provider.MusicaAlbumProvider): o caminho DIRETO por
// MBID de release-group, usado por GET /external/musicbrainz/album/{mbid} e
// por ImportarFaixaAvulsa (POST /import type=faixa) — devolve o release
// OFICIAL com as 2 faixas (a 2ª nunca entrou pela árvore do artista).
func (m *Musicbrainz) BuscarAlbumPorMbid(_ context.Context, albumMBID string, _ []string) (provider.AlbumExterno, error) {
	if albumMBID != MusicbrainzAlbumID {
		return provider.AlbumExterno{}, provider.ErrNaoEncontrado
	}
	return provider.AlbumExterno{
		Name:  "Album Fake",
		MBID:  MusicbrainzAlbumID,
		Year:  "2000",
		Genre: "rock",
		Faixas: []provider.FaixaExterna{
			{Title: "Faixa Um", MBID: MusicbrainzFaixa1ID, TrackNumber: 1, DiscNumber: 1, DiscTotal: 1},
			{Title: "Faixa Dois", MBID: MusicbrainzFaixa2ID, TrackNumber: 2, DiscNumber: 1, DiscTotal: 1},
		},
	}, nil
}
