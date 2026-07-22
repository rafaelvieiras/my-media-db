import { http, HttpResponse } from 'msw'
import type { components } from '@/lib/api-types'

// Handlers MSW da Fase 3 (briefing 0068) — tipados contra os schemas GERADOS do
// openapi.yaml (api-types.ts), nunca à mão solto: uma deriva de contrato aqui
// quebra o type-check, não fica silenciosa. Cobre POR ORA só os endpoints
// exercidos pelas 3 jornadas de `src/routes/__msw__/*.test.tsx` (busca+import,
// enrich não-destrutivo, erro de fonte) — extensível por handler conforme
// entram novas jornadas de integração (mesma filosofia incremental do 0028).

type ResolvedEntity = components['schemas']['ResolvedEntity']
type LanguageSettings = components['schemas']['LanguageSettings']
type FieldDescriptor = components['schemas']['FieldDescriptor']
type OverrideBlob = components['schemas']['OverrideBlob']
type ArtworkResponse = components['schemas']['ArtworkResponse']
type FileList = components['schemas']['FileList']
type ErrorBody = components['schemas']['Error']

// ── Fixtures/factories ───────────────────────────────────────────────────────

// entidadeResolvidaPadrao preenche TODOS os campos obrigatórios de
// `ResolvedEntity` com um default neutro — os testes só sobrescrevem o que
// importa à jornada (sempre precisam informar `id`/`title`, os únicos sem
// default óbvio).
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
    ...overrides,
  }
}

// camposEditoriaisPadrao: só os 3 campos que as jornadas do 0068 tocam
// (título/sinopse/tags) — suficiente para o SchemaForm renderizar e validar.
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

// envelopeErro monta o corpo `{error:{code,message}}` (0008) de uma resposta
// não-2xx — o mesmo envelope que `erroDaResposta` (src/lib/api.ts) espera.
export function envelopeErro(code: string, message: string): ErrorBody {
  return { error: { code, message } }
}

// ── Estado em memória, por teste ────────────────────────────────────────────
// Cada arquivo de teste popula estes mapas ANTES de `renderRota` — os handlers
// abaixo leem daqui por `id`, para os mesmos handlers (paths reais do
// contrato) servirem fixtures diferentes por jornada. Módulo reavaliado por
// arquivo de teste (isolamento padrão do Vitest), então não há vazamento entre
// arquivos.
export const entidades = new Map<string, ResolvedEntity>()
export const overridesPorEntidade = new Map<string, OverrideBlob>()
export const artworkPorEntidade = new Map<string, ArtworkResponse>()
export const arquivosPorEntidade = new Map<string, FileList>()

export const handlers = [
  // Config de idioma (LanguageProvider, montado por TODA rota via rootRoute).
  http.get('/api/v1/settings/languages', () =>
    HttpResponse.json(configuracaoIdiomaPadrao()),
  ),

  // Descritores do formulário editorial (imutável entre deploys — um default
  // fixo basta).
  http.get('/api/v1/schema/entity-fields', () =>
    HttpResponse.json({ fields: camposEditoriaisPadrao() }),
  ),

  // Entidade resolvida por id (GET /entities/{id}) — 404 se o teste não a
  // semeou em `entidades`.
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

  // Blob cru de override — `{}` (sem override) quando o teste não semeou nada.
  http.get('/api/v1/entities/:id/override', ({ params }) =>
    HttpResponse.json(overridesPorEntidade.get(String(params.id)) ?? {}),
  ),

  // Arte agrupada — vazio quando o teste não semeou nada (a galeria/selos
  // ficam sem itens, sem quebrar a tela).
  http.get('/api/v1/entities/:id/artwork', ({ params }) =>
    HttpResponse.json(artworkPorEntidade.get(String(params.id)) ?? {}),
  ),

  // Arquivos (mediainfo) — `[]` quando o teste não semeou nada.
  http.get('/api/v1/entities/:id/files', ({ params }) =>
    HttpResponse.json(
      arquivosPorEntidade.get(String(params.id)) ?? { items: [] },
    ),
  ),
]
