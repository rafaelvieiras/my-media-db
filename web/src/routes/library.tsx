import { DatasourceList } from '@/components/datasource-list'
import { useT } from '@/lib/i18n'
import { ScreenHeader } from '@/components/screen-header'

// Route `/library`: datasource management (add, scan with progress,
// remove). It is where the owner points at the folders and triggers the scans.
export function LibraryScreen() {
  const t = useT()
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <ScreenHeader
        title={t('library.list.title')}
        subtitle={t('library.list.description')}
      />
      <DatasourceList />
    </main>
  )
}
