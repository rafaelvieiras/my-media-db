// PURE helpers for CATALOGING completeness: fields/artwork filled
// according to the ruler configured in /settings. A NEW concept, DIFFERENT from
// OWNERSHIP completeness (`web/src/lib/entity-badges.ts`) — this module
// reuses NOTHING from there (nor `estadoCompletude`/`temCompletude`/`unidadeCompletude`),
// a decision inherited from §6 of the settings spec and repeated in §6 of the completeness spec, to keep the two concepts
// clearly separated in the code, not only in the UI.
import type { components } from '@/lib/api-types'
import type { Translate } from '@/lib/i18n/types'

export type CatalogCompleteness =
  components['schemas']['CatalogCompletenessResult'] | null

// Catalog keys of the artwork kinds — the SAME map the settings form shows
// (`web/src/components/catalog-completeness-form.tsx`, `KINDS_ARTE_UI`).
const CHAVE_KIND_ARTE: Record<string, string> = {
  poster: 'review.completeness.artworkKind.poster',
  fanart: 'review.completeness.artworkKind.fanart',
  logo: 'review.completeness.artworkKind.logo',
  clearart: 'review.completeness.artworkKind.clearart',
  banner: 'review.completeness.artworkKind.banner',
  thumb: 'review.completeness.artworkKind.thumb',
  disc: 'review.completeness.artworkKind.disc',
}

// Catalog keys of the STABLE keys of `missing` (§4). An unknown key echoes
// raw (never breaks the UI, same pattern as `rotuloTipo` in entity-badges.ts).
const CHAVE_CAMPO: Record<string, string> = {
  description: 'review.completeness.field.description',
  runtime: 'review.completeness.field.runtime',
  certification: 'review.completeness.field.certification',
  keywords: 'review.completeness.field.keywords',
  cast: 'review.completeness.field.cast',
  media_file: 'review.completeness.field.media_file',
}

// precisaAtencaoCatalogacao says whether the badge/section should appear: only when there is an
// evaluated result (`cc` non-null) and it is incomplete. `null` (draft, or not yet
// evaluated by the job) and `complete: true` never trigger the alert — cover/detail
// stay clean, same principle as the ownership badge.
export function precisaAtencaoCatalogacao(cc: CatalogCompleteness): boolean {
  return !!cc && !cc.complete
}

// rotuloItemFaltando translates ONE key of `missing`. Keys `artwork:<kind>` extract the
// kind and reuse the artwork label map of the settings form (`Poster`/`Fanart`/…),
// lowercased to match the style of the other labels (all lowercase, e.g. "synopsis"/
// "cast") and the example in the spec (§2.3): "Incomplete cataloging: cast, artwork
// fanart". An unknown kind echoes raw (e.g. "artwork:still" → "artwork still"). An
// unknown field key echoes raw.
export function rotuloItemFaltando(chave: string, t: Translate): string {
  if (chave.startsWith('artwork:')) {
    const kind = chave.slice('artwork:'.length)
    const chaveKind = CHAVE_KIND_ARTE[kind]
    const rotulo = chaveKind ? t(chaveKind) : kind
    return t('review.completeness.artworkItem', {
      kind: rotulo.toLowerCase(),
    })
  }
  const chaveCampo = CHAVE_CAMPO[chave]
  return chaveCampo ? t(chaveCampo).toLowerCase() : chave
}

// rotuloTooltipCompletude builds the full sentence of the badge tooltip/aria-label:
// "Incomplete cataloging: cast, artwork fanart". A null/complete `cc` returns the
// sentence with an empty list (the UI should not call this when `!precisaAtencaoCatalogacao`,
// but the function does not break if called anyway).
export function rotuloTooltipCompletude(
  cc: CatalogCompleteness,
  t: Translate,
): string {
  const faltando = cc?.missing ?? []
  return t('review.completeness.incomplete', {
    missing: faltando.map((chave) => rotuloItemFaltando(chave, t)).join(', '),
  })
}
