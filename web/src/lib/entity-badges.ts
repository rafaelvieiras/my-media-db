// PURE helpers for the catalog card state (ownership, review, draft).
//
// Isolated from the rendering so they are testable and keep the badge semantics
// in a single place. The REVIEW badge only SIGNALS low confidence (ADR-0008);
// confirming/correcting the match is a Phase 3 flow.
import type { components } from '@/lib/api-types'

export type EntityCard = components['schemas']['EntityCard']

// Confidence threshold (0–100 scale of the scanner; see internal/scanner/match.go).
// Title+year matches at 70 (a high guess, it does not signal); title without year matches at
// 40 (low → signals review). Below the threshold, we show the alert.
export const LIMIAR_BAIXA_CONFIANCA = 70

// Unidentified item (no match): it appears as `draft` in the detail and in the card.
export function ehDraft(status: string): boolean {
  return status === 'draft'
}

// It needs review when there is a known, positive confidence below the threshold,
// and the item is NOT a draft (that one has its own label). Confidence `null` or 0 does not
// trigger the review alert (it is an absence of match, not a doubtful match).
export function precisaRevisao(card: {
  status: string
  match_confidence: number | null
}): boolean {
  if (ehDraft(card.status)) {
    return false
  }
  const c = card.match_confidence
  return c !== null && c > 0 && c < LIMIAR_BAIXA_CONFIANCA
}

// COMPLETENESS state of a season/series from the aggregated ownership:
// `completa` (it has every episode cataloged), `parcial` (it has some), `vazia`
// (it has none in the library, but there are cataloged ones) or `null` (no badge — total
// unknown/zero). It is the SINGLE rule the UI reads to choose the badge and the highlight.
export type EstadoCompletude = 'completa' | 'parcial' | 'vazia' | null

// estadoCompletude classifies the aggregated ownership of the card. total==0 → null (nothing to
// say). owned>=total (total>0) → completa. 0<owned<total → parcial. owned==0 (with
// total>0) → vazia. Absence of `ownership` (movie/episode/collection) → null.
export function estadoCompletude(
  ownership: { owned_count: number; total_count: number } | null | undefined,
): EstadoCompletude {
  if (!ownership || ownership.total_count <= 0) {
    return null
  }
  const { owned_count, total_count } = ownership
  if (owned_count >= total_count) {
    return 'completa'
  }
  if (owned_count > 0) {
    return 'parcial'
  }
  return 'vazia'
}

// temCompletude says whether the type of the card has a completeness badge (shallow ownership): only the series
// (it counts seasons) and the season (it counts episodes). movie/episode/collection never —
// the single gate the UI uses to NOT show the badge/indent on those types, even if an
// ownership leaks into the DTO.
export function temCompletude(type: string): boolean {
  return type === 'tv' || type === 'season'
}

// Unit counted by the completeness badge according to the type of the card: the `tv` card
// counts SEASONS (shallow ownership), the `season` card counts EPISODES. It is what
// changes the badge label between the home (series) and the member list (season).
// Any other type has no completeness badge — it falls back to the neutral plural "itens".
export function unidadeCompletude(type: string): string {
  if (type === 'tv') {
    return 'temporadas'
  }
  if (type === 'season') {
    return 'episódios'
  }
  return 'itens'
}

// pt-BR label of the cataloging status of the item. The backend only uses two values
// (`draft`/`cataloged` — see internal/scanner/match.go,
// internal/server/completeness.go); `draft` already has special handling
// (`ehDraft` → "não identificado"), this function covers the rest. An unknown
// value echoes raw (it never breaks the UI).
export function rotuloStatus(status: string): string {
  const mapa: Record<string, string> = {
    cataloged: 'catalogado',
  }
  return mapa[status] ?? status
}

// pt-BR label of the media type for display. It covers the canonical types: the
// top-level ones (filme/série/coleção), the nodes of the series tree (temporada/episódio)
// and those of the music vertical (artista/álbum/faixa — names in
// Portuguese in the schema, not `artist`/`album`/`track`). An unknown type echoes
// raw (it never breaks the UI).
export function rotuloTipo(type: string): string {
  const mapa: Record<string, string> = {
    movie: 'Filme',
    tv: 'Série',
    collection: 'Coleção',
    season: 'Temporada',
    episode: 'Episódio',
    album: 'Álbum',
    artista: 'Artista',
    faixa: 'Faixa',
  }
  return mapa[type] ?? type
}

// Cover frame (Tailwind class `aspect-*`) according to the type of the card:
// album and artist use a SQUARE cover (1:1 — same frame as the hero, `CapaQuadrada`
// in rich-detail-view.tsx), the other types keep the classic 2:3 poster. SINGLE
// source for any grid that renders `PosterCard` (library, carousel,
// search, discography) — avoids repeating the type check at every callsite;
// `PosterCard` only ignores it when the CALLER passes an explicit `aspectClass` (the
// episode case, which uses a 16:9 still instead of a poster). An unknown type falls back to
// 2:3 (it never breaks the UI, same idiom as `rotuloTipo`).
export function aspectPorTipo(type: string): string {
  if (type === 'album' || type === 'artista') {
    return 'aspect-square'
  }
  return 'aspect-[2/3]'
}

// PLURAL pt-BR label of the type, for section/category headers — the
// "adicionados recentemente" carousel of the home and the
// library dropdown of the navbar, which list the type as a CATEGORY, not as an
// inline badge of an item ("Filmes" above a shelf, not "Filme" as
// in `rotuloTipo`). Same source of types as `rotuloTipo` (central, no separate
// duplicate map); a type without a registered plural falls back to the singular (it never
// breaks the UI).
export function rotuloTipoPlural(type: string): string {
  const mapa: Record<string, string> = {
    movie: 'Filmes',
    tv: 'Séries',
    collection: 'Coleções',
    season: 'Temporadas',
    episode: 'Episódios',
    album: 'Álbuns',
    artista: 'Artistas',
    faixa: 'Faixas',
  }
  return mapa[type] ?? rotuloTipo(type)
}
