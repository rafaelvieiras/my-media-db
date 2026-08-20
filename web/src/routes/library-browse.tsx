import { useParams } from '@tanstack/react-router'
import { PosterGrid } from '@/components/poster-grid'
import { EmptyState } from '@/components/query-states'
import { rotuloTipoPlural } from '@/lib/entity-badges'
import { slugParaTipo } from '@/lib/library-slugs'
import { useLanguage } from '@/lib/language-context'

// Rota `/library/$entitySlug` (briefing 0114/ADR-0030): a listagem completa e
// paginada de UM tipo de topo por vez — o `PosterGrid` que antes vivia dentro
// da home (`routes/index.tsx`, filtrado por `?type=`, briefing 0110) virou
// esta tela própria, deep-linkável, alcançada pelo "Ver Todos" de cada
// carrossel (`RecentlyAddedRail`) e pelo dropdown "Biblioteca" da navbar
// (`app-header.tsx`). O nome do arquivo/componente evita colidir com o
// `routes/library.tsx` JÁ existente (`LibraryScreen`, gestão de datasources em
// `/dashboard/library`, briefing 0071) — são telas completamente diferentes.
//
// Slug inválido/sem tipo mapeado (`library-slugs.ts`) cai num estado vazio
// coerente — nunca 404 hard nem crash (decisão explícita do briefing).
export function LibraryBrowseScreen() {
  const { entitySlug } = useParams({ from: '/library/$entitySlug' })
  const { lang } = useLanguage()
  const type = slugParaTipo(entitySlug)

  if (!type) {
    return (
      <main className="mx-auto max-w-6xl px-4 py-6">
        <EmptyState
          title="Tipo de biblioteca não encontrado"
          description="O link usado não corresponde a nenhum tipo de biblioteca conhecido."
        />
      </main>
    )
  }

  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <h1 className="mb-4 font-heading text-xl font-semibold">
        {rotuloTipoPlural(type)}
      </h1>
      <PosterGrid lang={lang} type={type} />
    </main>
  )
}
