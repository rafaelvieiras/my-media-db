import { useEffect, useState } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Check,
  ImageOff,
  LoaderCircle,
  PencilLine,
  Search,
  Sparkles,
  Wand2,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { reviewQueueQueryOptions, searchQueryOptions } from '@/lib/queries'
import {
  ApiError,
  autoMatchBatch,
  autoMatchEntity,
  confirmMatch,
  correctMatch,
  posterSrc,
  type AutoMatchBatchResult,
  type EntityCard,
  type ExternalResult,
} from '@/lib/api'
import { ehDraft, rotuloTipo } from '@/lib/entity-badges'
import { useLanguage } from '@/lib/language-context'

const DEBOUNCE_MS = 350

// Rota `/review` (0018): a fila de curadoria. Lista os itens que o scanner
// SINALIZOU — `draft` (não identificados) e baixa confiança — e dá a AÇÃO que a
// Fase 2 não tinha: confirmar o palpite, corrigir a identidade (re-enriquece) ou
// abrir o detalhe. Nada aqui é destrutivo; corrigir/confirmar apenas re-resolvem.
export function ReviewScreen() {
  const qc = useQueryClient()
  const { data, isPending, isError, error } = useQuery(reviewQueueQueryOptions)
  const [resumoLote, setResumoLote] = useState<string | null>(null)

  const autoMatchLote = useMutation({
    mutationFn: () => autoMatchBatch(),
    onSuccess: (resultado: AutoMatchBatchResult) => {
      qc.invalidateQueries({ queryKey: ['review'] })
      qc.invalidateQueries({ queryKey: ['entities'] })
      setResumoLote(
        `${resultado.resolved_count} resolvidos automaticamente, ${resultado.pending_count} continuam para revisão manual.`,
      )
    },
  })

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            <h1 className="font-heading text-xl font-semibold">Revisão</h1>
            <p className="text-sm text-muted-foreground">
              Itens casados com baixa confiança e não identificados. Confirme o
              palpite ou corrija a identidade.
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            disabled={autoMatchLote.isPending || !data || data.length === 0}
            onClick={() => {
              setResumoLote(null)
              autoMatchLote.mutate()
            }}
          >
            {autoMatchLote.isPending ? (
              <LoaderCircle className="animate-spin" aria-hidden />
            ) : (
              <Wand2 aria-hidden />
            )}
            {autoMatchLote.isPending
              ? 'Sincronizando…'
              : 'Auto-sincronizar elegíveis'}
          </Button>
        </div>
        {resumoLote ? (
          <p role="status" className="text-sm text-muted-foreground">
            {resumoLote}
          </p>
        ) : null}
      </div>

      {isPending ? (
        <LoadingState label="Carregando a fila de revisão…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar a revisão"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : data.length === 0 ? (
        <EmptyState
          title="Nada para revisar"
          description="Nenhum item pendente de curadoria — tudo casado com confiança."
        />
      ) : (
        <ul className="flex flex-col gap-3">
          {data.map((card) => (
            <ReviewCard key={card.id} card={card} />
          ))}
        </ul>
      )}
    </main>
  )
}

// Motivo da revisão, em pt-BR: draft = não identificado; senão, baixa confiança
// com o percentual do card (o backend só põe na fila os < limiar).
function motivoRevisao(card: EntityCard): string {
  if (ehDraft(card.status)) {
    return 'não identificado'
  }
  return `baixa confiança (${card.match_confidence}%)`
}

// Invalidação após confirmar/corrigir: a fila (`['review']`) muda, o grid
// (`['entities']`) reflete o novo estado e o detalhe (`['entity', id]`, prefixo)
// derruba todas as variações de idioma.
function invalidarRevisao(qc: QueryClient, id: string) {
  qc.invalidateQueries({ queryKey: ['review'] })
  qc.invalidateQueries({ queryKey: ['entities'] })
  qc.invalidateQueries({ queryKey: ['entity', id] })
}

// Rótulo pt-BR da fonte do candidato aplicado pelo auto-match (0050): "local"
// veio do catálogo já importado (fundiu via `fundirEntities`), "external" veio
// do provedor remoto (mesmo caminho do "Corrigir").
function rotuloMetodo(method: 'local' | 'external' | 'none'): string {
  return method === 'local' ? 'catálogo local' : 'provedor externo'
}

function ReviewCard({ card }: { card: EntityCard }) {
  const qc = useQueryClient()
  const [falhouArte, setFalhouArte] = useState(false)
  const [corrigindo, setCorrigindo] = useState(false)
  // Avisa quando "Corrigir" fundiu uma duplicata automaticamente (0051): a
  // entity sem investimento do dono some sozinha da fila/biblioteca.
  const [avisoFusao, setAvisoFusao] = useState(false)
  // Mensagem do auto-match individual (0050): sucesso (resolveu, some da fila
  // na invalidação) ou neutro (não achou candidato confiável, item continua).
  const [avisoAuto, setAvisoAuto] = useState<string | null>(null)
  const src = posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte

  const confirmar = useMutation({
    mutationFn: () => confirmMatch(card.id),
    onSuccess: () => invalidarRevisao(qc, card.id),
  })

  const autoSincronizar = useMutation({
    mutationFn: () => autoMatchEntity(card.id),
    onSuccess: (resultado) => {
      if (resultado.resolved) {
        setAvisoAuto(
          `Resolvido automaticamente via ${rotuloMetodo(resultado.method)} (score ${resultado.score}).`,
        )
        invalidarRevisao(qc, card.id)
      } else {
        setAvisoAuto(
          'Nenhum candidato confiável encontrado — revise manualmente.',
        )
      }
    },
  })

  const mensagemErro =
    confirmar.error instanceof ApiError
      ? confirmar.error.message
      : autoSincronizar.error instanceof ApiError
        ? autoSincronizar.error.message
        : null

  return (
    <li className="flex flex-col gap-3 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex gap-3">
        <div className="relative aspect-[2/3] w-20 shrink-0 overflow-hidden rounded-lg bg-muted">
          {mostrarPlaceholder ? (
            <div
              data-testid="review-poster-placeholder"
              className="flex h-full w-full items-center justify-center text-muted-foreground"
            >
              <ImageOff className="size-6" aria-hidden />
            </div>
          ) : (
            <img
              src={src}
              alt={`Pôster de ${card.title}`}
              loading="lazy"
              onError={() => setFalhouArte(true)}
              className="h-full w-full object-cover"
            />
          )}
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <Link
              to="/entities/$id"
              params={{ id: card.id }}
              className="line-clamp-1 font-medium hover:underline"
            >
              {card.title}
            </Link>
            <span className="text-xs text-muted-foreground">
              {rotuloTipo(card.type)}
            </span>
          </div>
          <div>
            <Badge variant={ehDraft(card.status) ? 'secondary' : 'destructive'}>
              {motivoRevisao(card)}
            </Badge>
          </div>
          <div className="mt-auto flex flex-wrap items-center gap-2 pt-1">
            <Button
              size="sm"
              disabled={confirmar.isPending}
              onClick={() => confirmar.mutate()}
            >
              {confirmar.isPending ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Check aria-hidden />
              )}
              {confirmar.isPending ? 'Confirmando…' : 'Confirmar'}
            </Button>
            <Button
              size="sm"
              variant="outline"
              aria-expanded={corrigindo}
              onClick={() => setCorrigindo((v) => !v)}
            >
              <PencilLine aria-hidden />
              Corrigir
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={autoSincronizar.isPending}
              onClick={() => {
                setAvisoAuto(null)
                autoSincronizar.mutate()
              }}
            >
              {autoSincronizar.isPending ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Sparkles aria-hidden />
              )}
              {autoSincronizar.isPending
                ? 'Sincronizando…'
                : 'Auto-sincronizar'}
            </Button>
            <Button asChild size="sm" variant="ghost">
              <Link to="/entities/$id" params={{ id: card.id }}>
                Ver detalhe
              </Link>
            </Button>
            {mensagemErro ? (
              <span
                role="alert"
                className="text-xs font-medium text-destructive"
              >
                {mensagemErro}
              </span>
            ) : null}
          </div>
        </div>
      </div>

      {avisoFusao ? (
        <p role="status" className="text-xs text-muted-foreground">
          Duplicata mesclada automaticamente — o item já importado foi combinado
          com este.
        </p>
      ) : null}

      {avisoAuto ? (
        <p role="status" className="text-xs text-muted-foreground">
          {avisoAuto}
        </p>
      ) : null}

      {corrigindo ? (
        <CorrigirPainel
          entityId={card.id}
          onCorrigido={(duplicataMesclada) => {
            setCorrigindo(false)
            setAvisoFusao(duplicataMesclada)
          }}
        />
      ) : null}
    </li>
  )
}

// Painel inline de correção: o dono busca o título certo na fonte externa e
// escolhe o resultado; a UI grava a identidade escolhida com `correctMatch`
// (source/source_id do item), o backend re-enriquece e re-resolve. Reusa o
// `searchQueryOptions` (mesma busca do 0015), mas a ação é CORRIGIR, não importar.
function CorrigirPainel({
  entityId,
  onCorrigido,
}: {
  entityId: string
  onCorrigido: (duplicataMesclada: boolean) => void
}) {
  const qc = useQueryClient()
  const { lang } = useLanguage()
  const [termo, setTermo] = useState('')
  const [debounced, setDebounced] = useState('')

  useEffect(() => {
    const t = setTimeout(() => setDebounced(termo.trim()), DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [termo])

  const busca = useQuery(searchQueryOptions(debounced, undefined, lang))

  const corrigir = useMutation({
    mutationFn: (escolhido: ExternalResult) =>
      correctMatch(entityId, escolhido.source, escolhido.source_id),
    onSuccess: (resultado) => {
      invalidarRevisao(qc, entityId)
      onCorrigido(resultado.duplicataMesclada)
    },
  })

  const mensagemErro =
    corrigir.error instanceof ApiError ? corrigir.error.message : null

  return (
    <div className="flex flex-col gap-3 rounded-lg bg-muted/40 p-3">
      <div className="relative">
        <Search
          className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
          aria-hidden
        />
        <Input
          type="search"
          aria-label="Buscar o título correto"
          placeholder="Buscar o título correto…"
          value={termo}
          onChange={(e) => setTermo(e.target.value)}
          className="h-9 pl-8"
        />
      </div>

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      <ResultadosCorrecao
        query={debounced}
        isPending={busca.isPending}
        isError={busca.isError}
        error={busca.error}
        externos={busca.data?.external ?? []}
        disponivel={busca.data?.external_available ?? true}
        escolhendo={corrigir.isPending}
        onEscolher={(r) => corrigir.mutate(r)}
      />
    </div>
  )
}

// Lista os resultados EXTERNOS da busca com um botão "Usar este" por item — a
// escolha vira a identidade correta. Só os externos importam aqui (a identidade
// que o dono informa vem de uma fonte, não do catálogo local).
function ResultadosCorrecao({
  query,
  isPending,
  isError,
  error,
  externos,
  disponivel,
  escolhendo,
  onEscolher,
}: {
  query: string
  isPending: boolean
  isError: boolean
  error: unknown
  externos: ExternalResult[]
  disponivel: boolean
  escolhendo: boolean
  onEscolher: (r: ExternalResult) => void
}) {
  if (query === '') {
    return (
      <p className="text-sm text-muted-foreground">
        Digite o título correto para escolher a identidade certa.
      </p>
    )
  }
  if (isPending) {
    return <LoadingState label="Buscando…" />
  }
  if (isError) {
    return (
      <ErrorState
        title="Não foi possível buscar"
        message={error instanceof Error ? error.message : undefined}
      />
    )
  }
  if (!disponivel) {
    return (
      <ErrorState
        title="Busca externa indisponível"
        message="A fonte externa não respondeu ou não está configurada."
      />
    )
  }
  if (externos.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Nenhum resultado externo para esta busca.
      </p>
    )
  }
  return (
    <ul className="flex flex-col gap-2">
      {externos.map((r) => (
        <li
          key={`${r.source}:${r.source_id}`}
          className="flex items-center gap-3 rounded-lg bg-card p-2 ring-1 ring-foreground/10"
        >
          <div className="flex min-w-0 flex-1 items-baseline gap-2">
            <span className="line-clamp-1 text-sm font-medium">{r.title}</span>
            {r.year ? (
              <span className="shrink-0 text-xs text-muted-foreground">
                {r.year}
              </span>
            ) : null}
          </div>
          <Button size="sm" disabled={escolhendo} onClick={() => onEscolher(r)}>
            Usar este
          </Button>
        </li>
      ))}
    </ul>
  )
}
