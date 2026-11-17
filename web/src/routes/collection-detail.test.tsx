import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CollectionDetailScreen } from '@/routes/collection-detail'
import type { ExternalCollectionDetail } from '@/lib/api'
import { I18nProvider } from '@/lib/i18n'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson, stubExternalFetch } from '@/test/detail-harness'

const estado = vi.hoisted(() => ({ lang: 'pt-BR' }))
const spies = vi.hoisted(() => ({
  navigate: vi.fn(),
  back: vi.fn(),
  params: { source: 'tmdb', id: '726871' } as Record<string, string>,
}))

// The factories of vi.mock are hoisted above the imports: they use a DYNAMIC import() of the
// harness (resolved at call time, without TDZ) and read the hoisted `estado`/`spies`.
vi.mock('@/lib/language-context', async () => {
  const { fakeLanguage } = await import('@/test/detail-harness')
  return { useLanguage: () => fakeLanguage(estado.lang) }
})
vi.mock('@tanstack/react-router', async () => {
  const { routerMock } = await import('@/test/detail-harness')
  return routerMock(spies)
})

function colecao(
  over: Partial<ExternalCollectionDetail> = {},
): ExternalCollectionDetail {
  return {
    source: 'tmdb',
    source_id: '726871',
    name: 'Coleção Duna',
    overview: 'A saga de Arrakis.',
    poster_url: null,
    backdrop_url: null,
    parts: [
      {
        source_id: '438631',
        type: 'movie',
        title: 'Duna',
        year: '2021',
        overview: 'Paul desperta.',
        poster_url: null,
      },
      {
        source_id: '693134',
        type: 'movie',
        title: 'Duna: Parte 2',
        year: '2024',
        overview: null,
        poster_url: null,
      },
    ],
    ...over,
  }
}

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  spies.back.mockReset()
  estado.lang = 'pt-BR'
})

describe('CollectionDetailScreen', () => {
  it('renders the header and the parts of the collection', async () => {
    stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)

    expect(
      await screen.findByRole('heading', { name: 'Coleção Duna' }),
    ).toBeTruthy()
    expect(screen.getByText('A saga de Arrakis.')).toBeTruthy()
    expect(screen.getByText('Duna')).toBeTruthy()
    expect(screen.getByText('Duna: Parte 2')).toBeTruthy()
  })

  it('renders the backdrop of the collection when present', async () => {
    stubExternalFetch(() =>
      okJson(colecao({ backdrop_url: 'https://img.tmdb.org/colbg.jpg' })),
    )
    renderWithClient(<CollectionDetailScreen />)
    await screen.findByRole('heading', { name: 'Coleção Duna' })

    const banner = document.querySelector(
      'img[src="https://img.tmdb.org/colbg.jpg"]',
    )
    expect(banner).not.toBeNull()
  })

  it('a part navigates to the external detail and brings the Import shortcut', async () => {
    stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)

    const link = await screen.findByRole('link', { name: /Duna: Parte 2/ })
    expect(link.getAttribute('href')).toBe('/external/tmdb/movie/693134')
    // Reuse of ExternalResultCard: each part has the Import button (EXACT match —
    // the header has "Import collection", which must not count here).
    expect(screen.getAllByRole('button', { name: 'Import' }).length).toBe(2)
  })

  it('a part already in the library becomes an internal card (link /entities, no Import) — 0026', async () => {
    const comInterno = colecao({
      parts: [
        {
          source_id: '438631',
          type: 'movie',
          title: 'Duna',
          year: '2021',
          overview: null,
          poster_url: null,
          // internal_id is additive: the cast avoids depending on the regenerated type.
          internal_id: 'ent-duna',
        },
        {
          source_id: '693134',
          type: 'movie',
          title: 'Duna: Parte 2',
          year: '2024',
          overview: null,
          poster_url: null,
        },
      ] as ExternalCollectionDetail['parts'],
    })
    stubExternalFetch(() => okJson(comInterno))
    renderWithClient(<CollectionDetailScreen />)
    await screen.findByRole('heading', { name: 'Coleção Duna' })

    // The imported part links to the internal one and gains the badge; the other one stays external.
    const interno = screen
      .getAllByRole('link')
      .find((l) => l.getAttribute('href') === '/entities/ent-duna')
    expect(interno).toBeTruthy()
    expect(screen.getByText('In the library')).toBeTruthy()
    // 1 part "Import" is left (the non-imported one); the header is still "Import collection".
    expect(screen.getAllByRole('button', { name: 'Import' }).length).toBe(1)
  })

  it('an already imported collection swaps "Import collection" for the internal "View collection" — 0026', async () => {
    stubExternalFetch(() =>
      okJson(
        colecao({
          internal_id: 'ent-col',
        } as Partial<ExternalCollectionDetail>),
      ),
    )
    renderWithClient(<CollectionDetailScreen />)

    const ver = await screen.findByRole('link', { name: /View collection/ })
    expect(ver.getAttribute('href')).toBe('/entities/ent-col')
    expect(
      screen.queryByRole('button', { name: 'Import collection' }),
    ).toBeNull()
  })

  it('"Import collection" imports with type=collection and navigates to the entity', async () => {
    const fetchMock = stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)

    const botao = await screen.findByRole('button', {
      name: 'Import collection',
    })
    await userEvent.setup().click(botao)

    await vi.waitFor(() =>
      expect(spies.navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'ent-1' },
      }),
    )
    const chamada = fetchMock.mock.calls.find(
      (c) =>
        new URL(String(c[0]), 'http://localhost').pathname === '/api/v1/import',
    )
    expect(
      JSON.parse(String((chamada?.[1] as RequestInit).body)),
    ).toMatchObject({ source: 'tmdb', source_id: '726871', type: 'collection' })
  })

  it('a collection without items shows the notice', async () => {
    stubExternalFetch(() => okJson(colecao({ parts: [] })))
    renderWithClient(<CollectionDetailScreen />)
    expect(
      await screen.findByText('This collection has no items.'),
    ).toBeTruthy()
  })

  it('passes the active language into the query (it enters the queryKey)', async () => {
    estado.lang = 'en'
    const fetchMock = stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)
    await screen.findByRole('heading', { name: 'Coleção Duna' })

    const chamada = fetchMock.mock.calls.find((c) =>
      String(c[0]).includes('/collection/'),
    )
    expect(String(chamada?.[0])).toContain('lang=en')
  })

  it('404: collection missing in the source', async () => {
    stubExternalFetch(() => erroJson(404, 'not_found', 'não existe'))
    renderWithClient(<CollectionDetailScreen />)
    expect(
      await screen.findByText('Collection not found in the source'),
    ).toBeTruthy()
  })

  it('503: source without the collection', async () => {
    stubExternalFetch(() => erroJson(503, 'unavailable', 'sem coleção'))
    renderWithClient(<CollectionDetailScreen />)
    expect(await screen.findByText('Source unavailable right now')).toBeTruthy()
  })

  it('a generic error (400) falls back to the default message', async () => {
    stubExternalFetch(() => erroJson(400, 'bad_request', 'idioma inválido'))
    renderWithClient(<CollectionDetailScreen />)
    expect(
      await screen.findByText('Could not load the collection'),
    ).toBeTruthy()
  })

  it('Back returns through the history', async () => {
    stubExternalFetch(() => okJson(colecao()))
    renderWithClient(<CollectionDetailScreen />)
    await screen.findByRole('heading', { name: 'Coleção Duna' })

    await userEvent.setup().click(screen.getByRole('button', { name: /Back/ }))
    expect(spies.back).toHaveBeenCalledTimes(1)
  })

  it('renders the copy in pt-BR when the interface language is pt-BR', async () => {
    // The interface language is a browser preference (ADR-0032): pin it before
    // rendering so the Portuguese copy is the one under test.
    localStorage.setItem('mmdb.ui-lang', 'pt-BR')
    stubExternalFetch(() => okJson(colecao()))
    renderWithClient(
      <I18nProvider>
        <CollectionDetailScreen />
      </I18nProvider>,
    )

    expect(
      await screen.findByRole('heading', { name: 'Filmes da coleção' }),
    ).toBeTruthy()
    expect(
      screen.getByRole('button', { name: 'Importar coleção' }),
    ).toBeTruthy()
  })
})
