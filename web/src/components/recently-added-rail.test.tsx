import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import {
  RecentlyAddedRail,
  RecentlyAddedSection,
} from '@/components/recently-added-rail'
import type { EntityCard, EntityType } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { entityCardBase } from '@/test/detail-harness'

// card: same defaults as entityCardBase, it only swaps id/title to the values that
// this file already used (id 'x', title 'Matrix') — avoids duplicating the whole
// factory (jscpd) just because of those two fields.
function card(over: Partial<EntityCard> = {}): EntityCard {
  return entityCardBase({ id: 'x', title: 'Matrix', ...over })
}

// The <Link> of TanStack Router requires router context; mocked by the
// shared `LinkMock` (it resolves `params` into the href), to assert the destination
// of the "Ver Todos" link — same pattern as app-header.test.tsx.
vi.mock('@tanstack/react-router', async () => ({
  Link: (await import('@/test/link-mock')).LinkMock,
}))

function jsonResponse(body: unknown, ok = true, status = 200): Response {
  return { ok, status, json: async () => body } as Response
}

function entityList(items: EntityCard[]) {
  return jsonResponse({
    items,
    total: items.length,
    limit: 10,
    offset: 0,
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RecentlyAddedRail', () => {
  it('fetches with sort=-created_at/owned=true/limit=10 and renders the real items', async () => {
    const items = [
      card({ id: 'a', title: 'Matrix', type: 'movie' }),
      card({ id: 'b', title: 'Duna', type: 'movie' }),
    ]
    const fetchMock = vi.fn().mockResolvedValue(entityList(items))
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<RecentlyAddedRail type="movie" label="Movies" />)

    await waitFor(() => expect(screen.getByText('Matrix')).toBeInTheDocument())
    expect(screen.getByText('Duna')).toBeInTheDocument()
    expect(screen.getByText('Movies')).toBeInTheDocument()

    const url = new URL(fetchMock.mock.calls[0][0] as string, 'http://x')
    expect(url.pathname).toBe('/api/v1/entities')
    expect(url.searchParams.get('type')).toBe('movie')
    expect(url.searchParams.get('sort')).toBe('-created_at')
    expect(url.searchParams.get('owned')).toBe('true')
    expect(url.searchParams.get('limit')).toBe('10')
  })

  it('the title has a "View all" link to "/library/movies" (briefing 0114)', async () => {
    const items = [card({ id: 'a', title: 'Matrix', type: 'movie' })]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(entityList(items)))

    renderWithClient(<RecentlyAddedRail type="movie" label="Movies" />)

    const link = await screen.findByRole('link', { name: 'View all' })
    expect(link).toHaveAttribute('href', '/library/movies')
  })

  it('without a slug mapped for the type, it does not render "View all" (defensive)', async () => {
    const items = [card({ id: 'a', title: 'Album X', type: 'album' })]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(entityList(items)))

    renderWithClient(<RecentlyAddedRail type="album" label="Albums" />)

    await waitFor(() => expect(screen.getByText('Album X')).toBeInTheDocument())
    expect(
      screen.queryByRole('link', { name: 'View all' }),
    ).not.toBeInTheDocument()
  })

  it('shows the loading state with the label', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )
    renderWithClient(<RecentlyAddedRail type="movie" label="Movies" />)
    expect(screen.getByText('Movies')).toBeInTheDocument()
    expect(screen.getByText(/Loading movies/i)).toBeInTheDocument()
  })

  it('shows the error state', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse({ error: { code: 'x', message: 'falhou' } }, false, 500),
        ),
    )
    renderWithClient(<RecentlyAddedRail type="movie" label="Movies" />)
    await waitFor(() =>
      expect(screen.getByText('Could not load "Movies"')).toBeInTheDocument(),
    )
  })

  it('renders nothing (not even the title) when the schema has no item', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(entityList([])))
    const { container } = renderWithClient(
      <RecentlyAddedRail type="album" label="Albums" />,
    )
    await waitFor(() => expect(container).toBeEmptyDOMElement())
    expect(screen.queryByText('Albums')).not.toBeInTheDocument()
  })
})

describe('RecentlyAddedSection', () => {
  it('renders nothing when GET /entities/types returns an empty list (empty library)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = new URL(input as string, 'http://x')
        if (url.pathname === '/api/v1/entities/types') {
          return Promise.resolve(jsonResponse([] satisfies EntityType[]))
        }
        throw new Error(`fetch inesperado: ${url.pathname}`)
      }),
    )
    const { container } = renderWithClient(<RecentlyAddedSection />)
    await waitFor(() => expect(container).toBeEmptyDOMElement())
  })

  it('renders one carousel per returned type, music included (0105-0107), with no hardcoded list', async () => {
    const tipos: EntityType[] = [
      { type: 'movie', count: 21 },
      { type: 'album', count: 3 },
      { type: 'artista', count: 2 },
    ]
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = new URL(input as string, 'http://x')
        if (url.pathname === '/api/v1/entities/types') {
          return Promise.resolve(jsonResponse(tipos))
        }
        if (url.pathname === '/api/v1/entities') {
          const tipo = url.searchParams.get('type')
          return Promise.resolve(
            entityList([
              card({ id: `${tipo}-1`, type: tipo as EntityCard['type'] }),
            ]),
          )
        }
        throw new Error(`fetch inesperado: ${url.pathname}`)
      }),
    )

    renderWithClient(<RecentlyAddedSection lang="pt-BR" />)

    await waitFor(() => expect(screen.getByText('Movies')).toBeInTheDocument())
    expect(screen.getByText('Albums')).toBeInTheDocument()
    expect(screen.getByText('Artists')).toBeInTheDocument()
  })
})
