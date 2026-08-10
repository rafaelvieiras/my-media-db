import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  EstadoAssistidoBadge,
  HistoricoSection,
} from '@/components/historico-section'
import type { HistoricoEvento } from '@/lib/api'
import { renderWithClient } from '@/test/render'
import { stubRota, stubRotas } from '@/test/stub-fetch'

// Testa o histórico de consumo (briefing 0094, dá rosto ao 0092/0093/ADR-0022):
// o selo "Assistido" (`EstadoAssistidoBadge`) e a seção "Histórico"
// (`HistoricoSection`) — carregando/erro/vazio (mesmo padrão da `SimilaresRail`:
// vazio some por inteiro) e a ação de apagar um evento (confirmação em 2
// etapas, mesmo padrão de `MembroEditavel`/collection-members.tsx).

function eventosResponse(eventos: HistoricoEvento[]) {
  return { ok: true, status: 200, json: async () => ({ eventos }) } as Response
}

function erroResponse(status: number, code: string, message: string) {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  } as Response
}

function eventoFixture(over: Partial<HistoricoEvento> = {}): HistoricoEvento {
  return {
    id: 'ev1',
    kind: 'assistir',
    status: 'completed',
    progress_percent: null,
    position_seconds: null,
    occurred_at: '2026-01-05T20:00:00Z',
    source: 'jellyfin-webhook',
    payload: null,
    created_at: '2026-01-05T20:00:00Z',
    ...over,
  }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('EstadoAssistidoBadge', () => {
  it('mostra "Assistido" com a data quando o evento assistir mais recente é completed', async () => {
    stubRota(
      (p) => p === '/api/v1/entities/e1/historico/estado',
      () =>
        ({
          ok: true,
          status: 200,
          json: async () => ({
            estado: {
              assistir: {
                status: 'completed',
                occurred_at: '2026-01-05T20:00:00Z',
                source: 'jellyfin-webhook',
              },
            },
          }),
        }) as Response,
    )
    renderWithClient(<EstadoAssistidoBadge entityId="e1" />)
    expect(await screen.findByText(/Assistido em/)).toBeInTheDocument()
  })

  it('sem estado (nunca assistido), não mostra nada', async () => {
    stubRota(
      (p) => p === '/api/v1/entities/e1/historico/estado',
      () =>
        ({
          ok: true,
          status: 200,
          json: async () => ({ estado: {} }),
        }) as Response,
    )
    const { container } = renderWithClient(
      <EstadoAssistidoBadge entityId="e1" />,
    )
    await waitFor(() => expect(container).not.toHaveTextContent('Assistido'))
  })
})

describe('HistoricoSection', () => {
  it('mostra carregando e depois a timeline com status/data/origem', async () => {
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/historico' && m === 'GET',
      () =>
        eventosResponse([
          eventoFixture({
            id: 'ev1',
            status: 'completed',
            source: 'jellyfin-webhook',
          }),
        ]),
    )
    renderWithClient(<HistoricoSection entityId="e1" />)
    expect(screen.getByText('Carregando histórico…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByText('Assistiu')).toBeInTheDocument(),
    )
    expect(screen.getByText(/Jellyfin/)).toBeInTheDocument()
  })

  it('mostra erro quando a busca falha', async () => {
    stubRota(
      (p) => p === '/api/v1/entities/e1/historico',
      () => erroResponse(404, 'not_found', 'não achou'),
    )
    renderWithClient(<HistoricoSection entityId="e1" />)
    await waitFor(() =>
      expect(
        screen.getByText('Não foi possível carregar o histórico'),
      ).toBeInTheDocument(),
    )
  })

  it('some por inteiro (sem heading) quando não há nenhum evento', async () => {
    stubRota(
      (p) => p === '/api/v1/entities/e1/historico',
      () => eventosResponse([]),
    )
    const { container } = renderWithClient(<HistoricoSection entityId="e1" />)
    await waitFor(() =>
      expect(screen.queryByText('Carregando histórico…')).toBeNull(),
    )
    expect(screen.queryByRole('heading', { name: 'Histórico' })).toBeNull()
    expect(container).toBeEmptyDOMElement()
  })

  it('apaga um evento após confirmação em 2 etapas', async () => {
    const user = userEvent.setup()
    const fetchMock = stubRotas([
      [
        (p, m) => p === '/api/v1/entities/e1/historico' && m === 'GET',
        () => eventosResponse([eventoFixture({ id: 'ev1' })]),
      ],
      [
        (p, m) => p === '/api/v1/entities/e1/historico/ev1' && m === 'DELETE',
        () => ({ ok: true, status: 204, json: async () => ({}) }) as Response,
      ],
    ])

    renderWithClient(<HistoricoSection entityId="e1" />)
    await screen.findByText('Assistiu')

    // 1ª etapa: clique no ícone abre "Apagar?"/"Cancelar" — ainda não apaga.
    await user.click(screen.getByRole('button', { name: 'Apagar evento' }))
    const confirmar = screen.getByRole('button', { name: 'Apagar?' })
    expect(
      fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE'),
    ).toBe(false)

    // 2ª etapa: confirma — dispara o DELETE.
    await user.click(confirmar)
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url) === '/api/v1/entities/e1/historico/ev1' &&
            init?.method === 'DELETE',
        ),
      ).toBe(true),
    )
  })

  it('"Cancelar" na confirmação não apaga o evento', async () => {
    const user = userEvent.setup()
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/historico' && m === 'GET',
      () => eventosResponse([eventoFixture({ id: 'ev1' })]),
    )
    renderWithClient(<HistoricoSection entityId="e1" />)
    await screen.findByText('Assistiu')

    await user.click(screen.getByRole('button', { name: 'Apagar evento' }))
    await user.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(
      screen.getByRole('button', { name: 'Apagar evento' }),
    ).toBeInTheDocument()
  })
})
