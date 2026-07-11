import { useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, FilmIcon } from 'lucide-react'
import { Select } from '@/components/ui/select'
import { SchemaForm } from '@/components/schema-form'
import { ErrorState, LoadingState } from '@/components/query-states'
import { createItem } from '@/lib/api'
import { entityFieldsQueryOptions } from '@/lib/queries'
import { useCriarEntidade } from '@/lib/use-criar-entidade'
import {
  criaveis,
  schemaZod,
  valoresDeCriacao,
  type ValoresForm,
} from '@/lib/field-schema'

// Rota `/new/item` (briefing 0030): cria um item LOCAL (filme/série) do zero, sem
// fonte externa. O formulário dos campos editoriais é DIRIGIDO PELOS DESCRITORES
// (subconjunto `creatable`, corpo plano); o seletor de Tipo (movie/tv) NÃO é campo
// editorial, então a rota o injeta no topo do form. Ao criar, navega para
// `/entities/{id}`, onde o dono fixa os IDs externos e re-enriquece.
export function ItemNewScreen() {
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
        Novo item
      </h1>
      {fieldsQuery.isPending ? (
        <LoadingState label="Carregando formulário…" />
      ) : fieldsQuery.isError ? (
        <ErrorState title="Não foi possível carregar o formulário" />
      ) : (
        <ItemNewForm fields={fieldsQuery.data} />
      )}
    </main>
  )
}

function ItemNewForm({
  fields,
}: {
  fields: import('@/lib/api').FieldDescriptor[]
}) {
  const navigate = useNavigate()
  const [type, setType] = useState<'movie' | 'tv'>('movie')
  const criaveisFields = criaveis(fields)
  const { mutation, mensagemErro } = useCriarEntidade(
    createItem,
    'Não foi possível criar o item.',
  )

  function onSubmit(valores: ValoresForm) {
    const title = String(valores.title ?? '').trim()
    if (!title) {
      return
    }
    mutation.mutate({
      type,
      title,
      original_title: str(valores.original_title),
      description: str(valores.description),
      release_date: str(valores.release_date),
    })
  }

  const header = (
    <label className="flex flex-col gap-1.5 text-sm">
      <span className="font-medium">Tipo</span>
      <Select
        value={type}
        onChange={(e) => setType(e.target.value as 'movie' | 'tv')}
        aria-label="Tipo do item"
        className="max-w-40"
      >
        <option value="movie">Filme</option>
        <option value="tv">Série</option>
      </Select>
    </label>
  )

  return (
    <SchemaForm
      fields={criaveisFields}
      defaultValues={valoresDeCriacao(criaveisFields)}
      schema={schemaZod(criaveisFields, 'criacao')}
      onSubmit={onSubmit}
      onCancel={() => navigate({ to: '/' })}
      submitLabel="Criar item"
      submitPendingLabel="Criando…"
      submitIcon={<FilmIcon aria-hidden />}
      isPending={mutation.isPending}
      mensagemErro={mensagemErro}
      header={header}
    />
  )
}

// str normaliza um valor de campo do form para o corpo do POST: string aparada e
// não-vazia, ou `undefined` (o opcional não vai no corpo).
function str(v: unknown): string | undefined {
  const t = String(v ?? '').trim()
  return t === '' ? undefined : t
}
