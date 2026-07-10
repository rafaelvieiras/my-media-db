import { render, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, vi } from 'vitest'
import type { ReactNode } from 'react'

// Scaffolding compartilhado dos testes de TELA (fila de revisão/órfãs e outras
// que espionam invalidações). Centralizado em src/test (fora da cobertura e da
// catraca de duplicação) para os arquivos de teste não repetirem o mesmo envelope
// de erro e o mesmo boilerplate de render + spy. O mock de <Link> mora ao lado,
// em `link-mock.tsx` (arquivo só-componente).

// Envelope de uma resposta de erro do padrão `{ error: { code, message } }`.
export function erroJson(status: number, code: string, message: string) {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  } as Response
}

// Contexto de uma rota mockada: caminho, método, corpo/opções e a URL já parseada
// (para ler query params). Evita repetir o preâmbulo de parsing em cada teste.
export interface CtxRota {
  path: string
  method: string
  init?: RequestInit
  u: URL
}

// Monta um `fetch` mock que delega ao handler com o contexto já parseado.
export function criarFetch(
  handler: (ctx: CtxRota) => Promise<Response> | Response,
) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    return handler({ path: u.pathname, method: init?.method ?? 'GET', init, u })
  })
}

// Renderiza a UI num QueryClient próprio (retries off — o erro aparece na hora) e
// espiona `invalidateQueries` para afirmar as invalidações pós-mutação.
export function renderComEspioes(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>)
  return { queryClient, invalidateSpy }
}

// Chaves invalidadas (achatadas a string) — para afirmar quais queries caíram.
export function chavesInvalidadas(calls: unknown[][]): string[] {
  return calls.map((c) => {
    const filtro = c[0] as { queryKey?: unknown } | undefined
    return JSON.stringify(filtro?.queryKey)
  })
}

// Espera (até o próximo tick) que TODAS as chaves esperadas tenham sido
// invalidadas pelo spy — as chaves vêm achatadas a string (ex.: `["review"]`).
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
