import { useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { PosterGrid } from '@/components/poster-grid'
import { EmptyState } from '@/components/query-states'
import { rotuloTipoPlural } from '@/lib/entity-badges'
import { slugParaTipo } from '@/lib/library-slugs'
import { useLanguage } from '@/lib/language-context'
import { useT } from '@/lib/i18n'

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
  const t = useT()
  const { entitySlug } = useParams({ from: '/library/$entitySlug' })
  // The page is a SEARCH PARAM (`?page=2`), not component state: going back from a detail
  // returns to the page the owner was on and a refresh keeps the position. Absent = first
  // page (the URL stays clean).
  const { page } = useSearch({ from: '/library/$entitySlug' })
  const navigate = useNavigate()
  const { lang } = useLanguage()
  const type = slugParaTipo(entitySlug)

  const irParaPagina = (destino: number) => {
    navigate({
      to: '/library/$entitySlug',
      params: { entitySlug },
      // Page 1 does not write the param (clean URL, same rule as the `ordering` of the
      // series detail).
      search: destino > 0 ? { page: destino + 1 } : {},
    })
  }

  if (!type) {
    return (
      <main className="mx-auto max-w-6xl px-4 py-6">
        <EmptyState
          title={t('library.browse.notFoundTitle')}
          description={t('library.browse.notFoundDescription')}
        />
      </main>
    )
  }

  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <h1 className="mb-4 font-heading text-xl font-semibold">
        {rotuloTipoPlural(type, t)}
      </h1>
      <PosterGrid
        lang={lang}
        type={type}
        page={Math.max(0, (page ?? 1) - 1)}
        onPageChange={irParaPagina}
      />
    </main>
  )
}
