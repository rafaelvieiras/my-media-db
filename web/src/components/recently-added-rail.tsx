import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { entitiesQueryOptions, entityTypesQueryOptions } from '@/lib/queries'
import { PosterCard } from '@/components/poster-card'
import { ErrorState, LoadingState } from '@/components/query-states'
import { rotuloTipoPlural } from '@/lib/entity-badges'
import { tipoParaSlug } from '@/lib/library-slugs'
import { useT } from '@/lib/i18n'
import {
  Carousel,
  CarouselContent,
  CarouselItem,
  CarouselNext,
  CarouselPrevious,
} from '@/components/ui/carousel'

const ITENS_POR_CARROSSEL = 10

// RecentlyAddedRail is the "recently added" carousel of ONE schema:
// it fires ITS OWN useQuery, independent of any
// other instance — an explicit requirement of the owner ("each slide should fetch
// separately"), not a parent that fetches everything at once and distributes. It sorts
// by created_at desc (`sort: '-created_at'`), up to 10 items, only what is IN THE
// library (`owned: true`). It reuses the PosterCard already used by the grid
// (same EntityCard, same ownership/review badges) and the shadcn Carousel
// (embla) — without new cards nor a carousel written from scratch.
//
// Empty: when the query comes back empty the component renders NOTHING (not even the
// title) — the home (`routes/index.tsx`) already mounts only one rail per `type`
// returned by `GET /entities/types` (which filters out types without data), but this
// component is also defensive on its own, so that a phantom "Adicionados recentemente"
// section never remains.
export function RecentlyAddedRail({
  type,
  label,
  lang,
}: {
  type: string
  label: string
  lang?: string
}) {
  const t = useT()
  const { data, isPending, isError, isSuccess, error } = useQuery(
    entitiesQueryOptions(
      { type, sort: '-created_at', limit: ITENS_POR_CARROSSEL, owned: true },
      lang,
    ),
  )

  if (isSuccess && data.items.length === 0) {
    return null
  }

  return (
    <section className="flex flex-col gap-3">
      {isPending ? (
        <>
          <RailTitulo type={type} label={label} />
          <LoadingState
            label={t('media.rail.loading', { label: label.toLowerCase() })}
          />
        </>
      ) : isError ? (
        <>
          <RailTitulo type={type} label={label} />
          <ErrorState
            title={t('media.rail.loadError', { label })}
            message={error instanceof Error ? error.message : undefined}
          />
        </>
      ) : (
        <Carousel opts={{ align: 'start' }} className="w-full">
          <div className="mb-3 flex items-center justify-between gap-3">
            <RailTitulo type={type} label={label} />
            <div className="flex gap-1">
              <CarouselPrevious className="static translate-y-0" />
              <CarouselNext className="static translate-y-0" />
            </div>
          </div>
          <CarouselContent>
            {data.items.map((card) => (
              <CarouselItem
                key={card.id}
                className="basis-1/2 sm:basis-1/3 md:basis-1/4 lg:basis-1/6"
              >
                <PosterCard card={card} />
              </CarouselItem>
            ))}
          </CarouselContent>
        </Carousel>
      )}
    </section>
  )
}

// RailTitulo (ADR-0030) gains a "Ver Todos" link next to the
// title, pointing to the route `/library/$entitySlug` of the type of that
// carousel — the same slug↔type map as `app-header.tsx` (`library-slugs.ts`).
// A type without a mapped slug (defensive — today `GET /entities/types` already excludes
// sub-nodes, so this should not happen in practice) only omits the link, without
// breaking the title.
function RailTitulo({ type, label }: { type: string; label: string }) {
  const t = useT()
  const slug = tipoParaSlug(type)
  return (
    <div className="flex items-center gap-3">
      <h2 className="font-heading text-lg font-medium">{label}</h2>
      {slug ? (
        <Link
          to="/library/$entitySlug"
          params={{ entitySlug: slug }}
          className="text-sm text-muted-foreground hover:text-foreground hover:underline"
        >
          {t('media.rail.viewAll')}
        </Link>
      ) : null}
    </div>
  )
}

// RecentlyAddedSection is the home section: it fetches the `type`s
// with cataloged data (`GET /entities/types`, `entityTypesQueryOptions`) and
// mounts one RecentlyAddedRail per type — WITHOUT a fixed list of types hardcoded
// here, it covers any new vertical (e.g. music) as soon as the backend has
// data for it. The label of each section comes from `rotuloTipoPlural` (central map
// of `entity-badges.ts`, shared with the rest of the UI). Empty library
// (`GET /entities/types` returns `[]`) → the whole section renders nothing
// (no phantom "Adicionados recentemente" header). An error in the discovery of
// types also renders nothing: it is a discreet failure — the paginated grid
// below keeps working normally and already has its own error state.
export function RecentlyAddedSection({ lang }: { lang?: string } = {}) {
  const { data, isSuccess } = useQuery(entityTypesQueryOptions)
  const t = useT()

  if (!isSuccess || data.length === 0) {
    return null
  }

  return (
    <div className="mb-8 flex flex-col gap-8">
      {data.map((tipo) => (
        <RecentlyAddedRail
          key={tipo.type}
          type={tipo.type}
          label={rotuloTipoPlural(tipo.type, t)}
          lang={lang}
        />
      ))}
    </div>
  )
}
