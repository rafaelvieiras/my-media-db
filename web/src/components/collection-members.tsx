import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CollectionMemberPicker } from '@/components/collection-member-picker'
import { GradeMembros } from '@/components/entity-members'
import { PosterCard } from '@/components/poster-card'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'
import { removeCollectionMember, type EntityCard } from '@/lib/api'

// CollectionMembers lists the ITEMS of a collection: the entities linked by
// entity_relation (kind='collection'), served by the backend as internal cards
// (EntityCard) ordered by year. It reuses the `PosterCard` of the library — each member
// navigates to `/entities/{id}` (INTERNAL destination, not /external). Used by the
// entity screen when `type==='collection'`, in place of similars. It handles loading/error/
// empty. The active language enters the search (re-fetch on change).
//
// `editable` turns on the EDITING of members: "Adicionar item" button (it opens the
// catalog picker) and a remove control on each card. Add/remove return the
// already UPDATED list, written straight into the cache (`setQueryData`) — without re-fetching.
export function CollectionMembers({
  id,
  editable = false,
}: {
  id: string
  editable?: boolean
}) {
  const { lang } = useLanguage()
  const queryClient = useQueryClient()
  const membersKey = entityMembersQueryOptions(id, lang).queryKey
  const { data, isPending, isError } = useQuery(
    entityMembersQueryOptions(id, lang),
  )
  const [adicionando, setAdicionando] = useState(false)

  // The add/remove list comes back ready from the backend (ordered by year) — it writes to the
  // cache of the current key, reflected immediately without a 2nd round trip.
  function atualizar(membros: EntityCard[]) {
    queryClient.setQueryData(membersKey, membros)
  }

  const idsAtuais = new Set((data ?? []).map((c) => c.id))

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-heading text-lg font-medium">Itens da coleção</h2>
        {editable && !adicionando ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setAdicionando(true)}
          >
            <Plus aria-hidden />
            Adicionar item
          </Button>
        ) : null}
      </div>

      {editable && adicionando ? (
        <CollectionMemberPicker
          collectionId={id}
          excludeIds={idsAtuais}
          onAdded={atualizar}
          onClose={() => setAdicionando(false)}
        />
      ) : null}

      <GradeMembros
        itens={data}
        isPending={isPending}
        isError={isError}
        carregando="Carregando itens da coleção…"
        erro="Não foi possível carregar os itens da coleção"
        vazio="Esta coleção ainda não tem itens."
        render={(card) =>
          editable ? (
            <MembroEditavel
              colecaoId={id}
              card={card}
              onAtualizar={atualizar}
            />
          ) : (
            <PosterCard card={card} />
          )
        }
      />
    </section>
  )
}

// MembroEditavel wraps the PosterCard with the REMOVE control. The
// remove button is a SIBLING of the card (overlay), not nested in the <a> of the PosterCard —
// clicking does not navigate. Removal in two steps (× → "Remover?") to avoid mistakes.
function MembroEditavel({
  colecaoId,
  card,
  onAtualizar,
}: {
  colecaoId: string
  card: EntityCard
  onAtualizar: (membros: EntityCard[]) => void
}) {
  const [confirmando, setConfirmando] = useState(false)
  const mutation = useMutation({
    mutationFn: () => removeCollectionMember(colecaoId, card.id),
    onSuccess: (membros) => onAtualizar(membros),
  })

  return (
    <div className="relative">
      <PosterCard card={card} />
      <div className="absolute top-1.5 right-1.5">
        {confirmando ? (
          <div className="flex items-center gap-1 rounded-lg bg-card/95 p-1 ring-1 ring-foreground/10">
            <Button
              variant="destructive"
              size="xs"
              disabled={mutation.isPending}
              onClick={() => mutation.mutate()}
            >
              {mutation.isPending ? 'Removendo…' : 'Remover?'}
            </Button>
            <Button
              variant="ghost"
              size="xs"
              disabled={mutation.isPending}
              onClick={() => setConfirmando(false)}
            >
              Cancelar
            </Button>
          </div>
        ) : (
          <Button
            variant="secondary"
            size="icon-sm"
            aria-label={`Remover ${card.title} da coleção`}
            onClick={() => setConfirmando(true)}
          >
            <Trash2 aria-hidden />
          </Button>
        )}
      </div>
    </div>
  )
}
