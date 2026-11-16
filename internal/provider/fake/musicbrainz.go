// Musicbrainz is a DETERMINISTIC fixture provider for the MUSIC vertical,
// companion of `Provider` (which serves the movie/series fixture under
// Nome()="tmdb"): it implements only what the artist import (provider.MusicaProvider)
// and the external screen of "missing tracks" of the album
// (provider.MusicaAlbumProvider) require, WITHOUT network/secret. Registered
// under Nome()="musicbrainz" -- the real name of the source, the same
// convention of `Provider` using "tmdb" (the generated columns `external_id_mbid`
// deduplicate correctly, internal-first works the same as with the real source).
//
// Wired into the binary by MYMEDIADB_FAKE_PROVIDER=1 (cmd/mymediadb,
// opcoesBusca) next to the movie/series fake -- used by the E2E tests of BOTH
// layers: Layer 1 (internal/e2e) injects it straight into the harness; Layer 2
// (Playwright) reaches it through the real binary.
package fake

import (
	"context"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Fixed ids of the fake discography: one artist, one album with 2 OFFICIAL
// tracks in the release -- but the ARTIST tree (BuscarArtistaArvore, what POST
// /import type=artista brings) deliberately includes only the 1st one: it proves
// the "album track missing" path without needing a real scan (the E2E harness
// does not bring up the scanner).
const (
	MusicbrainzArtistaID = "artista-fake-1"
	MusicbrainzAlbumID   = "album-fake-1"
	MusicbrainzFaixa1ID  = "faixa-fake-1"
	MusicbrainzFaixa2ID  = "faixa-fake-2"
)

// Interface assertion: Musicbrainz is a core Provider + the two music
// capabilities (full tree + selective album).
var (
	_ provider.Provider            = (*Musicbrainz)(nil)
	_ provider.MusicaProvider      = (*Musicbrainz)(nil)
	_ provider.MusicaAlbumProvider = (*Musicbrainz)(nil)
)

// Musicbrainz is the fake -- immutable, with no mutable state, safe for
// concurrency (same spirit as Provider).
type Musicbrainz struct{}

// NewMusicbrainz builds the music fake. Without parameters: the fixture does not
// serve images (Cover Art Archive stays out of this fake, best-effort in the real
// enrich).
func NewMusicbrainz() *Musicbrainz { return &Musicbrainz{} }

func (m *Musicbrainz) Nome() string { return "musicbrainz" }

// Buscar/BuscarDetalhe/MapearDetalhe are not exercised by the music import (which
// always resolves by explicit id, never by textual search/generic detail enrich)
// -- inert stubs, only to satisfy the core provider.Provider.
func (m *Musicbrainz) Buscar(_ context.Context, _, _, _ string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (m *Musicbrainz) BuscarDetalhe(_ context.Context, _, _ string, _ []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (m *Musicbrainz) MapearDetalhe(_ json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// BuscarArtistaArvore (provider.MusicaProvider): the tree that POST /import
// type=artista imports as a whole -- only track 1 (see the package comment).
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

// BuscarAlbumDaFaixa/BuscarAlbumPorNome (provider.MusicaAlbumProvider, the
// by-track/by-name paths of the SCOPED import of the scanner) are not exercised
// by the E2E tests (neither layer brings up the real scanner) -- stubs without a
// match (gracefully).
func (m *Musicbrainz) BuscarAlbumDaFaixa(_ context.Context, _ string, _ []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (m *Musicbrainz) BuscarAlbumPorNome(_ context.Context, _, _ string, _ provider.AlvoFaixaAlbum, _ []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

// BuscarAlbumPorMbid (provider.MusicaAlbumProvider): the DIRECT path by
// release-group MBID, used by GET /external/musicbrainz/album/{mbid} and by
// ImportarFaixaAvulsa (POST /import type=faixa) -- it returns the OFFICIAL
// release with the 2 tracks (the 2nd one never came in through the artist tree).
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
