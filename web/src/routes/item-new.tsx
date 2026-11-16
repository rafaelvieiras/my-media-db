import { useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
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

// Route `/new/item`: creates a LOCAL item (movie/series) from scratch, without an
// external source. The form of the editorial fields is DRIVEN BY THE DESCRIPTORS
// (`creatable` subset, flat body); the Type selector (movie/tv) is NOT an
// editorial field, so the route injects it at the top of the form. On creation, it navigates to
// `/entities/{id}`, where the owner fixes the external IDs and re-enriches.
//
// Cross-link to the Search: whoever clicks "Novo item" expecting
// to import an already existing title (close to TMDB) lands here on a blank
// manual form with no hint that `/search` is the right path — the
// `CrossLinkBusca` block at the top, before the form, points there. The manual
// form stays as it is (registering what does not exist in an external source).
export function ItemNewScreen() {
  return (
    <NewEntityPage title="Novo item" topo={<CrossLinkBusca />}>
      {(fields) => <ItemNewForm fields={fields} />}
    </NewEntityPage>
  )
}

function CrossLinkBusca() {
  return (
    <p className="mb-4 text-sm text-muted-foreground">
      Procurando um título existente?{' '}
      <Link
        to="/search"
        className="font-medium text-foreground underline underline-offset-2"
      >
        Use a Busca
      </Link>{' '}
      para importar direto do catálogo.
    </p>
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

// str normalizes a form field value for the POST body: a trimmed, non-empty
// string, or `undefined` (the optional one does not go in the body).
function str(v: unknown): string | undefined {
  const t = String(v ?? '').trim()
  return t === '' ? undefined : t
}
