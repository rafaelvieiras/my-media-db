import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EntityDetailScreen } from '@/routes/entity-detail'
import { LanguageProvider } from '@/lib/language-context'
import { I18nProvider } from '@/lib/i18n'
import type { EntityCard, ResolvedEntity } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import {
  criarFetchRouterEntity,
  entityCardBase as card,
} from '@/test/detail-harness'
import { baseEntity, okJson } from '@/test/entity-fixtures'

// erroMembros simulates GET /entities/{id}/members failing (500) — to exercise
// the error state of TracklistAlbum/DiscografiaArtista (same error shape
// that the HTTP client, `lerJson`, expects).
function erroMembros(): Response {
  return {
    ok: false,
    status: 500,
    json: async () => ({
      error: { code: 'erro_interno', message: 'falha ao listar membros' },
    }),
  } as Response
}

// Tests the MUSIC SECTIONS via EntityDetailScreen:
// an album shows the tracklist (TracklistAlbum), an artist shows the discography
// (DiscografiaArtista) — and the regression test guarantees that a movie/series does NOT
// get those sections (same pattern as entity-detail-series.test.tsx).
vi.mock('@tanstack/react-router', async () => ({
  useParams: () => ({ id: 'e1' }),
  useSearch: () => ({ ordering: undefined }),
  useNavigate: () => vi.fn(),
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function renderDetail(
  entity: ResolvedEntity,
  membros: EntityCard[] = [],
  membrosResp?: () => Response,
) {
  vi.stubGlobal(
    'fetch',
    criarFetchRouterEntity(entity, membrosResp ?? (() => okJson(membros))),
  )
  return renderWithClient(
    <I18nProvider>
      <LanguageProvider>
        <EntityDetailScreen />
      </LanguageProvider>
    </I18nProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

const faixa1 = card({ id: 'fx1', type: 'faixa', title: 'Rio' })
const faixa2 = card({ id: 'fx2', type: 'faixa', title: 'My Own Way' })
const album1 = card({ id: 'al1', type: 'album', title: 'Rio' })
const album2 = card({
  id: 'al2',
  type: 'album',
  title: 'Seven and the Ragged Tiger',
})
const faixaSolta = card({
  id: 'fx9',
  type: 'faixa',
  title: 'Colaboração avulsa',
})

describe('EntityDetailScreen — album: tracklist (0111)', () => {
  it('shows the tracks of the album, in the order the API returns them', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [faixa1, faixa2],
    )
    await waitFor(() => expect(screen.getByText('Tracks')).toBeInTheDocument())
    const secao = screen.getByText('Tracks').closest('section') as HTMLElement
    expect(within(secao).getByText('Rio')).toBeInTheDocument()
    expect(within(secao).getByText('My Own Way')).toBeInTheDocument()
    // Ordinal position (1, 2) in the arrival order of the array.
    expect(within(secao).getByText('1')).toBeInTheDocument()
    expect(within(secao).getByText('2')).toBeInTheDocument()
  })

  it('album without tracks: empty state, without breaking', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [],
    )
    await waitFor(() => expect(screen.getByText('Tracks')).toBeInTheDocument())
    expect(
      screen.getByText('This album has no tracks cataloged yet.'),
    ).toBeInTheDocument()
  })

  it('each track links to its own detail', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [faixa1],
    )
    const link = await screen.findByText('Rio')
    expect(link.closest('a')?.getAttribute('href')).toBe('/entities/fx1')
  })

  it('does not show the Discography (artist section) on the album, nor "See missing tracks" without mbid', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [faixa1],
    )
    await waitFor(() => expect(screen.getByText('Tracks')).toBeInTheDocument())
    expect(screen.queryByText('Discography')).toBeNull()
    expect(screen.queryByText('See missing tracks')).toBeNull()
  })

  it('error loading the tracks: shows the error state, without breaking', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'album', title: 'Álbum Teste' }),
      [],
      erroMembros,
    )
    expect(
      await screen.findByText('Could not load the tracks'),
    ).toBeInTheDocument()
  })

  // "See missing tracks" (ADR-0029): link to the external
  // album screen, only when the album has external_ids.mbid (MusicBrainz).
  it('album with mbid: shows the "See missing tracks" link to the external one', async () => {
    renderDetail(
      baseEntity({
        id: 'e1',
        type: 'album',
        title: 'Álbum Teste',
        external_ids: { mbid: 'rg-1' },
      }),
      [faixa1],
    )
    const link = await screen.findByRole('link', {
      name: /See missing tracks/,
    })
    expect(link.getAttribute('href')).toBe('/external/musicbrainz/album/rg-1')
  })
})

describe('EntityDetailScreen — artist: discography (0111)', () => {
  it('shows the ALBUMS of the artist, filtering loose tracks out', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'artista', title: 'Duran Duran' }),
      [album1, album2, faixaSolta],
    )
    await waitFor(() =>
      expect(screen.getByText('Discography')).toBeInTheDocument(),
    )
    expect(screen.getByText('Rio')).toBeInTheDocument()
    expect(screen.getByText('Seven and the Ragged Tiger')).toBeInTheDocument()
    // The loose track linked by music_artista_principal (ADR-0027) does NOT enter
    // the discography grid (only type=album).
    expect(screen.queryByText('Colaboração avulsa')).toBeNull()
  })

  it('artist without albums: empty state, without breaking', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'artista', title: 'Duran Duran' }),
      [],
    )
    await waitFor(() =>
      expect(screen.getByText('Discography')).toBeInTheDocument(),
    )
    expect(
      screen.getByText('This artist has no albums cataloged yet.'),
    ).toBeInTheDocument()
  })

  it('does not show the tracklist (album section) on the artist', async () => {
    renderDetail(
      baseEntity({ id: 'e1', type: 'artista', title: 'Duran Duran' }),
      [album1],
    )
    await waitFor(() =>
      expect(screen.getByText('Discography')).toBeInTheDocument(),
    )
    expect(screen.queryByText('Tracks')).toBeNull()
  })
})

// ADR-0027: only the TRACK links directly to a media_file — an album/artist "owned"
// (owned:true via the tree, completarOwnedViaArvore) never has a real folder
// OF ITS OWN (resolverDestino does not choose among the directories of the several
// tracks). "Save information (NFO)" would hit a 404 (no destination folder)
// if enabled with the raw `owned` — which is why the screen forces those two
// items to disabled in these two verticals, even with owned:true.
describe('EntityDetailScreen — music: "Save information (NFO)" stays disabled even with owned:true (regression)', () => {
  it('album owned through the tree: the item stays disabled with a tooltip', async () => {
    const user = userEvent.setup()
    renderDetail(
      baseEntity({
        id: 'e1',
        type: 'album',
        title: 'Álbum Teste',
        owned: true,
      }),
      [faixa1],
    )
    await waitFor(() => expect(screen.getByText('Tracks')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: /Manage/ }))
    const item = screen.getByRole('menuitem', {
      name: /Save information \(NFO\)/,
    })
    expect(item).toHaveAttribute('aria-disabled', 'true')
  })

  it('artist owned through the tree: the item stays disabled with a tooltip', async () => {
    const user = userEvent.setup()
    renderDetail(
      baseEntity({
        id: 'e1',
        type: 'artista',
        title: 'Duran Duran',
        owned: true,
      }),
      [album1],
    )
    await waitFor(() =>
      expect(screen.getByText('Discography')).toBeInTheDocument(),
    )
    await user.click(screen.getByRole('button', { name: /Manage/ }))
    const item = screen.getByRole('menuitem', {
      name: /Save information \(NFO\)/,
    })
    expect(item).toHaveAttribute('aria-disabled', 'true')
  })
})

describe('EntityDetailScreen — regression: movie/series without music sections (0111)', () => {
  it('movie: neither "Tracks" nor "Discography" appears', async () => {
    renderDetail(baseEntity({ id: 'e1', type: 'movie', title: 'Dune' }))
    await waitFor(() => expect(screen.getByText('Dune')).toBeInTheDocument())
    expect(screen.queryByText('Tracks')).toBeNull()
    expect(screen.queryByText('Discography')).toBeNull()
  })

  it('series: neither "Tracks" nor "Discography" appears (the series section stays the same)', async () => {
    renderDetail(baseEntity({ id: 'e1', type: 'tv', title: 'Série X' }), [
      card({ id: 'se1', type: 'season', title: 'Temporada 1' }),
    ])
    await waitFor(() => expect(screen.getByText('Seasons')).toBeInTheDocument())
    expect(screen.queryByText('Tracks')).toBeNull()
    expect(screen.queryByText('Discography')).toBeNull()
  })
})
