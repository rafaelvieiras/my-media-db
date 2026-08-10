import { vi } from 'vitest'

// stubRota instala um `fetch` global que responde `resp()` quando `casa(pathname, method)`
// e lança nas demais rotas (falha alto, não silenciosa). Centraliza o boilerplate de mock
// de rede dos testes de mutação (jscpd) — cada teste só descreve a rota que espera.
export function stubRota(
  casa: (pathname: string, method: string) => boolean,
  resp: () => Response,
) {
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (casa(u.pathname, method)) {
      return resp()
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

// stubRotas é a variante multi-rota de stubRota: casa a URL contra uma lista
// ORDENADA de [predicado, resp] e responde com a primeira que bater; lança nas
// demais (mesmo padrão fail-loud). Usado quando um teste precisa de mais de uma
// resposta condicional (ex.: GET busca uma lista, DELETE apaga um item dela) —
// centraliza o parsing de URL/method pra esses testes não o duplicarem (jscpd).
export function stubRotas(
  rotas: Array<
    [casa: (pathname: string, method: string) => boolean, resp: () => Response]
  >,
) {
  const m = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    const rota = rotas.find(([casa]) => casa(u.pathname, method))
    if (rota) {
      return rota[1]()
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

// corpoEnviado extrai e faz JSON.parse do corpo da N-ésima chamada do mock de fetch.
export function corpoEnviado(
  m: ReturnType<typeof vi.fn>,
  chamada = 0,
): unknown {
  return JSON.parse(String(m.mock.calls[chamada][1]?.body))
}
