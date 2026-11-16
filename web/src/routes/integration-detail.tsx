import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { ErrorState, LoadingState } from '@/components/query-states'
import { IntegrationForm } from '@/components/integration-form'
import { integrationDetailQueryOptions } from '@/lib/queries'

// Route `/dashboard/integrations/$slug`: form generated
// from the schema of ONE integration (`GET /integrations/{slug}`). A 404 (unknown
// slug) lands on the same `ErrorState` as any other failure — the
// pt-BR message comes from the backend envelope ("integração desconhecida").
export function IntegrationDetailScreen() {
  const { slug } = useParams({ from: '/dashboard/integrations/$slug' })
  const { data, isPending, isError, error } = useQuery(
    integrationDetailQueryOptions(slug),
  )

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <Link
        to="/dashboard/integrations"
        className="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        Voltar às integrações
      </Link>

      {isPending ? (
        <LoadingState label="Carregando integração…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar a integração"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : (
        <>
          <div className="mb-4 flex flex-col gap-1">
            <h1 className="font-heading text-xl font-semibold">{data.nome}</h1>
            <p className="text-sm text-muted-foreground">{data.descricao}</p>
          </div>

          {/* `key` on the `slug` (not on the `dataUpdatedAt` of the refetch): the
              `IntegrationForm` reseeds its own fields from the ECHO of the
              PUT internally (without unmounting) — unmounting on every refetch
              would erase the success message ("Salvo. Reinicie o servidor...")
              before the owner manages to read it. It only needs to remount when
              switching slug (another integration — different schema and mutation). */}
          <IntegrationForm key={slug} slug={data.slug} campos={data.campos} />
        </>
      )}
    </main>
  )
}
