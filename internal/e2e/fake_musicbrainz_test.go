package e2e

// fakeMusicbrainz é um provider MÍNIMO, só para a jornada de música da Camada 1
// (briefing 0115): registra sob Nome()="musicbrainz" as duas capacidades que o
// import de artista (0106, provider.MusicaProvider — árvore inteira) e a tela
// externa de álbum (0115 Parte 2, provider.MusicaAlbumProvider — release
// seletivo por MBID) exigem. Dados FIXOS (sem rede), moldados no shape que
// provider.ArtistaArvore/AlbumExterno esperam — molde de internal/provider/fake
// (fake.Provider), mas dedicado à vertical de música (Nome() distinto, "tmdb" já
// é usado por fake.Provider em todas as demais jornadas).
//
// O artista fake tem 1 álbum com 2 faixas OFICIAIS no release (BuscarAlbumPorMbid),
// mas a ÁRVORE do artista (BuscarArtistaArvore, usada por POST /import type=artista)
// só traz a 1ª faixa — de propósito, para a jornada exercitar o caminho de "faixa
// faltando do álbum" (GET /external/musicbrainz/album/{mbid} + POST /import
// type=faixa) sem precisar do scanner (que o harness E2E desta camada não sobe).

import (
	"context"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	mbArtistaMBID = "artista-fake-1"
	mbAlbumMBID   = "album-fake-1"
	mbFaixa1MBID  = "faixa-fake-1"
	mbFaixa2MBID  = "faixa-fake-2"
)

type fakeMusicbrainz struct{}

func (fakeMusicbrainz) Nome() string { return "musicbrainz" }

// Buscar/BuscarDetalhe/MapearDetalhe não são exercitados pela jornada de música
// (que sempre importa por id explícito, nunca por busca textual/enrich de
// detalhe) — stubs inertes, só para satisfazer o core provider.Provider.
func (fakeMusicbrainz) Buscar(_ context.Context, _, _, _ string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (fakeMusicbrainz) BuscarDetalhe(_ context.Context, _, _ string, _ []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (fakeMusicbrainz) MapearDetalhe(_ json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// BuscarArtistaArvore (provider.MusicaProvider): a árvore que POST /import
// type=artista importa por inteiro — só a faixa 1, de propósito (ver comentário
// do pacote).
func (fakeMusicbrainz) BuscarArtistaArvore(_ context.Context, _ string, _ []string) (provider.ArtistaArvore, error) {
	return provider.ArtistaArvore{
		Name: "Artista Fake",
		MBID: mbArtistaMBID,
		Albuns: []provider.AlbumExterno{
			{
				Name:  "Album Fake",
				MBID:  mbAlbumMBID,
				Year:  "2000",
				Genre: "rock",
				Faixas: []provider.FaixaExterna{
					{Title: "Faixa Um", MBID: mbFaixa1MBID, TrackNumber: 1, DiscNumber: 1, DiscTotal: 1},
				},
			},
		},
	}, nil
}

// BuscarAlbumDaFaixa/BuscarAlbumPorNome (provider.MusicaAlbumProvider, caminhos
// por faixa/nome do import ESCOPADO do scanner) não são exercitados por esta
// jornada (o harness E2E não sobe o scanner) — stubs sem match (graça).
func (fakeMusicbrainz) BuscarAlbumDaFaixa(_ context.Context, _ string, _ []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (fakeMusicbrainz) BuscarAlbumPorNome(_ context.Context, _, _ string, _ []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

// BuscarAlbumPorMbid (provider.MusicaAlbumProvider): o caminho DIRETO por MBID de
// release-group, usado por GET /external/musicbrainz/album/{mbid} e por
// ImportarFaixaAvulsa (POST /import type=faixa) — devolve o release OFICIAL com
// as 2 faixas (a 2ª nunca entrou pela árvore do artista).
func (fakeMusicbrainz) BuscarAlbumPorMbid(_ context.Context, albumMBID string, _ []string) (provider.AlbumExterno, error) {
	if albumMBID != mbAlbumMBID {
		return provider.AlbumExterno{}, provider.ErrNaoEncontrado
	}
	return provider.AlbumExterno{
		Name:  "Album Fake",
		MBID:  mbAlbumMBID,
		Year:  "2000",
		Genre: "rock",
		Faixas: []provider.FaixaExterna{
			{Title: "Faixa Um", MBID: mbFaixa1MBID, TrackNumber: 1, DiscNumber: 1, DiscTotal: 1},
			{Title: "Faixa Dois", MBID: mbFaixa2MBID, TrackNumber: 2, DiscNumber: 1, DiscTotal: 1},
		},
	}, nil
}
