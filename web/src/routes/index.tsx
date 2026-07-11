import { Link } from '@tanstack/react-router'
import { FilmIcon, FolderPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { PosterGrid } from '@/components/poster-grid'
import { useLanguage } from '@/lib/language-context'

// Home (rota `/`): o grid de pôsteres é a tela principal do dono — o catálogo
// detectado. Decisão do 0011: a home vira o grid (a antiga tela de saúde foi
// para `/health`). O idioma de leitura (global) é repassado ao grid para o
// `?lang=` — coerência com o detalhe e base para a resolução de card futura. As
// ações "Novo item"/"Nova coleção" (0030) navegam para as rotas de criação
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
      <PosterGrid lang={lang} />
    </main>
  )
}
