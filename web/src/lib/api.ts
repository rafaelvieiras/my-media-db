// Cliente HTTP da API do MyMediaDB.
//
// A base é configurável via `VITE_API_BASE`, mas o padrão (dev e prod) é vazio:
// isso faz as chamadas usarem caminho RELATIVO (`/api/...`). Em dev, o proxy do
// Vite encaminha para o backend Go (:8080); em prod, o binário serve tudo no
// mesmo origin. `VITE_API_BASE` só é preenchido como override (ex.: API remota).
import type { components } from '@/lib/api-types'

const base = import.meta.env.VITE_API_BASE ?? ''

// Tipos gerados do OpenAPI (0010) — nunca redigitados à mão.
export type Source = components['schemas']['Source']
export type SourceList = components['schemas']['SourceList']
export type CreateSource = components['schemas']['CreateSource']
export type ScanStatus = components['schemas']['ScanStatus']
export type StartScan = components['schemas']['StartScan']
export type EntityCard = components['schemas']['EntityCard']
export type EntityList = components['schemas']['EntityList']
export type OrderingsInfo = components['schemas']['OrderingsInfo']
export type EpisodeGroups = components['schemas']['EpisodeGroups']
export type EpisodeGroup = components['schemas']['EpisodeGroup']
export type ResolvedEntity = components['schemas']['ResolvedEntity']
export type AncestorRef = components['schemas']['AncestorRef']
export type MediaFile = components['schemas']['MediaFile']
export type FileList = components['schemas']['FileList']
export type SearchResult = components['schemas']['SearchResult']
export type ExternalResult = components['schemas']['ExternalResult']
export type ExternalDetail = components['schemas']['ExternalDetail']
export type ExternalVideo = components['schemas']['ExternalVideo']
export type ExternalCollection = components['schemas']['ExternalCollection']
export type ExternalCollectionDetail =
  components['schemas']['ExternalCollectionDetail']
export type ExternalCollectionPart =
  components['schemas']['ExternalCollectionPart']
export type ExternalSimilar = components['schemas']['ExternalSimilar']
export type ImportRequest = components['schemas']['ImportRequest']
export type LanguageSettings = components['schemas']['LanguageSettings']
export type OverrideBlob = components['schemas']['OverrideBlob']
export type OverridePatch = components['schemas']['OverridePatch']
export type FieldDescriptor = components['schemas']['FieldDescriptor']
export type FieldSchema = components['schemas']['FieldSchema']
export type Asset = components['schemas']['Asset']
export type AssetList = components['schemas']['AssetList']
export type CorrectRequest = components['schemas']['CorrectRequest']
export type PurgeResult = components['schemas']['PurgeResult']
export type PurgeBatchResult = components['schemas']['PurgeBatchResult']

// Forma do corpo de `GET /api/v1/health` (contrato do briefing 0001).
export interface Health {
  status: string
  version: string
}

// Erro tipado do envelope padrão `{ error: { code, message } }` (0008). Carrega
// o `code` estável (para ramificar) e a `message` em pt-BR (para exibir inline).
export class ApiError extends Error {
  readonly code: string
  readonly status: number

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

// Lê o envelope de erro de uma resposta não-2xx e devolve um ApiError. Se o corpo
// não for o envelope esperado, cai num erro genérico com o status HTTP.
async function erroDaResposta(res: Response): Promise<ApiError> {
  try {
    const corpo = (await res.json()) as components['schemas']['Error']
    if (corpo?.error?.message) {
      return new ApiError(res.status, corpo.error.code, corpo.error.message)
    }
  } catch {
    // corpo ausente ou não-JSON — cai no genérico abaixo.
  }
  return new ApiError(res.status, 'http_error', `HTTP ${res.status}`)
}

async function lerJson<T>(res: Response): Promise<T> {
  if (!res.ok) {
    throw await erroDaResposta(res)
  }
  return (await res.json()) as T
}

// getHealth consulta o endpoint de saúde. Lança em status HTTP não-2xx ou em
// falha de rede, de modo que o TanStack Query trate isso como estado de erro.
export async function getHealth(): Promise<Health> {
  const res = await fetch(`${base}/api/v1/health`)
  if (!res.ok) {
    throw new Error(`health respondeu HTTP ${res.status}`)
  }
  return (await res.json()) as Health
}

// ── Datasources (biblioteca) ─────────────────────────────────────────────────

export async function listLibrary(): Promise<SourceList> {
  return lerJson<SourceList>(await fetch(`${base}/api/v1/library`))
}

export async function createLibrary(body: CreateSource): Promise<Source> {
  const res = await fetch(`${base}/api/v1/library`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<Source>(res)
}

export async function deleteLibrary(id: string): Promise<void> {
  const res = await fetch(`${base}/api/v1/library/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    throw await erroDaResposta(res)
  }
}

export async function startScan(id: string): Promise<StartScan> {
  const res = await fetch(
    `${base}/api/v1/library/${encodeURIComponent(id)}/scan`,
    { method: 'POST' },
  )
  return lerJson<StartScan>(res)
}

export async function getScanStatus(id: string): Promise<ScanStatus> {
  const res = await fetch(
    `${base}/api/v1/library/${encodeURIComponent(id)}/scan`,
  )
  return lerJson<ScanStatus>(res)
}

// ── Descritor de formulário editorial (0030) ─────────────────────────────────

// Descritores dos campos editáveis (briefing 0030), servidos pelo backend a
// partir de `resolver.CamposEditaveis()` + metadados de apresentação. É metadado
// de UI IMUTÁVEL entre deploys — a UI monta o formulário e o schema Zod a partir
// dele, de modo que um campo novo no backend faça o input aparecer sem tocar JSX.
// Devolve a lista já ordenada por `order`.
export async function getEntityFields(): Promise<FieldDescriptor[]> {
  const schema = await lerJson<FieldSchema>(
    await fetch(`${base}/api/v1/schema/entity-fields`),
  )
  return schema.fields
}

// ── Entities (catálogo) ──────────────────────────────────────────────────────

export interface EntityListParams {
  type?: string
  owned?: boolean
  limit?: number
  offset?: number
  sort?: 'title' | '-title'
}

export async function listEntities(
  params: EntityListParams = {},
  lang?: string,
): Promise<EntityList> {
  const qs = new URLSearchParams()
  if (params.type) {
    qs.set('type', params.type)
  }
  if (params.owned !== undefined) {
    qs.set('owned', String(params.owned))
  }
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit))
  }
  if (params.offset !== undefined) {
    qs.set('offset', String(params.offset))
  }
  if (params.sort) {
    qs.set('sort', params.sort)
  }
  // `lang` vazio/omitido = sem o parâmetro (backend cai no principal — compat).
  if (lang) {
    qs.set('lang', lang)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<EntityList>(await fetch(`${base}/api/v1/entities${suffix}`))
}

export async function getEntity(
  id: string,
  lang?: string,
): Promise<ResolvedEntity> {
  const qs = new URLSearchParams()
  if (lang) {
    qs.set('lang', lang)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<ResolvedEntity>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}${suffix}`),
  )
}

// Membros de uma COLEÇÃO (0025): os itens ligados por entity_relation
// (kind='collection') como cards internos (o mesmo EntityCard da lista), ordenados
// por ano pelo backend. `[]` quando a entity não é coleção ou não tem membros. `lang`
// resolve os títulos dos membros no idioma ativo.
export async function getEntityMembers(
  id: string,
  lang?: string,
): Promise<EntityCard[]> {
  const qs = new URLSearchParams()
  if (lang) {
    qs.set('lang', lang)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<EntityCard[]>(
    await fetch(
      `${base}/api/v1/entities/${encodeURIComponent(id)}/members${suffix}`,
    ),
  )
}

// Episódios da SÉRIE agrupados por ordem de exibição (0040): todos os episódios
// descendentes, projetados na ordem `ordering` (official/dvd/absolute…) e agrupados
// por temporada da ordem — reagrupados/renumerados na LEITURA a partir do numbering,
// sem criar entidade de temporada (ADR-0021). `ordering` ausente/inválido cai no
// default da série. `lang` resolve os títulos no idioma ativo (paridade com /members).
export async function getEntityEpisodes(
  id: string,
  ordering?: string,
  lang?: string,
): Promise<EpisodeGroups> {
  const qs = new URLSearchParams()
  if (ordering) {
    qs.set('ordering', ordering)
  }
  if (lang) {
    qs.set('lang', lang)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<EpisodeGroups>(
    await fetch(
      `${base}/api/v1/entities/${encodeURIComponent(id)}/episodes${suffix}`,
    ),
  )
}

// ── Coleção: criação manual + membros (0025 Fase B) ──────────────────────────

// Cria uma coleção LOCAL (sem source): o dono monta um agrupamento do zero. Só o
// título é obrigatório (o backend responde 422 se vier vazio); a descrição é
// opcional. Devolve a `ResolvedEntity` recém-criada (tem `.id`) — a UI navega para
// `/entities/{id}`. Usa um corpo INLINE (não há schema gerado dedicado): o contrato
// é `{ title, description? }`.
export async function createCollection(body: {
  title: string
  description?: string
}): Promise<ResolvedEntity> {
  const res = await fetch(`${base}/api/v1/collections`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<ResolvedEntity>(res)
}

// Liga um item (`childId`) a uma coleção (`id`) como membro. Devolve a lista de
// membros ATUALIZADA (o mesmo `EntityCard[]` de `getEntityMembers`, ordenada por
// ano) — a UI reflete sem uma 2ª ida. Idempotente no backend (o UNIQUE protege).
// Erros a `ApiError` com a `message` pt-BR: 422 (parent não é coleção / item
// inexistente / self / aninhar coleção), 404 (coleção inexistente).
export async function addCollectionMember(
  id: string,
  childId: string,
): Promise<EntityCard[]> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/members`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ child_id: childId }),
    },
  )
  return lerJson<EntityCard[]>(res)
}

// Desliga um membro (`childId`) de uma coleção (`id`). Idempotente (remover um item
// não-ligado responde 200 mesmo assim). Devolve a lista de membros ATUALIZADA.
export async function removeCollectionMember(
  id: string,
  childId: string,
): Promise<EntityCard[]> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/members/${encodeURIComponent(
      childId,
    )}`,
    { method: 'DELETE' },
  )
  return lerJson<EntityCard[]>(res)
}

// ── Item local + identidade externa (0027) ───────────────────────────────────

// Cria um item LOCAL (sem fonte externa): filme ou série do zero, para o dono
// catalogar algo que não veio de um scan nem de um import. Só `type` e `title` são
// obrigatórios (o backend responde 422 se faltarem/forem inválidos); os demais são
// opcionais. Devolve a `ResolvedEntity` recém-criada (tem `.id`) — a UI navega para
// `/entities/{id}`. Corpo INLINE (contrato `{ type, title, ... }`, sem schema gerado
// dedicado). Depois o dono pode fixar os `external_ids` (updateExternalIds) e
// re-enriquecer para sincronizar com a fonte.
export async function createItem(body: {
  type: 'movie' | 'tv'
  title: string
  original_title?: string
  description?: string
  release_date?: string
}): Promise<ResolvedEntity> {
  const res = await fetch(`${base}/api/v1/entities`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<ResolvedEntity>(res)
}

// Grava (FULL REPLACE) a identidade externa da entity (`external_ids`, MD4): o dono
// fixa qual id de cada fonte a entity usa (ex.: tmdb=603), base para uma sincronização
// futura — os ids da fonte podem mudar ou o item ser substituído por outro. O backend
// descarta valores vazios (remover uma fonte = deixar o id em branco). As colunas
// geradas (external_id_tmdb/imdb) re-derivam sozinhas. Devolve a entity RESOLVIDA.
// Corpo INLINE `{ external_ids: {source: id} }`.
export async function updateExternalIds(
  id: string,
  externalIds: Record<string, string>,
): Promise<ResolvedEntity> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/external-ids`,
    {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ external_ids: externalIds }),
    },
  )
  return lerJson<ResolvedEntity>(res)
}

// ── Settings de idioma (0019) ────────────────────────────────────────────────

// Config de idioma vigente (principal, fallback, permitidos). Quando não há
// linha gravada, o backend responde o default seguro (pt-BR / en / [pt-BR, en]).
export async function getLanguageSettings(): Promise<LanguageSettings> {
  return lerJson<LanguageSettings>(
    await fetch(`${base}/api/v1/settings/languages`),
  )
}

// Grava (upsert) a config de idioma. O 422 do backend (principal/fallback fora
// dos permitidos, ou permitidos vazio) vira `ApiError` com a mensagem pt-BR —
// exibida inline pela tela de configurações.
export async function putLanguageSettings(
  body: LanguageSettings,
): Promise<LanguageSettings> {
  const res = await fetch(`${base}/api/v1/settings/languages`, {
    method: 'PUT',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<LanguageSettings>(res)
}

export async function getEntityFiles(id: string): Promise<FileList> {
  return lerJson<FileList>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}/files`),
  )
}

// ── Override do dono (0016) — edição por idioma ──────────────────────────────

// Blob CRU do override guardado (`{}` quando não há). A UI o usa para distinguir
// "overridado" de "herdado" POR CAMPO e POR IDIOMA: campos traduzíveis vêm como
// mapa por idioma (`{"pt-BR":…}`), neutros como o escalar direto. Não é a entity
// resolvida — é o patch como está no banco (o resolver interpreta na leitura).
export async function getOverrideBlob(id: string): Promise<OverrideBlob> {
  return lerJson<OverrideBlob>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}/override`),
  )
}

// Funde um patch esparso no override, POR IDIOMA. O `?lang=` é o "pincel" dos
// campos traduzíveis (title/description/tags): o handler grava `override[campo][lang]`,
// preservando os outros idiomas. Campos neutros (original_title/release_date)
// ignoram o `lang`. `null` num campo = "definir vazio" (distinto de LIMPAR).
// Devolve a entity RESOLVIDA no `lang` (a UI reflete na hora). 422 = patch inválido.
export async function patchOverride(
  id: string,
  lang: string,
  patch: OverridePatch,
): Promise<ResolvedEntity> {
  const suffix = lang ? `?lang=${encodeURIComponent(lang)}` : ''
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/override${suffix}`,
    {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(patch),
    },
  )
  return lerJson<ResolvedEntity>(res)
}

// LIMPAR override de um campo (≠ definir vazio). Com `lang` presente e campo
// traduzível, remove só `override[campo][lang]` (volta a HERDAR naquele idioma);
// sem `lang`, ou campo neutro, remove a chave inteira. Idempotente. Devolve a
// entity RESOLVIDA.
export async function clearOverrideField(
  id: string,
  campo: string,
  lang?: string,
): Promise<ResolvedEntity> {
  const suffix = lang ? `?lang=${encodeURIComponent(lang)}` : ''
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/override/${encodeURIComponent(
      campo,
    )}${suffix}`,
    { method: 'DELETE' },
  )
  return lerJson<ResolvedEntity>(res)
}

// RESET — apaga o override inteiro: todos os campos voltam a herdar a base.
// Devolve a entity RESOLVIDA (na base, sem override).
export async function resetOverride(id: string): Promise<ResolvedEntity> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/override`,
    { method: 'DELETE' },
  )
  return lerJson<ResolvedEntity>(res)
}

// ── Busca + import (0015) ────────────────────────────────────────────────────

// Busca unificada: `local` (catálogo) + `external` (fonte externa, "não salvos").
// `q` vazio é rejeitado pelo backend (400) — a UI só chama com termo preenchido.
// `type` opcional filtra por `movie`|`tv`. `include` (0033) religa sub-nós da árvore
// de séries (`season`/`episode`), que por padrão ficam fora da busca; vêm no mesmo
// `local[]`, e a UI os separa por `type` em seções próprias.
export async function searchMedia(
  q: string,
  type?: string,
  lang?: string,
  include?: string[],
): Promise<SearchResult> {
  const qs = new URLSearchParams({ q })
  if (type) {
    qs.set('type', type)
  }
  // `lang` = idioma de leitura ativo: a busca externa devolve títulos/sinopses
  // nele. Vazio/omitido = o backend cai no principal.
  if (lang) {
    qs.set('lang', lang)
  }
  if (include && include.length > 0) {
    qs.set('include', include.join(','))
  }
  return lerJson<SearchResult>(
    await fetch(`${base}/api/v1/search?${qs.toString()}`),
  )
}

// Importa um resultado externo para o catálogo. Devolve o `ResolvedEntity`
// (tem `.id`) tanto no 201 (criado) quanto no 200 (já existia).
export async function importExternal(
  body: ImportRequest,
): Promise<ResolvedEntity> {
  const res = await fetch(`${base}/api/v1/import`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<ResolvedEntity>(res)
}

// Re-enriquece uma entity EXISTENTE pela fonte (0015): puxa o detalhe e regrava a
// base externa + pôster. Sem corpo, usa `source=tmdb` e o `source_id` de
// `external_ids[tmdb]` da própria entity — o passo de sincronização após fixar o id
// (0027). Devolve a entity re-resolvida. Erros da fonte/identidade (422 sem id, 502
// fonte, 503 sem provider) viram `ApiError` com a mensagem pt-BR do envelope.
export async function enrichEntity(
  id: string,
  body?: { source?: string; source_id?: string },
): Promise<ResolvedEntity> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/enrich`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body ?? {}),
    },
  )
  return lerJson<ResolvedEntity>(res)
}

// ── Detalhe externo rico (0022/0023) ─────────────────────────────────────────

// Detalhe rico de um item NA FONTE, ANTES de importar (briefing 0022): o que o
// TMDB mostra (sinopse, gêneros, keywords, duração, classificação, slogan,
// vídeos, coleção, similares, links). Os campos traduzíveis já vêm resolvidos no
// `lang` pedido; `lang` omitido cai no principal do backend. O backend persiste
// na 1ª ida e serve do cache depois. `type` é `movie|tv`. Erros mapeados a
// `ApiError`: 404 (inexistente na fonte), 502 (fonte falhou), 503 (sem provider),
// 400 (idioma fora dos permitidos), 422 (tipo inválido).
export async function getExternalDetail(
  source: string,
  type: string,
  id: string,
  lang?: string,
): Promise<ExternalDetail> {
  const qs = new URLSearchParams()
  if (lang) {
    qs.set('lang', lang)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<ExternalDetail>(
    await fetch(
      `${base}/api/v1/external/${encodeURIComponent(source)}/${encodeURIComponent(
        type,
      )}/${encodeURIComponent(id)}${suffix}`,
    ),
  )
}

// Coleção externa como página (briefing 0024, follow-up do 0023): os metadados do
// conjunto e as partes (filmes) navegáveis ao detalhe externo. Localizada no `lang`
// pedido (mono-idioma); `lang` omitido cai no principal. Erros a `ApiError`: 404
// (inexistente na fonte), 502 (fonte falhou), 503 (fonte sem coleção), 400 (idioma
// fora dos permitidos).
export async function getCollectionDetail(
  source: string,
  id: string,
  lang?: string,
): Promise<ExternalCollectionDetail> {
  const qs = new URLSearchParams()
  if (lang) {
    qs.set('lang', lang)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<ExternalCollectionDetail>(
    await fetch(
      `${base}/api/v1/external/${encodeURIComponent(
        source,
      )}/collection/${encodeURIComponent(id)}${suffix}`,
    ),
  )
}

// URL absoluta (respeitando a base) do binário do pôster local, ou `null` quando
// o card não tem arte — a UI cai no placeholder. `poster_url` já vem como rota
// pronta do card (ex.: `/api/v1/entities/{id}/assets/poster`).
export function posterSrc(posterUrl: string | null | undefined): string | null {
  if (!posterUrl) {
    return null
  }
  return `${base}${posterUrl}`
}

// ── Arte custom: galeria + upload (0017) ─────────────────────────────────────

// Tipos de imagem aceitos pelo upload (mesmo conjunto que o backend valida pelos
// magic bytes). A UI reusa isto para o `accept` do <input> e para a validação
// no cliente ANTES de enviar (rejeita cedo, sem gastar uma ida ao servidor).
export const KINDS_ARTE = ['poster', 'fanart'] as const
export type KindArte = (typeof KINDS_ARTE)[number]
export const MIMES_ARTE = ['image/png', 'image/jpeg', 'image/webp'] as const

// URL absoluta (respeitando a base) do binário de um asset para as miniaturas.
// O `Asset.url` já vem como rota pronta do backend (rota do /file) — só aplica a
// base, como `posterSrc`.
export function assetFileSrc(url: string): string {
  return `${base}${url}`
}

// Galeria: todos os assets da entity (poster/fanart, user/external/local). `[]`
// quando não há arte.
export async function listAssets(id: string): Promise<AssetList> {
  return lerJson<AssetList>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}/assets`),
  )
}

// Upload multipart do binário do dono (`origin=user`). Monta um FormData com os
// campos `file` e `kind` — NÃO seta `content-type` à mão: o browser preenche o
// header com o boundary do multipart. Os limites do backend (415 tipo, 413
// tamanho, 422 corpo) viram `ApiError` com a `message` pt-BR do envelope.
export async function uploadAsset(
  id: string,
  file: File,
  kind: KindArte,
): Promise<Asset> {
  const form = new FormData()
  form.append('file', file)
  form.append('kind', kind)
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/assets`,
    { method: 'POST', body: form },
  )
  return lerJson<Asset>(res)
}

// Marca um asset como preferido do seu kind (desempata entre várias do dono). O
// backend limpa a flag dos outros do mesmo `(entity, kind)`.
export async function preferAsset(id: string, assetId: string): Promise<Asset> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/assets/${encodeURIComponent(
      assetId,
    )}/prefer`,
    { method: 'PUT' },
  )
  return lerJson<Asset>(res)
}

// Remove uma arte do dono (`origin=user`). O backend recusa apagar `external`
// (409/422) — a UI só oferece o botão nas do dono.
export async function deleteAsset(id: string, assetId: string): Promise<Asset> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/assets/${encodeURIComponent(
      assetId,
    )}`,
    { method: 'DELETE' },
  )
  return lerJson<Asset>(res)
}

// ── Curadoria: revisão + órfãs + correção de match (0018) ────────────────────

// Fila de REVISÃO: cards `draft` (não-identificados) OU com baixa confiança
// (`0 < match_confidence < 70`). Devolve o array direto (não é EntityList — a
// fila é uma query enxuta). `[]` quando não há nada a revisar.
export async function getReviewQueue(): Promise<EntityCard[]> {
  return lerJson<EntityCard[]>(await fetch(`${base}/api/v1/review`))
}

// Fila de ÓRFÃS: entidades auto-criadas (`cataloged`) com arquivo(s) todos
// `missing` e SEM investimento do dono (sem override, sem asset custom). Array
// direto; `[]` quando não há órfãs.
export async function getOrphans(): Promise<EntityCard[]> {
  return lerJson<EntityCard[]>(await fetch(`${base}/api/v1/orphans`))
}

// CONFIRMAR o match: o dono aceita o palpite. O backend sobe o `match_confidence`
// dos arquivos da entity para 100 (autoridade do dono — some o alerta). Devolve a
// entity re-resolvida.
export async function confirmMatch(id: string): Promise<ResolvedEntity> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/confirm`,
    { method: 'POST' },
  )
  return lerJson<ResolvedEntity>(res)
}

// CORRIGIR o match: o dono informa a identidade certa (`source`/`source_id`). O
// backend grava `external_ids[source]`, re-enriquece pela fonte e sobe a
// confiança (identidade vence). `source` default "tmdb". Devolve a entity
// re-resolvida. Erros da fonte (422/404/502/503) viram `ApiError` com a
// mensagem pt-BR do envelope.
export async function correctMatch(
  id: string,
  source: string,
  sourceId: string,
): Promise<ResolvedEntity> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/correct`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ source, source_id: sourceId }),
    },
  )
  return lerJson<ResolvedEntity>(res)
}

// PURGA individual (DELETE): remove a entity e seus derivados (media_file /
// external / override / assets via CASCADE). Única ação destrutiva — a UI só a
// dispara após confirmação explícita do dono. Devolve o resumo `{purged}`.
export async function purgeEntity(id: string): Promise<PurgeResult> {
  const res = await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  return lerJson<PurgeResult>(res)
}

// PURGA em lote: manda os ids selecionados; o backend purga cada um (idempotente
// por id ausente) e devolve quantos foram de fato removidos.
export async function purgeBatch(ids: string[]): Promise<PurgeBatchResult> {
  const res = await fetch(`${base}/api/v1/entities/purge`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ ids }),
  })
  return lerJson<PurgeBatchResult>(res)
}
