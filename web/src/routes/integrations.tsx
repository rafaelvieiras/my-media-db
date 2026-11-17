import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
import { ListaScreen } from '@/components/query-states'
import { integrationsQueryOptions } from '@/lib/queries'
import { useT } from '@/lib/i18n'
import type { IntegrationSummary } from '@/lib/api'

// Route `/dashboard/integrations` (second half of the Integrations
// epic — ADR-0023): lists the integrations registered in the Registry
// (today only TMDB) with the `Configured`/`Not configured` badge already computed
// by the backend — the front never reimplements that rule, each provider
// has its own (e.g.: TMDB accepts token OR api_key).
export function IntegrationsListScreen() {
  const t = useT()
  const { data, isPending, isError, error } = useQuery(integrationsQueryOptions)

  return (
    <ListaScreen
      title={t('settings.integrations.title')}
      subtitle={t('settings.integrations.subtitle')}
      isPending={isPending}
      isError={isError}
      error={error}
      itens={data ?? []}
      textos={{
        loading: t('settings.integrations.loading'),
        loadError: t('settings.integrations.loadFailed'),
        emptyTitle: t('settings.integrations.emptyTitle'),
        emptyDescription: t('settings.integrations.emptyDescription'),
      }}
      listaClassName="grid gap-3 sm:grid-cols-2"
      renderItem={(integracao) => (
        <IntegrationCard key={integracao.slug} integracao={integracao} />
      )}
    />
  )
}

function IntegrationCard({ integracao }: { integracao: IntegrationSummary }) {
  const t = useT()

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
              {t('settings.integrations.configured')}
            </Badge>
          ) : (
            <Badge variant="secondary">
              {t('settings.integrations.notConfigured')}
            </Badge>
          )}
        </div>
        <p className="text-sm text-muted-foreground">{integracao.descricao}</p>
      </Link>
    </li>
  )
}
