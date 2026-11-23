import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HistoricoOrfaosScreen } from '@/routes/historico-orfaos'
import type { HistoricoEventoOrfao, HistoricoEventoOrfaoList } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { erroJson, okJson } from '@/test/detail-harness'

// Factory of an orphan-list item — `over` overrides
// fields per test (raw_title absent, partial raw_external_ids, etc.).
function eventoOrfao(
  over: Partial<HistoricoEventoOrfao> = {},
): HistoricoEventoOrfao {
  return {
    id: 'evt-1',
    kind: 'assistir',
    status: 'completed',
    occurred_at: '2026-08-01T12:00:00Z',
    source: 'jellyfin-webhook',
    raw_title: 'Duna',
    raw_year: '2021',
    raw_external_ids: { tmdb: '438631' },
    ...over,
  }
}

// Routes the fetch of `GET /api/v1/historico/orfaos` — `paginas` maps offset
// -> response, allowing a page advance to be simulated without a real server.
function fetchRouter(opts?: {
  pagina?: (params: URLSearchParams) => HistoricoEventoOrfaoList
  erro?: { status: number; code: string; message: string }
}) {
  return vi.fn(async (url: string) => {
    const u = new URL(String(url), 'http://localhost')
    if (u.pathname !== '/api/v1/historico/orfaos') {
      throw new Error(`rota nao mockada: ${u.pathname}`)
    }
    if (opts?.erro) {
      return erroJson(opts.erro.status, opts.erro.code, opts.erro.message)
    }
    const resposta = opts?.pagina
      ? opts.pagina(u.searchParams)
      : { items: [], total: 0, limit: 20, offset: 0 }
    return okJson(resposta)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('HistoricoOrfaosScreen', () => {
  it('an empty list shows the empty state message', async () => {
    vi.stubGlobal('fetch', fetchRouter())
    renderWithClient(<HistoricoOrfaosScreen />)

    expect(
      await screen.findByText('No event without a related item'),
    ).toBeInTheDocument()
  })

  it('a list with items shows the title and the formatted IDs', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        pagina: () => ({
          items: [eventoOrfao()],
          total: 1,
          limit: 20,
          offset: 0,
        }),
      }),
    )
    renderWithClient(<HistoricoOrfaosScreen />)

    expect(await screen.findByText('Duna')).toBeInTheDocument()
    expect(screen.getByText('(2021)')).toBeInTheDocument()
    expect(screen.getByText(/Jellyfin/)).toBeInTheDocument()
  })

  it('an item without raw_title falls back to the raw IDs', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        pagina: () => ({
          items: [
            eventoOrfao({
              raw_title: null,
              raw_year: null,
              raw_external_ids: { tmdb: '123', imdb: 'tt999' },
            }),
          ],
          total: 1,
          limit: 20,
          offset: 0,
        }),
      }),
    )
    renderWithClient(<HistoricoOrfaosScreen />)

    expect(
      await screen.findByText('Item without a title (tmdb=123 / imdb=tt999)'),
    ).toBeInTheDocument()
  })

  it('a real GET error shows the ErrorState', async () => {
    vi.stubGlobal(
      'fetch',
      fetchRouter({
        erro: { status: 500, code: 'erro_interno', message: 'falhou' },
      }),
    )
    renderWithClient(<HistoricoOrfaosScreen />)

    expect(
      await screen.findByText(
        'Could not load the events without a related item',
      ),
    ).toBeInTheDocument()
  })

  it('pagination: "Next" triggers a new call with a bigger offset', async () => {
    const user = userEvent.setup()
    const fetchMock = fetchRouter({
      pagina: (params) => ({
        items: [eventoOrfao({ id: `evt-${params.get('offset') ?? '0'}` })],
        total: 50,
        limit: 20,
        offset: Number(params.get('offset') ?? '0'),
      }),
    })
    vi.stubGlobal('fetch', fetchMock)
    renderWithClient(<HistoricoOrfaosScreen />)

    await screen.findByText('Duna')
    const botaoProxima = screen.getByRole('button', { name: 'Next' })
    expect(botaoProxima).not.toBeDisabled()

    await user.click(botaoProxima)

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([url]) => String(url).includes('offset=20')),
      ).toBe(true)
    })
  })
})

it('renders the "Events without a related item" heading', async () => {
  vi.stubGlobal('fetch', fetchRouter())
  renderWithClient(<HistoricoOrfaosScreen />)
  expect(
    await screen.findByRole('heading', {
      name: 'Events without a related item',
    }),
  ).toBeInTheDocument()
})

it('a failed load offers a retry', async () => {
  const user = userEvent.setup()
  let chamadas = 0
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (!String(url).includes('/api/v1/historico/orfaos')) {
        throw new Error('rota nao mockada')
      }
      chamadas += 1
      if (chamadas === 1) {
        return erroJson(503, 'indisponivel', 'sem resposta')
      }
      return okJson({
        items: [eventoOrfao()],
        total: 1,
        limit: 20,
        offset: 0,
      })
    }),
  )
  renderWithClient(<HistoricoOrfaosScreen />)

  await user.click(await screen.findByRole('button', { name: 'Try again' }))

  expect(await screen.findByText('Duna')).toBeInTheDocument()
})
