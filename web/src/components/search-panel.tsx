import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { searchQueryOptions } from '@/lib/queries'
import { useLanguage } from '@/lib/language-context'
import type { SearchResult } from '@/lib/api'
import { PosterCard } from '@/components/poster-card'
import { ExternalResultCard } from '@/components/external-result-card'
import { internalIdDe } from '@/lib/internal-id'
import {
  MediaTypeFilter,
  type MediaTypeFilterValue,
} from '@/components/media-type-filter'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'

const DEBOUNCE_MS = 350

type FiltroTipo = MediaTypeFilterValue

// Painel de busca: input com debounce dispara `GET /search`; enquanto vazio, a
// tela fica em estado de instrução (não consulta). Separa "No seu catálogo"
// (cards locais) de "Resultados externos" (não salvos, com Importar), e trata
// `external_available:false` como indisponibilidade — não como zero resultados.
export function SearchPanel() {
  const [termo, setTermo] = useState('')
  const [tipo, setTipo] = useState<FiltroTipo>('all')
  const [debounced, setDebounced] = useState('')
  const { lang } = useLanguage()

  // Debounce: só propaga o termo (trim) para a query após a pausa de digitação.
  useEffect(() => {
    const id = setTimeout(() => setDebounced(termo.trim()), DEBOUNCE_MS)
    return () => clearTimeout(id)
  }, [termo])

  const busca = useQuery(
    searchQueryOptions(debounced, tipo === 'all' ? undefined : tipo, lang),
  )

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative min-w-64 flex-1">
          <Search
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <Input
            type="search"
            aria-label="Buscar título"
            placeholder="Buscar um filme ou série…"
            value={termo}
            onChange={(e) => setTermo(e.target.value)}
            className="h-9 pl-8"
          />
        </div>
        <MediaTypeFilter value={tipo} onChange={setTipo} />
      </div>

      <SearchResults
        query={debounced}
        isPending={busca.isPending}
        isError={busca.isError}
        error={busca.error}
        data={busca.data}
      />
    </div>
  )
}

// Corpo de resultados: isola os estados (inicial / carregando / erro / dados) do
// controle de input acima. Mantido separado para o painel não inchar.
function SearchResults({
  query,
  isPending,
  isError,
  error,
  data,
}: {
  query: string
  isPending: boolean
  isError: boolean
  error: unknown
  data: SearchResult | undefined
}) {
  // Estado inicial: sem termo, a query está desabilitada (isPending fica true).
  if (query === '') {
    return (
      <EmptyState
        title="Busque no seu catálogo e em fontes externas"
        description="Digite o título de um filme ou série para ver o que já está na sua biblioteca e o que pode importar."
      />
    )
  }
  if (isPending) {
    return <LoadingState label="Buscando…" />
  }
  if (isError || !data) {
    return (
      <ErrorState
        title="Não foi possível buscar"
        message={error instanceof Error ? error.message : undefined}
      />
    )
  }
  return (
    <div className="flex flex-col gap-8">
      <LocalSection cards={data.local} />
      <ExternalSection
        results={data.external}
        available={data.external_available}
      />
    </div>
  )
}

function LocalSection({ cards }: { cards: SearchResult['local'] }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">No seu catálogo</h2>
      {cards.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          Nenhum item do seu catálogo corresponde à busca.
        </p>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
          {cards.map((card) => (
            <PosterCard key={card.id} card={card} />
          ))}
        </div>
      )}
    </section>
  )
}

function ExternalSection({
  results,
  available,
}: {
  results: SearchResult['external']
  available: boolean
}) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Resultados externos</h2>
      {!available ? (
        <ErrorState
          title="Busca externa indisponível"
          message="A fonte externa não respondeu ou não está configurada. Isto é diferente de não haver resultados — tente novamente mais tarde."
        />
      ) : results.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          Nenhum resultado externo para importar.
        </p>
      ) : (
        <ul className="flex flex-col gap-3">
          {results.map((result) => (
            <ExternalResultCard
              key={`${result.source}:${result.source_id}`}
              result={result}
              internalId={internalIdDe(result)}
            />
          ))}
        </ul>
      )}
    </section>
  )
}
