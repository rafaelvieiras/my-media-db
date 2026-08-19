import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { PosterGrid } from '@/components/poster-grid'
import type { EntityCard } from '@/lib/api'
import { renderWithClient } from '@/test/render'

// O filtro de tipo (briefing 0110) migrou de `useState` local pro search
// param `?type=` da rota `/` — mesmo padrão de mock de router com estado
// mutável de `entity-detail-series.test.tsx` (`state.search`/`state.navigate`),
// pra afirmar tanto a LEITURA (refresh com `?type=X` filtra de cara) quanto a
// ESCRITA (trocar o Select navega com o search certo).
const state = vi.hoisted(() => ({
  search: { type: undefined as string | undefined },
  navigate: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  useSearch: () => state.search,
  useNavigate: () => state.navigate,
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}))

function page(items: EntityCard[], total = items.length) {
  return {
    ok: true,
    status: 200,
    json: async () => ({ items, total, limit: 24, offset: 0 }),
  } as Response
}

beforeEach(() => {
  state.search = { type: undefined }
  state.navigate = vi.fn()
})

// renderEObterTypeDaQuery renderiza o grid e devolve o `?type=` que a PRIMEIRA
// chamada de fetch usou — o núcleo comum dos testes de leitura da URL na
// montagem (jscpd).
async function renderEObterTypeDaQuery(): Promise<string | null> {
  const fetchMock = vi.fn().mockResolvedValue(page([]))
  vi.stubGlobal('fetch', fetchMock)
  renderWithClient(<PosterGrid />)
  await waitFor(() => expect(fetchMock).toHaveBeenCalled())
  const url = new URL(fetchMock.mock.calls[0][0] as string, 'http://x')
  return url.searchParams.get('type')
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PosterGrid', () => {
  it('mostra o estado de carregamento', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )
    renderWithClient(<PosterGrid />)
    expect(screen.getByText('Carregando catálogo…')).toBeInTheDocument()
  })

  it('mostra o estado de erro', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({ error: { code: 'x', message: 'falhou' } }),
      } as Response),
    )
    renderWithClient(<PosterGrid />)
    await waitFor(() =>
      expect(
        screen.getByText('Não foi possível carregar o catálogo'),
      ).toBeInTheDocument(),
    )
  })

  it('mostra o estado vazio que orienta o usuário', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(page([], 0)))
    renderWithClient(<PosterGrid />)
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
    renderWithClient(<PosterGrid />)
    await waitFor(() => expect(screen.getByText('Alfa')).toBeInTheDocument())
    expect(screen.getByText('1–1 de 1')).toBeInTheDocument()
  })
})

// URL como fonte de verdade do filtro de tipo (briefing 0110).
describe('PosterGrid — filtro de tipo via URL (0110)', () => {
  it('refresh com "?type=album" na URL já filtra o catálogo por album (leitura na montagem)', async () => {
    state.search = { type: 'album' }
    expect(await renderEObterTypeDaQuery()).toBe('album')
  })

  it('sem "?type=" na URL, busca sem filtro de tipo (comportamento atual preservado)', async () => {
    expect(await renderEObterTypeDaQuery()).toBeNull()
  })

  it('trocar o MediaTypeFilter da tela navega pra "/" com o novo "?type="', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(page([])))
    renderWithClient(<PosterGrid />)

    const select = await screen.findByLabelText('Filtrar por tipo')
    fireEvent.change(select, { target: { value: 'tv' } })

    expect(state.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ to: '/', search: { type: 'tv' } }),
    )
  })

  it('voltar pra "Todos" no MediaTypeFilter navega com search vazio (sem "type")', async () => {
    state.search = { type: 'movie' }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(page([])))
    renderWithClient(<PosterGrid />)

    const select = await screen.findByLabelText('Filtrar por tipo')
    fireEvent.change(select, { target: { value: 'all' } })

    expect(state.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ to: '/', search: {} }),
    )
  })
})
