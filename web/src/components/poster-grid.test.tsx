import { afterEach, describe, expect, it } from 'vitest'
import { vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { PosterGrid } from '@/components/poster-grid'
import type { EntityCard } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson } from '@/test/detail-harness'
import {
  pageCards as cards,
  paginaResposta as page,
  urlEnviada,
} from '@/test/stub-fetch'

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

// renderNaPagina mounts the grid on a given page (a full page of 24 cards, `total` items)
// and waits for the cards: the pagination only renders with items. Shared by the six tests
// below so the stub+render+wait pattern is not repeated (jscpd).
async function renderNaPagina(pagina: number, total: number) {
  const fetchMock = vi.fn().mockResolvedValue(page(cards(24), total))
  vi.stubGlobal('fetch', fetchMock)
  const onPageChange = vi.fn()
  renderWithClient(
    <PosterGrid type="movie" page={pagina} onPageChange={onPageChange} />,
  )
  await screen.findByText('Filme 0')
  return { fetchMock, onPageChange }
}

describe('PosterGrid', () => {
  it('shows the loading state', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )
    renderWithClient(<PosterGrid type="movie" />)
    expect(screen.getByText('Loading the catalog…')).toBeInTheDocument()
  })

  it('shows the error state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(erroJson(500, 'x', 'falhou')),
    )
    renderWithClient(<PosterGrid type="movie" />)
    await waitFor(() =>
      expect(
        screen.getByText('Could not load the catalog'),
      ).toBeInTheDocument(),
    )
  })

  it('shows the empty state that guides the user', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(page([], 0)))
    renderWithClient(<PosterGrid type="movie" />)
    await waitFor(() =>
      expect(screen.getByText('No items in the catalog')).toBeInTheDocument(),
    )
    expect(screen.getByText(/add a datasource/i)).toBeInTheDocument()
  })

  it('renders the cards and the pagination count', async () => {
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
    expect(screen.getByText(/1–1 of 1/)).toBeInTheDocument()
    // The control says WHICH page of how many — the count the audit missed.
    expect(screen.getByText(/Page 1 of 1/)).toBeInTheDocument()
  })

  it('the page comes from the parent (URL) and the offset follows it (pagination in the URL)', async () => {
    const { fetchMock } = await renderNaPagina(2, 1361)
    expect(urlEnviada(fetchMock).searchParams.get('offset')).toBe('48')
    expect(screen.getByText(/Page 3 of 57/)).toBeInTheDocument()
  })

  it('"Next" asks the parent for the next page instead of keeping local state', async () => {
    const { onPageChange } = await renderNaPagina(0, 100)
    fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(onPageChange).toHaveBeenCalledWith(1)
  })

  it('a numbered page button jumps straight to that page, marking the current one', async () => {
    const { onPageChange } = await renderNaPagina(2, 1361)
    expect(
      screen.getByRole('button', { name: 'Go to page 3' }),
    ).toHaveAttribute('aria-current', 'page')
    fireEvent.click(screen.getByRole('button', { name: 'Go to page 57' }))
    expect(onPageChange).toHaveBeenCalledWith(56)
  })

  it('changing the ownership filter goes back to the first page', async () => {
    const { onPageChange } = await renderNaPagina(3, 100)
    fireEvent.change(screen.getByLabelText('Filter by ownership'), {
      target: { value: 'owned' },
    })
    expect(onPageChange).toHaveBeenCalledWith(0)
  })

  it('fetches with the `type` received via prop, fixed (0114)', async () => {
    const fetchMock = vi.fn().mockResolvedValue(page([]))
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<PosterGrid type="artista" />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(urlEnviada(fetchMock).searchParams.get('type')).toBe('artista')
  })
})
