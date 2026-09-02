import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { ErrorState, LoadingState } from '@/components/query-states'
import { IntegrationForm } from '@/components/integration-form'
import { integrationDetailQueryOptions } from '@/lib/queries'

// Rota `/dashboard/integrations/$slug` (briefing 0096): formulário gerado a
// partir do schema de UMA integração (`GET /integrations/{slug}`). 404 (slug
// desconhecido) cai no mesmo `ErrorState` de qualquer outra falha — a
// mensagem pt-BR vem do envelope do backend ("integração desconhecida").
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

          {/* `key` no `slug` (não no `dataUpdatedAt` do refetch): o
              `IntegrationForm` reseeda os próprios campos a partir do ECO do
              PUT internamente (sem desmontar) — desmontar a cada refetch
              apagaria a mensagem de sucesso ("Salvo. Reinicie o servidor...")
              antes do dono conseguir ler. Só precisa remontar ao trocar de
              slug (outra integração — schema e mutation diferentes). */}
          <IntegrationForm key={slug} slug={data.slug} campos={data.campos} />
        </>
      )}
    </main>
  )
}
