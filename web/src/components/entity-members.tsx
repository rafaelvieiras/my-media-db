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
// backend já devolve /members ordenado por `sort_index`; `numerar` marca o card do
// episódio para exibir seu número REAL (0039) — lido de `card.numbering` (projetado
// na ordenação da temporada-pai), não a posição ordinal na lista.
export function MembrosSomenteLeitura({
  id,
  titulo,
  carregando,
  erro,
  vazio,
  numerar = false,
  acao,
}: {
  id: string
  titulo: string
  carregando: string
  erro: string
  vazio: string
  numerar?: boolean
  // acao é um slot OPCIONAL à direita do cabeçalho — hoje o seletor de ordenação da
  // série (0040) fica ao lado de "Temporadas" sem duplicar a seção. null = sem ação.
  acao?: ReactNode
}) {
  const { lang } = useLanguage()
  const { data, isPending, isError } = useQuery(
    entityMembersQueryOptions(id, lang),
  )
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-heading text-lg font-medium">{titulo}</h2>
        {acao}
      </div>
      <GradeMembros
        itens={data}
        isPending={isPending}
        isError={isError}
        carregando={carregando}
        erro={erro}
        vazio={vazio}
        render={(card) => {
          if (numerar) {
            return <CardNumerado card={card} />
          }
          if (card.type === 'season') {
            return <CardTemporada card={card} />
          }
          return <PosterCard card={card} />
        }}
      />
    </section>
  )
}

// CardTemporada é o card de uma TEMPORADA na lista de membros da série (0036): hoje é
// só o PosterCard, que já renderiza o selo de completude compacto ("X/Y", unidade no
// aria-label) no topo-direito e deixa a temporada VAZIA sem badge, capa normal (0038)
// — a MESMA lógica de card raso compartilhada com a série na home (0037). Mantido como
// ponto de extensão nomeado da árvore de séries.
export function CardTemporada({ card }: { card: EntityCard }) {
  return <PosterCard card={card} />
}

// CardNumerado envolve o PosterCard com o número REAL do episódio (0039), lido de
// `card.numbering` (projetado na ordenação da temporada-pai pelo backend), NÃO a
// posição na lista. Rótulo: "E{number}" com 2 dígitos (ex.: E05); quando há
// `absolute_number` distinto do número da ordem, anexa " · #{absolute}" (anime/
// ordenação contínua). Sem `numbering` (episódio draft) → rótulo neutro "Episódio",
// sem inventar posição — biblioteca com E01+E05 mostra E01 e E05, não E01/E02.
export function CardNumerado({ card }: { card: EntityCard }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">
        {rotuloEpisodio(card.numbering)}
      </span>
      <PosterCard card={card} />
    </div>
  )
}

// rotuloEpisodio monta o rótulo do card de episódio a partir do numbering do card:
// "E05" (número com 2 dígitos) e, quando o absoluto existe e difere do número da
// ordem, "E05 · #128". Sem numbering (draft) → "Episódio" neutro.
function rotuloEpisodio(numbering: EntityCard['numbering']): string {
  if (!numbering) {
    return 'Episódio'
  }
  const base = `E${String(numbering.number).padStart(2, '0')}`
  if (
    numbering.absolute_number != null &&
    numbering.absolute_number !== numbering.number
  ) {
    return `${base} · #${numbering.absolute_number}`
  }
  return base
}
