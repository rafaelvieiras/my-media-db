import { Link } from '@tanstack/react-router'
import { FilmIcon, FolderPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { RecentlyAddedSection } from '@/components/recently-added-rail'
import { useLanguage } from '@/lib/language-context'

// Home (rota `/`): desde o briefing 0114/ADR-0030, a home mostra SÓ os
// carrosséis "Adicionados recentemente" (`RecentlyAddedSection`, um por tipo
// de topo) — o grid paginado completo (`PosterGrid`) saiu daqui e virou a
// rota própria `/library/$entitySlug` (ver `routes/library-browse.tsx`),
// alcançada pelo link "Ver Todos" de cada carrossel. O idioma de leitura
// (global) é repassado ao carrossel para o `?lang=` — coerência com o detalhe.
// As ações "Novo item"/"Nova coleção" (0030) navegam para as rotas de criação
// dedicadas (antes eram formulários inline).
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
