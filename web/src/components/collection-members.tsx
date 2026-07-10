import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CollectionMemberPicker } from '@/components/collection-member-picker'
import { PosterCard } from '@/components/poster-card'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'
import { removeCollectionMember, type EntityCard } from '@/lib/api'

// CollectionMembers lista os ITENS de uma coleção (0025): as entities ligadas por
// entity_relation (kind='collection'), servidas pelo backend como cards internos
// (EntityCard) ordenados por ano. Reusa o `PosterCard` da biblioteca — cada membro
// navega para `/entities/{id}` (destino INTERNO, não /external). Usado pela tela da
// entity quando `type==='collection'`, no lugar de similares. Trata carregando/erro/
// vazio. O idioma ativo entra na busca (re-busca ao trocar).
//
// `editable` (0025 Fase B) liga a EDIÇÃO de membros: botão "Adicionar item" (abre o
// picker do catálogo) e um controle de remover em cada card. Add/remove devolvem a
// lista já ATUALIZADA, gravada direto no cache (`setQueryData`) — sem re-buscar.
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

  // A lista de add/remove volta pronta do backend (ordenada por ano) — grava no
  // cache da chave corrente, refletindo na hora sem uma 2ª ida.
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

      {isPending ? (
        <LoadingState label="Carregando itens da coleção…" />
      ) : isError ? (
        <ErrorState title="Não foi possível carregar os itens da coleção" />
      ) : data.length > 0 ? (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">
          {data.map((card) => (
            <li key={card.id}>
              {editable ? (
                <MembroEditavel
                  colecaoId={id}
                  card={card}
                  onAtualizar={atualizar}
                />
              ) : (
                <PosterCard card={card} />
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">
          Esta coleção ainda não tem itens.
        </p>
      )}
    </section>
  )
}

// MembroEditavel envolve o PosterCard com o controle de REMOVER (0025 Fase B). O
// botão de remover é um IRMÃO do card (overlay), não aninhado no <a> do PosterCard —
// clicar não navega. Remoção em duas etapas (× → "Remover?") para evitar engano.
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
