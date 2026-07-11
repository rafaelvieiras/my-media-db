import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { FilmIcon } from 'lucide-react'
import { Select } from '@/components/ui/select'
import { SchemaForm } from '@/components/schema-form'
import { NewEntityPage } from '@/components/new-entity-page'
import { createItem } from '@/lib/api'
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
  return (
    <NewEntityPage title="Novo item">
      {(fields) => <ItemNewForm fields={fields} />}
    </NewEntityPage>
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
