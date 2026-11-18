import { http, HttpResponse } from 'msw'
import type { components } from '@/lib/api-types'

// Phase 3 MSW handlers — typed against the GENERATED schemas of
// openapi.yaml (api-types.ts), never loose by hand: a contract drift here
// breaks the type-check, it does not stay silent. It covers FOR NOW only the endpoints
// exercised by the 3 journeys of `src/routes/__msw__/*.test.tsx` (search+import,
// non-destructive enrich, source error) — extensible by handler as
// new integration journeys come in (the same incremental philosophy).

type ResolvedEntity = components['schemas']['ResolvedEntity']
type LanguageSettings = components['schemas']['LanguageSettings']
type FieldDescriptor = components['schemas']['FieldDescriptor']
type OverrideBlob = components['schemas']['OverrideBlob']
type ArtworkResponse = components['schemas']['ArtworkResponse']
type FileList = components['schemas']['FileList']
type ErrorBody = components['schemas']['Error']

// ── Fixtures/factories ───────────────────────────────────────────────────────

// entidadeResolvidaPadrao fills ALL the required fields of
// `ResolvedEntity` with a neutral default — the tests only override what
// matters to the journey (they always have to inform `id`/`title`, the only ones without an
// obvious default).
export function entidadeResolvidaPadrao(
  overrides: Partial<ResolvedEntity> & Pick<ResolvedEntity, 'id' | 'title'>,
): ResolvedEntity {
  return {
    type: 'movie',
    original_title: null,
    description: null,
    release_date: null,
    status: 'cataloged',
    tags: [],
    external_ids: {},
    owned: true,
    file_count: 1,
    poster_url: null,
    extension_data: {},
    lang: 'pt-BR',
    original_language: null,
    resolved_languages: {},
    tagline: null,
    keywords: [],
    runtime: null,
    certification: null,
    vote_average: null,
    local_rating: null,
    backdrop_url: null,
    videos: [],
    collection: null,
    similar: [],
    external_links: {},
    ancestors: [],
    ownership: null,
    catalog_completeness: { complete: true, missing: [] },
    ratings: [],
    ...overrides,
  }
}

// camposEditoriaisPadrao: only the 3 fields that the journeys touch
// (title/synopsis/tags) — enough for the SchemaForm to render and validate.
export function camposEditoriaisPadrao(): FieldDescriptor[] {
  return [
    {
      name: 'title',
      label: 'Título',
      group: 'traduzivel',
      widget: 'text',
      translatable: true,
      kind: 'escalar',
      order: 0,
      creatable: true,
    },
    {
      name: 'description',
      label: 'Sinopse',
      group: 'traduzivel',
      widget: 'textarea',
      translatable: true,
      kind: 'escalar',
      order: 1,
      creatable: true,
    },
    {
      name: 'tags',
      label: 'Tags',
      group: 'traduzivel',
      widget: 'tags',
      translatable: true,
      kind: 'lista',
      order: 2,
      creatable: false,
    },
  ]
}

export function configuracaoIdiomaPadrao(): LanguageSettings {
  return { primary: 'pt-BR', fallback: 'en', allowed: ['pt-BR', 'en'] }
}

// envelopeErro builds the `{error:{code,message}}` body of a non-2xx
// response — the same envelope that `erroDaResposta` (src/lib/api.ts) expects.
export function envelopeErro(code: string, message: string): ErrorBody {
  return { error: { code, message } }
}

// ── In-memory state, per test ───────────────────────────────────────────────
// Each test file populates these maps BEFORE `renderRota` — the handlers
// below read from here by `id`, so the same handlers (real paths of the
// contract) serve different fixtures per journey. Module re-evaluated per
// test file (default Vitest isolation), so there is no leakage between
// files.
export const entidades = new Map<string, ResolvedEntity>()
export const overridesPorEntidade = new Map<string, OverrideBlob>()
export const artworkPorEntidade = new Map<string, ArtworkResponse>()
export const arquivosPorEntidade = new Map<string, FileList>()
// Owner order per entity (ADR-0033): absent = "not declared".
export const ordensPorEntidade = new Map<
  string,
  { name: string; based_on: string; units: unknown[] }
>()

export const handlers = [
  // Language config (LanguageProvider, mounted by EVERY route via rootRoute).
  http.get('/api/v1/settings/languages', () =>
    HttpResponse.json(configuracaoIdiomaPadrao()),
  ),

  // Descriptors of the editorial form (immutable between deploys — a fixed
  // default is enough).
  http.get('/api/v1/schema/entity-fields', () =>
    HttpResponse.json({ fields: camposEditoriaisPadrao() }),
  ),

  // Types with cataloged data: the "Biblioteca" dropdown of the
  // navbar is mounted by EVERY route (via `AppHeader`, in the `rootRoute`) — `[]`
  // default (no MSW journey of this directory exercises the CONTENT of the
  // dropdown, it only has to not blow up `onUnhandledRequest: 'error'`). The LITERAL
  // path is registered BEFORE `/api/v1/entities/:id` below — MSW resolves
  // handlers in the declared order, and `:id` would match "types" as well.
  http.get('/api/v1/entities/types', () => HttpResponse.json([])),

  // Resolved entity by id (GET /entities/{id}) — 404 if the test did not
  // seed it in `entidades`.
  http.get('/api/v1/entities/:id', ({ params }) => {
    const entidade = entidades.get(String(params.id))
    if (!entidade) {
      return HttpResponse.json(
        envelopeErro('not_found', 'Entidade não encontrada.'),
        { status: 404 },
      )
    }
    return HttpResponse.json(entidade)
  }),

  // OWNER ORDER (ADR-0033): `units: []` (not declared) unless the test seeded one, and
  // the PUT echoes the body it received — the journeys assert the payload they send, not
  // a server transformation.
  http.get('/api/v1/entities/:id/owner-order', ({ params }) =>
    HttpResponse.json(
      ordensPorEntidade.get(String(params.id)) ?? {
        name: '',
        based_on: '',
        units: [],
      },
    ),
  ),
  http.put('/api/v1/entities/:id/owner-order', async ({ params, request }) => {
    const corpo = (await request.json()) as {
      name: string
      based_on: string
      units: unknown[]
    }
    const gravada = { ...corpo, units: corpo.units ?? [] }
    ordensPorEntidade.set(String(params.id), gravada)
    return HttpResponse.json(gravada)
  }),
  http.delete('/api/v1/entities/:id/owner-order', ({ params }) => {
    ordensPorEntidade.delete(String(params.id))
    return new HttpResponse(null, { status: 204 })
  }),

  // Raw override blob — `{}` (no override) when the test seeded nothing.
  http.get('/api/v1/entities/:id/override', ({ params }) =>
    HttpResponse.json(overridesPorEntidade.get(String(params.id)) ?? {}),
  ),

  // Grouped artwork — empty when the test seeded nothing (the gallery/badges
  // stay without items, without breaking the screen).
  http.get('/api/v1/entities/:id/artwork', ({ params }) =>
    HttpResponse.json(artworkPorEntidade.get(String(params.id)) ?? {}),
  ),

  // Files (mediainfo) — `[]` when the test seeded nothing.
  http.get('/api/v1/entities/:id/files', ({ params }) =>
    HttpResponse.json(
      arquivosPorEntidade.get(String(params.id)) ?? { items: [] },
    ),
  ),
]
