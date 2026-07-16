import type {
  Asset,
  FieldDescriptor,
  LanguageSettings,
  ResolvedEntity,
} from '@/lib/api'

// Fixtures compartilhadas pelos testes do detalhe da entity (leitura por idioma
// e editor de override). Centralizar aqui evita duplicar o factory e os helpers
// de resposta em cada arquivo de teste (a catraca de duplicação é zero). Fora da
// cobertura (src/test/** é excluído).

export const settingsPadrao: LanguageSettings = {
  primary: 'pt-BR',
  fallback: 'en',
  allowed: ['pt-BR', 'en'],
}

// Envelope de uma resposta 200 com corpo JSON (o shape mínimo que o cliente lê).
export function okJson<T>(body: T) {
  return { ok: true, status: 200, json: async () => body } as Response
}

// Descritores canônicos do formulário editorial (briefing 0030), espelhando o que
// o backend serve em GET /api/v1/schema/entity-fields. Compartilhado pelos testes
// das rotas de CRUD e do detalhe (selo "editado" lê `translatable` daqui).
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

// Envelope de resposta do endpoint de descritores (`{ fields }`).
export function okFields() {
  return okJson({ fields: descritoresPadrao })
}

// respostaComum roteia as rotas AUXILIARES do detalhe da entity (settings, descritores,
// override vazio, assets e files vazios) — o pano de fundo compartilhado por todos os
// fetchRouter de EntityDetailScreen. Devolve null quando a URL não é auxiliar (o caller
// trata as rotas específicas: a entity resolvida, membros, episódios). Centraliza o
// bloco para os testes não o duplicarem (jscpd). Não cobre `/override` com blob custom
// (o teste que precisa disso trata a rota antes).
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
  if (u.includes('/assets')) {
    return okJson([])
  }
  if (u.includes('/files')) {
    return okJson({ items: [] })
  }
  return null
}

// Asset de teste (arte da galeria, 0017), com sobrescritas pontuais por campo.
export function baseAsset(over: Partial<Asset> = {}): Asset {
  return {
    id: 'a1',
    kind: 'poster',
    origin: 'user',
    preferred: false,
    url: '/api/v1/entities/e1/assets/a1/file',
    ...over,
  }
}

// Entity resolvida de teste (Dune), com sobrescritas pontuais por campo.
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
    // Campos ricos (paridade com o externo, 0022/0023): default vazio/nulo.
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
    ...over,
  }
}
