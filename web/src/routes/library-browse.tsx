import { useParams } from '@tanstack/react-router'
import { PosterGrid } from '@/components/poster-grid'
import { EmptyState } from '@/components/query-states'
import { rotuloTipoPlural } from '@/lib/entity-badges'
import { slugParaTipo } from '@/lib/library-slugs'
import { useLanguage } from '@/lib/language-context'

// Route `/library/$entitySlug` (ADR-0030): the complete, paginated listing of
// ONE top-level type at a time — the `PosterGrid` that used to live inside
// the home (filtered by `?type=`) became
// this screen of its own, deep-linkable, reached through the "Ver Todos" of each
// carousel (`RecentlyAddedRail`) and through the "Biblioteca" dropdown of the navbar
// (`app-header.tsx`). The file/component name avoids colliding with the
// ALREADY existing `routes/library.tsx` (`LibraryScreen`, datasource management in
// `/dashboard/library`) — they are completely different screens.
//
// An invalid slug/type without a mapping (`library-slugs.ts`) falls into a coherent
// empty state — never a hard 404 nor a crash (an explicit decision).
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
