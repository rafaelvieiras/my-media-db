import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { SchemaForm } from '@/components/schema-form'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { EntityEditEstadoCarga } from '@/components/query-states'
import {
  ApiError,
  clearOverrideField,
  patchOverride,
  type FieldDescriptor,
  type OverrideBlob,
  type OverridePatch,
  type ResolvedEntity,
} from '@/lib/api'
import {
  entityFieldsQueryOptions,
  entityQueryOptions,
  overrideBlobQueryOptions,
} from '@/lib/queries'
import { useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
import { useLanguage } from '@/lib/language-context'
import {
  construirPatchEdicao,
  ordenados,
  schemaZod,
  valoresDeEdicao,
  type ValoresForm,
} from '@/lib/field-schema'

// Route `/entities/$id/edit` (split into subroutes later): the
// "Informações básicas" section — the form DRIVEN BY DESCRIPTORS, the default
// of the item edit (it keeps the usual URL, deep links do not break). It preserves the
// per-language semantics of the old editor:
//   - TRANSLATABLE fields edit the current reading language; NEUTRAL ones apply to all;
//   - an input emptied on Save sends `null` (SET EMPTY); "Limpar" is a DELETE per
//     language/of the key (it INHERITS again) — distinct actions;
//   - tags as {add,remove} relative to the value resolved on open.
// The shared sidebar/header live in the `EntityEditShell`; external IDs and
// cast/crew are now subroutes of their own (`entity-edit-ids.tsx`/
// `entity-edit-creditos.tsx`). A successful override goes back to `/entities/$id`;
// Cancel goes back without saving; an absent id → 404 state.
export function EntityEditBasicoScreen() {
  const { id } = useParams({ from: '/entities/$id/edit' })
  const { lang } = useLanguage()

  const entityQuery = useQuery(entityQueryOptions(id, lang))
  const blobQuery = useQuery(overrideBlobQueryOptions(id))
  const fieldsQuery = useQuery(entityFieldsQueryOptions)

  // "Leave without saving" guard: only the "dirty" state of this block — the subroute
  // mounts a single block, so there are no longer 3 booleans to combine.
  const [sujo, setSujo] = useState(false)

  const carregando =
    entityQuery.isPending || blobQuery.isPending || fieldsQuery.isPending
  const comErro =
    entityQuery.isError || blobQuery.isError || fieldsQuery.isError

  if (carregando || comErro || !entityQuery.data) {
    return (
      <EntityEditEstadoCarga carregando={carregando} erro={entityQuery.error} />
    )
  }

  return (
    <EntityEditShell
      id={id}
      entity={entityQuery.data}
      secaoAtiva="basico"
      sujo={sujo}
    >
      <EntityEditBasicoForm
        id={id}
        lang={lang}
        entity={entityQuery.data}
        blob={blobQuery.data!}
        fields={fieldsQuery.data!}
        onDirtyChange={setSujo}
        // It remounts the form when the language changes or the blob revalidates (after "Limpar"),
        // recomposing the seeds — the same pattern as the old editor.
        key={`${lang}:${blobQuery.dataUpdatedAt}`}
      />
    </EntityEditShell>
  )
}

function EntityEditBasicoForm({
  id,
  lang,
  entity,
  blob,
  fields,
  onDirtyChange,
}: {
  id: string
  lang: string
  entity: ResolvedEntity
  blob: OverrideBlob
  fields: FieldDescriptor[]
  onDirtyChange: (sujo: boolean) => void
}) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const t = useT()

  const sementes = valoresDeEdicao(fields, blob, lang, entity)

  // Invalidation after the edit: `['entity', id]` is a PREFIX — it drops the entity in all
  // the `lang` variations, the files and the override blob; `['entities']`
  // updates the cards of the list.
  function invalidar() {
    qc.invalidateQueries({ queryKey: ['entity', id] })
    qc.invalidateQueries({ queryKey: ['entities'] })
  }

  const salvar = useMutation({
    mutationFn: (patch: OverridePatch) => patchOverride(id, lang, patch),
    onSuccess: () => {
      invalidar()
      navigate({ to: '/entities/$id', params: { id } })
    },
  })

  // "Limpar": with `lang` for a translatable field (it inherits again in that language), without
  // `lang` for a neutral one (it removes the key). It does not navigate — the revalidation of the blob
  // remounts the form (via `key`) with the inherited value.
  const limpar = useMutation({
    mutationFn: ({
      campo,
      traduzivel,
    }: {
      campo: string
      traduzivel: boolean
    }) => clearOverrideField(id, campo, traduzivel ? lang : undefined),
    onSuccess: invalidar,
  })

  const ocupado = salvar.isPending || limpar.isPending

  function onSubmit(valores: ValoresForm) {
    const patch = construirPatchEdicao(fields, valores, sementes)
    if (Object.keys(patch).length === 0) {
      // Nothing changed — it goes back to the detail without hitting the API.
      navigate({ to: '/entities/$id', params: { id } })
      return
    }
    salvar.mutate(patch)
  }

  const mensagemErro =
    salvar.error instanceof ApiError
      ? salvar.error.message
      : limpar.error instanceof ApiError
        ? limpar.error.message
        : salvar.isError || limpar.isError
          ? t('entity.edit.basic.saveError')
          : null

  return (
    <SchemaForm
      fields={ordenados(fields)}
      defaultValues={sementes}
      schema={schemaZod(fields, 'edicao')}
      onSubmit={onSubmit}
      onCancel={() => navigate({ to: '/entities/$id', params: { id } })}
      submitLabel={t('entity.edit.basic.saveChanges')}
      submitPendingLabel={t('common.action.saving')}
      isPending={ocupado}
      mensagemErro={mensagemErro}
      placeholders={placeholdersDe(fields, entity, t)}
      onClear={(campo, traduzivel) => limpar.mutate({ campo, traduzivel })}
      clearDisabled={ocupado}
      lang={lang}
      onDirtyChange={onDirtyChange}
    />
  )
}

// placeholdersDe builds the inherited/resolved text per field (shown when the
// input is empty), iterating the descriptors: translatable/neutral ones use the
// resolved value; the local rating shows the rating of the SOURCE (distinct from yours). Fields without a
// resolved value fall back to the default of the descriptor itself.
function placeholdersDe(
  fields: FieldDescriptor[],
  entity: ResolvedEntity,
  t: Translate,
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const d of fields) {
    if (d.name === 'local_rating') {
      out[d.name] =
        entity.vote_average != null
          ? t('entity.edit.basic.sourceRating', {
              value: entity.vote_average.toFixed(1),
            })
          : t('entity.edit.basic.noRating')
      continue
    }
    const valor = (entity as unknown as Record<string, unknown>)[d.name]
    if (typeof valor === 'string' && valor !== '') {
      out[d.name] = valor
    } else if (d.widget === 'date') {
      out[d.name] = t('entity.edit.basic.datePlaceholder')
    }
  }
  return out
}
