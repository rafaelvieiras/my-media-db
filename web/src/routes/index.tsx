import { PosterGrid } from '@/components/poster-grid'
import { useLanguage } from '@/lib/language-context'

// Home (rota `/`): o grid de pôsteres é a tela principal do dono — o catálogo
// detectado. Decisão do 0011: a home vira o grid (a antiga tela de saúde foi
// para `/health`). O idioma de leitura (global) é repassado ao grid para o
// `?lang=` — coerência com o detalhe e base para a resolução de card futura.
export function LibraryItemsScreen() {
  const { lang } = useLanguage()
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">Biblioteca</h1>
        <p className="text-sm text-muted-foreground">
          Catálogo detectado a partir dos seus datasources.
        </p>
      </div>
      <PosterGrid lang={lang} />
    </main>
  )
}
