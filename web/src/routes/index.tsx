import { PosterGrid } from '@/components/poster-grid'

// Home (rota `/`): o grid de pôsteres é a tela principal do dono — o catálogo
// detectado. Decisão do 0011: a home vira o grid (a antiga tela de saúde foi
// para `/health`).
export function LibraryItemsScreen() {
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">Biblioteca</h1>
        <p className="text-sm text-muted-foreground">
          Catálogo detectado a partir dos seus datasources.
        </p>
      </div>
      <PosterGrid />
    </main>
  )
}
