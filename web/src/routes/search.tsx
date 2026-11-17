import { SearchPanel } from '@/components/search-panel'
import { useT } from '@/lib/i18n'
import { ScreenHeader } from '@/components/screen-header'

// Route `/search`: unified search of the catalog + external source, with import of
// external results into the catalog.
export function SearchScreen() {
  const t = useT()
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <ScreenHeader
        title={t('collections.search.screenTitle')}
        subtitle={t('collections.search.screenDescription')}
      />
      <SearchPanel />
    </main>
  )
}
