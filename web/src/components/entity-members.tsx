import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { PosterCard } from '@/components/poster-card'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'
import type { EntityCard } from '@/lib/api'

// Núcleo compartilhado da árvore pai-com-filhos (GET /entities/{id}/members): a
// COLEÇÃO (0025, editável) e a árvore de SÉRIES (0031, somente leitura) reusam o
// mesmo esqueleto de estados + grid de cards. Centralizar aqui zera a duplicação
// (jscpd) entre `CollectionMembers` e `MembrosSomenteLeitura`.

// GradeMembros: a casca de estados (carregando/erro/vazio) e o grid de cards, SEM
// a query — o dono da query passa `itens`/`isPending`/`isError` e um `render` por
// card (o PosterCard puro, o membro editável, ou o card numerado do episódio).
export function GradeMembros({
  itens,
  isPending,
  isError,
  carregando,
  erro,
  vazio,
  render,
}: {
  itens: EntityCard[] | undefined
  isPending: boolean
  isError: boolean
  carregando: string
  erro: string
  vazio: string
  render: (card: EntityCard, indice: number) => ReactNode
}) {
  if (isPending) {
    return <LoadingState label={carregando} />
  }
  if (isError) {
    return <ErrorState title={erro} />
  }
  if (!itens || itens.length === 0) {
    return <p className="text-sm text-muted-foreground">{vazio}</p>
  }
  return (
    <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">
      {itens.map((card, indice) => (
        <li key={card.id}>{render(card, indice)}</li>
      ))}
    </ul>
  )
}

// MembrosSomenteLeitura lista os FILHOS de um nó da árvore de séries (0031) como
// cards READ-ONLY: as temporadas de uma série, os episódios de uma temporada. A
// Fase 1 não edita a árvore à mão (sem add/remove — diferente da coleção). O
// backend já devolve /members ordenado por `sort_index`; `numerar` prefixa a
// posição ordinal (1,2,3…) ao card do episódio — o número na ordenação default,
// já que o DTO de card ainda não carrega o `numbering` multi-ordem.
export function MembrosSomenteLeitura({
  id,
  titulo,
  carregando,
  erro,
  vazio,
  numerar = false,
}: {
  id: string
  titulo: string
  carregando: string
  erro: string
  vazio: string
  numerar?: boolean
}) {
  const { lang } = useLanguage()
  const { data, isPending, isError } = useQuery(
    entityMembersQueryOptions(id, lang),
  )
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">{titulo}</h2>
      <GradeMembros
        itens={data}
        isPending={isPending}
        isError={isError}
        carregando={carregando}
        erro={erro}
        vazio={vazio}
        render={(card, indice) =>
          numerar ? (
            <CardNumerado card={card} numero={indice + 1} />
          ) : (
            <PosterCard card={card} />
          )
        }
      />
    </section>
  )
}

// CardNumerado envolve o PosterCard com o número do episódio (posição ordinal na
// ordenação default). Mínimo viável da Fase 1: número + título — o número vem da
// posição em /members (ordenado por sort_index no backend), não do DTO do card.
function CardNumerado({ card, numero }: { card: EntityCard; numero: number }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">
        Episódio {numero}
      </span>
      <PosterCard card={card} />
    </div>
  )
}
