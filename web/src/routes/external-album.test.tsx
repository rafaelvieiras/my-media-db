import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ExternalAlbumView } from '@/routes/external-album'
import type { ExternalAlbum } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson, stubExternalFetch } from '@/test/detail-harness'

// Tests the "missing tracks" screen of the album:
// modeled on collection-detail.test.tsx, adapted to album→tracks. ExternalAlbumView
// receives source/id/back as PROPS (not useParams) — ExternalDetailScreen is the one that
// decides to delegate here when type==='album'; the test exercises the component
// directly, without having to mock useParams.

const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))
const spies = vi.hoisted(() => ({ navigate: vi.fn() }))

vi.mock('@/lib/language-context', async () => {
  const { fakeLanguage } = await import('@/test/detail-harness')
  return { useLanguage: () => fakeLanguage(estado.lang) }
})
vi.mock('@tanstack/react-router', async () => ({
  useNavigate: () => spies.navigate,
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function album(over: Partial<ExternalAlbum> = {}): ExternalAlbum {
  return {
    source: 'musicbrainz',
    source_id: 'rg-1',
    name: 'Fake Album',
    year: '1999',
    genre: 'rock',
    internal_id: 'album-1',
    tracks: [
      {
        source_id: 'track-mbid-1',
        title: 'Track One',
        track_number: 1,
        disc_number: 1,
        isrc: null,
        internal_id: 'faixa-1',
      },
      {
        source_id: 'track-mbid-2',
        title: 'Track Two',
        track_number: 2,
        disc_number: 1,
        isrc: null,
        internal_id: null,
      },
    ],
    ...over,
  }
}

function renderAlbum() {
  return renderWithClient(
    <ExternalAlbumView
      source="musicbrainz"
      id="rg-1"
      back={<button type="button">Voltar</button>}
    />,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
  estado.lang = 'pt-BR'
})

describe('ExternalAlbumView', () => {
  it('shows the album header (name/year/genre) and the View album link when already internal', async () => {
    stubExternalFetch(() => okJson(album()))
    renderAlbum()

    expect(
      await screen.findByRole('heading', { name: 'Fake Album' }),
    ).toBeTruthy()
    expect(screen.getByText('1999 · rock')).toBeTruthy()
    const ver = screen.getByRole('link', { name: /View album/ })
    expect(ver.getAttribute('href')).toBe('/entities/album-1')
  })

  it('already cataloged track links to the internal detail and shows "in the library"', async () => {
    stubExternalFetch(() => okJson(album()))
    renderAlbum()
    await screen.findByRole('heading', { name: 'Fake Album' })

    const link = screen.getByRole('link', { name: 'Track One' })
    expect(link.getAttribute('href')).toBe('/entities/faixa-1')
    expect(screen.getByText('In the library')).toBeTruthy()
  })

  it('missing track gains the Import button', async () => {
    stubExternalFetch(() => okJson(album()))
    renderAlbum()
    await screen.findByRole('heading', { name: 'Fake Album' })

    expect(screen.getByText('Track Two')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Import' })).toBeTruthy()
  })

  it('Importing a track sends type=faixa and parent_id=internal album id', async () => {
    const fetchMock = stubExternalFetch(() => okJson(album()))
    renderAlbum()
    await screen.findByRole('heading', { name: 'Fake Album' })

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Import' }))

    await vi.waitFor(() => {
      const chamada = fetchMock.mock.calls.find(
        (c) =>
          new URL(String(c[0]), 'http://localhost').pathname ===
          '/api/v1/import',
      )
      expect(chamada).toBeTruthy()
      expect(
        JSON.parse(String((chamada?.[1] as RequestInit).body)),
      ).toMatchObject({
        source: 'musicbrainz',
        source_id: 'track-mbid-2',
        type: 'faixa',
        parent_id: 'album-1',
      })
    })
  })

  it('album without tracks shows the notice', async () => {
    stubExternalFetch(() => okJson(album({ tracks: [] })))
    renderAlbum()
    expect(
      await screen.findByText('This album has no tracks in the source.'),
    ).toBeTruthy()
  })

  it('404: album missing from the source', async () => {
    stubExternalFetch(() => erroJson(404, 'not_found', 'não existe'))
    renderAlbum()
    expect(
      await screen.findByText('Album not found in the source'),
    ).toBeTruthy()
  })

  it('503: source without the selective album capability', async () => {
    stubExternalFetch(() => erroJson(503, 'unavailable', 'sem album'))
    renderAlbum()
    expect(await screen.findByText('Source unavailable right now')).toBeTruthy()
  })
})
