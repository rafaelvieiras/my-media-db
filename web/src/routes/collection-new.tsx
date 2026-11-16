import { useNavigate } from '@tanstack/react-router'
import { FolderPlus } from 'lucide-react'
import { SchemaForm } from '@/components/schema-form'
import { NewEntityPage } from '@/components/new-entity-page'
import { createCollection, type FieldDescriptor } from '@/lib/api'
import { useCriarEntidade } from '@/lib/use-criar-entidade'
import {
  schemaZod,
  valoresDeCriacao,
  type ValoresForm,
} from '@/lib/field-schema'

// Route `/new/collection`: creates a LOCAL collection from scratch. A
// collection only exposes a title (required) + description — a subset of the
// generic editorial fields that apply to it (the backend `createCollection` only
// accepts those two). On creation, it navigates to `/entities/{id}` (the collection page,
// where the owner adds members). The 422 (empty title) appears inline.
const CAMPOS_COLECAO = ['title', 'description']

export function CollectionNewScreen() {
  return (
    <NewEntityPage title="Nova coleção">
      {(fields) => <CollectionNewForm fields={fields} />}
    </NewEntityPage>
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
