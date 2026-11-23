import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { ErrorState, LoadingState } from '@/components/query-states'
import { IntegrationForm } from '@/components/integration-form'
import { integrationDetailQueryOptions } from '@/lib/queries'
import { useT } from '@/lib/i18n'
import { ScreenHeader } from '@/components/screen-header'

// Route `/dashboard/integrations/$slug`: form generated
// from the schema of ONE integration (`GET /integrations/{slug}`). A 404 (unknown
// slug) lands on the same `ErrorState` as any other failure — the
// pt-BR message comes from the backend envelope ("integração desconhecida").
export function IntegrationDetailScreen() {
  const t = useT()
  const { slug } = useParams({ from: '/dashboard/integrations/$slug' })
  const { data, isPending, isError, error, refetch } = useQuery(
    integrationDetailQueryOptions(slug),
  )

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <Link
        to="/dashboard/integrations"
        className="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        {t('settings.integration.backToList')}
      </Link>

      {isPending ? (
        <LoadingState label={t('settings.integration.loading')} />
      ) : isError ? (
        <ErrorState
          title={t('settings.integration.loadFailed')}
          message={error instanceof Error ? error.message : undefined}
          onRetry={() => void refetch()}
        />
      ) : (
        <>
          <ScreenHeader title={data.nome} subtitle={data.descricao} />

          {/* `key` on the `slug` (not on the `dataUpdatedAt` of the refetch): the
              `IntegrationForm` reseeds its own fields from the ECHO of the
              PUT internally (without unmounting) — unmounting on every refetch
              would erase the success message ("Saved. Restart the server...")
              before the owner manages to read it. It only needs to remount when
              switching slug (another integration — different schema and mutation). */}
          <IntegrationForm key={slug} slug={data.slug} campos={data.campos} />
        </>
      )}
    </main>
  )
}
