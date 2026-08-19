import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { entitiesQueryOptions } from '@/lib/queries'
import type { EntityListParams } from '@/lib/api'
import { PosterCard } from '@/components/poster-card'
import {
  MediaTypeFilter,
  type MediaTypeFilterValue,
} from '@/components/media-type-filter'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'

const POR_PAGINA = 24

type FiltroPosse = 'all' | 'owned' | 'not'

// Traduz os filtros da UI para os params da API (undefined = sem filtro). `tipo`
// já chega no MESMO formato do param da API (string|undefined, briefing 0110) —
// não passa mais por `MediaTypeFilterValue`/'all' aqui, só na exibição do Select.
function montarParams(
  tipo: string | undefined,
  posse: FiltroPosse,
  pagina: number,
): EntityListParams {
  return {
    type: tipo || undefined,
    owned: posse === 'all' ? undefined : posse === 'owned',
    limit: POR_PAGINA,
    offset: pagina * POR_PAGINA,
    sort: 'title',
  }
}

// paraOpcaoSelect narrowing do `type` da URL (string livre — cobre qualquer
// schema, ex. "album") para as 3 opções que o `MediaTypeFilter` sabe exibir
// (briefing 0110 não expande esse Select, fora de escopo). Um tipo fora dessas
// 3 opções (ex. vindo do dropdown da navbar) continua filtrando o GRID
// corretamente via `montarParams` acima — só a exibição do Select cai em
// "Todos" (nenhuma opção rotulada para ele ainda).
function paraOpcaoSelect(tipo: string | undefined): MediaTypeFilterValue {
  return tipo === 'movie' || tipo === 'tv' ? tipo : 'all'
}

// Grid de pôsteres do catálogo: filtros (tipo/posse), paginação e os três
// estados claros (carregando / erro / vazio). É o componente "esperto": possui
// o estado de filtro/página e consome o TanStack Query. O filtro de TIPO
// (briefing 0110) migrou de `useState` local para o search param `?type=` da
// rota `/` — linkável, sobrevive a refresh e fica sincronizado com o dropdown
// "Biblioteca" da navbar (mesma fonte de verdade). Posse continua local (fora
// de escopo do 0110).
export function PosterGrid({ lang }: { lang?: string } = {}) {
  const { type: tipo } = useSearch({ from: '/' })
  const navigate = useNavigate()
  const [posse, setPosse] = useState<FiltroPosse>('all')
  const [pagina, setPagina] = useState(0)

  // Trocar de tipo (pelo Select OU pelo dropdown da navbar, que navega direto)
  // reinicia a página — mesmo efeito do `reiniciarPagina` de antes, só que o
  // tipo agora muda por navegação (search param), não por um setState direto
  // que já pudesse resetar a página no mesmo callback. Ajuste de estado
  // DURANTE o render (padrão recomendado pelo React para "resetar estado
  // quando um valor externo muda"), não num `useEffect` — evita o
  // cascading-render que o `react-hooks/set-state-in-effect` do lint aponta.
  const [ultimoTipo, setUltimoTipo] = useState(tipo)
  if (tipo !== ultimoTipo) {
    setUltimoTipo(tipo)
    setPagina(0)
  }

  const params = montarParams(tipo, posse, pagina)
  const { data, isPending, isError, error } = useQuery(
    entitiesQueryOptions(params, lang),
  )

  function reiniciarPagina<T>(setter: (v: T) => void) {
    return (v: T) => {
      setPagina(0)
      setter(v)
    }
  }

  const trocarTipo = (novo: MediaTypeFilterValue) => {
    navigate({ to: '/', search: novo === 'all' ? {} : { type: novo } })
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <MediaTypeFilter value={paraOpcaoSelect(tipo)} onChange={trocarTipo} />
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
