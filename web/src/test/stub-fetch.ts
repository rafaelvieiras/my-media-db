import { vi } from 'vitest'

// paginaResposta builds the paginated shape that `GET /entities` returns
// (`{items, total, limit, offset}`) — used by any test that stubs that
// route (`PosterGrid`/`LibraryBrowseScreen`), centralizing the boilerplate so as
// not to duplicate the same `page()` function in every file (jscpd).
export function paginaResposta(
  items: unknown[] = [],
  total = items.length,
): Response {
  return {
    ok: true,
    status: 200,
    json: async () => ({ items, total, limit: 24, offset: 0 }),
  } as Response
}

// stubRota installs a global `fetch` that answers `resp()` when `casa(pathname, method)`
// and throws on the other routes (fail loud, not silent). It centralizes the network mock
// boilerplate of the mutation tests (jscpd) — each test only describes the route it expects.
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

// stubRotas is the multi-route variant of stubRota: it matches the URL against an
// ORDERED list of [predicate, resp] and answers with the first one that matches; it throws on
// the others (same fail-loud pattern). Used when a test needs more than one
// conditional response (e.g.: GET fetches a list, DELETE removes an item of it) —
// it centralizes the URL/method parsing so those tests do not duplicate it (jscpd).
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

// corpoEnviado extracts and JSON.parses the body of the N-th call of the fetch mock.
export function corpoEnviado(
  m: ReturnType<typeof vi.fn>,
  chamada = 0,
): unknown {
  return JSON.parse(String(m.mock.calls[chamada][1]?.body))
}

// urlEnviada extracts and parses the URL of the N-th call of the fetch mock — the same
// pattern as corpoEnviado, for tests that assert query params (e.g. `?type=`)
// instead of the body.
export function urlEnviada(m: ReturnType<typeof vi.fn>, chamada = 0): URL {
  return new URL(m.mock.calls[chamada][0] as string, 'http://x')
}
