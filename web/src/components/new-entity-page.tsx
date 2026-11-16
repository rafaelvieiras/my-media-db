import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import type { ReactNode } from 'react'
import { ErrorState, LoadingState } from '@/components/query-states'
import type { FieldDescriptor } from '@/lib/api'
import { entityFieldsQueryOptions } from '@/lib/queries'

// SHARED scaffold of the local creation screens: the back-link
// "Voltar à biblioteca", the `<main>` container, the `<h1>` and the
// loading/error states that consume `entityFieldsQueryOptions`. The item and the
// collection routes only differ in the specific `<Form>`, so they receive the descriptors through a
// render-prop and mount only the form — the rest (identical) lives here.
// `topo` is an optional slot rendered BEFORE the form (after the `<h1>`),
// always visible regardless of the loading state — used by the cross-link
// "Novo item" ↔ Busca; routes that do not pass `topo` stay
// identical to before.
export function NewEntityPage({
  title,
  topo,
  children,
}: {
  title: string
  topo?: ReactNode
  children: (fields: FieldDescriptor[]) => ReactNode
}) {
  const fieldsQuery = useQuery(entityFieldsQueryOptions)

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <Link
        to="/"
        className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" aria-hidden />
        Voltar à biblioteca
      </Link>
      <h1 className="mt-4 mb-4 font-heading text-xl font-semibold">{title}</h1>
      {topo}
      {fieldsQuery.isPending ? (
        <LoadingState label="Carregando formulário…" />
      ) : fieldsQuery.isError ? (
        <ErrorState title="Não foi possível carregar o formulário" />
      ) : (
        children(fieldsQuery.data)
      )}
    </main>
  )
}
