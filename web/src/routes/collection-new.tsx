import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, FolderPlus } from 'lucide-react'
import { SchemaForm } from '@/components/schema-form'
import { ErrorState, LoadingState } from '@/components/query-states'
import { createCollection, type FieldDescriptor } from '@/lib/api'
import { entityFieldsQueryOptions } from '@/lib/queries'
import { useCriarEntidade } from '@/lib/use-criar-entidade'
import {
  schemaZod,
  valoresDeCriacao,
  type ValoresForm,
} from '@/lib/field-schema'

// Rota `/new/collection` (briefing 0030): cria uma coleção LOCAL do zero. Uma
// coleção só expõe título (obrigatório) + descrição — recorte dos campos
// editoriais genéricos que se aplicam a ela (o backend `createCollection` só
// aceita esses dois). Ao criar, navega para `/entities/{id}` (a página da coleção,
// onde o dono adiciona membros). O 422 (título vazio) aparece inline.
const CAMPOS_COLECAO = ['title', 'description']

export function CollectionNewScreen() {
  const fieldsQuery = useQuery(entityFieldsQueryOptions)

  const voltar = (
    <Link
      to="/"
      className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-4" aria-hidden />
      Voltar à biblioteca
    </Link>
  )

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      {voltar}
      <h1 className="mt-4 mb-4 font-heading text-xl font-semibold">
        Nova coleção
      </h1>
      {fieldsQuery.isPending ? (
        <LoadingState label="Carregando formulário…" />
      ) : fieldsQuery.isError ? (
        <ErrorState title="Não foi possível carregar o formulário" />
      ) : (
        <CollectionNewForm fields={fieldsQuery.data} />
      )}
    </main>
  )
}

function CollectionNewForm({ fields }: { fields: FieldDescriptor[] }) {
  const navigate = useNavigate()
  const camposColecao = fields.filter((f) => CAMPOS_COLECAO.includes(f.name))
  const { mutation, mensagemErro } = useCriarEntidade(
    createCollection,
    'Não foi possível criar a coleção.',
  )

  function onSubmit(valores: ValoresForm) {
    const title = String(valores.title ?? '').trim()
    if (!title) {
      return
    }
    const description = String(valores.description ?? '').trim()
    mutation.mutate({ title, description: description || undefined })
  }

  return (
    <SchemaForm
      fields={camposColecao}
      defaultValues={valoresDeCriacao(camposColecao)}
      schema={schemaZod(camposColecao, 'criacao')}
      onSubmit={onSubmit}
      onCancel={() => navigate({ to: '/' })}
      submitLabel="Criar coleção"
      submitPendingLabel="Criando…"
      submitIcon={<FolderPlus aria-hidden />}
      isPending={mutation.isPending}
      mensagemErro={mensagemErro}
    />
  )
}
