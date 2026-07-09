import type { ReactNode } from 'react'
import { LoaderCircle, TriangleAlert } from 'lucide-react'

// Estados de UI compartilhados (carregando / erro / vazio). Centralizados aqui
// para o grid e a lista de datasources não duplicarem markup (a catraca do 0005
// mede duplicação com jscpd).

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
