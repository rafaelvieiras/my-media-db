import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Check, ImageOff, TriangleAlert } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { posterSrc, type EntityCard } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useT } from '@/lib/i18n'
import {
  aspectPorTipo,
  ehDraft,
  estadoCompletude,
  precisaRevisao,
  rotuloTipo,
  temCompletude,
  unidadeCompletude,
} from '@/lib/entity-badges'
import {
  precisaAtencaoCatalogacao,
  rotuloTooltipCompletude,
} from '@/lib/catalog-completeness-badges'

// Poster card of the grid: artwork (with a placeholder fallback), title and badges of
// ownership ("na biblioteca") and review (low confidence / not identified).
// Clicking the artwork/title opens the detail of the entity; clicking the review badge
// goes to the curation queue (`/review`). The badges live in a SIBLING overlay of the
// card Link (not nested) so as not to produce an anchor inside an anchor — the overlay
// is `pointer-events-none` and only the badge link re-enables the click. The cover is NEVER
// removed: OWNERSHIP becomes a badge in the top-RIGHT (series/season → completeness
// badge; the rest → "na biblioteca"), and absent/empty = no badge.
// The REVIEW (attention) one stays in the top-LEFT.
// PosterCard optionally accepts its own ARTWORK and ASPECT: the
// EPISODE card passes the 16:9 still (via the image proxy) and `aspect-video` instead of the 2:3
// poster — the still is landscape and cropping it into the poster frame would look bad. An absent
// `artSrc` (undefined) keeps the classic behavior: local poster via `posterSrc(poster_url)`.
// `artSrc` present but null (e.g. episode without a still) falls back to the "Sem arte" placeholder in
// the requested aspect. WITHOUT an explicit `aspectClass`, the frame comes from `aspectPorTipo(card.type)`
// — album/artist render 1:1 in ANY grid (library, carousel, search,
// discography) without each callsite having to check the type; the other types stay 2:3.
export function PosterCard({
  card,
  artSrc,
  aspectClass,
  artAlt,
}: {
  card: EntityCard
  artSrc?: string | null
  aspectClass?: string
  artAlt?: string
}) {
  const t = useT()
  const [falhouArte, setFalhouArte] = useState(false)
  const src = artSrc !== undefined ? artSrc : posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte
  const aspecto = aspectClass ?? aspectPorTipo(card.type)

  return (
    <div className="group relative flex flex-col gap-2" data-slot="poster-card">
      <Link
        to="/entities/$id"
        params={{ id: card.id }}
        className="flex flex-col gap-2"
      >
        <div
          className={cn(
            'relative overflow-hidden rounded-xl bg-muted ring-1 ring-foreground/10',
            aspecto,
          )}
        >
          {mostrarPlaceholder ? (
            <div
              data-testid="poster-placeholder"
              className="flex h-full w-full flex-col items-center justify-center gap-1 text-muted-foreground"
            >
              <ImageOff className="size-8" aria-hidden />
              <span className="px-2 text-center text-xs">
                {t('media.poster.noArtwork')}
              </span>
            </div>
          ) : (
            <img
              src={src}
              alt={artAlt ?? t('media.poster.alt', { title: card.title })}
              loading="lazy"
              onError={() => setFalhouArte(true)}
              className="h-full w-full object-cover transition-transform group-hover:scale-[1.02]"
            />
          )}
        </div>
        <div className="flex flex-col gap-0.5">
          <span className="line-clamp-2 text-sm font-medium leading-snug">
            {card.title}
          </span>
          <span className="text-xs text-muted-foreground">
            {rotuloTipo(card.type, t)}
          </span>
        </div>
      </Link>
      <div className="pointer-events-none absolute top-1.5 left-1.5 flex flex-wrap gap-1">
        <EstadoMatchBadge card={card} />
        <IndicadorCompletudeCatalogacao card={card} />
      </div>
      <div className="pointer-events-none absolute top-1.5 right-1.5 flex flex-wrap justify-end gap-1">
        <IndicadorPosse card={card} />
      </div>
    </div>
  )
}

// IndicadorPosse chooses the OWNERSHIP badge of the top-right corner according to the
// type: series/season (aggregated ownership) show the completeness badge; the
// others (movie/episode/collection) show "na biblioteca" when owned. Empty or
// not-owned → no badge (clean cover). There are never both at the same time.
function IndicadorPosse({ card }: { card: EntityCard }) {
  if (temCompletude(card.type)) {
    return <SeloCompletude card={card} />
  }
  return <PosseBadge owned={card.owned} />
}

// SeloCompletude renders the SHALLOW ownership state of the card as a COMPACT badge (it fits
// over the cover) and TEXTUAL (never color only, accessible): COMPLETA (green, ✓ with
// aria-label "completa") or PARCIAL ("X/Y", with the full unit in the title/aria-label).
// The UNIT comes from the type: series → "temporadas", season → "episódios". EMPTY
// (owned_count==0) → no badge (clean cover); same for no ownership or total==0
// (movie/episode/collection never have a badge). Consistent with the SeloPosse of the detail.
export function SeloCompletude({ card }: { card: EntityCard }) {
  const t = useT()
  const estado = temCompletude(card.type)
    ? estadoCompletude(card.ownership)
    : null
  if (!estado || estado === 'vazia' || !card.ownership) {
    return null
  }
  const { owned_count, total_count } = card.ownership
  const unidade = unidadeCompletude(card.type, t)
  if (estado === 'completa') {
    return (
      <Badge
        className="w-fit border-transparent bg-emerald-600 px-1.5 text-white"
        aria-label={t('media.poster.complete')}
        title={t('media.poster.complete')}
      >
        <Check className="size-3.5" aria-hidden />
      </Badge>
    )
  }
  const rotulo = t('media.poster.owned', {
    owned: owned_count,
    total: total_count,
    unit: unidade,
  })
  return (
    <Badge
      variant="secondary"
      className="w-fit px-1.5 tabular-nums"
      aria-label={rotulo}
      title={rotulo}
    >
      {owned_count}/{total_count}
    </Badge>
  )
}

function PosseBadge({ owned }: { owned: boolean }) {
  const t = useT()
  if (!owned) {
    return null
  }
  return (
    <Badge className={cn('border-transparent bg-emerald-700 text-white')}>
      {t('media.poster.inLibrary')}
    </Badge>
  )
}

// Badge of the match state: draft (not identified) takes precedence over the
// low-confidence review alert. Neither of the two → no badge. Both are
// LINKS to the review queue (`/review`) — the signal of Phase 2 now leads to the action
// of Phase 3. `pointer-events-auto` re-enables the click inside the inert overlay.
function EstadoMatchBadge({ card }: { card: EntityCard }) {
  const t = useT()
  if (ehDraft(card.status)) {
    return (
      <Badge asChild variant="secondary" className="pointer-events-auto">
        <Link to="/dashboard/review">{t('media.poster.unidentified')}</Link>
      </Badge>
    )
  }
  if (precisaRevisao(card)) {
    return (
      <Badge asChild variant="destructive" className="pointer-events-auto">
        <Link to="/dashboard/review">{t('media.poster.review')}</Link>
      </Badge>
    )
  }
  return null
}

// IndicadorCompletudeCatalogacao signals, icon-only, that the item does not meet the
// CATALOGING completeness ruler — DIFFERENT from the ownership badge
// (`IndicadorPosse`/`SeloCompletude` above). It stays next to the review
// badge (top-LEFT), after it in the `flex-wrap`. `title`/`aria-label`
// list what is missing in PT-BR. No result (draft/not evaluated yet)
// or a complete item → no badge (clean cover, same principle as the ownership badge).
function IndicadorCompletudeCatalogacao({ card }: { card: EntityCard }) {
  const t = useT()
  if (!precisaAtencaoCatalogacao(card.catalog_completeness)) {
    return null
  }
  const rotulo = rotuloTooltipCompletude(card.catalog_completeness, t)
  return (
    <Badge
      variant="outline"
      className="pointer-events-none gap-1 px-1.5"
      aria-label={rotulo}
      title={rotulo}
    >
      <TriangleAlert className="size-3.5" aria-hidden />
    </Badge>
  )
}
