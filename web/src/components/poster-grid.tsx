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

// Traduz os filtros da UI para os params da API (undefined = sem filtro de
// posse). `tipo` é sempre FIXO (briefing 0114/ADR-0030 — ver comentário do
// componente abaixo), nunca "todos os tipos" aqui.
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

// Grid de pôsteres do catálogo: paginação, filtro de posse e os três estados
// claros (carregando / erro / vazio). Desde o briefing 0114/ADR-0030, o TIPO
// chega FIXO via prop (`type`, resolvido do slug da URL pela rota
// `/library/$entitySlug` — ver `routes/library-browse.tsx`), em vez do search
// param `?type=` da antiga rota `/` (briefing 0110): a home não tem mais grid
// completo, só carrosséis (`RecentlyAddedSection`), e a listagem paginada de
// cada tipo agora vive numa tela própria por tipo — por isso não há mais
// seletor de tipo (`MediaTypeFilter`) dentro do grid, o tipo já vem fixo da
// rota. Posse continua local (estado interno da tela).
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
