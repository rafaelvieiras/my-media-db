import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { PosterCard } from '@/components/poster-card'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
import { useLanguage } from '@/lib/language-context'
import { entityMembersQueryOptions } from '@/lib/queries'
import { arteSrc, type EntityCard } from '@/lib/api'

// Shared core of the parent-with-children tree (GET /entities/{id}/members): the
// COLLECTION (editable) and the SERIES tree (read-only) reuse the
// same skeleton of states + card grid. Centralizing it here eliminates the duplication
// (jscpd) between `CollectionMembers` and `MembrosSomenteLeitura`.

// GradeMembros: the shell of states (loading/error/empty) and the card grid, WITHOUT
// the query — the owner of the query passes `itens`/`isPending`/`isError` and a `render` per
// card (the pure PosterCard, the editable member, or the numbered episode card).
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

// MembrosSomenteLeitura lists the CHILDREN of a node of the series tree as
// READ-ONLY cards: the seasons of a series, the episodes of a season. Phase 1
// does not edit the tree by hand (no add/remove — unlike the collection). The
// backend already returns /members sorted by `sort_index`; `numerar` marks the card of the
// episode to show its REAL number — read from `card.numbering` (projected
// in the ordering of the parent season), not the ordinal position in the list.
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
  // acao is an OPTIONAL slot to the right of the header — today the ordering selector of the
  // series stays next to "Temporadas" without duplicating the section. null = no action.
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

// CardTemporada is the card of a SEASON in the member list of the series: today it is
// only the PosterCard, which already renders the compact completeness badge ("X/Y", unit in the
// aria-label) in the top-right and leaves the EMPTY season without a badge, normal cover
// — the SAME shallow card logic shared with the series on the home. Kept as a
// named extension point of the series tree.
export function CardTemporada({ card }: { card: EntityCard }) {
  return <PosterCard card={card} />
}

// CardNumerado wraps the PosterCard with the REAL number of the episode, read from
// `card.numbering` (projected in the ordering of the parent season by the backend), NOT the
// position in the list. Label: "E{number}" with 2 digits (e.g.: E05); when there is an
// `absolute_number` distinct from the order number, it appends " · #{absolute}" (anime/
// continuous ordering). Without `numbering` (draft episode) → neutral label "Episódio",
// without inventing a position — a library with E01+E05 shows E01 and E05, not E01/E02.
//
// The ARTWORK of the episode is the 16:9 STILL, resolved by `arteSrc`: a CUSTOM still
// of the owner comes as a local route (it receives the base) and WINS; the provider one comes raw (via proxy)
// — its own framing, not the 2:3 poster frame, so as not to crop the landscape. Without a
// still (`still_url` absent → null) the PosterCard falls back to the "Sem arte" 16:9 placeholder.
export function CardNumerado({ card }: { card: EntityCard }) {
  const t = useT()
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">
        {rotuloEpisodio(card.numbering, t)}
      </span>
      <PosterCard
        card={card}
        artSrc={arteSrc(card.still_url)}
        aspectClass="aspect-video"
        artAlt={t('entity.alt.still', { title: card.title })}
      />
    </div>
  )
}

// rotuloEpisodio builds the label of the episode card from the numbering of the card:
// "E05" (number with 2 digits) and, when the absolute one exists and differs from the number of the
// order, "E05 · #128". Without numbering (draft) → neutral label of the type.
function rotuloEpisodio(
  numbering: EntityCard['numbering'],
  t: Translate,
): string {
  if (!numbering) {
    return t('common.entityType.episode')
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
