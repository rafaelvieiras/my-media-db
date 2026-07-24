import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { ExternalIdsEditor } from '@/components/external-ids-editor'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { EntityEditEstadoCarga } from '@/components/query-states'
import { entityQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'

// Rota `/entities/$id/edit/ids` (briefing 0080, subrota da edição de item 0030):
// a seção "IDs de terceiros" — identidade externa (`external_ids`, MD4, 0027) com
// seu próprio salvar/re-enriquecer, isolada do formulário de informações básicas.
// Só precisa da entity (não do descritor de campos, que é exclusivo da seção
// "Informações básicas").
export function EntityEditIdsScreen() {
  const { id } = useParams({ from: '/entities/$id/edit/ids' })
  const { lang } = useLanguage()

  const entityQuery = useQuery(entityQueryOptions(id, lang))

  // Guard de "sair sem salvar" (0073/0080): só o "sujo" deste bloco.
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
