import { vi } from 'vitest'
import type { EntityCard, EpisodeGroup, ResolvedEntity } from '@/lib/api'
import type { RichDetail } from '@/lib/rich-detail'
import {
  okFields,
  respostaHistoricoVazia,
  settingsPadrao,
} from '@/test/entity-fixtures'

// richDetailBase é o RichDetail canônico (tudo vazio) que a RichDetailView consome:
// `over` sobrescreve só o que o teste precisa. Compartilhado para os testes da view
// não repetirem a fábrica de ~20 campos (jscpd).
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

// Scaffolding COMPARTILHADO dos testes das telas de detalhe/coleção externa: respostas
// fetch canônicas, o mock do idioma ativo e o mock do router. Centralizar aqui evita a
// duplicação (jscpd) entre os arquivos de teste que exercitam a mesma superfície.

// entityCardBase é o EntityCard canônico dos testes de coleção (membros/picker): um
// filme possuído, sem arte. `over` sobrescreve campos. Compartilhado para os arquivos
// de teste não repetirem a mesma fábrica (jscpd).
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

// episodioCard é um card de EPISÓDIO com numbering (0040): compartilhado pelos testes
// da view agrupada para não repetir o mesmo entityCardBase(episode + numbering) (jscpd).
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

// grupoEpisodios monta um EpisodeGroup (0040) a partir dos cards já prontos.
export function grupoEpisodios(
  season: number,
  label: string,
  episodes: EntityCard[],
): EpisodeGroup {
  return { season, label, episodes }
}

// okJson/erroJson montam Responses no shape que o cliente HTTP (api.ts) espera.
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

// fakeLanguage é o retorno do useLanguage mockado (idioma ativo controlável pelo teste).
export function fakeLanguage(lang: string) {
  return { lang, allowed: ['pt-BR', 'en'], primary: 'pt-BR', setLang: () => {} }
}

// EspioesRouter agrupa os spies de navegação e os params que o router mockado devolve.
export interface EspioesRouter {
  navigate: ReturnType<typeof vi.fn>
  back: ReturnType<typeof vi.fn>
  params: Record<string, string>
}

// routerMock devolve o módulo mockado do @tanstack/react-router: Link interpola os
// params no href (para afirmar destinos), e navigate/back/params vêm dos spies.
export async function routerMock(spies: EspioesRouter) {
  return {
    Link: (await import('@/test/link-mock')).LinkMock,
    useNavigate: () => spies.navigate,
    useRouter: () => ({ history: { back: spies.back } }),
    useParams: () => spies.params,
  }
}

// stubMembros roteia SÓ GET /entities/{id}/members com a Response dada — o núcleo
// compartilhado dos testes de lista de membros (coleção 0025 e árvore de séries
// 0031/0036), que exercitam a mesma superfície com corpos diferentes.
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

// criarFetchRouterEntity monta o fetch mock COMUM aos testes de EntityDetailScreen:
// settings/languages, schema/entity-fields, override, files, assets, histórico vazio
// e por fim GET .../{id} -> entity. `membrosResp` sobrescreve a resposta de
// .../members (default: array vazio) — o único ponto que varia entre os testes que
// consomem este roteiro. Compartilhado para os arquivos entity-detail*.test.tsx não
// repetirem a mesma sequência de rotas (jscpd).
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

// stubExternalFetch instala um fetch que roteia o GET do detalhe/coleção externa para
// `externoResp` e o POST /import para uma entity padrão. Devolve o mock para asserções.
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
