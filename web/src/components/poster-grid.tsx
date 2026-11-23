import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { entitiesQueryOptions } from '@/lib/queries'
import type { EntityListParams } from '@/lib/api'
import { PosterCard } from '@/components/poster-card'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { useT } from '@/lib/i18n'

const POR_PAGINA = 24

type FiltroPosse = 'all' | 'owned' | 'not'

// Translates the UI filters into the API params (undefined = no ownership
// filter). `tipo` is always FIXED (ADR-0030 — see the comment of the
// component below), never "all types" here.
function montarParams(
  tipo: string,
  posse: FiltroPosse,
  pagina: number,
): EntityListParams {
  return {
    type: tipo,
    owned: posse === 'all' ? undefined : posse === 'owned',
    limit: POR_PAGINA,
    offset: pagina * POR_PAGINA,
    sort: 'title',
  }
}

// Poster grid of the catalog: pagination, ownership filter and the three clear
// states (loading / error / empty). Since ADR-0030, the TYPE
// arrives FIXED via prop (`type`, resolved from the URL slug by the route
// `/library/$entitySlug` — see `routes/library-browse.tsx`), instead of the search
// param `?type=` of the former `/` route: the home no longer has a full
// grid, only carousels (`RecentlyAddedSection`), and the paginated listing of
// each type now lives on a screen of its own per type — which is why there is no
// type selector (`MediaTypeFilter`) inside the grid anymore, the type already comes fixed from the
// route. Ownership stays local (internal state of the screen).
export function PosterGrid({
  lang,
  type,
  page = 0,
  onPageChange,
}: {
  lang?: string
  type: string
  // Zero-based page, CONTROLLED by the route (the page lives in the URL search param, so
  // back/refresh keep the position — see `routes/library-browse.tsx`). Defaults to the
  // first page so the component stays usable on its own.
  page?: number
  onPageChange?: (page: number) => void
}) {
  const t = useT()
  const [posse, setPosse] = useState<FiltroPosse>('all')
  const pagina = Math.max(0, page)

  const params = montarParams(type, posse, pagina)
  const { data, isPending, isError, error } = useQuery(
    entitiesQueryOptions(params, lang),
  )

  // A filter change starts over on the first page (otherwise the offset of a far page
  // lands past the filtered total).
  function reiniciarPagina<T>(setter: (v: T) => void) {
    return (v: T) => {
      onPageChange?.(0)
      setter(v)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">
            {t('media.grid.ownership')}
          </span>
          <Select
            aria-label={t('media.grid.ownershipFilter')}
            value={posse}
            onChange={(e) =>
              reiniciarPagina(setPosse)(e.target.value as FiltroPosse)
            }
            className="w-40"
          >
            <option value="all">{t('media.grid.ownershipAll')}</option>
            <option value="owned">{t('media.grid.ownershipOwned')}</option>
            <option value="not">{t('media.grid.ownershipNotOwned')}</option>
          </Select>
        </label>
      </div>

      {isPending ? (
        <LoadingState label={t('media.grid.loading')} />
      ) : isError ? (
        <ErrorState
          title={t('media.grid.loadError')}
          message={error instanceof Error ? error.message : undefined}
        />
      ) : data.items.length === 0 ? (
        <EmptyState
          title={t('media.grid.emptyTitle')}
          description={t('media.grid.emptyDescription')}
          action={
            <Button asChild variant="outline" size="sm">
              <Link to="/dashboard/library">
                {t('media.grid.goToLibraries')}
              </Link>
            </Button>
          }
        />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
            {data.items.map((card) => (
              <PosterCard key={card.id} card={card} />
            ))}
          </div>
          <Paginacao
            total={data.total}
            offset={data.offset}
            limit={data.limit}
            page={pagina}
            onGoTo={(destino) => onPageChange?.(destino)}
          />
        </>
      )}
    </div>
  )
}

// Paginacao is the footer control: how many items the page holds, WHICH page of how many
// (the count the audit missed — with 1361 items the old control only had Previous/Next at
// the end of 4223 px and no sense of position) and the two buttons. The state lives in the
// URL (owner of the `page` search param), so the component is presentational.
function Paginacao({
  total,
  offset,
  limit,
  page,
  onGoTo,
}: {
  total: number
  offset: number
  limit: number
  page: number
  // Zero-based destination page — the SAME single channel for the arrows and the numbered
  // buttons, so an out-of-range `page` in the URL can never send the owner further away.
  onGoTo: (page: number) => void
}) {
  const inicio = total === 0 ? 0 : offset + 1
  const fim = Math.min(offset + limit, total)
  const paginas = Math.max(1, Math.ceil(total / limit))
  const atual = Math.min(page + 1, paginas)
  const temAnterior = atual > 1
  const temProximo = atual < paginas
  const t = useT()
  // A short window of page numbers around the current one (1 … 4 5 6 … 57): enough to jump
  // without rendering 57 buttons on a phone.
  const janela = janelaDePaginas(atual, paginas)
  return (
    <nav
      aria-label={t('media.grid.pageOf', { page: atual, pages: paginas })}
      className="flex flex-col gap-2 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between"
    >
      <span aria-live="polite">
        {t('media.grid.range', { from: inicio, to: fim, total })}
        {' · '}
        {t('media.grid.pageOf', { page: atual, pages: paginas })}
      </span>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={!temAnterior}
          onClick={() => onGoTo(atual - 2)}
        >
          {t('common.action.previous')}
        </Button>
        {janela.map((n, i) =>
          n === null ? (
            <span key={`gap-${i}`} aria-hidden className="px-1">
              …
            </span>
          ) : (
            <Button
              key={n}
              variant={n === atual ? 'default' : 'outline'}
              size="sm"
              aria-current={n === atual ? 'page' : undefined}
              aria-label={t('media.grid.goToPage', { page: n })}
              onClick={() => onGoTo(n - 1)}
            >
              {n}
            </Button>
          ),
        )}
        <Button
          variant="outline"
          size="sm"
          disabled={!temProximo}
          onClick={() => onGoTo(atual)}
        >
          {t('common.action.next')}
        </Button>
      </div>
    </nav>
  )
}

// janelaDePaginas returns the page numbers to render: all of them when there are few,
// otherwise the first, the last and the neighborhood of the current one, with `null`
// marking a gap.
function janelaDePaginas(atual: number, paginas: number): (number | null)[] {
  if (paginas <= 7) {
    return Array.from({ length: paginas }, (_, i) => i + 1)
  }
  const vizinhas = [atual - 1, atual, atual + 1].filter(
    (n) => n > 1 && n < paginas,
  )
  const janela: (number | null)[] = [1]
  if (vizinhas[0] != null && vizinhas[0] > 2) {
    janela.push(null)
  }
  janela.push(...vizinhas)
  const ultima = vizinhas[vizinhas.length - 1]
  if (ultima != null && ultima < paginas - 1) {
    janela.push(null)
  }
  janela.push(paginas)
  return janela
}
