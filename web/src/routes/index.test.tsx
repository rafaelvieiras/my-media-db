import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { HealthScreen } from '@/routes/index'

// Cada teste usa um QueryClient próprio com retry desligado, para o estado de
// erro aparecer imediatamente (sem os retries com backoff do padrão).
function renderWithClient(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('HealthScreen', () => {
  it('mostra o estado de carregamento antes da resposta', () => {
    // fetch que nunca resolve → a query fica pendente.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>(() => {})),
    )

    renderWithClient(<HealthScreen />)

    expect(screen.getByText(/Conectando/)).toBeInTheDocument()
    expect(screen.getByText('conectando')).toBeInTheDocument()
  })

  it('mostra "API online" com a versão em caso de sucesso', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ status: 'ok', version: '9.9.9' }),
      } as Response),
    )

    renderWithClient(<HealthScreen />)

    await waitFor(() =>
      expect(screen.getByText(/API online — v9\.9\.9/)).toBeInTheDocument(),
    )
    expect(screen.getByText('online')).toBeInTheDocument()
  })

  it('mostra "API indisponível" em caso de erro', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({}),
      } as Response),
    )

    renderWithClient(<HealthScreen />)

    await waitFor(() =>
      expect(screen.getByText('API indisponível')).toBeInTheDocument(),
    )
    expect(screen.getByText('offline')).toBeInTheDocument()
  })
})
