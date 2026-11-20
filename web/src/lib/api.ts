// HTTP client of the MyMediaDB API.
//
// The base is configurable via `VITE_API_BASE`, but the default (dev and prod) is empty:
// that makes the calls use a RELATIVE path (`/api/...`). In dev, the Vite proxy
// forwards to the Go backend (:8080); in prod, the binary serves everything on the
// same origin. `VITE_API_BASE` is only filled in as an override (e.g. a remote API).
import type { components } from '@/lib/api-types'

const base = import.meta.env.VITE_API_BASE ?? ''

// Types generated from the OpenAPI — never retyped by hand.
export type Source = components['schemas']['Source']
export type SourceList = components['schemas']['SourceList']
export type CreateSource = components['schemas']['CreateSource']
// Partial patch of `library_source` — `watch_enabled`
// and/or `path` (cascading re-basing of the datasource to a new path).
export type PatchSource = components['schemas']['PatchSource']
export type ScanStatus = components['schemas']['ScanStatus']
export type StartScan = components['schemas']['StartScan']
// Folder browser: response of `GET /library/browse`.
export type BrowseResult = components['schemas']['BrowseResult']
export type BrowseEntry = components['schemas']['BrowseEntry']
export type EntityCard = components['schemas']['EntityCard']
export type EntityList = components['schemas']['EntityList']
// Type of entity with cataloged data: it feeds the
// "adicionados recentemente" carousels of the home and the library dropdown of the
// navbar, without a fixed list of types in the frontend.
export type EntityType = components['schemas']['EntityType']
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
// External album as a page of its own (ADR-0029): official
// tracks of the release, with internal_id for the already cataloged ones — the "missing
// tracks" of the album, the same pattern as ExternalCollectionDetail/parts above.
export type ExternalAlbum = components['schemas']['ExternalAlbum']
export type ExternalAlbumTrack = components['schemas']['ExternalAlbumTrack']
// Credits (phase B): cast/crew/production companies. `profile_url`/`logo_url` already
// come PROXIED from the backend (direct use in the <img>). `episode_count` only in a series.
export type CreditCast = components['schemas']['CreditCast']
export type CreditCrew = components['schemas']['CreditCrew']
export type CreditCompany = components['schemas']['CreditCompany']
// Person deduplicated in the catalog + filmography (phase D, internal-first):
// destination of the cast/crew card when the credit carries a `person_id`.
export type Person = components['schemas']['Person']
export type PersonCredit = components['schemas']['PersonCredit']
// Work of the filmography NOT imported YET: external identity
// (source/source_id/type) instead of entity_id — the card navigates to the external detail.
export type PersonExternalCredit = components['schemas']['PersonExternalCredit']
export type ImportRequest = components['schemas']['ImportRequest']
export type LanguageSettings = components['schemas']['LanguageSettings']
// Config of CATALOGING completeness — per `entity.type`, which
// fields/artwork the owner considers desirable. A concept DIFFERENT from OWNERSHIP
// completeness (`web/src/lib/entity-badges.ts`) — do not reuse names from that
// module here.
export type CatalogCompletenessSettings =
  components['schemas']['CatalogCompletenessSettings']
export type TypeCompletenessConfig =
  components['schemas']['TypeCompletenessConfig']
export type OverrideBlob = components['schemas']['OverrideBlob']
export type OverridePatch = components['schemas']['OverridePatch']
export type FieldDescriptor = components['schemas']['FieldDescriptor']
export type FieldSchema = components['schemas']['FieldSchema']
export type Asset = components['schemas']['Asset']
export type AssetList = components['schemas']['AssetList']
export type ArtworkResponse = components['schemas']['ArtworkResponse']
export type ArtworkItem = components['schemas']['ArtworkItem']
export type SelectArtworkRequest = components['schemas']['SelectArtworkRequest']
export type CorrectRequest = components['schemas']['CorrectRequest']
export type PurgeResult = components['schemas']['PurgeResult']
export type PurgeBatchResult = components['schemas']['PurgeBatchResult']
// Result of POST .../nfo: the written path + the write timestamp.
export type NfoWriteResult = components['schemas']['NfoWriteResult']
// Consumption history: generic event (kind/status free TEXT
// — today only kind="assistir", status started/paused/stopped/completed) +
// derived state (the most recent per kind). Agnostic of vertical on purpose.
export type HistoricoEvento = components['schemas']['HistoricoEvento']
export type HistoricoEventoList = components['schemas']['HistoricoEventoList']
export type CreateHistoricoEvento =
  components['schemas']['CreateHistoricoEvento']
export type HistoricoEstado = components['schemas']['HistoricoEstado']
export type HistoricoEstadoItem = components['schemas']['HistoricoEstadoItem']
// History event WITHOUT a linked entity — global list of
// orphans, complementary to the per-entity timeline above.
export type HistoricoEventoOrfao = components['schemas']['HistoricoEventoOrfao']
export type HistoricoEventoOrfaoList =
  components['schemas']['HistoricoEventoOrfaoList']

// Shape of the body of `GET /api/v1/health`.
export interface Health {
  status: string
  version: string
}

// Typed error of the standard envelope `{ error: { code, message } }`. It carries
// the stable `code` (to branch on) and the pt-BR `message` (to show inline).
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

// It reads the error envelope of a non-2xx response and returns an ApiError. If the body
// is not the expected envelope, it falls into a generic error with the HTTP status.
async function erroDaResposta(res: Response): Promise<ApiError> {
  try {
    const corpo = (await res.json()) as components['schemas']['Error']
    if (corpo?.error?.message) {
      return new ApiError(res.status, corpo.error.code, corpo.error.message)
    }
  } catch {
    // absent or non-JSON body — it falls into the generic one below.
  }
  return new ApiError(res.status, 'http_error', `HTTP ${res.status}`)
}

async function lerJson<T>(res: Response): Promise<T> {
  if (!res.ok) {
    throw await erroDaResposta(res)
  }
  return (await res.json()) as T
}

// getHealth queries the health endpoint. It throws on a non-2xx HTTP status or on a
// network failure, so that TanStack Query treats it as an error state.
export async function getHealth(): Promise<Health> {
  const res = await fetch(`${base}/api/v1/health`)
  if (!res.ok) {
    throw new Error(`health respondeu HTTP ${res.status}`)
  }
  return (await res.json()) as Health
}

// System telemetry: database + operational usage + storage
// per source. SEPARATE from `getHealth` on purpose (its own endpoint, cached
// for 60s on the server) — a failure here must not bring down the basic status
// card, which consumes `getHealth` in isolation.
export type SystemHealth = components['schemas']['SystemHealth']
export type DatabaseHealth = components['schemas']['DatabaseHealth']
export type UsageCounts = components['schemas']['UsageCounts']
export type EntitiesUsage = components['schemas']['EntitiesUsage']
export type MediaFilesUsage = components['schemas']['MediaFilesUsage']
export type StorageSnapshot = components['schemas']['StorageSnapshot']
export type SourceStorage = components['schemas']['SourceStorage']
export type FilesystemStats = components['schemas']['FilesystemStats']

export async function getSystemHealth(): Promise<SystemHealth> {
  const res = await fetch(`${base}/api/v1/health/system`)
  if (!res.ok) {
    throw new Error(`health/system respondeu HTTP ${res.status}`)
  }
  return (await res.json()) as SystemHealth
}

// ── Datasources (library) ────────────────────────────────────────────────────

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

// Partial patch of a datasource: it turns the filesystem watcher on/off
// (`watch_enabled`), applied live by the server (without a restart),
// and/or re-points the path (`path`) — it re-bases media_file/asset/nfo in cascade
// to the new prefix, without a re-scan. Fields absent from the body stay unchanged.
export async function patchLibrary(
  id: string,
  body: PatchSource,
): Promise<Source> {
  const res = await fetch(`${base}/api/v1/library/${encodeURIComponent(id)}`, {
    method: 'PATCH',
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

// ─── Reprocessamento de uma biblioteca (POST/GET /library/{id}/reprocess) ───
// A ação do dono para corrigir o catálogo JÁ existente: retenta os drafts sem o
// teto de 4 min do pós-scan, purga os rascunhos vazios e, no modo `full`, reescaneia
// antes (o scan re-parseia o nome de cada arquivo). O dry-run é o padrão e é o que a
// UI mostra antes de qualquer escrita.
export type ReprocessReport = {
  mode: 'fast' | 'full'
  dry_run: boolean
  series_draft: number
  movies_pending: number
  draft_episodes: number
  orphans: number
  promoted: number
  orphans_deleted: number
  scan_queued?: boolean
}

export type ReprocessStatus = {
  running: boolean
  mode?: string
  phase?: string
  done: number
  total: number
  report?: ReprocessReport
  last_error?: string
}

export async function startReprocess(
  id: string,
  body: { mode?: 'fast' | 'full'; dry_run?: boolean; delete_orphans?: boolean },
): Promise<ReprocessReport | StartScan> {
  const res = await fetch(
    `${base}/api/v1/library/${encodeURIComponent(id)}/reprocess`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
  return lerJson<ReprocessReport | StartScan>(res)
}

export async function getReprocessStatus(id: string): Promise<ReprocessStatus> {
  const res = await fetch(
    `${base}/api/v1/library/${encodeURIComponent(id)}/reprocess`,
  )
  return lerJson<ReprocessStatus>(res)
}

export async function getScanStatus(id: string): Promise<ScanStatus> {
  const res = await fetch(
    `${base}/api/v1/library/${encodeURIComponent(id)}/scan`,
  )
  return lerJson<ScanStatus>(res)
}

// Lists the content (directories only) of an absolute filesystem path that the
// API process can see — used by the folder browser of the
// datasource create/edit form (`PathBrowser`). An absent/empty `path`
// falls back to the root `/` (same default as the backend). Invalid/
// non-directory/unreadable path errors arrive as `ApiError` (422
// `invalid_path`/`path_unreadable`) — the caller decides how to display them.
export async function browseLibraryPath(path?: string): Promise<BrowseResult> {
  const qs = new URLSearchParams()
  if (path) {
    qs.set('path', path)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<BrowseResult>(
    await fetch(`${base}/api/v1/library/browse${suffix}`),
  )
}

// ── Editorial form descriptor ────────────────────────────────────────────────

// Descriptors of the editable fields, served by the backend
// from `resolver.CamposEditaveis()` + presentation metadata. It is UI
// metadata that is IMMUTABLE between deploys — the UI builds the form and the Zod schema from
// it, so that a new field in the backend makes the input appear without touching JSX.
// It returns the list already sorted by `order`.
export async function getEntityFields(): Promise<FieldDescriptor[]> {
  const schema = await lerJson<FieldSchema>(
    await fetch(`${base}/api/v1/schema/entity-fields`),
  )
  return schema.fields
}

// ── Entities (catalog) ───────────────────────────────────────────────────────

export interface EntityListParams {
  type?: string
  owned?: boolean
  limit?: number
  offset?: number
  // '-created_at': most recent first — it feeds the
  // "adicionados recentemente" carousels of the home. Absent/'title' keeps the default
  // (ordering by title).
  sort?: 'title' | '-title' | '-created_at'
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
  // empty/omitted `lang` = without the parameter (the backend falls back to the primary — compat).
  if (lang) {
    qs.set('lang', lang)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<EntityList>(await fetch(`${base}/api/v1/entities${suffix}`))
}

// Types of entity with at least 1 cataloged item, from
// the most numerous to the least numerous — without a fixed list: it covers any `type`
// that actually exists in the database (movie/tv, but also album/artista/faixa since
// the music vertical). It feeds the "adicionados recentemente" carousels of the home
// and the library dropdown of the navbar.
export async function listEntityTypes(): Promise<EntityType[]> {
  return lerJson<EntityType[]>(await fetch(`${base}/api/v1/entities/types`))
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

// Members of a COLLECTION: the items linked by entity_relation
// (kind='collection') as internal cards (the same EntityCard as the list), ordered
// by year by the backend. `[]` when the entity is not a collection or has no members. `lang`
// resolves the titles of the members in the active language.
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

// Episodes of the SERIES grouped by display order: all the descendant
// episodes, projected in the `ordering` order (official/dvd/absolute…) and grouped
// by season of the order — regrouped/renumbered ON READ from the numbering,
// without creating a season entity (ADR-0021). An absent/invalid `ordering` falls back to
// the default of the series. `lang` resolves the titles in the active language (parity with /members).
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

// ── Collection: manual creation + members (Phase B) ──────────────────────────

// Creates a LOCAL collection (without a source): the owner builds a grouping from scratch. Only the
// title is required (the backend answers 422 if it comes empty); the description is
// optional. It returns the newly created `ResolvedEntity` (it has `.id`) — the UI navigates to
// `/entities/{id}`. It uses an INLINE body (there is no dedicated generated schema): the contract
// is `{ title, description? }`.
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

// Links an item (`childId`) to a collection (`id`) as a member. It returns the UPDATED
// member list (the same `EntityCard[]` as `getEntityMembers`, ordered by
// year) — the UI reflects it without a 2nd round trip. Idempotent in the backend (the UNIQUE protects it).
// Errors as `ApiError` with the pt-BR `message`: 422 (parent is not a collection / item
// does not exist / self / nesting a collection), 404 (collection does not exist).
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

// Unlinks a member (`childId`) from a collection (`id`). Idempotent (removing an item
// that is not linked answers 200 anyway). It returns the UPDATED member list.
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

// ── Local item + external identity ───────────────────────────────────────────

// Creates a LOCAL item (without an external source): a movie or a series from scratch, for the owner
// to catalog something that came from neither a scan nor an import. Only `type` and `title` are
// required (the backend answers 422 if they are missing/invalid); the rest are
// optional. It returns the newly created `ResolvedEntity` (it has `.id`) — the UI navigates to
// `/entities/{id}`. INLINE body (contract `{ type, title, ... }`, without a dedicated generated
// schema). Afterwards the owner can fix the `external_ids` (updateExternalIds) and
// re-enrich to synchronize with the source.
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

// Header returned by `PUT external-ids`/`POST correct` when the write
// of the identity collides with another entity WITHOUT investment from the owner: the backend merges
// the duplicate automatically and signals it here (id of the removed entity) so the UI can
// warn the owner, instead of letting them find the duplicate alone.
const HEADER_DUPLICATA_MESCLADA = 'X-Duplicate-Merged-Id'

// Result of a write of the external identity: the resolved entity +
// whether the response came with the automatic merge of a duplicate.
export interface EscritaIdentidade {
  entity: ResolvedEntity
  duplicataMesclada: boolean
}

async function lerEscritaIdentidade(res: Response): Promise<EscritaIdentidade> {
  const entity = await lerJson<ResolvedEntity>(res)
  return {
    entity,
    duplicataMesclada: Boolean(res.headers?.get(HEADER_DUPLICATA_MESCLADA)),
  }
}

// Writes (FULL REPLACE) the external identity of the entity (`external_ids`, MD4): the owner
// fixes which id of each source the entity uses (e.g. tmdb=603), the basis for a future
// synchronization — the ids of the source may change or the item may be replaced by another one. The backend
// discards empty values (removing a source = leaving the id blank). The generated
// columns (external_id_tmdb/imdb) re-derive on their own. INLINE body
// `{ external_ids: {source: id} }`. If the id written collides with another entity without
// investment from the owner, the backend merges the two — see `EscritaIdentidade`.
export async function updateExternalIds(
  id: string,
  externalIds: Record<string, string>,
): Promise<EscritaIdentidade> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/external-ids`,
    {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ external_ids: externalIds }),
    },
  )
  return lerEscritaIdentidade(res)
}

// ── Owner order (ADR-0033) ───────────────────────────────────────────────────

// A numbered unit of the owner's own order. `episodes` has 1..N ids and the ORDER is
// meaningful: it is the `ordinal` of the file link, so a unit with three episodes is a
// release entry that merged three aired episodes.
export interface OwnerOrderUnit {
  season: number
  number: number
  title?: string
  episodes: string[]
}

// The order the owner declares for a series. An empty `units` means "not declared".
// Anchored on episode IDS (never on numbers) so a re-scrape cannot shuffle it.
export interface OwnerOrder {
  name: string
  based_on: string
  units: OwnerOrderUnit[]
}

// Reads the owner order of the series (empty units when there is none).
export async function getOwnerOrder(id: string): Promise<OwnerOrder> {
  return lerJson<OwnerOrder>(
    await fetch(
      `${base}/api/v1/entities/${encodeURIComponent(id)}/owner-order`,
    ),
  )
}

// Writes (replaces) the whole owner order. The backend validates everything before
// writing: an id from another series, a duplicate episode or a duplicate
// (season, number) comes back as an `ApiError` and nothing is stored.
export async function putOwnerOrder(
  id: string,
  ordem: OwnerOrder,
): Promise<OwnerOrder> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/owner-order`,
    {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(ordem),
    },
  )
  return lerJson<OwnerOrder>(res)
}

// Removes the owner order, keeping every other override of the entity. Idempotent.
export async function deleteOwnerOrder(id: string): Promise<void> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/owner-order`,
    { method: 'DELETE' },
  )
  if (!res.ok) {
    throw await erroDaResposta(res)
  }
}

// ── Suggested match ("did you mean this?") ───────────────────────────────────

// The hint the backend keeps for an item it could not resolve: a cheaper VARIANT of the name
// on disk found this candidate. It is never applied on its own -- the action behind it is
// `confirmCandidate(id, { method: 'external', ... })`, the same endpoint the review card
// already uses.
export type MatchSuggestion = components['schemas']['MatchSuggestion']

// Dismisses the hint. The backend KEEPS the row (dismissed), so opening the item again
// neither re-queries the sources nor brings the warning back. Idempotent: 204 even when
// there was no hint.
export async function dismissMatchSuggestion(id: string): Promise<void> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/match-suggestion`,
    { method: 'DELETE' },
  )
  if (!res.ok) {
    throw await erroDaResposta(res)
  }
}

// ── Language settings ────────────────────────────────────────────────────────

// Current language config (primary, fallback, allowed). When there is no
// stored row, the backend answers the safe default (pt-BR / en / [pt-BR, en]).
export async function getLanguageSettings(): Promise<LanguageSettings> {
  return lerJson<LanguageSettings>(
    await fetch(`${base}/api/v1/settings/languages`),
  )
}

// Writes (upsert) the language config. The backend 422 (primary/fallback outside
// the allowed ones, or empty allowed) becomes an `ApiError` with the pt-BR message —
// shown inline by the settings screen.
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

// ── Settings of the retroactive enrichment throttle ──────────────────────────

export type RetroactiveEnrichmentSettings =
  components['schemas']['RetroactiveEnrichmentSettings']

// Minimum interval (in days) between two retroactive enrichment attempts
// of the SAME item, applied to the libraries without an override of their own
// (`Source.retroactive_enrichment_days`). When there is no stored row yet,
// the backend answers the safe default (`{"retry_days": 15}`).
export async function getRetroactiveEnrichmentSettings(): Promise<RetroactiveEnrichmentSettings> {
  return lerJson<RetroactiveEnrichmentSettings>(
    await fetch(`${base}/api/v1/settings/enrichment-retroativo`),
  )
}

// Writes (upsert) the throttle config. The backend 422 (`retry_days` outside
// [1, 365]) becomes an `ApiError` with the pt-BR message — shown inline.
export async function putRetroactiveEnrichmentSettings(
  body: RetroactiveEnrichmentSettings,
): Promise<RetroactiveEnrichmentSettings> {
  const res = await fetch(`${base}/api/v1/settings/enrichment-retroativo`, {
    method: 'PUT',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<RetroactiveEnrichmentSettings>(res)
}

// ── Settings of automatic artwork materialization ────────────────────────────

export type AutoMaterializeArtworkSettings =
  components['schemas']['AutoMaterializeArtworkSettings']

// Toggle: when on, every successful auto-import of the scan (movie or
// series) already materializes right away, into the real library folder, the artwork
// discovered in the inventory. When there is no stored row yet, the backend
// answers the safe default (`{"enabled": false}`).
export async function getAutoMaterializeArtworkSettings(): Promise<AutoMaterializeArtworkSettings> {
  return lerJson<AutoMaterializeArtworkSettings>(
    await fetch(`${base}/api/v1/settings/auto-materialize-artwork`),
  )
}

// Writes (upsert) the toggle. The backend 400 (malformed body) becomes an
// `ApiError` with the pt-BR message — shown inline.
export async function putAutoMaterializeArtworkSettings(
  body: AutoMaterializeArtworkSettings,
): Promise<AutoMaterializeArtworkSettings> {
  const res = await fetch(`${base}/api/v1/settings/auto-materialize-artwork`, {
    method: 'PUT',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<AutoMaterializeArtworkSettings>(res)
}

// ── Settings of automatic trailer/theme download during the scan ─────────────

// Manual shape (not via `components['schemas']`): the backend for this feature
// is being implemented in parallel and the openapi.yaml/gen:types does not
// have the new schema yet. The same shape as `AutoMaterializeArtworkSettings`
// (`{ enabled: boolean }`).
export type AutoDownloadTrailerTemaSettings = { enabled: boolean }

// Toggle: when on, importing/auto-matching a new item in the scan tries
// to download the trailer and the theme in the background (best-effort, it does not block the
// scan response). When there is no stored row yet, the backend answers the
// safe default (`{"enabled": false}`).
export async function getAutoDownloadTrailerTemaSettings(): Promise<AutoDownloadTrailerTemaSettings> {
  return lerJson<AutoDownloadTrailerTemaSettings>(
    await fetch(`${base}/api/v1/settings/auto-download-trailer-tema`),
  )
}

// Writes (upsert) the toggle. The backend 400 (malformed body) becomes an
// `ApiError` with the pt-BR message — shown inline.
export async function putAutoDownloadTrailerTemaSettings(
  body: AutoDownloadTrailerTemaSettings,
): Promise<AutoDownloadTrailerTemaSettings> {
  const res = await fetch(
    `${base}/api/v1/settings/auto-download-trailer-tema`,
    {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
  return lerJson<AutoDownloadTrailerTemaSettings>(res)
}

// ── Settings of the custom yt-dlp path (ADR-0025) ────────────────────────────

export type YtdlpSettings = components['schemas']['YtdlpSettings']
export type YtdlpCustomPath = components['schemas']['YtdlpCustomPath']

// EFFECTIVE status of the yt-dlp binary -- foundation of the YouTube downloader
// reused by the trailer and the theme song. `disponivel` says whether
// there is a usable binary now; `origem` distinguishes a custom path
// ("custom"), dynamically downloaded ("baixado") or unavailable ("nenhum").
export async function getYtdlpSettings(): Promise<YtdlpSettings> {
  return lerJson<YtdlpSettings>(await fetch(`${base}/api/v1/settings/ytdlp`))
}

// Writes (or clears, with `null`/empty string) the custom yt-dlp path.
// IMMEDIATE effect at runtime, without restarting the server.
export async function putYtdlpSettings(
  body: YtdlpCustomPath,
): Promise<YtdlpSettings> {
  const res = await fetch(`${base}/api/v1/settings/ytdlp`, {
    method: 'PUT',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<YtdlpSettings>(res)
}

// ── Jellyfin inbound webhook ─────────────────────────────────────────────────

export type JellyfinWebhookToken = components['schemas']['JellyfinWebhookToken']

// Current token of the Jellyfin inbound webhook. A 404 (no token generated
// yet) is a NORMAL state on the first visit — it must not become a query
// error, so it becomes `{token: null}` instead of throwing.
export async function getJellyfinWebhookToken(): Promise<{
  token: string | null
}> {
  const res = await fetch(`${base}/api/v1/webhooks/jellyfin/token`)
  if (res.status === 404) {
    return { token: null }
  }
  const dado = await lerJson<JellyfinWebhookToken>(res)
  return { token: dado.token }
}

// Generates (or replaces, if it already exists) the Jellyfin webhook token.
export async function regenerateJellyfinWebhookToken(): Promise<JellyfinWebhookToken> {
  const res = await fetch(`${base}/api/v1/webhooks/jellyfin/token`, {
    method: 'POST',
  })
  return lerJson<JellyfinWebhookToken>(res)
}

// ── Integrations configurable through the UI/database ────────────────────────

export type IntegrationSummary = components['schemas']['IntegrationSummary']
export type IntegrationsListResponse =
  components['schemas']['IntegrationsListResponse']
export type IntegrationField = components['schemas']['IntegrationField']
export type IntegrationFieldOption =
  components['schemas']['IntegrationFieldOption']
export type IntegrationDetail = components['schemas']['IntegrationDetail']
export type PutIntegrationRequest =
  components['schemas']['PutIntegrationRequest']

// Lists the integrations registered in the Registry (today only TMDB) with the
// `configurada` already computed by the backend — the front never recalculates that
// rule (each provider has its own, e.g.: TMDB accepts token OR api_key).
export async function listIntegrations(): Promise<IntegrationSummary[]> {
  const dado = await lerJson<IntegrationsListResponse>(
    await fetch(`${base}/api/v1/integrations`),
  )
  return dado.integrations
}

// Field schema + effective value (database → env var → absent) of ONE
// integration. A field `tipo:"secret"` never comes with a `valor` in the body — only
// `definido`/`origem` (even in the echo of `putIntegration` below).
export async function getIntegration(slug: string): Promise<IntegrationDetail> {
  return lerJson<IntegrationDetail>(
    await fetch(`${base}/api/v1/integrations/${encodeURIComponent(slug)}`),
  )
}

// PARTIAL upsert: only the keys present in `valores` change; `null` removes the
// key (the field inherits the env var again, or stays `origem:"nenhum"` if it
// is also empty). 404 (unknown slug) / 422 (empty valores or
// unknown key) become an `ApiError` with the pt-BR message — shown inline
// by the `IntegrationForm`.
export async function putIntegration(
  slug: string,
  valores: Record<string, string | null>,
): Promise<IntegrationDetail> {
  const body: PutIntegrationRequest = { valores }
  const res = await fetch(
    `${base}/api/v1/integrations/${encodeURIComponent(slug)}`,
    {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
  return lerJson<IntegrationDetail>(res)
}

// ── Settings of cataloging completeness ──────────────────────────────────────

// Current config of CATALOGING completeness (per `entity.type`). When there
// is no stored row, the backend answers the factory default (description +
// media_file + artwork:[poster] for movie/tv) without writing anything.
export async function getCatalogCompletenessSettings(): Promise<CatalogCompletenessSettings> {
  return lerJson<CatalogCompletenessSettings>(
    await fetch(`${base}/api/v1/settings/catalog-completeness`),
  )
}

// Writes (total replacement) the cataloging completeness config. The backend 422
// (empty type key, unknown artwork kind) becomes an `ApiError` with
// the pt-BR message — shown inline by the settings screen.
export async function putCatalogCompletenessSettings(
  body: CatalogCompletenessSettings,
): Promise<CatalogCompletenessSettings> {
  const res = await fetch(`${base}/api/v1/settings/catalog-completeness`, {
    method: 'PUT',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<CatalogCompletenessSettings>(res)
}

export async function getEntityFiles(id: string): Promise<FileList> {
  return lerJson<FileList>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}/files`),
  )
}

// ── Owner override — editing per language ────────────────────────────────────

// RAW blob of the stored override (`{}` when there is none). The UI uses it to distinguish
// "overridden" from "inherited" PER FIELD and PER LANGUAGE: translatable fields come as a
// map by language (`{"pt-BR":…}`), neutral ones as the direct scalar. It is not the resolved
// entity — it is the patch as it is in the database (the resolver interprets it on read).
export async function getOverrideBlob(id: string): Promise<OverrideBlob> {
  return lerJson<OverrideBlob>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}/override`),
  )
}

// Merges a sparse patch into the override, PER LANGUAGE. The `?lang=` is the "brush" of the
// translatable fields (title/description/tags): the handler writes `override[campo][lang]`,
// preserving the other languages. Neutral fields (original_title/release_date)
// ignore the `lang`. `null` in a field = "set empty" (distinct from CLEAR).
// It returns the entity RESOLVED in the `lang` (the UI reflects it right away). 422 = invalid patch.
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

// CLEAR the override of a field (≠ set empty). With `lang` present and a translatable
// field, it removes only `override[campo][lang]` (it INHERITS again in that language);
// without `lang`, or for a neutral field, it removes the whole key. Idempotent. It returns the
// entity RESOLVED.
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

// RESET — it deletes the whole override: every field inherits the base again.
// It returns the entity RESOLVED (on the base, without override).
export async function resetOverride(id: string): Promise<ResolvedEntity> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/override`,
    { method: 'DELETE' },
  )
  return lerJson<ResolvedEntity>(res)
}

// ── Search + import ──────────────────────────────────────────────────────────

// Unified search: `local` (catalog) + `external` (external source, "not saved").
// An empty `q` is rejected by the backend (400) — the UI only calls it with a filled term.
// An optional `type` filters by ANY real top-level type (ADR-0030 —
// no longer only `movie`|`tv`; the backend stopped validating against a fixed enum in
// the generic top types delivery). `include` turns the sub-nodes of the series tree back on (`season`/`episode`),
// which by default stay out of the search; they come in the same `local[]`, and the UI separates them by
// `type` into sections of their own.
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
  // `lang` = active reading language: the external search returns titles/synopses
  // in it. Empty/omitted = the backend falls back to the primary.
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

// Imports an external result into the catalog. It returns the `ResolvedEntity`
// (it has `.id`) both on 201 (created) and on 200 (it already existed).
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

// Re-enriches an EXISTING entity through the source: it fetches the detail and rewrites the
// external base + poster. Without a body, it uses `source=tmdb` and the `source_id` of
// `external_ids[tmdb]` of the entity itself — the synchronization step after fixing the id.
// It returns the re-resolved entity. Source/identity errors (422 without an id, 502
// source, 503 without a provider) become an `ApiError` with the pt-BR message of the envelope.
// `force: true` (item "Baixar Metadados" of the Gerenciar menu) skips the
// 24h cache of `external_detail` and always goes to the network — existing call sites that
// do not pass `force` keep being served from the cache normally (no regression).
export async function enrichEntity(
  id: string,
  body?: { source?: string; source_id?: string; force?: boolean },
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

// ── Rich external detail ─────────────────────────────────────────────────────

// Rich detail of an item IN THE SOURCE, BEFORE importing: what
// TMDB shows (synopsis, genres, keywords, runtime, certification, tagline,
// videos, collection, similars, links). The translatable fields already come resolved in
// the requested `lang`; an omitted `lang` falls back to the backend primary. The backend persists
// on the 1st round trip and serves from the cache afterwards. `type` is `movie|tv`. Errors mapped to
// `ApiError`: 404 (absent in the source), 502 (source failed), 503 (no provider),
// 400 (language outside the allowed ones), 422 (invalid type).
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

// External collection as a page: the metadata of the
// set and the parts (movies) navigable to the external detail. Localized in the requested
// `lang` (mono-language); an omitted `lang` falls back to the primary. Errors as `ApiError`: 404
// (absent in the source), 502 (source failed), 503 (source without a collection), 400 (language
// outside the allowed ones).
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

// External album as a page (ADR-0029): the OFFICIAL tracks
// of the release, with `internal_id` for the already cataloged ones — the "missing tracks" of the
// album, the same pattern as getCollectionDetail above. `id` is the MBID of the
// release-group. Errors as `ApiError`: 404 (absent in the source), 502 (source
// failed), 503 (source without the selective album capability).
export async function getExternalAlbum(
  source: string,
  id: string,
  lang?: string,
): Promise<ExternalAlbum> {
  const qs = new URLSearchParams()
  if (lang) {
    qs.set('lang', lang)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return lerJson<ExternalAlbum>(
    await fetch(
      `${base}/api/v1/external/${encodeURIComponent(
        source,
      )}/album/${encodeURIComponent(id)}${suffix}`,
    ),
  )
}

// ── Person (phase D, internal-first) ─────────────────────────────────────────

// getPerson fetches the person deduplicated in the catalog + their FILMOGRAPHY (the works already
// imported that credit them). Read-only in this phase. 404 (`ApiError`) when the id does not
// exist in the catalog.
export async function getPerson(id: string): Promise<Person> {
  return lerJson<Person>(
    await fetch(`${base}/api/v1/person/${encodeURIComponent(id)}`),
  )
}

// Absolute URL (respecting the base) of the local poster binary, or `null` when
// the card has no artwork — the UI falls back to the placeholder. `poster_url` already comes as a
// ready route of the card (e.g. `/api/v1/entities/{id}/assets/poster`).
export function posterSrc(posterUrl: string | null | undefined): string | null {
  if (!posterUrl) {
    return null
  }
  return `${base}${posterUrl}`
}

// imagemProxySrc builds the URL of the IMAGE PROXY (GET /api/v1/images?src=) for a RAW
// external image URL — today the episode still of the provider. The proxy fetches and
// caches the image ON DEMAND (by hash) on the 1st display, decoupling the UI from the CDN
// of the source at runtime. It differs from `posterSrc` (a ready LOCAL asset route) and from
// `srcDeImagem` (which only decides base × direct URL): here it ALWAYS routes through the proxy, since the
// external source is not directly servable at runtime. `null`/empty → null (the UI falls back to the
// placeholder). It respects the base (VITE_API_BASE), like `posterSrc`.
export function imagemProxySrc(
  rawUrl: string | null | undefined,
): string | null {
  if (!rawUrl) {
    return null
  }
  return `${base}/api/v1/images?src=${encodeURIComponent(rawUrl)}`
}

// arteSrc resolves ANY artwork URL for the `<img>`: an ABSOLUTE URL (raw from the
// provider — e.g. still/backdrop of TMDB/TVDB) goes through the IMAGE PROXY (`imagemProxySrc`,
// which fetches+caches on demand, decoupling from the CDN at runtime); a relative LOCAL ROUTE
// (an already servable asset — e.g. a custom `/assets/still`, a poster/fanart of the owner) receives only the
// base (`posterSrc`). It unifies the three divergent treatments (local poster, raw/local backdrop,
// raw/custom still) in a single helper. `null`/empty → null (the UI falls back to the
// placeholder), like the helpers it composes.
export function arteSrc(url: string | null | undefined): string | null {
  if (!url) {
    return null
  }
  return /^https?:\/\//.test(url) ? imagemProxySrc(url) : posterSrc(url)
}

// ── Custom artwork: gallery + upload ─────────────────────────────────────────

// Image types accepted by the upload (the same set the backend validates by
// magic bytes). The UI reuses this for the `accept` of the <input> and for the validation
// on the client BEFORE sending (it rejects early, without spending a round trip). The
// `still` is the kind of the 16:9 artwork of the episode; `kindsParaTipo` filters what
// each type offers in the upload.
export const KINDS_ARTE = ['poster', 'fanart', 'still'] as const
export type KindArte = (typeof KINDS_ARTE)[number]

// kindsParaTipo returns the kinds offered in the upload according to the TYPE of the entity:
// the EPISODE manages only the `still` (its 16:9 artwork); the other types manage
// `poster` (2:3) and `fanart` (16:9). It keeps the `still` out of the movie/series menu
// (where it makes no sense) without preventing the backend from accepting it generically.
export function kindsParaTipo(type: string): readonly KindArte[] {
  return type === 'episode' ? ['still'] : ['poster', 'fanart']
}

export const MIMES_ARTE = ['image/png', 'image/jpeg', 'image/webp'] as const

// Absolute URL (respecting the base) of the binary of an asset for the thumbnails.
// The `Asset.url` already comes as a ready route of the backend (route of the /file) — it only applies the
// base, like `posterSrc`.
export function assetFileSrc(url: string): string {
  return `${base}${url}`
}

// Gallery: all the assets of the entity (poster/fanart, user/external/local). `[]`
// when there is no artwork.
export async function listAssets(id: string): Promise<AssetList> {
  return lerJson<AssetList>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}/assets`),
  )
}

// ── Local theme song ─────────────────────────────────────────────────────────

// themePath builds the route of the theme binary — the single source of the template (the
// player of the hero uses the SAME route as the `src` of the `<audio>` and in the HEAD below).
function themePath(id: string): string {
  return `/api/v1/entities/${encodeURIComponent(id)}/theme`
}

// themeSrc is the absolute URL (respecting the base) of the theme binary, ready
// for the `src` of the `<audio>`.
export function themeSrc(id: string): string {
  return `${base}${themePath(id)}`
}

// hasEntityTheme checks whether the entity has a theme.{mp3,ogg,flac} in the root folder
// WITHOUT downloading the whole binary (HEAD — the backend serves the same handler,
// `http.ServeContent` already omits the body). Used once per screen load (without
// polling): 200 → true, 404 → false. A network failure also becomes `false`
// (graceful degradation — the player simply does not appear).
export async function hasEntityTheme(id: string): Promise<boolean> {
  try {
    const res = await fetch(`${base}${themePath(id)}`, { method: 'HEAD' })
    return res.ok
  } catch {
    return false
  }
}

// Automatic theme song through ThemerrDB. The
// candidate is read from the CACHE (media_external, written by the enrich) — it never
// queries ThemerrDB live. The YouTube URL never reaches the client
// (§6): the download happens entirely on the server.
export type ThemeCandidatoResult = components['schemas']['ThemeCandidatoResult']
export type ThemeDownloadResult = components['schemas']['ThemeDownloadResult']

// getEntityThemeCandidato checks whether there is a cached theme song candidate from
// ThemerrDB and whether a local theme.* already exists. A network error becomes the
// "no candidate, no local" state (graceful degradation — the button
// simply does not appear).
export async function getEntityThemeCandidato(
  id: string,
): Promise<ThemeCandidatoResult> {
  try {
    const res = await fetch(
      `${base}/api/v1/entities/${encodeURIComponent(id)}/theme/candidato`,
    )
    return await lerJson<ThemeCandidatoResult>(res)
  } catch {
    return { disponivel: false, ja_tem_local: false }
  }
}

// downloadEntityTheme downloads (via yt-dlp, on the server) the theme song
// candidate from ThemerrDB and materializes it as theme.mp3 in the root of the folder.
// SYNCHRONOUS: the promise only resolves when the download finishes. Errors (503 without
// yt-dlp, 422 without a candidate, 409 already exists, 502 download failure) arrive
// as `ApiError` with the pt-BR `message` of the envelope.
export async function downloadEntityTheme(
  id: string,
): Promise<ThemeDownloadResult> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/theme/download`,
    { method: 'POST' },
  )
  return lerJson<ThemeDownloadResult>(res)
}

// ── Trailer materialized on disk ─────────────────────────────────────────────

export type TrailerDownloadRequest =
  components['schemas']['TrailerDownloadRequest']
export type TrailerDownloadResult =
  components['schemas']['TrailerDownloadResult']

// trailerPath builds the route of the trailer binary — the single source of the template
// (same pattern as themePath).
function trailerPath(id: string): string {
  return `/api/v1/entities/${encodeURIComponent(id)}/trailer`
}

// trailerSrc is the absolute URL (respecting the base) of the binary of the trailer already
// materialized — used by the "Trailer já baixado" link (it opens/downloads the
// file straight from the browser).
export function trailerSrc(id: string): string {
  return `${base}${trailerPath(id)}`
}

// hasEntityTrailer checks whether the entity already has a materialized trailer WITHOUT
// downloading the whole binary (HEAD — the backend serves the same handler as the GET,
// `http.ServeContent` already omits the body). 200 → true, 404 → false. A network
// failure also becomes `false` (graceful degradation — the button simply
// offers "baixar" again).
export async function hasEntityTrailer(id: string): Promise<boolean> {
  try {
    const res = await fetch(`${base}${trailerPath(id)}`, { method: 'HEAD' })
    return res.ok
  } catch {
    return false
  }
}

// Downloads (via yt-dlp, on the server) the YouTube trailer of the item and materializes it under the
// canonical Kodi/Jellyfin name. SYNCHRONOUS: the promise only resolves when the
// download finishes (seconds to a few minutes, §6). Errors (503 without
// yt-dlp, 422 without a trailer, 409 already exists without `force`, 502 download
// failure) arrive as `ApiError` with the pt-BR `message` of the envelope — the
// caller decides how to display it.
export async function downloadEntityTrailer(
  id: string,
  body?: TrailerDownloadRequest,
): Promise<TrailerDownloadResult> {
  const res = await fetch(`${base}${trailerPath(id)}/download`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  })
  return lerJson<TrailerDownloadResult>(res)
}

// Multipart upload of the owner's binary (`origin=user`). It builds a FormData with the
// fields `file` and `kind` — it does NOT set `content-type` by hand: the browser fills the
// header with the multipart boundary. The backend limits (415 type, 413
// size, 422 body) become an `ApiError` with the pt-BR `message` of the envelope.
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

// Marks an asset as preferred/PRINCIPAL of its kind (it breaks ties between several of the
// owner). The backend clears the flag of the others of the same `(entity, kind)`. Since
// the principal source of truth work: if the asset has no `local_path` yet, it materializes first
// (downloads to the cache if needed + writes a physical copy into the real folder) and RENAMES the
// physical files of the kind to reflect the new order (numbered rotation); it sets
// `is_principal=1` on it (source of truth, distinct from the `preferred` of meta_json).
// Errors (404 without media_file/destination folder, 502 download, 503 without an assets
// directory) become an `ApiError` with the pt-BR message of the envelope.
export async function preferAsset(id: string, assetId: string): Promise<Asset> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/assets/${encodeURIComponent(
      assetId,
    )}/prefer`,
    { method: 'PUT' },
  )
  return lerJson<Asset>(res)
}

// Materializes an asset into the REAL library folder: it downloads to the
// internal cache if `path_or_uri` is not there yet and writes a physical COPY into
// the directory of the item, with the canonical Kodi/Emby name. It returns the updated asset with
// `local_path` filled in. Errors (404 without media_file/destination folder, 502
// download, 503 without an assets directory) become an `ApiError` with the pt-BR message of the
// envelope. For an INVENTORY candidate (without an `asset_id` yet), the UI chains
// `selectArtwork({prefer:false})` (it creates the asset) before calling this function.
export async function materializeAsset(
  id: string,
  assetId: string,
): Promise<Asset> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/assets/${encodeURIComponent(
      assetId,
    )}/materialize`,
    { method: 'POST' },
  )
  return lerJson<Asset>(res)
}

// Removes an artwork of the owner (`origin=user`). The backend refuses to delete `external`
// (409/422) — the UI only offers the button on the owner's ones.
export async function deleteAsset(id: string, assetId: string): Promise<Asset> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/assets/${encodeURIComponent(
      assetId,
    )}`,
    { method: 'DELETE' },
  )
  return lerJson<Asset>(res)
}

// ── Gallery grouped by kind + inventory selection ────────────────────────────

// AVAILABLE artwork grouped by kind: it merges local assets of the owner, external
// layers with a provider (TMDB) and the lazy inventory of fanart.tv. Every `url` already comes
// PROXIED (ready for the `<img src>` — do NOT reapply `arteSrc`). It also brings
// `available: {kind: count}` for the badges of the top card.
export async function getArtwork(id: string): Promise<ArtworkResponse> {
  return lerJson<ArtworkResponse>(
    await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}/artwork`),
  )
}

// Materializes an INVENTORY artwork (fanart.tv/TMDB) as a local asset `origin=
// 'external'`: it downloads the binary of the RAW `url` (`src_url` of the item) and, with
// `prefer=true`, marks it as preferred of its kind. Idempotent in the backend (it reuses the
// external asset with the same url+kind). The errors (422 kind/host, 502 download, 503
// without a directory) become an `ApiError` with the pt-BR message of the envelope.
export async function selectArtwork(
  id: string,
  body: SelectArtworkRequest,
): Promise<Asset> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/artwork/select`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
  return lerJson<Asset>(res)
}

// ── Curation: review + orphans + match correction ────────────────────────────

// REVIEW queue: `draft` (unidentified) cards OR with low confidence
// (`0 < match_confidence < 70`). It returns the array directly (it is not an EntityList — the
// queue is a lean query). `[]` when there is nothing to review.
export async function getReviewQueue(): Promise<EntityCard[]> {
  return lerJson<EntityCard[]>(await fetch(`${base}/api/v1/review`))
}

// ORPHAN queue: auto-created entities (`cataloged`) with file(s) all
// `missing` and WITHOUT investment from the owner (without override, without a custom asset). Array
// directly; `[]` when there are no orphans.
export async function getOrphans(): Promise<EntityCard[]> {
  return lerJson<EntityCard[]>(await fetch(`${base}/api/v1/orphans`))
}

// CONFIRM the match: the owner accepts the guess. The backend raises the `match_confidence`
// of the files of the entity to 100 (the owner's authority — the alert disappears). It returns the
// re-resolved entity.
export async function confirmMatch(id: string): Promise<ResolvedEntity> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/confirm`,
    { method: 'POST' },
  )
  return lerJson<ResolvedEntity>(res)
}

// CORRECT the match: the owner informs the right identity (`source`/`source_id`). The
// backend writes `external_ids[source]`, re-enriches through the source and raises the
// confidence (identity wins). `source` default "tmdb". Source errors
// (422/404/502/503) become an `ApiError` with the pt-BR message of the envelope. If the id
// collides with another entity without investment from the owner, the backend merges the two
// — see `EscritaIdentidade`.
export async function correctMatch(
  id: string,
  source: string,
  sourceId: string,
): Promise<EscritaIdentidade> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/correct`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ source, source_id: sourceId }),
    },
  )
  return lerEscritaIdentidade(res)
}

// Individual PURGE (DELETE): it removes the entity and its derivatives (media_file /
// external / override / assets via CASCADE). The only destructive action — the UI only
// fires it after an explicit confirmation from the owner. It returns the summary `{purged}`.
export async function purgeEntity(id: string): Promise<PurgeResult> {
  const res = await fetch(`${base}/api/v1/entities/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  return lerJson<PurgeResult>(res)
}

// BATCH purge: it sends the selected ids; the backend purges each one (idempotent
// for an absent id) and returns how many were actually removed.
export async function purgeBatch(ids: string[]): Promise<PurgeBatchResult> {
  const res = await fetch(`${base}/api/v1/entities/purge`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ ids }),
  })
  return lerJson<PurgeBatchResult>(res)
}

// ── Auto-match: search and automatic synchronization of the queue ────────────

// Result of the INDIVIDUAL auto-match: always `200`, even without a candidate — "did
// not find" is a valid result (`resolved: false, method: 'none'`), not an error.
// `target` is the applied candidate (local or external); `null` when it did not resolve.
export type AutoMatchResult = components['schemas']['AutoMatchResult']

// Result of the BATCH auto-match: the aggregated summary (how many resolved vs.
// pending) + the detail per item (without `target`, only what is needed for the UI).
export type AutoMatchBatchResult = components['schemas']['AutoMatchBatchResult']

// Individual AUTO-MATCH: it tries to resolve the item of the queue on its own —
// local-first, then external provider — applying only when the score matches
// the conservative threshold of the backend (`LimiarAutoMatch`). It never throws for "did not
// find a candidate": that is `{resolved: false, method: 'none'}`, a valid
// result in `200`, not a network/server error.
export async function autoMatchEntity(id: string): Promise<AutoMatchResult> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/auto-match`,
    { method: 'POST' },
  )
  return lerJson<AutoMatchResult>(res)
}

// BATCH AUTO-MATCH: it processes the given `ids` or, if omitted/empty,
// ALL the eligible ones of the review queue (the same predicate as `getReviewQueue`).
// It returns the aggregated summary + the detail per item.
export async function autoMatchBatch(
  ids?: string[],
): Promise<AutoMatchBatchResult> {
  const res = await fetch(`${base}/api/v1/review/auto-match`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(ids && ids.length > 0 ? { ids } : {}),
  })
  return lerJson<AutoMatchBatchResult>(res)
}

// ── Quick actions per item ───────────────────────────────────────────────────

// Result of POST .../enrich-missing: it mirrors `resultado` of
// `tentarEnriquecerOuSinalizar`, fired on demand for ONE item.
export type EnrichMissingResult = components['schemas']['EnrichMissingResult']

// Result of POST .../download-missing-artwork: kinds that gained a new
// asset × kinds without an option available in any provider now.
export type DownloadMissingArtworkResult =
  components['schemas']['DownloadMissingArtworkResult']

// ENRICH ON DEMAND: it fires the SAME local→external search of the
// job for this item only, outside the batch/throttle of the job (but it still stamps
// `last_external_match_attempt_at`). Always 200 — "did not find" is `outcome:
// "none"`, a valid result, not an error (same convention as the auto-match).
export async function enrichMissing(id: string): Promise<EnrichMissingResult> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/enrich-missing`,
    { method: 'POST' },
  )
  return lerJson<EnrichMissingResult>(res)
}

// DOWNLOAD MISSING ARTWORK: it reads `catalog_completeness.missing` live,
// filters the `artwork:<kind>` keys and downloads the best option available per
// kind (it reuses `selectArtwork`/`baixarEMaterializarArte` in the backend). Always
// 200 — a kind without an option in any provider becomes `unavailable`, not an error.
export async function downloadMissingArtwork(
  id: string,
): Promise<DownloadMissingArtworkResult> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/download-missing-artwork`,
    { method: 'POST' },
  )
  return lerJson<DownloadMissingArtworkResult>(res)
}

// Result of POST .../force-reconcile: it fires the SAME parent-child
// reconciliation logic of the batch job (`ReconciliarFaixaDraft`/
// `ReconciliarSerieDraft`), but for ONE item and ignoring the once-a-day throttle —
// a manual escape hatch when the owner wants immediate feedback.
export type ForceReconcileResult = components['schemas']['ForceReconcileResult']

// FORCE RECONCILIATION ON DEMAND: it re-links a track/series stuck in
// draft against the already identified parent, ignoring the
// `last_external_match_attempt_at` throttle (but stamping the attempt at the end, the same
// semantics as `enrichMissing`). Always 200 — `linked: false` is a valid
// result (no candidate found, or type not applicable), not an error.
export async function forceReconcile(
  id: string,
): Promise<ForceReconcileResult> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/force-reconcile`,
    { method: 'POST' },
  )
  return lerJson<ForceReconcileResult>(res)
}

// Result of POST .../refresh-files: an informative summary of what
// was rechecked/found/filled in this call — a loose format (feedback
// to the owner in the UI, not a strict contract for another integration to consume).
export type RefreshFilesResult = components['schemas']['RefreshFilesResult']

// Result of POST .../materialize-missing-media: it mirrors the
// return of `MaterializarMidiasFaltantes` (the same one used by the periodic
// `midias_faltantes` job).
export type MaterializeMissingMediaResult =
  components['schemas']['MaterializeMissingMediaResult']

// CHECK FILE UPDATES (unconditional item of the Gerenciar
// menu): it forces the re-hash/mediainfo of the `media_file`s of the entity even without a
// change of mtime/size, looks for a new/updated `origin='local'` asset in the
// folder(s) and fills in the database the local playback fields that are still empty
// from the `.nfo` (when present). No body. 404 if the entity does not exist.
export async function refreshEntityFiles(
  id: string,
): Promise<RefreshFilesResult> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/refresh-files`,
    { method: 'POST' },
  )
  return lerJson<RefreshFilesResult>(res)
}

// DOWNLOAD MISSING ARTWORK (unconditional item of the Gerenciar menu,
// different from `downloadMissingArtwork`/"Baixar arte faltando" which only appears
// conditioned on `catalog_completeness`): it forces a new query to the registered
// `ArteProvider`(s) (today: fanart.tv) to update the artwork inventory of the entity and,
// next, materializes into the real folder whatever is missing. No body. 404
// if the entity does not exist.
export async function materializeMissingMedia(
  id: string,
): Promise<MaterializeMissingMediaResult> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/materialize-missing-media`,
    { method: 'POST' },
  )
  return lerJson<MaterializeMissingMediaResult>(res)
}

// Result of POST .../audio-tags/write: an informative summary
// (loose format, not a strict contract) — how many tracks had their tag
// rewritten successfully and one entry per failure ("faixaID: motivo"). Partial
// success is normal in an album cascade.
export type AudioTagsWriteResult = components['schemas']['AudioTagsWriteResult']

// WRITE METADATA INTO THE AUDIO TAG (Gerenciar menu item
// for `faixa`/`album`): it rewrites the tag of the audio file(s) (ID3v2/
// Vorbis comment) with the ALREADY RESOLVED metadata (title/artist/album/year/
// track-disc/genre/MBIDs/ISRC/cover) — never read back into the database.
// An `album` `id` cascades to all the owned tracks, best-effort. No
// body. 404 if the entity does not exist; 422 if the `type` does not support it (e.g.
// `artista`); 503 if the writing mechanism is not available.
export async function writeAudioTags(
  id: string,
): Promise<AudioTagsWriteResult> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/audio-tags/write`,
    { method: 'POST' },
  )
  return lerJson<AudioTagsWriteResult>(res)
}

// ── NFO in the real folder ───────────────────────────────────────────────────

// (RE)GENERATES and writes the `.nfo` of the entity into the real library folder, from the
// SAME already resolved metadata (synopsis, cast, tagline, certification, genres,
// external ids). Idempotent: it always overwrites completely. An optional `lang`
// (language of the translatable fields; omitted falls back to the primary). 404 (`ApiError`)
// when the entity has no present `media_file` (no destination folder) —
// the UI then disables the button instead of letting the owner hit that error.
export async function writeEntityNfo(
  id: string,
  lang?: string,
): Promise<NfoWriteResult> {
  const qs = lang ? `?lang=${encodeURIComponent(lang)}` : ''
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/nfo${qs}`,
    { method: 'POST' },
  )
  return lerJson<NfoWriteResult>(res)
}

// ── Consumption history ──────────────────────────────────────────────────────

// Lists the history events of the entity, most recent first (occurred_at
// DESC, already sorted by the backend). `kind` filters (e.g. "assistir"); absent brings
// every kind already seen — agnostic of vertical (movie/series today, music/
// book/game in the future reuse the same endpoint).
export async function listHistoricoEventos(
  id: string,
  kind?: string,
): Promise<HistoricoEventoList> {
  const qs = kind ? `?kind=${encodeURIComponent(kind)}` : ''
  return lerJson<HistoricoEventoList>(
    await fetch(
      `${base}/api/v1/entities/${encodeURIComponent(id)}/historico${qs}`,
    ),
  )
}

// CURRENT state derived from the history, per kind (projection of the most recent event
// of each kind — without `id`/`payload`). An empty object when the entity never had any
// event. It feeds the "Assistido" badge of the card/hero (kind="assistir").
export async function getHistoricoEstado(id: string): Promise<HistoricoEstado> {
  return lerJson<HistoricoEstado>(
    await fetch(
      `${base}/api/v1/entities/${encodeURIComponent(id)}/historico/estado`,
    ),
  )
}

// Registers a history event (manual, by the owner, or reused by a
// future automatic source beyond the Jellyfin webhook). An absent `source`
// becomes "manual" on the server; an absent `occurred_at` becomes the server timestamp.
export async function createHistoricoEvento(
  id: string,
  body: CreateHistoricoEvento,
): Promise<HistoricoEvento> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/historico`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
  return lerJson<HistoricoEvento>(res)
}

// Deletes ONE event of the timeline (204 without a body). Used by the "delete" action of each
// row of the timeline and by "marcar como não assistido" (it deletes the most recent
// `completed` event).
export async function deleteHistoricoEvento(
  id: string,
  eventoId: string,
): Promise<void> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/historico/${encodeURIComponent(
      eventoId,
    )}`,
    { method: 'DELETE' },
  )
  if (!res.ok) {
    throw await erroDaResposta(res)
  }
}

// GLOBAL list (outside /entities/{id}) of events without a linked entity
// — webhook events whose raw external ids did not match
// any local entity at the moment they arrived. They disappear from the list when the
// backend links them through retroactive backfill (correction/confirmation of a match that
// makes the entity gain the same external ID). Ordered by `occurred_at` DESC.
export interface HistoricoOrfaoListParams {
  limit?: number
  offset?: number
}

export async function listHistoricoOrfaos(
  params: HistoricoOrfaoListParams = {},
): Promise<HistoricoEventoOrfaoList> {
  const qs = new URLSearchParams()
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit))
  }
  if (params.offset !== undefined) {
    qs.set('offset', String(params.offset))
  }
  const query = qs.toString()
  return lerJson<HistoricoEventoOrfaoList>(
    await fetch(`${base}/api/v1/historico/orfaos${query ? `?${query}` : ''}`),
  )
}

// ── Candidate preview + supervised confirmation ──────────────────────────────

// The candidate shown by the preview (local OR external) — poster/title/year/source.
export type MatchPreviewTarget = components['schemas']['MatchPreviewTarget']

// Response of GET .../match-preview: always 200, even without a candidate
// (`found: false` is a valid result, not an error) — the same convention as the auto-match.
// It reuses the SAME search/scoring, but WITHOUT the cut by `LimiarAutoMatch`:
// it returns the best candidate even below 92, so the owner can decide looking at the
// whole card (poster/title/year/score/source) before any click.
export type MatchPreview = components['schemas']['MatchPreview']

// Body of POST .../confirm-candidate: `local` (merges with `local_id`) or
// `external` (writes the identity and re-enriches, `source`+`source_id`).
export type ConfirmCandidateBody = components['schemas']['ConfirmCandidateBody']

// Candidate PREVIEW, a dry run WITHOUT side effect: it searches for the best
// candidate (local-first, then external) and returns score/source without applying
// anything to the database. Called on the mount of each `ReviewCard` — it never throws for "did not
// find": that is `{found: false, method: 'none'}`, a valid result in `200`.
export async function getMatchPreview(id: string): Promise<MatchPreview> {
  return lerJson<MatchPreview>(
    await fetch(
      `${base}/api/v1/entities/${encodeURIComponent(id)}/match-preview`,
    ),
  )
}

// Applies the candidate that the preview showed — WITHOUT checking `LimiarAutoMatch`
// (a human decision informed by the card, unlike the conservative cut of the
// unattended auto-match). It returns the resolved entity, like `confirmMatch`/
// `correctMatch`. Errors (422 incomplete body, 404, 409 identity collision,
// 502/503 source) become an `ApiError` with the pt-BR message of the envelope.
export async function confirmCandidate(
  id: string,
  body: ConfirmCandidateBody,
): Promise<ResolvedEntity> {
  const res = await fetch(
    `${base}/api/v1/entities/${encodeURIComponent(id)}/confirm-candidate`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
  return lerJson<ResolvedEntity>(res)
}

// ── Jobs (foundation of the job system) ──────────────────────────────────────
//
// Types generated from `api-types.ts` (schema `Job`, not `JobDefinition` — the name of the
// schema in `openapi.yaml`), like every other type in this file. Re-exported
// with the name `JobDefinition` here only so the name is more descriptive in the
// front; the shape is exactly that of `components['schemas']['Job']` (without
// `config_json`/`created_at`/`updated_at` — those stay only in the backend).
export type JobDefinition = components['schemas']['Job']
// Progresso ao vivo de uma execução de job (ADR-0040): etapa, item atual,
// contadores e tempo decorrido. `null` quando o job nunca rodou.
export type JobProgress = components['schemas']['JobProgress']

// A fila de execução (ADR-0040): scan e job no mesmo recurso.
export type QueueItem = components['schemas']['QueueItem']
export type QueueView = components['schemas']['QueueView']
export type QueueEvent = components['schemas']['QueueEvent']
export type QueueItemDetail = components['schemas']['QueueItemDetail']
type JobList = components['schemas']['JobList']

export type JobExecution = components['schemas']['JobExecution']
type JobExecutionList = components['schemas']['JobExecutionList']

export type PatchJobBody = components['schemas']['PatchJob']
export type RunJobResponse = components['schemas']['RunJob']

export interface JobExecutionsParams {
  limit?: number
  before?: string
}

// Lists all the `job_definition` with the current state (including `running`, from the
// in-memory Manager). Never empty after the foundation — there is always the
// `backup_vacuum` seed.
// The WHOLE queue in one request (ADR-0040): the running item (with step,
// current item, counters and elapsed time), the FIFO of waiting items (with
// position and wait time) and the most recently finished ones (with duration).
// `recent` limits how many finished items come back (default on the server: 20).
export async function getQueue(recent?: number): Promise<QueueView> {
  const query = recent === undefined ? '' : `?recent=${recent}`
  return lerJson<QueueView>(await fetch(`${base}/api/v1/queue${query}`))
}

// ONE queue item plus its trail: the per-item rows ("what each processed thing
// did") that survive the run. 404 (unknown id) becomes an `ApiError`.
export async function getQueueItem(
  id: string,
  events?: number,
): Promise<QueueItemDetail> {
  const query = events === undefined ? '' : `?events=${events}`
  return lerJson<QueueItemDetail>(
    await fetch(`${base}/api/v1/queue/${id}${query}`),
  )
}

export async function listJobs(): Promise<JobDefinition[]> {
  const data = await lerJson<JobList>(await fetch(`${base}/api/v1/jobs`))
  return data.items
}

// Edits `enabled` and/or `schedule_cron` (partial patch — omitting a field does not
// change it; `schedule_cron: null` turns the schedule off, leaving the job manual-only).
// 404 (unknown id) and 422 (`schedule_cron` that does not parse) become an `ApiError`
// with the pt-BR message of the envelope — the UI shows the 422 inline next to the field.
export async function patchJob(
  id: string,
  body: PatchJobBody,
): Promise<JobDefinition> {
  const res = await fetch(`${base}/api/v1/jobs/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  return lerJson<JobDefinition>(res)
}

// Fires a manual run (`trigger=manual`), independent of `enabled`.
// `202` with the id of the newly created run; 404 (unknown id), 409 (already
// running) and 503 (no registered `Handler`) become an `ApiError`.
export async function runJob(id: string): Promise<RunJobResponse> {
  const res = await fetch(`${base}/api/v1/jobs/${encodeURIComponent(id)}/run`, {
    method: 'POST',
  })
  return lerJson<RunJobResponse>(res)
}

// Paginated history of runs, most recent first. `limit` defaults to 20 in the
// backend (max 100); `before` (a `started_at`) pages backwards. 404 (unknown id)
// becomes an `ApiError`.
export async function listJobExecutions(
  id: string,
  params: JobExecutionsParams = {},
): Promise<JobExecution[]> {
  const qs = new URLSearchParams()
  if (params.limit !== undefined) {
    qs.set('limit', String(params.limit))
  }
  if (params.before) {
    qs.set('before', params.before)
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  const data = await lerJson<JobExecutionList>(
    await fetch(
      `${base}/api/v1/jobs/${encodeURIComponent(id)}/executions${suffix}`,
    ),
  )
  return data.items
}
