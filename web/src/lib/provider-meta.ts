import type { FC } from 'react'
import { TmdbLogo, ImdbLogo, TvdbLogo } from '@/components/provider-logos'

// PROVIDER_META (briefing 0086) é o registro EXTENSÍVEL de metadados visuais
// por fonte — reusado tanto pela fileira de NOTAS quanto pela fileira de
// LINKS externos. Fonte sem entrada cai no fallback de `metaDaFonte` (texto
// maiúsculo, sem logo) — nunca quebra, nunca esconde o dado. Adicionar uma
// fonte nova (ex. fanart.tv, quando fizer sentido linkar) é só uma entrada
// aqui, sem mudar nenhuma tela (AGENTS.md §6, independência de provider).
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
