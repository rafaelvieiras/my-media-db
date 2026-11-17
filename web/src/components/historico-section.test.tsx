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

// Tests the consumption history (it gives a face to ADR-0022):
// the "Watched" badge (`EstadoAssistidoBadge`) and the "History" section
// (`HistoricoSection`) — loading/error/empty (same pattern as `SimilaresRail`:
// empty disappears entirely) and the action of deleting an event (two-step
// confirmation, same pattern as `MembroEditavel`/collection-members.tsx).

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
  it('shows "Watched" with the date when the most recent watching event is completed', async () => {
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
    expect(await screen.findByText(/Watched on/)).toBeInTheDocument()
  })

  it('with no state (never watched), shows nothing', async () => {
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
    await waitFor(() => expect(container).not.toHaveTextContent('Watched'))
  })
})

describe('HistoricoSection', () => {
  it('shows loading and then the timeline with status/date/origin', async () => {
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
    expect(screen.getByText('Loading history…')).toBeInTheDocument()

    await waitFor(() => expect(screen.getByText('Watched')).toBeInTheDocument())
    expect(screen.getByText(/Jellyfin/)).toBeInTheDocument()
  })

  it('shows an error when the fetch fails', async () => {
    stubRota(
      (p) => p === '/api/v1/entities/e1/historico',
      () => erroResponse(404, 'not_found', 'não achou'),
    )
    renderWithClient(<HistoricoSection entityId="e1" />)
    await waitFor(() =>
      expect(
        screen.getByText('Could not load the history'),
      ).toBeInTheDocument(),
    )
  })

  it('disappears entirely (no heading) when there is no event', async () => {
    stubRota(
      (p) => p === '/api/v1/entities/e1/historico',
      () => eventosResponse([]),
    )
    const { container } = renderWithClient(<HistoricoSection entityId="e1" />)
    await waitFor(() =>
      expect(screen.queryByText('Loading history…')).toBeNull(),
    )
    expect(screen.queryByRole('heading', { name: 'History' })).toBeNull()
    expect(container).toBeEmptyDOMElement()
  })

  it('deletes an event after a two-step confirmation', async () => {
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
    await screen.findByText('Watched')

    // 1st step: clicking the icon opens "Delete?"/"Cancel" — it does not delete yet.
    await user.click(screen.getByRole('button', { name: 'Delete event' }))
    const confirmar = screen.getByRole('button', { name: 'Delete?' })
    expect(
      fetchMock.mock.calls.some(([, init]) => init?.method === 'DELETE'),
    ).toBe(false)

    // 2nd step: it confirms — triggers the DELETE.
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

  it('"Cancel" in the confirmation does not delete the event', async () => {
    const user = userEvent.setup()
    stubRota(
      (p, m) => p === '/api/v1/entities/e1/historico' && m === 'GET',
      () => eventosResponse([eventoFixture({ id: 'ev1' })]),
    )
    renderWithClient(<HistoricoSection entityId="e1" />)
    await screen.findByText('Watched')

    await user.click(screen.getByRole('button', { name: 'Delete event' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(
      screen.getByRole('button', { name: 'Delete event' }),
    ).toBeInTheDocument()
  })
})
