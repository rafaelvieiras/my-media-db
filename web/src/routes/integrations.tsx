import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { integrationsQueryOptions } from '@/lib/queries'
import type { IntegrationSummary } from '@/lib/api'

// Route `/dashboard/integrations` (second half of the Integrations
// epic — ADR-0023): lists the integrations registered in the Registry
// (today only TMDB) with the `configurada`/`não configurada` badge already computed
// by the backend — the front never reimplements that rule, each provider
// has its own (e.g.: TMDB accepts token OR api_key).
export function IntegrationsListScreen() {
  const { data, isPending, isError, error } = useQuery(integrationsQueryOptions)

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">Integrações</h1>
        <p className="text-sm text-muted-foreground">
          Credenciais de provider configuráveis pelo painel, sem editar .env nem
          redeployar.
        </p>
      </div>

      {isPending ? (
        <LoadingState label="Carregando integrações…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar as integrações"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : data.length === 0 ? (
        <EmptyState
          title="Nenhuma integração registrada"
          description="Nenhum provider com credenciais configuráveis está registrado neste binário."
        />
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2">
          {data.map((integracao) => (
            <IntegrationCard key={integracao.slug} integracao={integracao} />
          ))}
        </ul>
      )}
    </main>
  )
}

function IntegrationCard({ integracao }: { integracao: IntegrationSummary }) {
  return (
    <li>
      <Link
        to="/dashboard/integrations/$slug"
        params={{ slug: integracao.slug }}
        className="flex h-full flex-col gap-2 rounded-xl border border-border bg-card p-4 ring-1 ring-transparent transition-colors hover:ring-foreground/10"
      >
        <div className="flex items-center justify-between gap-2">
          <span className="font-medium">{integracao.nome}</span>
          {integracao.configurada ? (
            <Badge className="border-transparent bg-emerald-600 text-white">
              Configurada
            </Badge>
          ) : (
            <Badge variant="secondary">Não configurada</Badge>
          )}
        </div>
        <p className="text-sm text-muted-foreground">{integracao.descricao}</p>
      </Link>
    </li>
  )
}
