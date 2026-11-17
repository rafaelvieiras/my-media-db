import { Link } from '@tanstack/react-router'
import { FilmIcon, FolderPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { RecentlyAddedSection } from '@/components/recently-added-rail'
import { useLanguage } from '@/lib/language-context'
import { useT } from '@/lib/i18n'

// Home (route `/`): since ADR-0030, the home shows ONLY the
// "Adicionados recentemente" carousels (`RecentlyAddedSection`, one per top-level
// type) — the full paginated grid (`PosterGrid`) left here and became its own
// route `/library/$entitySlug` (see `routes/library-browse.tsx`),
// reached through the "Ver Todos" link of each carousel. The reading language
// (global) is passed to the carousel for the `?lang=` — coherence with the detail.
// The "Novo item"/"Nova coleção" actions navigate to the dedicated creation
// routes (they used to be inline forms).
export function LibraryItemsScreen() {
  const t = useT()
  const { lang } = useLanguage()
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="font-heading text-xl font-semibold">
            {t('library.index.title')}
          </h1>
          <p className="text-sm text-muted-foreground">
            {t('library.index.description')}
          </p>
        </div>
        <div className="flex flex-wrap items-start gap-2">
          <Button asChild variant="outline" size="sm">
            <Link to="/new/item">
              <FilmIcon aria-hidden />
              {t('library.index.newItem')}
            </Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to="/new/collection">
              <FolderPlus aria-hidden />
              {t('library.index.newCollection')}
            </Link>
          </Button>
        </div>
      </div>
      <RecentlyAddedSection lang={lang} />
    </main>
  )
}
