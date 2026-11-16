import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
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
import { rotuloTipoPlural } from '@/lib/entity-badges'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'

const DEBOUNCE_MS = 350

type FiltroTipo = MediaTypeFilterValue

// Sub-nodes of the series tree that the search only brings on demand: OFF by
// default. In general the user searches for a movie/series; bringing dozens of episodes/seasons
// from several series would drown the result. Turning a toggle on sends `include=` and refetches, and
// the sub-node appears in its OWN SECTION — never mixed with movie/series.
type SubNo = 'season' | 'episode'
const SUB_NOS: SubNo[] = ['season', 'episode']

// Search panel: an input with debounce fires `GET /search`; while empty, the
// screen stays in an instruction state (it does not query). It groups the TOP-LEVEL cards
// dynamically by real `type` ("Filmes", "Séries",
// "Artistas" etc., without a fixed "Filmes e séries" bucket that did not cover music),
// separately from "Temporadas"/"Episódios" (sub-nodes, only when they are on) and from
// "Resultados externos" (not saved, with Importar), treating
// `external_available:false` as unavailability — not as zero results.
export function SearchPanel() {
  const [termo, setTermo] = useState('')
  const [tipo, setTipo] = useState<FiltroTipo>('all')
  const [subNos, setSubNos] = useState<Record<SubNo, boolean>>({
    season: false,
    episode: false,
  })
  const [debounced, setDebounced] = useState('')
  const { lang } = useLanguage()

  // Debounce: it only propagates the (trimmed) term to the query after the typing pause.
  useEffect(() => {
    const id = setTimeout(() => setDebounced(termo.trim()), DEBOUNCE_MS)
    return () => clearTimeout(id)
  }, [termo])

  // `include`: the sub-nodes turned on, in the canonical order (stable cache key).
  const include = useMemo(() => SUB_NOS.filter((s) => subNos[s]), [subNos])

  const busca = useQuery(
    searchQueryOptions(
      debounced,
      tipo === 'all' ? undefined : tipo,
      lang,
      include,
    ),
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
        <SubNoToggles
          value={subNos}
          onToggle={(s) => setSubNos((prev) => ({ ...prev, [s]: !prev[s] }))}
        />
      </div>

      <SearchResults
        query={debounced}
        isPending={busca.isPending}
        isError={busca.isError}
        error={busca.error}
        data={busca.data}
        mostrarTemporadas={subNos.season}
        mostrarEpisodios={subNos.episode}
      />
    </div>
  )
}

// SubNoToggles: on/off chips to bring Seasons/Episodes into the search. Off by
// default; the pressed state (aria-pressed) reflects the active filter.
function SubNoToggles({
  value,
  onToggle,
}: {
  value: Record<SubNo, boolean>
  onToggle: (s: SubNo) => void
}) {
  return (
    <div className="flex items-center gap-2">
      <SubNoToggle
        label="Temporadas"
        ativo={value.season}
        onClick={() => onToggle('season')}
      />
      <SubNoToggle
        label="Episódios"
        ativo={value.episode}
        onClick={() => onToggle('episode')}
      />
    </div>
  )
}

function SubNoToggle({
  label,
  ativo,
  onClick,
}: {
  label: string
  ativo: boolean
  onClick: () => void
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={ativo ? 'default' : 'outline'}
      aria-pressed={ativo}
      onClick={onClick}
    >
      {label}
    </Button>
  )
}

// Results body: it isolates the states (initial / loading / error / data) from the
// input control above. Kept separate so the panel does not bloat. It splits the
// `local` one by type into distinct sections: top-level always visible, sub-nodes only
// when the respective toggle is on.
function SearchResults({
  query,
  isPending,
  isError,
  error,
  data,
  mostrarTemporadas,
  mostrarEpisodios,
}: {
  query: string
  isPending: boolean
  isError: boolean
  error: unknown
  data: SearchResult | undefined
  mostrarTemporadas: boolean
  mostrarEpisodios: boolean
}) {
  // Initial state: without a term, the query is disabled (isPending stays true).
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
  const topo = data.local.filter(
    (c) => c.type !== 'season' && c.type !== 'episode',
  )
  const temporadas = data.local.filter((c) => c.type === 'season')
  const episodios = data.local.filter((c) => c.type === 'episode')
  const gruposTopo = agruparPorTipo(topo)
  return (
    <div className="flex flex-col gap-8">
      {gruposTopo.length === 0 ? (
        // No top-level card: it keeps a section (even an empty one) so the owner
        // knows the catalog WAS queried — without that, with a local result of
        // zero the screen would jump straight to "Resultados externos" without saying
        // that the catalog had nothing (an implementation decision, not specified
        // by the owner).
        <CardGridSection
          title="Catálogo"
          cards={[]}
          emptyLabel="Nenhum item do seu catálogo corresponde à busca."
        />
      ) : (
        gruposTopo.map((grupo) => (
          <CardGridSection
            key={grupo.type}
            title={rotuloTipoPlural(grupo.type)}
            cards={grupo.cards}
            emptyLabel="Nenhum item do seu catálogo corresponde à busca."
          />
        ))
      )}
      {mostrarTemporadas && (
        <CardGridSection
          title="Temporadas"
          cards={temporadas}
          emptyLabel="Nenhuma temporada corresponde à busca."
        />
      )}
      {mostrarEpisodios && (
        <CardGridSection
          title="Episódios"
          cards={episodios}
          emptyLabel="Nenhum episódio corresponde à busca."
        />
      )}
      <ExternalSection
        results={data.external}
        available={data.external_available}
      />
    </div>
  )
}

// agruparPorTipo separates the TOP-LEVEL cards (already without season/episode, which have a section
// of their own) by the real `type` of each one — it generalizes the
// former single "Filmes e séries" bucket, which made no sense for types such as
// "artista". It preserves the relevance order of FTS5: inside each group the
// cards keep the arrival order in `cards`; the groups come out in the order of the
// FIRST card of each type (without reordering by name/alphabet).
function agruparPorTipo(
  cards: SearchResult['local'],
): Array<{ type: string; cards: SearchResult['local'] }> {
  const grupos = new Map<string, SearchResult['local']>()
  for (const card of cards) {
    const atual = grupos.get(card.type)
    if (atual) {
      atual.push(card)
    } else {
      grupos.set(card.type, [card])
    }
  }
  return Array.from(grupos, ([type, cards]) => ({ type, cards }))
}

// CardGridSection: a section of catalog cards (title + grid or empty state),
// shared by the top-level/seasons/episodes sections — it centralizes the markup of the
// grid (avoids the duplication measured by jscpd).
function CardGridSection({
  title,
  cards,
  emptyLabel,
}: {
  title: string
  cards: SearchResult['local']
  emptyLabel: string
}) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">{title}</h2>
      {cards.length === 0 ? (
        <p className="text-sm text-muted-foreground">{emptyLabel}</p>
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
