import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import type { ReactNode } from 'react'
import { ErrorState, LoadingState } from '@/components/query-states'
import type { FieldDescriptor } from '@/lib/api'
import { entityFieldsQueryOptions } from '@/lib/queries'

// Scaffold COMPARTILHADO das telas de criação local (briefing 0030): o back-link
// "Voltar à biblioteca", o container `<main>`, o `<h1>` e os estados de
// carregando/erro que consomem `entityFieldsQueryOptions`. As rotas de item e de
// coleção só diferem no `<Form>` específico, então recebem os descritores por
// render-prop e montam apenas o formulário — o resto (idêntico) mora aqui.
export function NewEntityPage({
  title,
  children,
}: {
  title: string
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
