import { vi } from 'vitest'
import type { EntityCard, EpisodeGroup, ResolvedEntity } from '@/lib/api'
import type { RichDetail } from '@/lib/rich-detail'
import {
  okFields,
  respostaHistoricoVazia,
  settingsPadrao,
} from '@/test/entity-fixtures'

// richDetailBase is the canonical RichDetail (everything empty) that the RichDetailView consumes:
// `over` overrides only what the test needs. Shared so the tests of the view
// do not repeat the ~20-field factory (jscpd).
export function richDetailBase(over: Partial<RichDetail> = {}): RichDetail {
  return {
    type: 'movie',
    title: 'Duna',
    originalTitle: null,
    description: null,
    tagline: null,
    year: null,
    runtime: null,
    certification: null,
    voteAverage: null,
    localRating: null,
    originalLanguage: null,
    posterUrl: null,
    backdropUrl: null,
    stillUrl: null,
    genres: [],
    keywords: [],
    videos: [],
    collection: null,
    similar: [],
    cast: [],
    crew: [],
    companies: [],
    externalLinks: {},
    ratings: [],
    ...over,
  }
}

// SHARED scaffolding of the tests of the external detail/collection screens: canonical
// fetch responses, the mock of the active language and the mock of the router. Centralizing it here avoids
// the duplication (jscpd) between the test files that exercise the same surface.

// entityCardBase is the canonical EntityCard of the collection tests (members/picker): an
// owned movie, without artwork. `over` overrides fields. Shared so the test files
// do not repeat the same factory (jscpd).
export function entityCardBase(over: Partial<EntityCard> = {}): EntityCard {
  return {
    id: 'e1',
    type: 'movie',
    title: 'Duna',
    status: 'cataloged',
    owned: true,
    file_count: 1,
    match_confidence: 100,
    poster_url: null,
    catalog_completeness: null,
    ...over,
  }
}

// episodioCard is an EPISODE card with numbering: shared by the tests
// of the grouped view so as not to repeat the same entityCardBase(episode + numbering) (jscpd).
export function episodioCard(
  id: string,
  season: number,
  number: number,
  title = 'Ep',
): EntityCard {
  return entityCardBase({
    id,
    type: 'episode',
    title,
    numbering: { season, number },
  })
}

// grupoEpisodios builds an EpisodeGroup from the cards already built.
export function grupoEpisodios(
  season: number,
  label: string,
  episodes: EntityCard[],
): EpisodeGroup {
  return { season, label, episodes }
}

// okJson/erroJson build Responses in the shape that the HTTP client (api.ts) expects.
export function okJson<T>(body: T, status = 200): Response {
  return { ok: true, status, json: async () => body } as Response
}

export function erroJson(
  status: number,
  code: string,
  message: string,
): Response {
  return {
    ok: false,
    status,
    json: async () => ({ error: { code, message } }),
  } as Response
}

// fakeLanguage is the return of the mocked useLanguage (active language controllable by the test).
export function fakeLanguage(lang: string) {
  return { lang, allowed: ['pt-BR', 'en'], primary: 'pt-BR', setLang: () => {} }
}

// EspioesRouter groups the navigation spies and the params that the mocked router returns.
export interface EspioesRouter {
  navigate: ReturnType<typeof vi.fn>
  back: ReturnType<typeof vi.fn>
  params: Record<string, string>
}

// routerMock returns the mocked module of @tanstack/react-router: Link interpolates the
// params into the href (to assert destinations), and navigate/back/params come from the spies.
export async function routerMock(spies: EspioesRouter) {
  return {
    Link: (await import('@/test/link-mock')).LinkMock,
    useNavigate: () => spies.navigate,
    useRouter: () => ({ history: { back: spies.back } }),
    useParams: () => spies.params,
  }
}

// stubMembros routes ONLY GET /entities/{id}/members with the given Response — the shared
// core of the member list tests (collection and series tree), which
// exercise the same surface with different bodies.
export function stubMembros(resp: () => Response) {
  const m = vi.fn(async (url: string) => {
    const u = new URL(String(url), 'http://localhost')
    if (/^\/api\/v1\/entities\/[^/]+\/members$/.test(u.pathname)) {
      return resp()
    }
    throw new Error(`rota nao mockada: ${u.pathname}`)
  })
  vi.stubGlobal('fetch', m)
  return m
}

// criarFetchRouterEntity builds the fetch mock COMMON to the EntityDetailScreen tests:
// settings/languages, schema/entity-fields, override, files, assets, empty history
// and finally GET .../{id} -> entity. `membrosResp` overrides the response of
// .../members (default: empty array) — the only point that varies between the tests that
// consume this script. Shared so the entity-detail*.test.tsx files do not
// repeat the same sequence of routes (jscpd).
export function criarFetchRouterEntity(
  entity: ResolvedEntity,
  membrosResp: () => Response = () => okJson([]),
) {
  return vi.fn(async (url: string) => {
    const u = String(url)
    if (u === '/api/v1/settings/languages') {
      return okJson(settingsPadrao)
    }
    if (u === '/api/v1/schema/entity-fields') {
      return okFields()
    }
    if (u.startsWith('/api/v1/entities/e1/override')) {
      return okJson({})
    }
    if (u.startsWith('/api/v1/entities/e1/files')) {
      return okJson({ items: [] })
    }
    if (u.startsWith('/api/v1/entities/e1/assets')) {
      return okJson([])
    }
    if (u.startsWith('/api/v1/entities/e1/members')) {
      return membrosResp()
    }
    const historico = respostaHistoricoVazia(u)
    if (historico) {
      return historico
    }
    if (u.startsWith('/api/v1/entities/e1')) {
      return okJson(entity)
    }
    throw new Error(`rota nao mockada: ${u}`)
  })
}

// stubExternalFetch installs a fetch that routes the GET of the external detail/collection to
// `externoResp` and the POST /import to a default entity. It returns the mock for assertions.
export function stubExternalFetch(externoResp: () => Response) {
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const u = new URL(String(url), 'http://localhost')
    const method = init?.method ?? 'GET'
    if (u.pathname.startsWith('/api/v1/external/') && method === 'GET') {
      return externoResp()
    }
    if (u.pathname === '/api/v1/import' && method === 'POST') {
      return okJson({ id: 'ent-1', type: 'movie', title: 'Duna' }, 201)
    }
    throw new Error(`rota nao mockada: ${method} ${u.pathname}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}
