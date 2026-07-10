import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { SearchPanel } from '@/components/search-panel'
import type { EntityCard, ExternalResult, SearchResult } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// Idioma fixo (pt-BR): isola o SearchPanel do LanguageProvider real — sem tocar o
// endpoint de settings e sem afetar as asserções de fetch da busca.
vi.mock('@/lib/language-context', () => ({
  useLanguage: () => ({
    lang: 'pt-BR',
    allowed: ['pt-BR'],
    primary: 'pt-BR',
    setLang: () => {},
  }),
}))

// Spy de navegação: o import bem-sucedido deve levar ao detalhe da entity.
const navigate = vi.fn()

// Mocka o router: `Link` (usado pelo PosterCard) vira um <a>, e `useNavigate`
// devolve o spy para verificar o redirecionamento pós-import.
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

// Roteia fetch por URL: /search devolve o resultado dado; /import devolve a
// entity resolvida (com id, para a navegação).
function fetchRouter(result: SearchResult) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
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

// Setup comum dos casos que buscam: stuba o fetch, renderiza e digita o termo
// (dispara o debounce). Centralizado para os testes não duplicarem o boilerplate.
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
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('dispara a busca após o debounce e separa locais de externos', async () => {
    const { fetchMock } = await buscar({
      query: 'matrix',
      local: [localCard],
      external: [externalItem],
      external_available: true,
    })

    await waitFor(() =>
      expect(screen.getByText('Matrix Local')).toBeInTheDocument(),
    )
    expect(screen.getByText('No seu catálogo')).toBeInTheDocument()
    expect(screen.getByText('Resultados externos')).toBeInTheDocument()
    expect(screen.getByText('The Matrix')).toBeInTheDocument()
    // Só chamou /search (com o termo debounced), não por caractere.
    expect(
      fetchMock.mock.calls.every(([u]) =>
        String(u).startsWith('/api/v1/search'),
      ),
    ).toBe(true)
  })

  it('resultado externo já na biblioteca vira "Na biblioteca" sem Importar', async () => {
    // Interno-first (0027): quando o item externo já é entity (internal_id), o card
    // some com o Importar e mostra o selo — evita reimportar o que já está no catálogo.
    // O internal_id vai defensivamente (cast) porque o tipo gerado só o ganha após a
    // regeneração do backend (feita em paralelo); o runtime carrega o campo.
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
    // O POST /import foi enviado com source/source_id/type do item.
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
