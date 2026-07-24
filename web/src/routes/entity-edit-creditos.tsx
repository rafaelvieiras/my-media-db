import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { CreditsEditor } from '@/components/credits-editor'
import { EntityEditShell } from '@/components/entity-edit-shell'
import { EntityEditEstadoCarga } from '@/components/query-states'
import { entityQueryOptions, overrideBlobQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'

// Rota `/entities/$id/edit/creditos` (briefing 0080, subrota da edição de item
// 0030): a seção "Elenco e equipe" — `CreditsEditor` (0029 fase C, reordenação em
// lote do 0076), isolada do formulário de informações básicas. Precisa da
// entity (cast/crew/companies) e do blob de override (baseline do que já foi
// salvo), mas não do descritor de campos.
export function EntityEditCreditosScreen() {
  const { id } = useParams({ from: '/entities/$id/edit/creditos' })
  const { lang } = useLanguage()

  const entityQuery = useQuery(entityQueryOptions(id, lang))
  const blobQuery = useQuery(overrideBlobQueryOptions(id))

  // Guard de "sair sem salvar" (0073/0080): só o "sujo" deste bloco.
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
