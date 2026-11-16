import { vi } from 'vitest'
import { okFields } from '@/test/entity-fixtures'

// SHARED helper of the local creation screens (item × collection):
// it installs a global `fetch` that serves the descriptors on GET
// `/api/v1/schema/entity-fields` and routes the creation POST (`postPath`) to
// `resp()`; any other route fails loudly. Centralized in src/test (outside
// coverage and the duplication ratchet) so the tests do not repeat the same
// network router — each file only informs the POST path it expects.
export function stubCriacao(postPath: string, resp: () => Response) {
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (u.pathname === '/api/v1/schema/entity-fields') {
      return okFields()
    }
    if (u.pathname === postPath && method === 'POST') {
      return resp()
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}
