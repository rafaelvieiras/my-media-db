import { SearchPanel } from '@/components/search-panel'

// Route `/search`: unified search of the catalog + external source, with import of
// external results into the catalog.
export function SearchScreen() {
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">Buscar</h1>
        <p className="text-sm text-muted-foreground">
          Procure no seu catálogo e em fontes externas; importe o que faltar.
        </p>
      </div>
      <SearchPanel />
    </main>
  )
}
