import { DatasourceList } from '@/components/datasource-list'

// Route `/library`: datasource management (add, scan with progress,
// remove). It is where the owner points at the folders and triggers the scans.
export function LibraryScreen() {
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">Bibliotecas</h1>
        <p className="text-sm text-muted-foreground">
          Aponte pastas de mídia, escaneie e acompanhe o progresso.
        </p>
      </div>
      <DatasourceList />
    </main>
  )
}
