import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { FolderPlus } from 'lucide-react'
import { SchemaForm } from '@/components/schema-form'
import { NewEntityPage } from '@/components/new-entity-page'
import { createCollection, type FieldDescriptor } from '@/lib/api'
import { useCriarEntidade } from '@/lib/use-criar-entidade'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'
import { useT } from '@/lib/i18n'
import {
  OBRIGATORIO_NA_CRIACAO,
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
  const t = useT()
  return (
    <NewEntityPage title={t('collections.create.title')}>
      {(fields) => <CollectionNewForm fields={fields} />}
    </NewEntityPage>
  )
}

function CollectionNewForm({ fields }: { fields: FieldDescriptor[] }) {
  const t = useT()
  const navigate = useNavigate()
  const camposColecao = fields.filter((f) => CAMPOS_COLECAO.includes(f.name))
  const { mutation, mensagemErro } = useCriarEntidade(
    createCollection,
    t('collections.create.failed'),
  )
  // "Leave without saving" guard, same as the item creation.
  const [sujo, setSujo] = useState(false)
  // Same guard wiring as the item creation: off while the write is in flight, back
  // when it fails (the success redirect is not a "leave without saving").
  const [criando, setCriando] = useState(false)
  const guardaSaida = useUnsavedGuard(sujo && !criando)

  async function onSubmit(valores: ValoresForm) {
    // An empty title is refused by the form validation (inline error + summary),
    // never by a silent `return`.
    setCriando(true)
    const description = String(valores.description ?? '').trim()
    try {
      await mutation.mutateAsync({
        title: String(valores.title ?? '').trim(),
        description: description || undefined,
      })
    } catch {
      setCriando(false)
    }
  }

  return (
    <>
      {guardaSaida}
      <SchemaForm
        fields={camposColecao}
        defaultValues={valoresDeCriacao(camposColecao)}
        schema={schemaZod(camposColecao, 'criacao', t)}
        onSubmit={onSubmit}
        onCancel={() => navigate({ to: '/' })}
        submitLabel={t('collections.create.submit')}
        submitPendingLabel={t('collections.create.submitting')}
        submitIcon={<FolderPlus aria-hidden />}
        isPending={mutation.isPending}
        mensagemErro={mensagemErro}
        obrigatorios={OBRIGATORIO_NA_CRIACAO}
        onDirtyChange={setSujo}
      />
    </>
  )
}
