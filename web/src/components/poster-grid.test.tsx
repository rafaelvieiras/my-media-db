import { afterEach, describe, expect, it } from 'vitest'
import { vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { PosterGrid } from '@/components/poster-grid'
import type { EntityCard } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson } from '@/test/detail-harness'
import { paginaResposta as page, urlEnviada } from '@/test/stub-fetch'

// Since ADR-0030, the TYPE arrives FIXED via prop (`type`) — the
// grid no longer reads `?type=` from the URL nor navigates when the type changes (that moved
// to `routes/library-browse.tsx`). The `<Link>` is only used by the empty state
// ("Ir para Bibliotecas"), mocked as a plain `<a>` (same pattern as
// other tests in this directory).
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PosterGrid', () => {
  it('mostra o estado de carregamento', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )
    renderWithClient(<PosterGrid type="movie" />)
    expect(screen.getByText('Carregando catálogo…')).toBeInTheDocument()
  })

  it('mostra o estado de erro', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(erroJson(500, 'x', 'falhou')),
    )
    renderWithClient(<PosterGrid type="movie" />)
    await waitFor(() =>
      expect(
        screen.getByText('Não foi possível carregar o catálogo'),
      ).toBeInTheDocument(),
    )
  })

  it('mostra o estado vazio que orienta o usuário', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(page([], 0)))
    renderWithClient(<PosterGrid type="movie" />)
    await waitFor(() =>
      expect(screen.getByText('Nenhum item no catálogo')).toBeInTheDocument(),
    )
    expect(screen.getByText(/adicione um datasource/i)).toBeInTheDocument()
  })

  it('renderiza os cards e a contagem da paginação', async () => {
    const items: EntityCard[] = [
      {
        id: 'a',
        type: 'movie',
        title: 'Alfa',
        status: 'cataloged',
        owned: true,
        file_count: 1,
        match_confidence: 100,
        poster_url: null,
        catalog_completeness: null,
      },
    ]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(page(items, 1)))
    renderWithClient(<PosterGrid type="movie" />)
    await waitFor(() => expect(screen.getByText('Alfa')).toBeInTheDocument())
    expect(screen.getByText('1–1 de 1')).toBeInTheDocument()
  })

  it('busca com o `type` recebido via prop, fixo (0114)', async () => {
    const fetchMock = vi.fn().mockResolvedValue(page([]))
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<PosterGrid type="artista" />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(urlEnviada(fetchMock).searchParams.get('type')).toBe('artista')
  })
})
