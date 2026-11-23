import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowLeft, LoaderCircle, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ScreenHeader } from '@/components/screen-header'
import { useT } from '@/lib/i18n'

// Shared UI states (loading / error / empty). Centralized here
// so the grid and the datasource list do not duplicate markup (the ratchet
// measures duplication with jscpd).

// VoltarBiblioteca is the "back" link common to the detail screens (item, person): it navigates to the
// home (the library). Extracted so the screens do not duplicate the same markup (jscpd).
export function VoltarBiblioteca() {
  const t = useT()
  return (
    <Link
      to="/"
      className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-4" aria-hidden />
      {t('jobs.queryStates.backToLibrary')}
    </Link>
  )
}

export function LoadingState({ label }: { label?: string }) {
  const t = useT()
  return (
    <div
      role="status"
      className="flex items-center justify-center gap-2 py-16 text-muted-foreground"
    >
      <LoaderCircle className="size-4 animate-spin" aria-hidden />
      <span>{label ?? t('common.state.loading')}</span>
    </div>
  )
}

// erroTecnico are the messages the browser/fetch layer produces. They are not copy: they
// arrive in English ("Failed to fetch") in the middle of a translated screen, and they say
// nothing the user can act on. The retry button is the action; the raw text is dropped.
const ERRO_TECNICO =
  /failed to fetch|networkerror|network error|load failed|fetch failed/i

function mensagemUtil(message: string | undefined): string | undefined {
  if (!message) return undefined
  return ERRO_TECNICO.test(message) ? undefined : message
}

export function ErrorState({
  title,
  message,
  onRetry,
}: {
  title?: string
  message?: string
  /** Retry of the failed query (TanStack `refetch`). Without it the screen is a dead end. */
  onRetry?: () => void
}) {
  const t = useT()
  const util = mensagemUtil(message)
  return (
    <div
      role="alert"
      className="flex flex-col items-center gap-2 py-16 text-center"
    >
      <TriangleAlert className="size-6 text-destructive" aria-hidden />
      <p className="font-medium text-destructive">
        {title ?? t('common.state.error')}
      </p>
      {util ? (
        <p className="max-w-md text-sm text-muted-foreground">{util}</p>
      ) : null}
      {onRetry ? (
        <Button variant="outline" onClick={onRetry} className="mt-2">
          {t('common.action.retry')}
        </Button>
      ) : null}
    </div>
  )
}

// FormSaveFooter is the footer common to the settings forms (languages, completeness):
// error message (role="alert") OR success message (role="status"), followed by the
// save button. Extracted so LanguageSettingsForm/CompletenessSettingsForm
// (settings.tsx, catalog-completeness-form.tsx) do not duplicate the same markup
// (jscpd).
// `desabilitado` lets a form keep the button visible but inert while nothing changed
// (the settings sections that save manually), instead of hiding the action.
export function FormSaveFooter({
  erro,
  sucesso,
  mensagemSucesso,
  salvando,
  desabilitado,
}: {
  erro: string | null
  sucesso: boolean
  mensagemSucesso?: string
  salvando: boolean
  desabilitado?: boolean
}) {
  const t = useT()
  return (
    <>
      {erro ? (
        <p role="alert" className="text-sm text-destructive">
          {erro}
        </p>
      ) : null}
      {sucesso && !erro ? (
        <p role="status" className="text-sm text-muted-foreground">
          {mensagemSucesso ?? t('settings.form.saved')}
        </p>
      ) : null}

      <div>
        <Button type="submit" disabled={salvando || desabilitado}>
          {salvando ? t('common.action.saving') : t('common.action.save')}
        </Button>
      </div>
    </>
  )
}

// EntityEditEstadoCarga is the loading/error state common to the 3 item
// edit subroutes (entity-edit-basico/-ids/-creditos.tsx) —
// extracted so the three do not duplicate the same wrapper (jscpd).
export function EntityEditEstadoCarga({
  carregando,
  erro,
}: {
  carregando: boolean
  erro?: unknown
}) {
  const t = useT()
  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      {carregando ? (
        <LoadingState label={t('entity.detail.loading')} />
      ) : (
        <ErrorState
          title={t('entity.detail.error')}
          message={erro instanceof Error ? erro.message : undefined}
        />
      )}
    </main>
  )
}

// The three states of any "thing loaded from the API": spinner, inline error,
// content. The content arrives as a render prop so the narrowed (non-undefined)
// type reaches the child without a cast.
export function CorpoComCarga<T>({
  dados,
  carregando,
  falhou,
  erro,
  textos,
  children,
}: {
  dados: T | undefined
  carregando: boolean
  falhou: boolean
  erro: unknown
  textos: { loading: string; loadError: string }
  children: (dados: T) => React.ReactNode
}) {
  if (carregando) {
    return <LoadingState label={textos.loading} />
  }
  if (falhou || dados === undefined) {
    return (
      <ErrorState
        title={textos.loadError}
        message={erro instanceof Error ? erro.message : undefined}
      />
    )
  }
  return <>{children(dados)}</>
}

// Screen with a header and the three standard states around a list: loading,
// error, empty, then the items. `/dashboard/jobs` and `/dashboard/integrations`
// are the same screen over different data, so the plumbing lives here once
// instead of twice (jscpd measured it).
export function ListaScreen<T>({
  title,
  subtitle,
  isPending,
  isError,
  error,
  itens,
  textos,
  renderItem,
  listaClassName,
  onRetry,
}: {
  title: string
  subtitle: string
  isPending: boolean
  isError: boolean
  error: unknown
  itens: T[]
  textos: {
    loading: string
    loadError: string
    emptyTitle: string
    emptyDescription: string
  }
  renderItem: (item: T) => React.ReactNode
  listaClassName?: string
  onRetry?: () => void
}) {
  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <ScreenHeader title={title} subtitle={subtitle} />

      {isPending ? (
        <LoadingState label={textos.loading} />
      ) : isError ? (
        <ErrorState
          title={textos.loadError}
          message={error instanceof Error ? error.message : undefined}
          onRetry={onRetry}
        />
      ) : itens.length === 0 ? (
        <EmptyState
          title={textos.emptyTitle}
          description={textos.emptyDescription}
        />
      ) : (
        <ul className={listaClassName}>{itens.map(renderItem)}</ul>
      )}
    </main>
  )
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: ReactNode
}) {
  return (
    <div className="flex flex-col items-center gap-3 py-16 text-center">
      <p className="font-medium">{title}</p>
      {description ? (
        <p className="max-w-md text-sm text-muted-foreground">{description}</p>
      ) : null}
      {action}
    </div>
  )
}
