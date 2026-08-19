import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import {
  RecentlyAddedRail,
  RecentlyAddedSection,
} from '@/components/recently-added-rail'
import type { EntityCard, EntityType } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { entityCardBase } from '@/test/detail-harness'

// card: mesmos defaults de entityCardBase, só troca id/título pros valores que
// este arquivo já usava (id 'x', título 'Matrix') — evita duplicar a fábrica
// inteira (jscpd) só por causa desses dois campos.
function card(over: Partial<EntityCard> = {}): EntityCard {
  return entityCardBase({ id: 'x', title: 'Matrix', ...over })
}

// O <Link> do TanStack Router exige contexto de router; mockado por um <a>
// simples, mesmo padrão de poster-card.test.tsx/poster-grid.test.tsx.
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
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
  it('busca com sort=-created_at/owned=true/limit=10 e renderiza os itens reais', async () => {
    const items = [
      card({ id: 'a', title: 'Matrix', type: 'movie' }),
      card({ id: 'b', title: 'Duna', type: 'movie' }),
    ]
    const fetchMock = vi.fn().mockResolvedValue(entityList(items))
    vi.stubGlobal('fetch', fetchMock)

    renderWithClient(<RecentlyAddedRail type="movie" label="Filmes" />)

    await waitFor(() => expect(screen.getByText('Matrix')).toBeInTheDocument())
    expect(screen.getByText('Duna')).toBeInTheDocument()
    expect(screen.getByText('Filmes')).toBeInTheDocument()

    const url = new URL(fetchMock.mock.calls[0][0] as string, 'http://x')
    expect(url.pathname).toBe('/api/v1/entities')
    expect(url.searchParams.get('type')).toBe('movie')
    expect(url.searchParams.get('sort')).toBe('-created_at')
    expect(url.searchParams.get('owned')).toBe('true')
    expect(url.searchParams.get('limit')).toBe('10')
  })

  it('mostra o estado de carregamento com o rótulo', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )
    renderWithClient(<RecentlyAddedRail type="movie" label="Filmes" />)
    expect(screen.getByText('Filmes')).toBeInTheDocument()
    expect(screen.getByText(/Carregando filmes/i)).toBeInTheDocument()
  })

  it('mostra o estado de erro', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse({ error: { code: 'x', message: 'falhou' } }, false, 500),
        ),
    )
    renderWithClient(<RecentlyAddedRail type="movie" label="Filmes" />)
    await waitFor(() =>
      expect(
        screen.getByText('Não foi possível carregar "Filmes"'),
      ).toBeInTheDocument(),
    )
  })

  it('não renderiza nada (nem o título) quando o schema não tem item', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(entityList([])))
    const { container } = renderWithClient(
      <RecentlyAddedRail type="album" label="Álbuns" />,
    )
    await waitFor(() => expect(container).toBeEmptyDOMElement())
    expect(screen.queryByText('Álbuns')).not.toBeInTheDocument()
  })
})

describe('RecentlyAddedSection', () => {
  it('não renderiza nada quando GET /entities/types devolve lista vazia (biblioteca zerada)', async () => {
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

  it('renderiza um carrossel por tipo devolvido, incluindo música (0105-0107), sem lista hardcoded', async () => {
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

    await waitFor(() => expect(screen.getByText('Filmes')).toBeInTheDocument())
    expect(screen.getByText('Álbuns')).toBeInTheDocument()
    expect(screen.getByText('Artistas')).toBeInTheDocument()
  })
})
