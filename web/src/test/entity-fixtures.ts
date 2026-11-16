import type {
  Asset,
  FieldDescriptor,
  LanguageSettings,
  ResolvedEntity,
} from '@/lib/api'

// Fixtures shared by the tests of the entity detail (per-language reading
// and override editor). Centralizing them here avoids duplicating the factory and the response
// helpers in every test file (the duplication ratchet is zero). Outside
// coverage (src/test/** is excluded).

export const settingsPadrao: LanguageSettings = {
  primary: 'pt-BR',
  fallback: 'en',
  allowed: ['pt-BR', 'en'],
}

// Envelope of a 200 response with a JSON body (the minimal shape that the client reads).
export function okJson<T>(body: T) {
  return { ok: true, status: 200, json: async () => body } as Response
}

// Canonical descriptors of the editorial form, mirroring what
// the backend serves in GET /api/v1/schema/entity-fields. Shared by the tests
// of the CRUD routes and of the detail (the "edited" badge reads `translatable` from here).
export const descritoresPadrao: FieldDescriptor[] = [
  {
    name: 'title',
    label: 'Título',
    group: 'traduzivel',
    widget: 'text',
    translatable: true,
    kind: 'escalar',
    order: 10,
    creatable: true,
  },
  {
    name: 'description',
    label: 'Sinopse',
    group: 'traduzivel',
    widget: 'textarea',
    translatable: true,
    kind: 'escalar',
    order: 20,
    creatable: true,
  },
  {
    name: 'tags',
    label: 'Tags',
    group: 'traduzivel',
    widget: 'tags',
    translatable: true,
    kind: 'lista',
    order: 30,
    creatable: false,
  },
  {
    name: 'original_title',
    label: 'Título original',
    group: 'neutro',
    widget: 'text',
    translatable: false,
    kind: 'escalar',
    order: 40,
    creatable: true,
  },
  {
    name: 'release_date',
    label: 'Data de lançamento',
    group: 'neutro',
    widget: 'date',
    translatable: false,
    kind: 'escalar',
    order: 50,
    creatable: true,
  },
  {
    name: 'local_rating',
    label: 'Sua nota',
    group: 'neutro',
    widget: 'number',
    translatable: false,
    kind: 'numero',
    order: 60,
    creatable: false,
    min: 0,
    max: 10,
    step: 0.1,
  },
]

// Response envelope of the descriptors endpoint (`{ fields }`).
export function okFields() {
  return okJson({ fields: descritoresPadrao })
}

// respostaComum routes the AUXILIARY routes of the entity detail (settings, descriptors,
// empty override, empty assets and files, absent theme song) — the background
// shared by every fetchRouter of EntityDetailScreen. It returns null when the
// URL is not auxiliary (the caller handles the specific routes: the resolved entity, members,
// episodes). It centralizes the block so the tests do not duplicate it (jscpd). It does not cover
// `/override` with a custom blob (the test that needs that handles the route before).
export function respostaComum(u: string): Response | null {
  if (u === '/api/v1/settings/languages') {
    return okJson(settingsPadrao)
  }
  if (u === '/api/v1/schema/entity-fields') {
    return okFields()
  }
  if (u.includes('/override')) {
    return okJson({})
  }
  if (u.includes('/theme')) {
    // Theme song: HEAD without theme.* in the folder -> 404 (graceful
    // degradation, the player simply does not appear).
    return {
      ok: false,
      status: 404,
      json: async () => ({
        error: { code: 'not_found', message: 'tema musical não encontrado' },
      }),
    } as Response
  }
  if (u.includes('/assets')) {
    return okJson([])
  }
  if (u.includes('/files')) {
    return okJson({ items: [] })
  }
  return respostaHistoricoVazia(u)
}

// Sub-route of the consumption history: empty state (without the
// "Assistido" badge) and without events (without the "Histórico" section) by default — the test that
// needs to exercise the history itself handles the route before (same pattern as
// `/override`). Extracted separately (not just embedded in `respostaComum`) because
// some local `fetchRouter`s (entity-detail.test.tsx,
// entity-detail-collection.test.tsx) build their own routing instead of
// calling the whole `respostaComum` — they reuse only this piece (jscpd).
export function respostaHistoricoVazia(u: string): Response | null {
  if (u.includes('/historico/estado')) {
    return okJson({ estado: {} })
  }
  if (u.includes('/historico')) {
    return okJson({ eventos: [] })
  }
  return null
}

// Test asset (artwork of the gallery), with occasional per-field overrides.
// `is_principal` is the new source of truth — default `false`.
export function baseAsset(over: Partial<Asset> = {}): Asset {
  return {
    id: 'a1',
    kind: 'poster',
    origin: 'user',
    preferred: false,
    is_principal: false,
    url: '/api/v1/entities/e1/assets/a1/file',
    ...over,
  }
}

// Resolved test entity (Dune), with occasional per-field overrides.
export function baseEntity(over: Partial<ResolvedEntity> = {}): ResolvedEntity {
  return {
    id: 'e1',
    type: 'movie',
    title: 'Dune',
    original_title: 'Dune',
    description: 'Paul Atreides chega a Arrakis.',
    release_date: null,
    status: 'cataloged',
    tags: [],
    external_ids: {},
    owned: true,
    file_count: 0,
    poster_url: null,
    extension_data: {},
    lang: 'pt-BR',
    original_language: 'en',
    resolved_languages: {},
    // Rich fields (parity with the external one): default empty/null.
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
    catalog_completeness: null,
    ratings: [],
    ...over,
  }
}
