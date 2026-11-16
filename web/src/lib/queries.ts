import { queryOptions } from '@tanstack/react-query'
import {
  browseLibraryPath,
  getCollectionDetail,
  getEntity,
  getEntityEpisodes,
  getEntityFields,
  getEntityFiles,
  getEntityMembers,
  getArtwork,
  getExternalAlbum,
  getExternalDetail,
  getCatalogCompletenessSettings,
  getEntityThemeCandidato,
  getHealth,
  getHistoricoEstado,
  getSystemHealth,
  hasEntityTheme,
  hasEntityTrailer,
  listHistoricoEventos,
  listHistoricoOrfaos,
  getIntegration,
  getJellyfinWebhookToken,
  getLanguageSettings,
  getRetroactiveEnrichmentSettings,
  getAutoMaterializeArtworkSettings,
  getAutoDownloadTrailerTemaSettings,
  getYtdlpSettings,
  getMatchPreview,
  listJobExecutions,
  listJobs,
  getOrphans,
  getOverrideBlob,
  getPerson,
  getReviewQueue,
  getScanStatus,
  listAssets,
  listEntities,
  listEntityTypes,
  listIntegrations,
  listLibrary,
  searchMedia,
  type EntityListParams,
  type HistoricoOrfaoListParams,
  type JobExecutionsParams,
} from '@/lib/api'

// Options shared per route. Centralizing them in `queryOptions` keeps the
// queryKey and the queryFn cohesive and reusable (component, router loader,
// prefetch, invalidation). The keys follow the resource: `['library']`,
// `['entities', params]`, `['entity', id]`, `['scan', id]`.
export const healthQueryOptions = queryOptions({
  queryKey: ['health'],
  queryFn: getHealth,
})

// System telemetry: its own key `['health', 'system']` — UNDER the
// same prefix as `healthQueryOptions` (it is the same screen/domain), but with an
// independent `queryFn`: a failure here must not mark `['health']` as an
// error (they are two distinct `useQuery`s on the screen, see health.tsx).
export const systemHealthQueryOptions = queryOptions({
  queryKey: ['health', 'system'],
  queryFn: getSystemHealth,
})

export const libraryQueryOptions = queryOptions({
  queryKey: ['library'],
  queryFn: listLibrary,
})

// Folder browser: the key carries the navigated path — each
// folder visited caches under its own key (going back to a folder already seen in the
// session does not fetch again). Used by the `PathBrowser`.
export function libraryBrowseQueryOptions(path: string) {
  return queryOptions({
    queryKey: ['library', 'browse', path],
    queryFn: () => browseLibraryPath(path),
    // An invalid path (422) is a permanent error — retrying does not change the result
    // without changing `path`. A retry here only generates 3 identical calls to the server.
    retry: false,
  })
}

// `lang` enters the queryKey: switching the reading language is a cache miss = re-fetch
// by `?lang=`, without reloading the page. An omitted `lang` uses `null` in the key
// (stable key; equivalent to "no language" = the backend primary).
export function entitiesQueryOptions(params: EntityListParams, lang?: string) {
  return queryOptions({
    queryKey: ['entities', params, lang ?? null],
    queryFn: () => listEntities(params, lang),
  })
}

// Types of entity with cataloged data. Their own key
// `['entities', 'types']` — UNDER the `['entities']` prefix (same domain as
// `entitiesQueryOptions`), but with an independent `queryFn` and without `params`
// (the endpoint accepts no filter). Consumed by the home (one `RecentlyAddedRail`
// per type) and by the navbar (library dropdown) — a named
// export on purpose so the navbar reuses it.
export const entityTypesQueryOptions = queryOptions({
  queryKey: ['entities', 'types'],
  queryFn: listEntityTypes,
})

export function entityQueryOptions(id: string, lang?: string) {
  return queryOptions({
    queryKey: ['entity', id, lang ?? null],
    queryFn: () => getEntity(id, lang),
  })
}

// Members of a collection. `lang` enters the key: switching the language re-fetches the
// titles of the members. The key is under `['entity', id, 'members', ...]` so that the invalidation
// per entity reaches it.
export function entityMembersQueryOptions(id: string, lang?: string) {
  return queryOptions({
    queryKey: ['entity', id, 'members', lang ?? null],
    queryFn: () => getEntityMembers(id, lang),
  })
}

// Episodes of the series grouped by display order. The key carries
// `(id, ordering, lang)` under the prefix `['entity', id]`: switching the order (selector) is a
// cache miss = re-fetch; the language re-resolves the titles. It is only queried when the
// chosen order differs from the default (the default view uses the season cards).
export function entityEpisodesQueryOptions(
  id: string,
  ordering: string,
  lang?: string,
) {
  return queryOptions({
    queryKey: ['entity', id, 'episodes', ordering, lang ?? null],
    queryFn: () => getEntityEpisodes(id, ordering, lang),
  })
}

// Descriptors of the editorial form. IMMUTABLE metadata between deploys
// (it derives from a static registry of the backend), so `staleTime: Infinity`
// avoids re-fetches — the three CRUD routes share this cache. Its own key
// `['schema', 'entity-fields']`, outside the per-entity prefix (it does not depend on an id).
export const entityFieldsQueryOptions = queryOptions({
  queryKey: ['schema', 'entity-fields'],
  queryFn: getEntityFields,
  staleTime: Infinity,
})

// Language config: single source for the selector and the admin. Key
// `['settings', 'languages']` — invalidated by the PUT of the settings screen.
export const languageSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'languages'],
  queryFn: getLanguageSettings,
})

// Config of the retroactive enrichment throttle: single source for the
// admin (settings.tsx) and the placeholder of the per-library override
// (library-new.tsx/library-edit.tsx). Key `['settings', 'enrichment-retroativo']`
// — invalidated by the PUT of the settings screen.
export const retroactiveEnrichmentSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'enrichment-retroativo'],
  queryFn: getRetroactiveEnrichmentSettings,
})

// Config of the automatic artwork materialization toggle: single source
// for the artwork card of the settings screen. Key
// `['settings', 'auto-materialize-artwork']` — invalidated by the PUT of the switch.
export const autoMaterializeArtworkSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'auto-materialize-artwork'],
  queryFn: getAutoMaterializeArtworkSettings,
})

// Config of the automatic trailer/theme download during the scan:
// single source for the "Trailer e tema musical" card of the settings screen.
// Key `['settings', 'auto-download-trailer-tema']` — invalidated by the PUT
// of the switch.
export const autoDownloadTrailerTemaSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'auto-download-trailer-tema'],
  queryFn: getAutoDownloadTrailerTemaSettings,
})

// Status of the yt-dlp binary + custom path (ADR-0025): single source
// for the "yt-dlp" card of the settings screen. Key `['settings', 'ytdlp']`
// — invalidated by the PUT of the form.
export const ytdlpSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'ytdlp'],
  queryFn: getYtdlpSettings,
})

// Token of the Jellyfin inbound webhook: single source for the screen
// `/dashboard/webhook`. Key `['webhooks', 'jellyfin', 'token']` — invalidated
// after generating/regenerating the token.
export const jellyfinWebhookTokenQueryOptions = queryOptions({
  queryKey: ['webhooks', 'jellyfin', 'token'],
  queryFn: getJellyfinWebhookToken,
})

// Integrations configurable through the UI/database (ADR-0023):
// single source for `/dashboard/integrations` (list, `configurada` badge) and the
// form per slug below. Key `['integrations']` — invalidated by the PUT
// of the form (the badge of the list has to reflect it right away).
export const integrationsQueryOptions = queryOptions({
  queryKey: ['integrations'],
  queryFn: listIntegrations,
})

// Schema + effective value of the fields of ONE integration. Key
// `['integrations', slug]` — invalidated by the PUT itself (the echo after the write
// is not used directly so as not to rehydrate a `secret` field; the screen remounts the
// form from the refetch, see `IntegrationDetailScreen`).
export function integrationDetailQueryOptions(slug: string) {
  return queryOptions({
    queryKey: ['integrations', slug],
    queryFn: () => getIntegration(slug),
  })
}

// Config of CATALOGING completeness: single source for the admin
// form. Key `['settings', 'catalog-completeness']` — invalidated by the PUT of the
// settings screen. A concept DIFFERENT from OWNERSHIP completeness
// (`entity-badges.ts`) — it shares no key/cache with that one.
export const catalogCompletenessSettingsQueryOptions = queryOptions({
  queryKey: ['settings', 'catalog-completeness'],
  queryFn: getCatalogCompletenessSettings,
})

export function entityFilesQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'files'],
    queryFn: () => getEntityFiles(id),
  })
}

// RAW blob of the override. The key `['entity', id, 'override']` sits UNDER the
// prefix `['entity', id]` — so an invalidation by `['entity', id]` after the
// edit drops at once the entity (all the `lang` variations), the files
// and this blob, without enumerating languages. It does not carry `lang`: the blob is the same for
// all of them (the UI reads the slice of the current language from it).
export function overrideBlobQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'override'],
    queryFn: () => getOverrideBlob(id),
  })
}

// Artwork gallery. Its own key `['assets', id]` (not under `['entity',
// id]`): changing the artwork invalidates THIS key AND `['entity', id]` (the poster of the
// detail/grid reflects the new precedence) AND `['entities']` (the grid) —
// independent invalidations, without dropping one through the other.
export function assetsQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['assets', id],
    queryFn: () => listAssets(id),
  })
}

// Local theme song: it checks (HEAD, once per load) whether the
// entity has a theme.{mp3,ogg,flac} in the root folder — without its own invalidation
// (the file only changes through a manual action of the owner on the disk, outside the UI).
export function entityThemeQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'theme'],
    queryFn: () => hasEntityTheme(id),
  })
}

// Theme song candidate from ThemerrDB: the single
// source of the "Baixar tema" button. Its own key `['entity', id, 'theme',
// 'candidato']` (under the prefix `['entity', id]`, but distinct from ['entity',
// id, 'theme'] above, which is the HEAD of the local binary) — invalidated
// explicitly by the download mutation (the only UI action that changes
// `ja_tem_local`; the candidate itself only changes on the next
// enrichment cycle, outside the UI).
export function entityThemeCandidatoQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'theme', 'candidato'],
    queryFn: () => getEntityThemeCandidato(id),
  })
}

// Trailer materialized on disk: it checks (HEAD,
// once per load) whether the entity already has a downloaded trailer — invalidated
// explicitly by the download mutation (the only UI action that changes that
// state; outside it, the file only changes through a manual action of the owner on the disk).
export function entityTrailerQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['entity', id, 'trailer'],
    queryFn: () => hasEntityTrailer(id),
  })
}

// Gallery grouped by kind + availability badges. Its own key
// `['artwork', id]` — consumed TWICE (the row of badges on the top card and the
// gallery in rows), which TanStack Query dedupes into a single round trip. Changing the artwork
// (prefer/download/remove) invalidates THIS key (the gallery/badges reflect it), AND
// `['entity', id]` + `['entities']` (the poster of the detail and of the grid follow the new
// precedence), like the asset gallery.
export function artworkQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['artwork', id],
    queryFn: () => getArtwork(id),
  })
}

// Consumption history. Raw events, most
// recent first (`kind` optional filters; absent brings all). Its own key
// `['historico', id, 'eventos', kind]` — UNDER the prefix `['historico', id]`, so that
// an invalidation by `['historico', id]` (after creating/deleting an event) drops
// at once this key AND `historicoEstadoQueryOptions` below, without enumerating
// `kind`s nor duplicating the invalidation call.
export function historicoEventosQueryOptions(id: string, kind?: string) {
  return queryOptions({
    queryKey: ['historico', id, 'eventos', kind ?? null],
    queryFn: () => listHistoricoEventos(id, kind),
  })
}

// Current state derived from the history, per kind — it feeds the
// "Assistido" badge of the hero (kind="assistir"). Key `['historico', id, 'estado']`,
// the same prefix `['historico', id]` as the event query above (same
// invalidation after creating/deleting).
export function historicoEstadoQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['historico', id, 'estado'],
    queryFn: () => getHistoricoEstado(id),
  })
}

// GLOBAL list of orphan events, screen `/dashboard/historico-
// orfaos`. Its own key `['historico', 'orfaos', params]` — OUTSIDE the prefix
// `['historico', id]` above (it is a global list, not of one entity), so the
// per-entity invalidation does not need to (nor should) touch it.
export function historicoOrfaosQueryOptions(params: HistoricoOrfaoListParams) {
  return queryOptions({
    queryKey: ['historico', 'orfaos', params],
    queryFn: () => listHistoricoOrfaos(params),
  })
}

// Unified search. The key carries term+type+language to cache per
// query: the same search in different languages has distinct external results.
// `enabled: q !== ''` prevents it firing while the input is empty (initial/
// instruction state) — the component already debounces the term before it gets here.
export function searchQueryOptions(
  q: string,
  type?: string,
  lang?: string,
  include?: string[],
) {
  // `include` enters the key SORTED so that the cache is stable (same selection of
  // sub-nodes = same key, regardless of the order in which the toggles were turned on).
  const inc = include && include.length > 0 ? [...include].sort() : null
  return queryOptions({
    queryKey: ['search', q, type ?? null, lang ?? null, inc],
    queryFn: () => searchMedia(q, type, lang, include),
    enabled: q !== '',
  })
}

// Rich external detail. The key carries `(source, type, id, lang)`:
// the language enters the key because the SAME screen in different languages is different
// content (the same lesson as the search) — switching the language is a cache miss = re-fetch by
// `?lang=`. Each external item visited (including similars/collection navigated)
// caches under its own key. `lang ?? null` keeps the key stable.
export function externalDetailQueryOptions(
  source: string,
  type: string,
  id: string,
  lang?: string,
) {
  return queryOptions({
    queryKey: ['external', source, type, id, lang ?? null],
    queryFn: () => getExternalDetail(source, type, id, lang),
  })
}

// External collection as a page. The key carries `(source, id, lang)` — the
// language enters the key (the same lesson as the external detail): the same collection in different
// languages is different content. `lang ?? null` keeps the key stable.
export function collectionDetailQueryOptions(
  source: string,
  id: string,
  lang?: string,
) {
  return queryOptions({
    queryKey: ['collection', source, id, lang ?? null],
    queryFn: () => getCollectionDetail(source, id, lang),
  })
}

// External album as a page (ADR-0029): official tracks of the release
// + internal_id of the already cataloged ones ("faixas faltando"). Same key lesson as
// collectionDetailQueryOptions above: `(source, id, lang)`, language in the key.
export function externalAlbumQueryOptions(
  source: string,
  id: string,
  lang?: string,
) {
  return queryOptions({
    queryKey: ['external-album', source, id, lang ?? null],
    queryFn: () => getExternalAlbum(source, id, lang),
  })
}

// Deduplicated person + filmography (phase D). Its own key `['person', id]` —
// read-only in this phase, without invalidation (the person is not editable through the UI).
export function personQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['person', id],
    queryFn: () => getPerson(id),
  })
}

// Review queue. Its own key `['review']` — invalidated after confirming
// or correcting a match (together with `['entities']` and `['entity', id]`, which also
// change state). A lean query (array of cards), without params.
export const reviewQueueQueryOptions = queryOptions({
  queryKey: ['review'],
  queryFn: getReviewQueue,
})

// Orphan queue. Key `['orphans']` — invalidated after purging (individual
// or batch), together with `['entities']` (the card also disappears from the grid).
export const orphansQueryOptions = queryOptions({
  queryKey: ['orphans'],
  queryFn: getOrphans,
})

// Candidate preview per card of the review queue. Key `['match-preview',
// id]` — one `useQuery` per `ReviewCard` on mount (not lazy; see §6, one
// request per card is acceptable for the volume of the queue). Without its own invalidation: the
// card disappears from the queue (and the query with it) as soon as `confirm`/`confirm-candidate`
// resolves, via `invalidarRevisao` of ['review'].
export function matchPreviewQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['match-preview', id],
    queryFn: () => getMatchPreview(id),
  })
}

// Scan status with POLLING: while `running`, TanStack Query re-fetches
// every 1.5s; when it finishes, `refetchInterval` returns `false` and the polling stops
// on its own (session decision — §6: polling, not websocket).
export function scanStatusQueryOptions(id: string) {
  return queryOptions({
    queryKey: ['scan', id],
    queryFn: () => getScanStatus(id),
    refetchInterval: (query) => (query.state.data?.running ? 1500 : false),
  })
}

// Jobs (foundation). Its own key `['jobs']` — invalidated after PATCH
// (enable/schedule) and after firing `run`. Light POLLING while ANY job of the
// list is `running` (the card in "rodando…" disappears on its own without the owner reloading);
// it stops when none is, avoiding polling forever (same spirit as the scan).
export const jobsQueryOptions = queryOptions({
  queryKey: ['jobs'],
  queryFn: listJobs,
  refetchInterval: (query) =>
    query.state.data?.some((job) => job.running) ? 4000 : false,
})

// Paginated history of the executions of ONE job. Key under `['job', id,
// 'executions', ...]` — invalidated after firing `run` of that job (a new
// `running` row appears at the top). `limit`/`before` enter the key to cache
// per page.
export function jobExecutionsQueryOptions(
  id: string,
  params: JobExecutionsParams = {},
) {
  return queryOptions({
    queryKey: [
      'job',
      id,
      'executions',
      params.limit ?? null,
      params.before ?? null,
    ],
    queryFn: () => listJobExecutions(id, params),
  })
}
