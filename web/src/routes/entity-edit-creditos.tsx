import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { CreditsEditor } from '@/components/credits-editor'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { EntityEditEstadoCarga } from '@/components/query-states'
import { entityQueryOptions, overrideBlobQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'

// Route `/entities/$id/edit/creditos` (subroute of the item edit):
// the "Elenco e equipe" section — `CreditsEditor` (batch reordering),
// isolated from the basic information form. It needs the
// entity (cast/crew/companies) and the override blob (baseline of what was already
// saved), but not the field descriptor.
export function EntityEditCreditosScreen() {
  const { id } = useParams({ from: '/entities/$id/edit/creditos' })
  const { lang } = useLanguage()

  const entityQuery = useQuery(entityQueryOptions(id, lang))
  const blobQuery = useQuery(overrideBlobQueryOptions(id))

  // "Leave without saving" guard: only the "dirty" state of this block.
  const [sujo, setSujo] = useState(false)

  const carregando = entityQuery.isPending || blobQuery.isPending
  const comErro = entityQuery.isError || blobQuery.isError

  if (carregando || comErro || !entityQuery.data || !blobQuery.data) {
    return (
      <EntityEditEstadoCarga carregando={carregando} erro={entityQuery.error} />
    )
  }

  return (
    <EntityEditShell
      id={id}
      entity={entityQuery.data}
      secaoAtiva="creditos"
      sujo={sujo}
    >
      <CreditsEditor
        id={id}
        lang={lang}
        entity={entityQuery.data}
        blob={blobQuery.data}
        onDirtyChange={setSujo}
      />
    </EntityEditShell>
  )
}
