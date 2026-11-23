import { useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { FilmIcon } from 'lucide-react'
import { Select } from '@/components/ui/select'
import { SchemaForm } from '@/components/schema-form'
import { NewEntityPage } from '@/components/new-entity-page'
import { createItem } from '@/lib/api'
import { useCriarEntidade } from '@/lib/use-criar-entidade'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'
import { useT } from '@/lib/i18n'
import {
  OBRIGATORIO_NA_CRIACAO,
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
// Cross-link to the Search: whoever clicks "New item" expecting
// to import an already existing title (close to TMDB) lands here on a blank
// manual form with no hint that `/search` is the right path — the
// `CrossLinkBusca` block at the top, before the form, points there. The manual
// form stays as it is (registering what does not exist in an external source).
export function ItemNewScreen() {
  const t = useT()
  return (
    <NewEntityPage
      title={t('collections.item.title')}
      topo={<CrossLinkBusca />}
    >
      {(fields) => <ItemNewForm fields={fields} />}
    </NewEntityPage>
  )
}

function CrossLinkBusca() {
  const t = useT()
  return (
    <p className="mb-4 text-sm text-muted-foreground">
      {t('collections.item.crossLinkBefore')}{' '}
      <Link
        to="/search"
        className="font-medium text-foreground underline underline-offset-2"
      >
        {t('collections.item.crossLinkAction')}
      </Link>{' '}
      {t('collections.item.crossLinkAfter')}
    </p>
  )
}

function ItemNewForm({
  fields,
}: {
  fields: import('@/lib/api').FieldDescriptor[]
}) {
  const navigate = useNavigate()
  const t = useT()
  const [type, setType] = useState<'movie' | 'tv'>('movie')
  // "Leave without saving" guard: the creation screen used to navigate away on a
  // single tap of "Back to the library"/Cancel with the form half filled.
  const [sujo, setSujo] = useState(false)
  // While the creation is in flight the guard steps aside: the success redirect is
  // not a "leave without saving". A failure puts it back (the typed data stays).
  const [criando, setCriando] = useState(false)
  const guardaSaida = useUnsavedGuard(sujo && !criando)
  const criaveisFields = criaveis(fields)
  const { mutation, mensagemErro } = useCriarEntidade(
    createItem,
    t('collections.item.failed'),
  )

  async function onSubmit(valores: ValoresForm) {
    // The empty title never reaches the API: the SchemaForm marks the fields as
    // touched and shows the inline error + the summary at the top.
    setCriando(true)
    try {
      await mutation.mutateAsync({
        type,
        title: String(valores.title ?? '').trim(),
        original_title: str(valores.original_title),
        description: str(valores.description),
        release_date: str(valores.release_date),
      })
    } catch {
      // A failed creation leaves the owner on the form: the guard comes back so
      // the typed data is not lost by accident.
      setCriando(false)
    }
  }

  const header = (
    <label className="flex flex-col gap-1.5 text-sm">
      <span className="font-medium">{t('common.label.type')}</span>
      <Select
        value={type}
        onChange={(e) => setType(e.target.value as 'movie' | 'tv')}
        aria-label={t('collections.item.typeLabel')}
        className="max-w-40"
      >
        <option value="movie">{t('common.entityType.movie')}</option>
        <option value="tv">{t('common.entityType.tv')}</option>
      </Select>
    </label>
  )

  return (
    <>
      {guardaSaida}
      <SchemaForm
        fields={criaveisFields}
        defaultValues={valoresDeCriacao(criaveisFields)}
        schema={schemaZod(criaveisFields, 'criacao', t)}
        onSubmit={onSubmit}
        onCancel={() => navigate({ to: '/' })}
        submitLabel={t('collections.item.submit')}
        submitPendingLabel={t('collections.item.submitting')}
        submitIcon={<FilmIcon aria-hidden />}
        isPending={mutation.isPending}
        mensagemErro={mensagemErro}
        header={header}
        obrigatorios={OBRIGATORIO_NA_CRIACAO}
        onDirtyChange={setSujo}
      />
    </>
  )
}

// str normalizes a form field value for the POST body: a trimmed, non-empty
// string, or `undefined` (the optional one does not go in the body).
function str(v: unknown): string | undefined {
  const t = String(v ?? '').trim()
  return t === '' ? undefined : t
}
