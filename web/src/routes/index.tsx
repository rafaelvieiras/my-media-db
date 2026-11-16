import { Link } from '@tanstack/react-router'
import { FilmIcon, FolderPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { RecentlyAddedSection } from '@/components/recently-added-rail'
import { useLanguage } from '@/lib/language-context'

// Home (route `/`): since ADR-0030, the home shows ONLY the
// "Adicionados recentemente" carousels (`RecentlyAddedSection`, one per top-level
// type) — the full paginated grid (`PosterGrid`) left here and became its own
// route `/library/$entitySlug` (see `routes/library-browse.tsx`),
// reached through the "Ver Todos" link of each carousel. The reading language
// (global) is passed to the carousel for the `?lang=` — coherence with the detail.
// The "Novo item"/"Nova coleção" actions navigate to the dedicated creation
// routes (they used to be inline forms).
export function LibraryItemsScreen() {
  const { lang } = useLanguage()
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="font-heading text-xl font-semibold">Biblioteca</h1>
          <p className="text-sm text-muted-foreground">
            Catálogo detectado a partir dos seus datasources.
          </p>
        </div>
        <div className="flex flex-wrap items-start gap-2">
          <Button asChild variant="outline" size="sm">
            <Link to="/new/item">
              <FilmIcon aria-hidden />
              Novo item
            </Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to="/new/collection">
              <FolderPlus aria-hidden />
              Nova coleção
            </Link>
          </Button>
        </div>
      </div>
      <RecentlyAddedSection lang={lang} />
    </main>
  )
}
