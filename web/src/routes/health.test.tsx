import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { HealthScreen } from '@/routes/health'
import { renderWithClient } from '@/test/render'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('HealthScreen', () => {
  it('mostra o estado de carregamento antes da resposta', () => {
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
