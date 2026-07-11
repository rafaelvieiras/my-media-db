import { vi } from 'vitest'
import { okFields } from '@/test/entity-fixtures'

// Helper COMPARTILHADO das telas de criação local (item × coleção, briefing 0030):
// instala um `fetch` global que serve os descritores no GET
// `/api/v1/schema/entity-fields` e roteia o POST de criação (`postPath`) para
// `resp()`; qualquer outra rota falha alto. Centralizado em src/test (fora da
// cobertura e da catraca de duplicação) para os testes não repetirem o mesmo
// roteador de rede — cada arquivo só informa o caminho do POST que espera.
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
