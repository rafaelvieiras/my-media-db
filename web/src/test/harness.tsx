import { render, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, vi } from 'vitest'
import type { ReactNode } from 'react'

// Shared scaffolding of the SCREEN tests (review/orphan queue and others
// that spy on invalidations). Centralized in src/test (outside coverage and the
// duplication ratchet) so the test files do not repeat the same error envelope
// and the same render + spy boilerplate. The <Link> mock lives next to it,
// in `link-mock.tsx` (a component-only file).

// Envelope of an error response in the pattern `{ error: { code, message } }`.
export function erroJson(status: number, code: string, message: string) {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  } as Response
}

// Context of a mocked route: path, method, body/options and the already parsed URL
// (to read query params). It avoids repeating the parsing preamble in every test.
export interface CtxRota {
  path: string
  method: string
  init?: RequestInit
  u: URL
}

// Builds a mock `fetch` that delegates to the handler with the already parsed context.
export function criarFetch(
  handler: (ctx: CtxRota) => Promise<Response> | Response,
) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    return handler({ path: u.pathname, method: init?.method ?? 'GET', init, u })
  })
}

// Renders the UI in its own QueryClient (retries off — the error appears right away) and
// spies on `invalidateQueries` to assert the invalidations after the mutation.
export function renderComEspioes(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>)
  return { queryClient, invalidateSpy }
}

// Invalidated keys (flattened into a string) — to assert which queries were dropped.
export function chavesInvalidadas(calls: unknown[][]): string[] {
  return calls.map((c) => {
    const filtro = c[0] as { queryKey?: unknown } | undefined
    return JSON.stringify(filtro?.queryKey)
  })
}

// Waits (until the next tick) for ALL the expected keys to have been
// invalidated by the spy — the keys come flattened into a string (e.g. `["review"]`).
export function esperarChaves(
  invalidateSpy: { mock: { calls: unknown[][] } },
  esperadas: string[],
) {
  return waitFor(() => {
    const chaves = chavesInvalidadas(invalidateSpy.mock.calls)
    for (const chave of esperadas) {
      expect(chaves).toContain(chave)
    }
  })
}
