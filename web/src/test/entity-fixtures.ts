import type { Asset, LanguageSettings, ResolvedEntity } from '@/lib/api'

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
    backdrop_url: null,
    videos: [],
    collection: null,
    similar: [],
    external_links: {},
    ...over,
  }
}
