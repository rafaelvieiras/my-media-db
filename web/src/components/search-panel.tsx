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
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'

const DEBOUNCE_MS = 350

type FiltroTipo = MediaTypeFilterValue

// Sub-nós da árvore de séries que a busca só traz sob demanda (0033): DESLIGADOS por
// padrão. No geral o usuário busca filme/série; trazer dezenas de episódios/temporadas
// de várias séries afogaria o resultado. Ligar um toggle envia `include=` e refetch, e
// o sub-nó aparece numa SEÇÃO PRÓPRIA — nunca misturado com filme/série.
type SubNo = 'season' | 'episode'
const SUB_NOS: SubNo[] = ['season', 'episode']

// Painel de busca: input com debounce dispara `GET /search`; enquanto vazio, a
// tela fica em estado de instrução (não consulta). Separa "Filmes e séries"
// (cards de topo) de "Temporadas"/"Episódios" (sub-nós, só quando ligados) e de
// "Resultados externos" (não salvos, com Importar), tratando
// `external_available:false` como indisponibilidade — não como zero resultados.
export function SearchPanel() {
  const [termo, setTermo] = useState('')
  const [tipo, setTipo] = useState<FiltroTipo>('all')
  const [subNos, setSubNos] = useState<Record<SubNo, boolean>>({
    season: false,
    episode: false,
  })
  const [debounced, setDebounced] = useState('')
  const { lang } = useLanguage()

  // Debounce: só propaga o termo (trim) para a query após a pausa de digitação.
  useEffect(() => {
    const id = setTimeout(() => setDebounced(termo.trim()), DEBOUNCE_MS)
    return () => clearTimeout(id)
  }, [termo])

  // `include`: os sub-nós ligados, na ordem canônica (chave de cache estável).
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

// SubNoToggles: chips liga/desliga para trazer Temporadas/Episódios à busca. Off por
// padrão; o estado pressionado (aria-pressed) reflete o filtro ativo.
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

// Corpo de resultados: isola os estados (inicial / carregando / erro / dados) do
// controle de input acima. Mantido separado para o painel não inchar. Parte o
// `local` por tipo em seções distintas (0033): topo sempre visível, sub-nós só
// quando o respectivo toggle está ligado.
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
  const topo = data.local.filter(
    (c) => c.type !== 'season' && c.type !== 'episode',
  )
  const temporadas = data.local.filter((c) => c.type === 'season')
  const episodios = data.local.filter((c) => c.type === 'episode')
  return (
    <div className="flex flex-col gap-8">
      <CardGridSection
        title="Filmes e séries"
        cards={topo}
        emptyLabel="Nenhum item do seu catálogo corresponde à busca."
      />
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

// CardGridSection: uma seção de cards do catálogo (título + grid ou estado vazio),
// compartilhada pelas seções de topo/temporadas/episódios — centraliza o markup do
// grid (evita a duplicação medida pelo jscpd).
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
