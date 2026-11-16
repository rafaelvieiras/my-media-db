import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { entitiesQueryOptions } from '@/lib/queries'
import type { EntityListParams } from '@/lib/api'
import { PosterCard } from '@/components/poster-card'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'

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
export function PosterGrid({ lang, type }: { lang?: string; type: string }) {
  const [posse, setPosse] = useState<FiltroPosse>('all')
  const [pagina, setPagina] = useState(0)

  const params = montarParams(type, posse, pagina)
  const { data, isPending, isError, error } = useQuery(
    entitiesQueryOptions(params, lang),
  )

  function reiniciarPagina<T>(setter: (v: T) => void) {
    return (v: T) => {
      setPagina(0)
      setter(v)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">Posse</span>
          <Select
            aria-label="Filtrar por posse"
            value={posse}
            onChange={(e) =>
              reiniciarPagina(setPosse)(e.target.value as FiltroPosse)
            }
            className="w-40"
          >
            <option value="all">Todos</option>
            <option value="owned">Na biblioteca</option>
            <option value="not">Fora da biblioteca</option>
          </Select>
        </label>
      </div>

      {isPending ? (
        <LoadingState label="Carregando catálogo…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar o catálogo"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : data.items.length === 0 ? (
        <EmptyState
          title="Nenhum item no catálogo"
          description="Adicione um datasource e escaneie para popular a biblioteca."
          action={
            <Button asChild variant="outline" size="sm">
              <Link to="/dashboard/library">Ir para Bibliotecas</Link>
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
            onPrev={() => setPagina((p) => Math.max(0, p - 1))}
            onNext={() => setPagina((p) => p + 1)}
          />
        </>
      )}
    </div>
  )
}

function Paginacao({
  total,
  offset,
  limit,
  onPrev,
  onNext,
}: {
  total: number
  offset: number
  limit: number
  onPrev: () => void
  onNext: () => void
}) {
  const inicio = total === 0 ? 0 : offset + 1
  const fim = Math.min(offset + limit, total)
  const temAnterior = offset > 0
  const temProximo = fim < total
  return (
    <div className="flex items-center justify-between gap-3 text-sm text-muted-foreground">
      <span>
        {inicio}–{fim} de {total}
      </span>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={!temAnterior}
          onClick={onPrev}
        >
          Anterior
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={!temProximo}
          onClick={onNext}
        >
          Próxima
        </Button>
      </div>
    </div>
  )
}
