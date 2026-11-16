import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { ExternalIdsEditor } from '@/components/external-ids-editor'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { EntityEditEstadoCarga } from '@/components/query-states'
import { entityQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'

// Route `/entities/$id/edit/ids` (subroute of the item edit):
// the "IDs de terceiros" section — external identity (`external_ids`, MD4) with
// its own save/re-enrich, isolated from the basic information form.
// It only needs the entity (not the field descriptor, which is exclusive to the
// "Informações básicas" section).
export function EntityEditIdsScreen() {
  const { id } = useParams({ from: '/entities/$id/edit/ids' })
  const { lang } = useLanguage()

  const entityQuery = useQuery(entityQueryOptions(id, lang))

  // "Leave without saving" guard: only the "dirty" state of this block.
  const [sujo, setSujo] = useState(false)

  if (entityQuery.isPending || entityQuery.isError || !entityQuery.data) {
    return (
      <EntityEditEstadoCarga
        carregando={entityQuery.isPending}
        erro={entityQuery.error}
      />
    )
  }

  return (
    <EntityEditShell
      id={id}
      entity={entityQuery.data}
      secaoAtiva="ids"
      sujo={sujo}
    >
      <ExternalIdsEditor
        id={id}
        externalIds={entityQuery.data.external_ids}
        onDirtyChange={setSujo}
      />
    </EntityEditShell>
  )
}
