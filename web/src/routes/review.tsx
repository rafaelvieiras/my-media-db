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
  ChevronDown,
  ChevronRight,
  ImageOff,
  LoaderCircle,
  PencilLine,
  Search,
  Wand2,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { CorrectMatchResultCard } from '@/components/external-result-card'
import {
  matchPreviewQueryOptions,
  reviewQueueQueryOptions,
  searchQueryOptions,
} from '@/lib/queries'
import {
  ApiError,
  arteSrc,
  autoMatchBatch,
  confirmCandidate,
  confirmMatch,
  correctMatch,
  posterSrc,
  type AutoMatchBatchResult,
  type ConfirmCandidateBody,
  type EntityCard,
  type ExternalResult,
  type MatchPreview,
  type MatchPreviewTarget,
} from '@/lib/api'
import { aspectPorTipo, ehDraft, rotuloTipo } from '@/lib/entity-badges'
import { internalIdDe } from '@/lib/internal-id'
import { useLanguage } from '@/lib/language-context'

const DEBOUNCE_MS = 350

// Route `/review`: the curation queue. It lists the items
// that the scanner FLAGGED — `draft` (not identified) and low confidence — and gives the
// ACTION that Phase 2 did not have: confirm the guess, correct the identity
// (re-enriches) or open the detail. Nothing here is destructive; correcting/confirming
// only re-resolves.
//
// The queue is GROUPED by the file context (the album of the track, the series
// of the episode, the folder of the movie) and opens CLOSED: 43 albums of tracks become 43
// sections instead of 185 cards, and the candidate preview is only fetched when the owner
// opens the row (before, one request per card on mount — N+1).
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

  const grupos = agruparRevisao(data ?? [])

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            <h1 className="font-heading text-xl font-semibold">Revisão</h1>
            <p className="text-sm text-muted-foreground">
              Itens casados com baixa confiança e não identificados, agrupados
              pelo álbum, série ou pasta de origem.
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
        {data && data.length > 0 ? (
          <p className="text-xs text-muted-foreground">
            {data.length} {data.length === 1 ? 'item' : 'itens'} em{' '}
            {grupos.length} {grupos.length === 1 ? 'grupo' : 'grupos'}
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
      ) : grupos.length === 0 ? (
        <EmptyState
          title="Nada para revisar"
          description="Nenhum item pendente de curadoria — tudo casado com confiança."
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {grupos.map((grupo) => (
            <SecaoRevisao key={grupo.key} grupo={grupo} />
          ))}
        </ul>
      )}
    </main>
  )
}

// ── Grouping ────────────────────────────────────────────────────────────────

type GrupoRevisao = {
  key: string
  kind: string
  label: string
  items: EntityCard[]
}

// ORDEM_KINDS decides the order of the sections on the screen: movie folders, then series,
// then albums, and finally what has no file. Inside the same kind, the
// biggest group comes first (it is what weighs most in the queue).
const ORDEM_KINDS = ['pasta', 'serie', 'album', 'sem_arquivo']

const ROTULO_KIND: Record<string, string> = {
  pasta: 'pasta',
  serie: 'série',
  album: 'álbum',
  sem_arquivo: 'sem arquivo',
}

// agruparRevisao builds the sections from the `review_group` annotated by the backend.
// An item without a group (older backend) falls into its own "sem arquivo" section — the screen
// never hides an item of the queue.
function agruparRevisao(cards: EntityCard[]): GrupoRevisao[] {
  const porChave = new Map<string, GrupoRevisao>()
  for (const card of cards) {
    const grupo = card.review_group
    const key = grupo?.key ?? 'sem_arquivo:'
    const existente = porChave.get(key)
    if (existente) {
      existente.items.push(card)
      continue
    }
    porChave.set(key, {
      key,
      kind: grupo?.kind ?? 'sem_arquivo',
      label: grupo?.label ?? 'Sem arquivo',
      items: [card],
    })
  }
  return [...porChave.values()].sort((a, b) => {
    const ka = ORDEM_KINDS.indexOf(a.kind)
    const kb = ORDEM_KINDS.indexOf(b.kind)
    const pa = ka === -1 ? ORDEM_KINDS.length : ka
    const pb = kb === -1 ? ORDEM_KINDS.length : kb
    if (pa !== pb) {
      return pa - pb
    }
    if (a.items.length !== b.items.length) {
      return b.items.length - a.items.length
    }
    return a.label.localeCompare(b.label, 'pt-BR')
  })
}

// Reason for the review, in pt-BR: draft = not identified; otherwise, low confidence
// with the percentage of the card (the backend only queues the ones < the threshold).
function motivoRevisao(card: EntityCard): string {
  if (ehDraft(card.status)) {
    return 'não identificado'
  }
  return `baixa confiança (${card.match_confidence}%)`
}

// resumoDoGrupo describes the closed section: number of items + the DOMINANT
// reason (the first item already comes ordered from the backend by worst confidence).
function resumoDoGrupo(grupo: GrupoRevisao): string {
  const itens = grupo.items.length
  const unidade = itens === 1 ? 'item' : 'itens'
  return `${itens} ${unidade} · ${motivoRevisao(grupo.items[0])}`
}

// Invalidation after confirming/correcting: the queue (`['review']`) changes, the grid
// (`['entities']`) reflects the new state and the detail (`['entity', id]`, prefix)
// drops all the language variations.
function invalidarRevisao(qc: QueryClient, id: string) {
  qc.invalidateQueries({ queryKey: ['review'] })
  qc.invalidateQueries({ queryKey: ['entities'] })
  qc.invalidateQueries({ queryKey: ['entity', id] })
}

// pt-BR label of the source of the candidate: "local" came from the catalog already
// imported (it merges via `fundirEntities`), "external" came from the remote provider
// (same path as "Corrigir"). Used both by the warning of the batch auto-match
// and by the source badge of the preview (`PreviewBloco`).
function rotuloMetodo(method: 'local' | 'external' | 'none'): string {
  return method === 'local' ? 'catálogo local' : 'provedor externo'
}

// Candidate "applicable" from the preview: `found=true` AND a non-null `target` —
// the SAME check used by the visual block (`PreviewBloco`) and by the main
// button (`ReviewRow`), guaranteeing that they never diverge: if the row SHOWS a
// candidate, the main button ALWAYS applies exactly that candidate.
function candidatoValido(
  data: MatchPreview | undefined,
): data is MatchPreview & { target: MatchPreviewTarget } {
  return !!data && data.found === true && data.target !== null
}

// ── Section (group) ─────────────────────────────────────────────────────────

function SecaoRevisao({ grupo }: { grupo: GrupoRevisao }) {
  const qc = useQueryClient()
  const [aberto, setAberto] = useState(false)
  const [resumo, setResumo] = useState<string | null>(null)

  const autoMatchGrupo = useMutation({
    mutationFn: () => autoMatchBatch(grupo.items.map((item) => item.id)),
    onSuccess: (resultado: AutoMatchBatchResult) => {
      qc.invalidateQueries({ queryKey: ['review'] })
      qc.invalidateQueries({ queryKey: ['entities'] })
      setResumo(
        `${resultado.resolved_count} resolvido(s) · ${resultado.pending_count} ainda pendente(s)`,
      )
    },
  })

  return (
    <li className="overflow-hidden rounded-xl bg-card ring-1 ring-foreground/10">
      <button
        type="button"
        data-testid="review-grupo"
        aria-expanded={aberto}
        onClick={() => setAberto((v) => !v)}
        className="flex w-full items-center gap-2 p-3 text-left hover:bg-muted/40"
      >
        {aberto ? (
          <ChevronDown className="size-4 shrink-0" aria-hidden />
        ) : (
          <ChevronRight className="size-4 shrink-0" aria-hidden />
        )}
        <span className="min-w-0 flex-1">
          <span className="line-clamp-1 text-sm font-medium">
            {grupo.label}
          </span>
          <span className="text-xs text-muted-foreground">
            {resumoDoGrupo(grupo)}
          </span>
        </span>
        <Badge variant="outline" className="shrink-0">
          {ROTULO_KIND[grupo.kind] ?? grupo.kind}
        </Badge>
      </button>

      {aberto ? (
        <div className="flex flex-col gap-2 border-t border-foreground/10 p-2">
          <div className="flex flex-wrap items-center gap-2 px-1">
            <Button
              size="sm"
              variant="outline"
              disabled={autoMatchGrupo.isPending}
              onClick={() => {
                setResumo(null)
                autoMatchGrupo.mutate()
              }}
            >
              {autoMatchGrupo.isPending ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Wand2 aria-hidden />
              )}
              {autoMatchGrupo.isPending
                ? 'Sincronizando…'
                : 'Auto-sincronizar este grupo'}
            </Button>
            {resumo ? (
              <span role="status" className="text-xs text-muted-foreground">
                {resumo}
              </span>
            ) : null}
            {autoMatchGrupo.error instanceof ApiError ? (
              <span
                role="alert"
                className="text-xs font-medium text-destructive"
              >
                {autoMatchGrupo.error.message}
              </span>
            ) : null}
          </div>
          <ul className="flex flex-col">
            {grupo.items.map((card) => (
              <ReviewRow key={card.id} card={card} />
            ))}
          </ul>
        </div>
      ) : null}
    </li>
  )
}

// ── Compact row + detail under demand ───────────────────────────────────────

function ReviewRow({ card }: { card: EntityCard }) {
  const qc = useQueryClient()
  const [falhouArte, setFalhouArte] = useState(false)
  const [expandido, setExpandido] = useState(false)
  const [corrigindo, setCorrigindo] = useState(false)
  // Warns when "Corrigir" merged a duplicate automatically: the
  // entity without investment from the owner disappears on its own from the queue/library.
  const [avisoFusao, setAvisoFusao] = useState(false)
  const src = posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte

  // Candidate preview LAZY: it only fetches when the owner opens the row.
  // `enabled` avoids one request per card when the queue mounts (the old N+1).
  const preview = useQuery({
    ...matchPreviewQueryOptions(card.id),
    enabled: expandido,
  })
  const achouCandidato = candidatoValido(preview.data)

  const confirmar = useMutation({
    mutationFn: () => confirmMatch(card.id),
    onSuccess: () => invalidarRevisao(qc, card.id),
  })

  // Applies exactly the candidate shown by the preview — local (merges) or
  // external (writes the identity + re-enriches), even below the automatic
  // threshold (a human decision informed by the card, not a silent cut).
  const confirmarCandidato = useMutation({
    mutationFn: () => {
      const dadosPrevia = preview.data
      if (!candidatoValido(dadosPrevia)) {
        throw new Error('sem candidato para confirmar')
      }
      const { target } = dadosPrevia
      const body: ConfirmCandidateBody =
        dadosPrevia.method === 'local'
          ? { method: 'local', local_id: target.local_id ?? '' }
          : {
              method: 'external',
              source: target.source ?? '',
              source_id: target.source_id ?? '',
            }
      return confirmCandidate(card.id, body)
    },
    onSuccess: () => invalidarRevisao(qc, card.id),
  })

  const acaoPendente = achouCandidato
    ? confirmarCandidato.isPending
    : confirmar.isPending

  const mensagemErro =
    confirmar.error instanceof ApiError
      ? confirmar.error.message
      : confirmarCandidato.error instanceof ApiError
        ? confirmarCandidato.error.message
        : null

  return (
    <li className="flex flex-col gap-2 rounded-lg px-1 py-1.5 hover:bg-muted/30">
      <div className="flex items-center gap-2">
        <div
          className={`relative ${aspectPorTipo(card.type)} w-10 shrink-0 overflow-hidden rounded bg-muted`}
        >
          {mostrarPlaceholder ? (
            <div
              data-testid="review-poster-placeholder"
              className="flex h-full w-full items-center justify-center text-muted-foreground"
            >
              <ImageOff className="size-4" aria-hidden />
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
        <div className="flex min-w-0 flex-1 flex-col">
          <Link
            to="/entities/$id"
            params={{ id: card.id }}
            className="line-clamp-1 text-sm font-medium hover:underline"
          >
            {card.title}
          </Link>
          <span className="text-xs text-muted-foreground">
            {rotuloTipo(card.type)} · {motivoRevisao(card)}
          </span>
        </div>
        <Button
          size="sm"
          variant="ghost"
          data-testid="review-linha"
          aria-expanded={expandido}
          onClick={() => setExpandido((v) => !v)}
        >
          {expandido ? (
            <ChevronDown className="size-4" aria-hidden />
          ) : (
            <ChevronRight className="size-4" aria-hidden />
          )}
          {expandido ? 'Fechar' : 'Ver candidato'}
        </Button>
      </div>

      {expandido ? (
        <div className="flex flex-col gap-2 pl-12">
          <PreviewBloco
            tipo={card.type}
            isPending={preview.isPending}
            isError={preview.isError}
            data={preview.data}
          />

          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              disabled={acaoPendente || preview.isPending}
              onClick={() =>
                achouCandidato
                  ? confirmarCandidato.mutate()
                  : confirmar.mutate()
              }
            >
              {acaoPendente ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Check aria-hidden />
              )}
              {achouCandidato
                ? acaoPendente
                  ? 'Confirmando…'
                  : 'Confirmar candidato'
                : acaoPendente
                  ? 'Confirmando…'
                  : 'Manter como está (sem metadados externos)'}
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

          {avisoFusao ? (
            <p role="status" className="text-xs text-muted-foreground">
              Duplicata mesclada automaticamente — o item já importado foi
              combinado com este.
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
        </div>
      ) : null}
    </li>
  )
}

// Candidate preview block: between the item header and the row of
// buttons. Four states — loading (discreet skeleton), found a candidate
// (poster + title + year + badges), did not find (neutral message) and lookup failure
// (short error message, it does NOT block the rest — `ReviewRow` already treats it as
// "no candidate" via `candidatoValido`, which is only `true` with `data` present).
// `tipo` is the type of the ITEM being reviewed (`card.type` of the parent `ReviewRow`) — the
// candidate (`MatchPreviewTarget`) does not carry its own type in the API contract,
// but the match never suggests a candidate of a type different from the target (album only matches
// with album), so reusing the type of the parent for the cover frame is safe.
function PreviewBloco({
  tipo,
  isPending,
  isError,
  data,
}: {
  tipo: string
  isPending: boolean
  isError: boolean
  data: MatchPreview | undefined
}) {
  const [falhouArte, setFalhouArte] = useState(false)

  if (isPending) {
    return (
      <div
        role="status"
        className="flex items-center gap-2 rounded-lg bg-muted/40 p-2 text-muted-foreground"
      >
        <LoaderCircle className="size-4 animate-spin" aria-hidden />
        <span className="text-xs">Buscando candidato…</span>
      </div>
    )
  }

  if (isError) {
    return (
      <p role="alert" className="text-xs font-medium text-destructive">
        Não foi possível buscar um candidato — tente novamente mais tarde.
      </p>
    )
  }

  if (!data || data.found === false || !data.target) {
    return (
      <p className="text-xs text-muted-foreground">
        nenhum candidato encontrado — nem no catálogo local, nem na fonte
        externa
      </p>
    )
  }

  const { target } = data
  const candidatoSrc =
    data.method === 'local'
      ? posterSrc(target.poster_url)
      : arteSrc(target.poster_url)
  const mostrarPlaceholder = candidatoSrc === null || falhouArte

  return (
    <div className="flex items-center gap-3 rounded-lg bg-muted/40 p-2">
      <div
        className={`relative ${aspectPorTipo(tipo)} w-12 shrink-0 overflow-hidden rounded bg-muted`}
      >
        {mostrarPlaceholder ? (
          <div className="flex h-full w-full items-center justify-center text-muted-foreground">
            <ImageOff className="size-4" aria-hidden />
          </div>
        ) : (
          <img
            src={candidatoSrc}
            alt={`Pôster de ${target.title}`}
            loading="lazy"
            onError={() => setFalhouArte(true)}
            className="h-full w-full object-cover"
          />
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline gap-2">
          <span className="line-clamp-1 text-sm font-medium">
            {target.title}
          </span>
          {target.year ? (
            <span className="shrink-0 text-xs text-muted-foreground">
              {target.year}
            </span>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline">score {data.score.toFixed(1)}</Badge>
          <Badge variant="secondary">{rotuloMetodo(data.method)}</Badge>
          {data.meets_auto_threshold ? (
            <Badge>match forte</Badge>
          ) : (
            <Badge variant="outline">confira antes de confirmar</Badge>
          )}
        </div>
        {target.matched_title && target.matched_title !== target.title ? (
          <p className="line-clamp-1 text-xs text-muted-foreground">
            via título alternativo: &quot;{target.matched_title}&quot;
          </p>
        ) : null}
      </div>
    </div>
  )
}

// Inline correction panel: the owner searches for the right title in the external source and
// chooses the result; the UI writes the chosen identity with `correctMatch`
// (source/source_id of the item), the backend re-enriches and re-resolves. It reuses the
// `searchQueryOptions` (the same search), but the action is CORRECT, not import.
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

// Lists the EXTERNAL results of the search as the same rich card of `/search`
// — poster, truncated synopsis and "Na biblioteca" badge when the result already is
// an internal entity — but here the button is "Usar este" (not Importar), and the badge
// does NOT hide the action (it is the duplicate merge case). The choice becomes the
// correct identity. Only the external ones matter here (the identity that the owner
// informs comes from a source, not from the local catalog).
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
    <ul className="flex flex-col gap-3">
      {externos.map((r) => (
        <CorrectMatchResultCard
          key={`${r.source}:${r.source_id}`}
          result={r}
          internalId={internalIdDe(r)}
          pendente={escolhendo}
          onEscolher={onEscolher}
        />
      ))}
    </ul>
  )
}
