import type { FC } from 'react'
import { TmdbLogo, ImdbLogo, TvdbLogo } from '@/components/provider-logos'

// PROVIDER_META is the EXTENSIBLE registry of per-source visual
// metadata — reused both by the RATINGS row and by the
// external LINKS row. A source without an entry falls back to `metaDaFonte` (uppercase
// text, no logo) — it never breaks, never hides the data. Adding a
// new source (e.g. fanart.tv, when linking it makes sense) is just one entry
// here, without changing any screen (AGENTS.md §6, provider independence).
export const PROVIDER_META: Record<
  string,
  { label: string; Logo?: FC<{ className?: string }> }
> = {
  tmdb: { label: 'TMDB', Logo: TmdbLogo },
  imdb: { label: 'IMDb', Logo: ImdbLogo },
  tvdb: { label: 'TVDB', Logo: TvdbLogo },
}

export function metaDaFonte(source: string) {
  return (
    PROVIDER_META[source] ?? { label: source.toUpperCase(), Logo: undefined }
  )
}
