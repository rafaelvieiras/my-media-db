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
  it('resolve "movies" para o tipo "movie" e renderiza o grid com o título "Filmes"', async () => {
    spies.params = { entitySlug: 'movies' }
    const fetchMock = vi.fn().mockResolvedValue(page([]))
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<LibraryBrowseScreen />)

    expect(screen.getByRole('heading', { name: 'Filmes' })).toBeInTheDocument()
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(urlEnviada(fetchMock).searchParams.get('type')).toBe('movie')
  })

  it('resolve "artists" para o tipo "artista"', async () => {
    spies.params = { entitySlug: 'artists' }
    const fetchMock = vi.fn().mockResolvedValue(page([]))
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<LibraryBrowseScreen />)

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(urlEnviada(fetchMock).searchParams.get('type')).toBe('artista')
  })

  it('slug desconhecido cai em estado vazio coerente, sem crash nem fetch', () => {
    spies.params = { entitySlug: 'nao-existe' }
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<LibraryBrowseScreen />)

    expect(
      screen.getByText('Tipo de biblioteca não encontrado'),
    ).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
