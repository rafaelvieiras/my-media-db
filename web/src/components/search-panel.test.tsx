import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { SearchPanel } from '@/components/search-panel'
import type { EntityCard, ExternalResult, SearchResult } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Fixed language (pt-BR): it isolates the SearchPanel from the real LanguageProvider — without touching the
// settings endpoint and without affecting the fetch assertions of the search.
vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: 'pt-BR',
    allowed: ['pt-BR'],
    primary: 'pt-BR',
    setLang: () => {},
  }),
}))

// Navigation spy: a successful import has to take you to the detail of the entity.
const navigate = vi.fn()

// Mocks the router: `Link` (used by the PosterCard) becomes an <a>, and `useNavigate`
// returns the spy to check the redirect after the import.
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useNavigate: () => navigate,
}))

function okJson<T>(body: T, status = 200) {
  return { ok: true, status, json: async () => body } as Response
}

const localCard: EntityCard = {
  id: 'local-1',
  type: 'movie',
  title: 'Matrix Local',
  status: 'cataloged',
  owned: true,
  file_count: 1,
  match_confidence: 100,
  poster_url: null,
  catalog_completeness: null,
}

const seasonCard: EntityCard = {
  id: 'season-1',
  type: 'season',
  title: 'Temporada 1 Local',
  status: 'cataloged',
  owned: false,
  file_count: 0,
  match_confidence: null,
  poster_url: null,
  catalog_completeness: null,
}

const episodeCard: EntityCard = {
  id: 'episode-1',
  type: 'episode',
  title: 'Episodio 1 Local',
  status: 'cataloged',
  owned: false,
  file_count: 0,
  match_confidence: null,
  poster_url: null,
  catalog_completeness: null,
}

const artistaCard: EntityCard = {
  id: 'artista-1',
  type: 'artista',
  title: 'Artista Local',
  status: 'cataloged',
  owned: true,
  file_count: 3,
  match_confidence: null,
  poster_url: null,
  catalog_completeness: null,
}

const externalItem: ExternalResult = {
  source: 'tmdb',
  source_id: '603',
  type: 'movie',
  title: 'The Matrix',
  year: '1999',
  overview: 'Um hacker descobre a verdade.',
  poster_url: null,
  saved: false,
}

// Top-level types returned by GET /entities/types: the `MediaTypeFilter` (inside
// the SearchPanel) fires that query by itself on mount — every
// test has to route that URL, otherwise `fetchRouter` blows up with "unmocked
// route".
const TIPOS_DE_TOPO = [
  { type: 'movie', count: 1 },
  { type: 'tv', count: 1 },
  { type: 'artista', count: 1 },
]

// Routes the fetch by URL: /entities/types feeds the MediaTypeFilter; /search
// returns the given result; /import returns the resolved entity (with an id, for the
// navigation).
function fetchRouter(result: SearchResult) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u.startsWith('/api/v1/entities/types') && method === 'GET') {
      return okJson(TIPOS_DE_TOPO)
    }
    if (u.startsWith('/api/v1/search') && method === 'GET') {
      return okJson(result)
    }
    if (u === '/api/v1/import' && method === 'POST') {
      return okJson(
        { id: 'ent-imported', type: 'movie', title: 'The Matrix' },
        201,
      )
    }
    throw new Error(`rota não mockada: ${method} ${u}`)
  })
}

// Common setup of the cases that search: it stubs the fetch, renders and types the term
// (it fires the debounce). Centralized so the tests do not duplicate the boilerplate.
async function buscar(result: SearchResult) {
  const user = userEvent.setup()
  const fetchMock = fetchRouter(result)
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<SearchPanel />)
  await user.type(screen.getByLabelText('Buscar título'), 'matrix')
  return { user, fetchMock }
}

afterEach(() => {
  vi.restoreAllMocks()
  navigate.mockReset()
})

describe('SearchPanel', () => {
  it('mostra o estado inicial de instrução e não busca com input vazio', () => {
    const fetchMock = fetchRouter({
      query: '',
      local: [],
      external: [],
      external_available: true,
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<SearchPanel />)

    expect(
      screen.getByText('Busque no seu catálogo e em fontes externas'),
    ).toBeInTheDocument()
    // With an empty input the search (`/search`) does not fire — but the MediaTypeFilter
    // fetches the top-level types (`/entities/types`) by itself, on
    // mount, regardless of the term. It is no longer "fetch never called".
    expect(
      fetchMock.mock.calls.some(([u]) =>
        String(u).startsWith('/api/v1/search'),
      ),
    ).toBe(false)
  })

  it('dispara a busca após o debounce e agrupa os locais por tipo real (0113)', async () => {
    const { fetchMock } = await buscar({
      query: 'matrix',
      local: [localCard],
      external: [externalItem],
      external_available: true,
    })

    await waitFor(() =>
      expect(screen.getByText('Matrix Local')).toBeInTheDocument(),
    )
    // The fixed "Filmes e séries" bucket no longer exists: a `movie` card
    // becomes the "Filmes" section. `getByRole('heading', ...)` (not `getByText`)
    // because "Filmes" now is ALSO an `<option>` of the dynamic MediaTypeFilter
    // — the text alone is no longer unique on the screen.
    expect(screen.getByRole('heading', { name: 'Filmes' })).toBeInTheDocument()
    expect(screen.queryByText('Filmes e séries')).toBeNull()
    expect(screen.getByText('Resultados externos')).toBeInTheDocument()
    expect(screen.getByText('The Matrix')).toBeInTheDocument()
    // It only called /search and /entities/types (with the debounced term), not per
    // character.
    expect(
      fetchMock.mock.calls.every(
        ([u]) =>
          String(u).startsWith('/api/v1/search') ||
          String(u).startsWith('/api/v1/entities/types'),
      ),
    ).toBe(true)
  })

  it('agrupa um resultado de tipo não-filme/série (artista) na própria seção, sem balde "Filmes e séries" (0113)', async () => {
    await buscar({
      query: 'artista',
      local: [localCard, artistaCard],
      external: [],
      external_available: true,
    })

    await waitFor(() =>
      expect(screen.getByText('Artista Local')).toBeInTheDocument(),
    )
    // `getByRole('heading', ...)`, not `getByText`: "Filmes"/"Artistas" are also
    // `<option>` of the dynamic MediaTypeFilter, so the text alone
    // is no longer unique on the screen.
    expect(screen.getByRole('heading', { name: 'Filmes' })).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Artistas' }),
    ).toBeInTheDocument()
    expect(screen.queryByText('Filmes e séries')).toBeNull()
    expect(screen.getByText('Matrix Local')).toBeInTheDocument()
  })

  it('mantém sub-nós ocultos por padrão, mesmo vindo no payload (0033)', async () => {
    await buscar({
      query: 'simpsons',
      local: [localCard, seasonCard, episodeCard],
      external: [],
      external_available: true,
    })
    await waitFor(() =>
      expect(screen.getByText('Matrix Local')).toBeInTheDocument(),
    )
    // Without a toggle on: neither the section nor the season/episode cards.
    expect(screen.queryByRole('heading', { name: 'Temporadas' })).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Episódios' })).toBeNull()
    expect(screen.queryByText('Temporada 1 Local')).toBeNull()
    expect(screen.queryByText('Episodio 1 Local')).toBeNull()
  })

  // A single path, exercised for each sub-node: turning the toggle on opens its own
  // section with the card of that type, keeps the other sub-node hidden and redoes the search
  // with the corresponding `include=`. Parameterized so the two toggles do not
  // duplicate the same test body (measured by jscpd).
  const casosSubNo = [
    {
      toggle: 'Temporadas',
      visivel: 'Temporada 1 Local',
      oculto: 'Episodio 1 Local',
      include: 'include=season',
    },
    {
      toggle: 'Episódios',
      visivel: 'Episodio 1 Local',
      oculto: 'Temporada 1 Local',
      include: 'include=episode',
    },
  ] as const

  it.each(casosSubNo)(
    'liga "$toggle": abre a seção com o card e envia $include',
    async ({ toggle, visivel, oculto, include }) => {
      const { user, fetchMock } = await buscar({
        query: 'simpsons',
        local: [localCard, seasonCard, episodeCard],
        external: [],
        external_available: true,
      })
      await waitFor(() =>
        expect(screen.getByText('Matrix Local')).toBeInTheDocument(),
      )

      await user.click(screen.getByRole('button', { name: toggle }))

      await waitFor(() =>
        expect(
          screen.getByRole('heading', { name: toggle }),
        ).toBeInTheDocument(),
      )
      // The sub-node turned on appears in its own section; the other stays hidden.
      expect(screen.getByText(visivel)).toBeInTheDocument()
      expect(screen.queryByText(oculto)).toBeNull()
      // The refetch carried the corresponding include=.
      await waitFor(() =>
        expect(
          fetchMock.mock.calls.some(([u]) => String(u).includes(include)),
        ).toBe(true),
      )
    },
  )

  it('resultado externo já na biblioteca vira "Na biblioteca" sem Importar', async () => {
    // Internal-first: when the external item already is an entity (internal_id), the card
    // drops the Importar and shows the badge — it avoids reimporting what is already in the catalog.
    // The internal_id goes defensively (cast) because the generated type only gains it after the
    // backend regeneration (done in parallel); the runtime carries the field.
    const externalSalvo = {
      ...externalItem,
      internal_id: 'ent-603',
    } as ExternalResult
    await buscar({
      query: 'matrix',
      local: [],
      external: [externalSalvo],
      external_available: true,
    })

    await waitFor(() =>
      expect(screen.getByText('The Matrix')).toBeInTheDocument(),
    )
    expect(screen.getByText('Na biblioteca')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Importar/ })).toBeNull()
  })

  it('avisa quando a busca externa está indisponível (não é zero resultados)', async () => {
    await buscar({
      query: 'matrix',
      local: [],
      external: [],
      external_available: false,
    })

    await waitFor(() =>
      expect(
        screen.getByText('Busca externa indisponível'),
      ).toBeInTheDocument(),
    )
    expect(
      screen.queryByText('Nenhum resultado externo para importar.'),
    ).not.toBeInTheDocument()
  })

  it('importa um resultado externo, invalida o catálogo e navega ao detalhe', async () => {
    const { user, fetchMock } = await buscar({
      query: 'matrix',
      local: [],
      external: [externalItem],
      external_available: true,
    })
    await waitFor(() =>
      expect(screen.getByText('The Matrix')).toBeInTheDocument(),
    )

    await user.click(screen.getByRole('button', { name: /Importar/ }))

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: '/entities/$id',
        params: { id: 'ent-imported' },
      }),
    )
    // The POST /import was sent with source/source_id/type of the item.
    const importCall = fetchMock.mock.calls.find(
      ([u, init]) =>
        String(u) === '/api/v1/import' &&
        (init as RequestInit | undefined)?.method === 'POST',
    )
    expect(importCall).toBeDefined()
    expect(JSON.parse((importCall![1] as RequestInit).body as string)).toEqual({
      source: 'tmdb',
      source_id: '603',
      type: 'movie',
    })
  })
})
