import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { LibraryBrowseScreen } from '@/routes/library-browse'
import { renderWithClient } from '@/test/render'
import { paginaResposta as page, urlEnviada } from '@/test/stub-fetch'

// Hoisted mutable state: the slug read by the route `/library/$entitySlug`.
const spies = vi.hoisted(() => ({
  params: { entitySlug: 'movies' },
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
  Link: (await import('@/test/link-mock')).LinkMock,
}))

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LibraryBrowseScreen', () => {
  it('resolves "movies" to the "movie" type and renders the grid with the "Movies" heading', async () => {
    spies.params = { entitySlug: 'movies' }
    const fetchMock = vi.fn().mockResolvedValue(page([]))
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<LibraryBrowseScreen />)

    expect(screen.getByRole('heading', { name: 'Movies' })).toBeInTheDocument()
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(urlEnviada(fetchMock).searchParams.get('type')).toBe('movie')
  })

  it('resolves "artists" to the "artista" type', async () => {
    spies.params = { entitySlug: 'artists' }
    const fetchMock = vi.fn().mockResolvedValue(page([]))
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<LibraryBrowseScreen />)

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(urlEnviada(fetchMock).searchParams.get('type')).toBe('artista')
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
