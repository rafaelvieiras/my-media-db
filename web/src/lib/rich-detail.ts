import {
  arteSrc,
  posterSrc,
  type CreditCast,
  type CreditCompany,
  type CreditCrew,
  type ExternalCollection,
  type ExternalDetail,
  type ExternalSimilar,
  type ExternalVideo,
  type ResolvedEntity,
} from '@/lib/api'

// NORMALIZED model of the shared rich screen (RichDetailView) and the adapters of the
// two sources. It lives outside the component file so it does not trigger
// `react-refresh/only-export-components` (the component module only exports
// components). The external detail and the resolved entity
// serve the SAME rich shape in the backend; here each DTO becomes a `RichDetail`.

// RichDetail is the shape that RichDetailView consumes. The translatable fields already come
// resolved in the active language (the backend resolves it). Image URLs already come READY
// for the `<img>` (the adapter applies the local proxy to the poster of the entity; the external one uses
// the direct URL of the source).
export interface RichDetail {
  type: string
  title: string
  originalTitle: string | null
  description: string | null
  tagline: string | null
  year: string | null
  runtime: number | null
  certification: string | null
  // Display SITUATION of a SERIES: RAW string from the source (TMDB "Returning
  // Series"|"Ended"|… or TVDB status.name), exposed verbatim — the UI translates the
  // label into pt-BR via `rotuloSituacao`. Absent/undefined for a movie and for a TVDB-only series without
  // a match; in that case the situation badge does not appear.
  airingStatus?: string | null
  voteAverage: number | null
  // LOCAL rating of the owner (ADR-0012): the owner's own score, stored in the override, which
  // complements/overrides the rating of the source. null when not rated (or on the
  // external screen, which has no local rating — the UI then shows only the rating of the source).
  localRating: number | null
  originalLanguage: string | null
  posterUrl: string | null
  backdropUrl: string | null
  // 16:9 still of the EPISODE, already ready for the `<img>` (via the image proxy). null
  // for non-episodes and for the external detail (which has no still) — the UI only
  // renders the still frame when the type is `episode`.
  stillUrl: string | null
  genres: string[]
  keywords: string[]
  videos: ExternalVideo[]
  collection: ExternalCollection | null
  similar: ExternalSimilar[]
  // Credits (phase B) — same shape on both screens (external × entity parity).
  // `cast`/`crew`/`companies` arrive with the photos/logos ALREADY proxied by the backend
  // (direct use in the `<img>`, without re-proxying). Lists are always `[]` when absent — the
  // respective section only appears when non-empty. In a series, `episode_count` comes
  // filled in; in a movie it is null (the UI omits the "· N eps").
  cast: CreditCast[]
  crew: CreditCrew[]
  companies: CreditCompany[]
  externalLinks: Record<string, string>
  // Ratings per SOURCE: unlike `voteAverage` (the rating merged by
  // priority, a single value, kept for compatibility with whoever still reads it), this is
  // ONE entry per source that models `vote_average` — the ratings row of the
  // hero shows them side by side. Always `[]` when empty (never undefined).
  ratings: { source: string; value: number }[]
}

// deExternalDetail normalizes the EXTERNAL detail DTO. The poster/backdrop of the hero may
// come as a LOCAL PROXY ROUTE (relative, cached) or as a DIRECT URL of the source
// (degradation): `srcDeImagem` resolves both (relative receives the base, absolute passes).
export function deExternalDetail(d: ExternalDetail): RichDetail {
  return {
    type: d.type,
    title: d.title,
    originalTitle: d.original_title,
    description: d.description,
    tagline: d.tagline,
    year: d.year,
    runtime: d.runtime,
    certification: d.certification,
    airingStatus: d.airing_status ?? null,
    voteAverage: d.vote_average,
    // The external detail has no local rating (it is not an item of the owner yet).
    localRating: null,
    originalLanguage: d.original_language,
    posterUrl: srcDeImagem(d.poster_url),
    backdropUrl: srcDeImagem(d.backdrop_url),
    // The external detail has no episode still.
    stillUrl: null,
    genres: d.genres,
    keywords: d.keywords,
    videos: d.videos,
    collection: d.collection,
    similar: d.similar,
    // The credits already come with the URLs proxied by the backend — they pass straight through.
    cast: d.cast ?? [],
    crew: d.crew ?? [],
    companies: d.companies ?? [],
    externalLinks: d.external_links,
    // Synthesizes 1 entry — the source being viewed itself (`d.source`, which already exists
    // in the DTO). The external one has no "crossed sources" before importing; the
    // ratings row shows only that, but it does show it (owner's decision: the row
    // appears even with a single source).
    ratings:
      d.vote_average != null
        ? [{ source: d.source, value: d.vote_average }]
        : [],
  }
}

// deResolvedEntity normalizes the resolved ENTITY. The GENRES are the `tags` (which already
// carry them). The POSTER is the cached LOCAL route (via `posterSrc`); the BACKDROP comes from the
// external layer as a direct URL (it is not cached locally in this phase).
export function deResolvedEntity(e: ResolvedEntity): RichDetail {
  return {
    type: e.type,
    title: e.title,
    originalTitle: e.original_title,
    description: e.description,
    tagline: e.tagline,
    year: anoDeData(e.release_date),
    runtime: e.runtime,
    certification: e.certification,
    airingStatus: e.airing_status ?? null,
    voteAverage: e.vote_average,
    localRating: e.local_rating,
    originalLanguage: e.original_language,
    // `arteSrc` resolves the three in a single path: a local route (servable asset)
    // receives the base; the raw URL of the source goes through the image proxy.
    posterUrl: arteSrc(e.poster_url),
    // Backdrop: LOCAL ROUTE when cached (own/inherited fanart) or
    // RAW URL of the source in the fallback — the raw one now goes through the proxy (it does not hit the CDN).
    backdropUrl: arteSrc(e.backdrop_url),
    // 16:9 still of the episode: LOCAL ROUTE when there is a CUSTOM still from the owner
    // (/assets/still) or RAW URL from the provider (via proxy). null for
    // non-episodes — the front only shows it in the episode detail.
    stillUrl: arteSrc(e.still_url),
    genres: e.tags,
    keywords: e.keywords,
    videos: e.videos,
    collection: e.collection,
    similar: e.similar,
    // Same shape as the external one (parity): photos/logos already proxied by the backend.
    cast: e.cast ?? [],
    crew: e.crew ?? [],
    companies: e.companies ?? [],
    externalLinks: e.external_links,
    // Comes ready from the backend (`ratingsPorFonte`) — always
    // `[]` when no external layer models `vote_average`.
    ratings: e.ratings,
  }
}

// anoDeData extracts the year (first 4 digits) of an ISO date, or null.
function anoDeData(data: string | null): string | null {
  return data && data.length >= 4 ? data.slice(0, 4) : null
}

// srcDeImagem resolves an image URL that may be ABSOLUTE (direct URL of the
// source, e.g. TMDB) or a RELATIVE LOCAL ROUTE (image cache / proxy):
// the absolute one passes through; the relative one receives the base via `posterSrc` (it respects
// `VITE_API_BASE`). `null`/empty → null. Exported so the backdrop of other screens
// (e.g. the collection page) resolve the artwork the same way.
export function srcDeImagem(url: string | null): string | null {
  if (!url) {
    return null
  }
  return /^https?:\/\//.test(url) ? url : posterSrc(url)
}

// ── Series situation (airing_status) ─────────────────────────────────────────

// The situation arrives RAW from the source: the backend does not translate it. The pt-BR label and the
// color of the badge are a UI decision. `variant` reuses the variants of the design system of
// `<Badge>`: `default` (highlight, primary) for the "live" states (airing/
// production/premiere) and `secondary` (neutral) for the "closed" ones (ended/canceled/
// planned) — the same tone that the certification already uses.
export type VarianteSituacao = 'default' | 'secondary'

export interface RotuloSituacao {
  label: string
  variant: VarianteSituacao
}

// Covers BOTH vocabularies (the value may come from either source): TMDB ("Returning
// Series"/"Ended"/"Canceled"/"In Production"/"Planned"/"Pilot") and TVDB ("Continuing"/
// "Ended"/"Upcoming"/"Canceled"). Keys normalized (lowercase) to match
// case variations and the British "Cancelled".
const SITUACAO_LABELS: Record<string, RotuloSituacao> = {
  // TMDB
  'returning series': { label: 'Em exibição', variant: 'default' },
  ended: { label: 'Encerrada', variant: 'secondary' },
  canceled: { label: 'Cancelada', variant: 'secondary' },
  cancelled: { label: 'Cancelada', variant: 'secondary' },
  'in production': { label: 'Em produção', variant: 'default' },
  planned: { label: 'Planejada', variant: 'secondary' },
  pilot: { label: 'Piloto', variant: 'secondary' },
  // TVDB
  continuing: { label: 'Em exibição', variant: 'default' },
  upcoming: { label: 'Em breve', variant: 'default' },
}

// rotuloSituacao translates the raw situation into the pt-BR label + badge variant. An ABSENT/
// empty value → null (the UI does not render the badge — that is the case of a movie and of a TVDB-only series
// without a match). An UNKNOWN RAW value (outside the vocabularies) → it shows the raw
// one capitalized with a neutral color, it never disappears silently.
export function rotuloSituacao(
  status: string | null | undefined,
): RotuloSituacao | null {
  if (!status || !status.trim()) {
    return null
  }
  const conhecido = SITUACAO_LABELS[status.trim().toLowerCase()]
  if (conhecido) {
    return conhecido
  }
  const cru = status.trim()
  return {
    label: cru.charAt(0).toUpperCase() + cru.slice(1),
    variant: 'secondary',
  }
}
