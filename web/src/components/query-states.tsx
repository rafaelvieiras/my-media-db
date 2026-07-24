import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowLeft, LoaderCircle, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'

// Estados de UI compartilhados (carregando / erro / vazio). Centralizados aqui
// para o grid e a lista de datasources não duplicarem markup (a catraca do 0005
// mede duplicação com jscpd).

// VoltarBiblioteca é o link "voltar" comum às telas de detalhe (item, pessoa): navega à
// home (a biblioteca). Extraído para as telas não duplicarem o mesmo markup (jscpd).
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

// FormSaveFooter é o rodapé comum aos formulários de settings (idiomas, 0057):
// mensagem de erro (role="alert") OU de sucesso (role="status"), seguida do
// botão de salvar. Extraído para LanguageSettingsForm/CompletenessSettingsForm
// (settings.tsx, catalog-completeness-form.tsx) não duplicarem o mesmo markup
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

// EntityEditEstadoCarga é o estado de carregando/erro comum às 3 subrotas de
// edição de item (briefing 0080: entity-edit-basico/-ids/-creditos.tsx) —
// extraído para as três não duplicarem o mesmo wrapper (jscpd).
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
