import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowLeft, LoaderCircle, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'

// Shared UI states (loading / error / empty). Centralized here
// so the grid and the datasource list do not duplicate markup (the ratchet
// measures duplication with jscpd).

// VoltarBiblioteca is the "back" link common to the detail screens (item, person): it navigates to the
// home (the library). Extracted so the screens do not duplicate the same markup (jscpd).
export function VoltarBiblioteca() {
  return (
    <Link
      to="/"
      className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-4" aria-hidden />
      Voltar à biblioteca
    </Link>
  )
}

export function LoadingState({ label = 'Carregando…' }: { label?: string }) {
  return (
    <div
      role="status"
      className="flex items-center justify-center gap-2 py-16 text-muted-foreground"
    >
      <LoaderCircle className="size-4 animate-spin" aria-hidden />
      <span>{label}</span>
    </div>
  )
}

export function ErrorState({
  title = 'Algo deu errado',
  message,
}: {
  title?: string
  message?: string
}) {
  return (
    <div
      role="alert"
      className="flex flex-col items-center gap-2 py-16 text-center"
    >
      <TriangleAlert className="size-6 text-destructive" aria-hidden />
      <p className="font-medium text-destructive">{title}</p>
      {message ? (
        <p className="max-w-md text-sm text-muted-foreground">{message}</p>
      ) : null}
    </div>
  )
}

// FormSaveFooter is the footer common to the settings forms (languages, completeness):
// error message (role="alert") OR success message (role="status"), followed by the
// save button. Extracted so LanguageSettingsForm/CompletenessSettingsForm
// (settings.tsx, catalog-completeness-form.tsx) do not duplicate the same markup
// (jscpd).
export function FormSaveFooter({
  erro,
  sucesso,
  mensagemSucesso = 'Configurações salvas.',
  salvando,
}: {
  erro: string | null
  sucesso: boolean
  mensagemSucesso?: string
  salvando: boolean
}) {
  return (
    <>
      {erro ? (
        <p role="alert" className="text-sm text-destructive">
          {erro}
        </p>
      ) : null}
      {sucesso && !erro ? (
        <p role="status" className="text-sm text-muted-foreground">
          {mensagemSucesso}
        </p>
      ) : null}

      <div>
        <Button type="submit" disabled={salvando}>
          {salvando ? 'Salvando…' : 'Salvar'}
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
  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      {carregando ? (
        <LoadingState label="Carregando item…" />
      ) : (
        <ErrorState
          title="Não foi possível carregar o item"
          message={erro instanceof Error ? erro.message : undefined}
        />
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
