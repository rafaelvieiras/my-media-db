import { afterEach, describe, expect, it, vi } from 'vitest'
import { getHealth } from '@/lib/api'

// Mocka `fetch` por teste; restaura depois para não vazar entre casos.
afterEach(() => {
  vi.restoreAllMocks()
})

describe('getHealth', () => {
  it('retorna o corpo em resposta 2xx', async () => {
    const body = { status: 'ok', version: '1.2.3' }
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => body,
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    const health = await getHealth()

    expect(health).toEqual(body)
    // Caminho relativo (base vazio por padrão): /api/v1/health.
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/health')
  })

  it('lança em resposta não-2xx', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({}),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await expect(getHealth()).rejects.toThrow('health respondeu HTTP 503')
  })
})
