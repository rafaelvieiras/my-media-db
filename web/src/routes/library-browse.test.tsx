import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { LibraryBrowseScreen } from '@/routes/library-browse'
import { renderWithClient } from '@/test/render'
import {
  pageCards as cards,
  paginaResposta as page,
  urlEnviada,
} from '@/test/stub-fetch'

// Hoisted mutable state: the slug and the search params read by the route
// `/library/$entitySlug` (the page of the listing lives in `?page=`), plus the navigate spy.
const spies = vi.hoisted(() => ({
  params: { entitySlug: 'movies' },
  search: { page: undefined as number | undefined },
  navigate: vi.fn(),
}))

vi.mock('@/lib/language-context', async () => {
  const { fakeLanguage } = await import('@/test/detail-harness')
  return { useLanguage: () => fakeLanguage('pt-BR') }
})

// Mocked router: `useParams` returns the controllable slug; the `Link` of the empty
// state of the PosterGrid reuses the shared mock (same pattern as
// collection-members.test.tsx/external-album.test.tsx).
vi.mock('@tanstack/react-router', async () => ({
  useParams: () => spies.params,
  useSearch: () => spies.search,
  useNavigate: () => spies.navigate,
  Link: (await import('@/test/link-mock')).LinkMock,
}))

beforeEach(() => {
  spies.search = { page: undefined }
  spies.navigate = vi.fn()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// renderNaPagina mounts the screen with the page in the URL for `movies`, a full page of
// 24 cards and `total` items, and waits for the cards (the pagination only renders with
// items). Shared by the pagination tests (jscpd).
async function renderNaPagina(pagina: number | undefined, total: number) {
  spies.params = { entitySlug: 'movies' }
  spies.search = { page: pagina }
  const fetchMock = vi.fn().mockResolvedValue(page(cards(24), total))
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<LibraryBrowseScreen />)
  await screen.findByText('Filme 0')
  return fetchMock
}

describe('LibraryBrowseScreen', () => {
  it('resolves "movies" to the "movie" type and renders the grid with the "Movies" heading', async () => {
    spies.params = { entitySlug: 'movies' }
    const fetchMock = vi.fn().mockResolvedValue(page([]))
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<LibraryBrowseScreen />)

    expect(screen.getByRole('heading', { name: 'Movies' })).toBeInTheDocument()
    await screen.findByText('No items in the catalog')
    expect(urlEnviada(fetchMock).searchParams.get('type')).toBe('movie')
  })

  it('resolves "artists" to the "artista" type', async () => {
    spies.params = { entitySlug: 'artists' }
    const fetchMock = vi.fn().mockResolvedValue(page([]))
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<LibraryBrowseScreen />)

    await screen.findByText('No items in the catalog')
    expect(urlEnviada(fetchMock).searchParams.get('type')).toBe('artista')
  })

  it('reads the page from the URL search param (deep link to page 3)', async () => {
    const fetchMock = await renderNaPagina(3, 1361)
    // Zero-based offset of the third page (24 per page).
    expect(urlEnviada(fetchMock).searchParams.get('offset')).toBe('48')
    expect(screen.getByText(/Page 3 of 57/)).toBeInTheDocument()
  })

  it('"Next" writes the next page to the URL search param', async () => {
    await renderNaPagina(undefined, 100)
    fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(spies.navigate).toHaveBeenCalledWith(
      expect.objectContaining({
        to: '/library/$entitySlug',
        params: { entitySlug: 'movies' },
        search: { page: 2 },
      }),
    )
  })

  it('going back to the first page REMOVES the param (clean URL)', async () => {
    await renderNaPagina(2, 100)
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }))
    expect(spies.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ search: {} }),
    )
  })

  it('falls back to a coherent empty state on an unknown slug, without crash or fetch', () => {
    spies.params = { entitySlug: 'nao-existe' }
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<LibraryBrowseScreen />)

    expect(screen.getByText('Library type not found')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
