// PURE helpers for CATALOGING completeness: fields/artwork filled
// according to the ruler configured in /settings. A NEW concept, DIFFERENT from
// OWNERSHIP completeness (`web/src/lib/entity-badges.ts`) — this module
// reuses NOTHING from there (nor `estadoCompletude`/`temCompletude`/`unidadeCompletude`),
// a decision inherited from §6 of the settings spec and repeated in §6 of the completeness spec, to keep the two concepts
// clearly separated in the code, not only in the UI.
import type { components } from '@/lib/api-types'

export type CatalogCompleteness =
  components['schemas']['CatalogCompletenessResult'] | null

// Same map of kind labels used in the settings form
// (`web/src/components/catalog-completeness-form.tsx`, `KINDS_ARTE_UI`).
const ROTULO_KIND_ARTE: Record<string, string> = {
  poster: 'Poster',
  fanart: 'Fanart',
  logo: 'Logo',
  clearart: 'Clearart',
  banner: 'Banner',
  thumb: 'Thumb',
  disc: 'Capa de disco',
}

// PT-BR map of the STABLE keys of `missing` (§4). An unknown key echoes
// raw (never breaks the UI, same pattern as `rotuloTipo` in entity-badges.ts).
const ROTULO_CAMPO: Record<string, string> = {
  description: 'sinopse',
  runtime: 'duração',
  certification: 'classificação indicativa',
  keywords: 'palavras-chave',
  cast: 'elenco',
  media_file: 'arquivo de mídia',
}

// precisaAtencaoCatalogacao says whether the badge/section should appear: only when there is an
// evaluated result (`cc` non-null) and it is incomplete. `null` (draft, or not yet
// evaluated by the job) and `complete: true` never trigger the alert — cover/detail
// stay clean, same principle as the ownership badge.
export function precisaAtencaoCatalogacao(cc: CatalogCompleteness): boolean {
  return !!cc && !cc.complete
}

// rotuloItemFaltando translates ONE key of `missing` into PT-BR. Keys `artwork:
// <kind>` extract the kind and reuse the artwork label map of the settings form
// (`Poster`/`Fanart`/…), lowercased to match the style of the other labels
// (all lowercase, e.g. "sinopse"/"elenco") and the example in the spec (§2.3):
// "Catalogação incompleta: elenco, arte fanart". An unknown kind echoes raw (e.g.
// "artwork:still" → "arte still"). An unknown field key echoes raw.
export function rotuloItemFaltando(chave: string): string {
  if (chave.startsWith('artwork:')) {
    const kind = chave.slice('artwork:'.length)
    const rotulo = ROTULO_KIND_ARTE[kind] ?? kind
    return `arte ${rotulo.toLowerCase()}`
  }
  return ROTULO_CAMPO[chave] ?? chave
}

// rotuloTooltipCompletude builds the full sentence of the badge tooltip/aria-label:
// "Catalogação incompleta: elenco, arte fanart". A null/complete `cc` returns the
// sentence with an empty list (the UI should not call this when `!precisaAtencaoCatalogacao`,
// but the function does not break if called anyway).
export function rotuloTooltipCompletude(cc: CatalogCompleteness): string {
  const faltando = cc?.missing ?? []
  return `Catalogação incompleta: ${faltando.map(rotuloItemFaltando).join(', ')}`
}
