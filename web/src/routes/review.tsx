import { useEffect, useMemo, useState } from 'react'
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
import { Select } from '@/components/ui/select'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { ScreenHeader } from '@/components/screen-header'
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
import { aspectPorTipo, rotuloTipo } from '@/lib/entity-badges'
import {
  REVIEW_PAGE_SIZE,
  filterReviewItems,
  groupContext,
  groupKind,
  groupReviewItems,
  reviewReason,
  sortReviewGroups,
  type ReviewGroup,
  type ReviewReasonFilter,
} from '@/lib/review-filters'
import { internalIdDe } from '@/lib/internal-id'
import { useLanguage } from '@/lib/language-context'
import { useLocale, useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'

const DEBOUNCE_MS = 350
// The queue search is local (the whole queue already arrived), so it only has to
// avoid filtering on every keystroke — 250 ms is enough to keep typing smooth.
const FILTER_DEBOUNCE_MS = 250

// The catalog has no ICU plural rules, so the caller picks the `_one`/`_other` suffix.
function pluralKey(base: string, n: number): string {
  return `${base}_${n === 1 ? 'one' : 'other'}`
}

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
//
// With 1692 items the screen needs the header to work as an index: the search and the
// filters narrow the queue on the client, the biggest group comes first (the 422 items
// without a file used to be the last section, 11,890 px down) and an open group renders
// at most `REVIEW_PAGE_SIZE` rows.
export function ReviewScreen() {
  const t = useT()
  const locale = useLocale()
  const qc = useQueryClient()
  const { data, isPending, isError, error, refetch } = useQuery(
    reviewQueueQueryOptions,
  )
  const [resumoLote, setResumoLote] = useState<string | null>(null)
  // Last decision applied, announced with `role="status"`: the row disappears when the
  // queue is invalidated, so the confirmation has to live above it.
  const [aviso, setAviso] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [kind, setKind] = useState('all')
  const [reason, setReason] = useState<ReviewReasonFilter>('all')
  const [confirmingBatch, setConfirmingBatch] = useState(false)

  useEffect(() => {
    const timer = setTimeout(
      () => setDebouncedSearch(search.trim()),
      FILTER_DEBOUNCE_MS,
    )
    return () => clearTimeout(timer)
  }, [search])

  const autoMatchLote = useMutation({
    mutationFn: () => autoMatchBatch(),
    onSuccess: (resultado: AutoMatchBatchResult) => {
      qc.invalidateQueries({ queryKey: ['review'] })
      qc.invalidateQueries({ queryKey: ['entities'] })
      setResumoLote(
        t('review.queue.autoSyncSummary', {
          resolved: resultado.resolved_count,
          pending: resultado.pending_count,
        }),
      )
    },
  })

  const noFileLabel = t('review.group.noFileLabel')
  const cards = useMemo(() => data ?? [], [data])
  const filtered = useMemo(
    () =>
      filterReviewItems(
        cards,
        { query: debouncedSearch, kind, reason },
        noFileLabel,
      ),
    [cards, debouncedSearch, kind, reason, noFileLabel],
  )
  const grupos = useMemo(
    () => sortReviewGroups(groupReviewItems(filtered, noFileLabel), locale),
    [filtered, noFileLabel, locale],
  )
  // Kinds present in the queue, in the fixed order of the scanner — the filter only
  // offers what exists, instead of a list of empty options.
  const kinds = useMemo(() => {
    const present = [...new Set(cards.map(groupKind))]
    return present.sort((a, b) => {
      const ia = ORDEM_KINDS.indexOf(a)
      const ib = ORDEM_KINDS.indexOf(b)
      const pa = ia === -1 ? ORDEM_KINDS.length : ia
      const pb = ib === -1 ? ORDEM_KINDS.length : ib
      return pa !== pb ? pa - pb : a.localeCompare(b)
    })
  }, [cards])
  const filtrando = debouncedSearch !== '' || kind !== 'all' || reason !== 'all'

  function limparFiltros() {
    setSearch('')
    setDebouncedSearch('')
    setKind('all')
    setReason('all')
  }

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <ScreenHeader
        title={t('review.queue.title')}
        subtitle={t('review.queue.subtitle')}
        actions={
          <Button
            size="sm"
            variant="outline"
            disabled={autoMatchLote.isPending || cards.length === 0}
            onClick={() => {
              setResumoLote(null)
              setConfirmingBatch(true)
            }}
          >
            {autoMatchLote.isPending ? (
              <LoaderCircle className="animate-spin" aria-hidden />
            ) : (
              <Wand2 aria-hidden />
            )}
            {autoMatchLote.isPending
              ? t('review.queue.syncing')
              : t('review.queue.autoSyncAll')}
          </Button>
        }
      />

      {/* The percentages of the copy mirror the backend thresholds: 70 is
          `LIMIAR_BAIXA_CONFIANCA` (scanner) and 92 is `matching.LimiarAutoMatch`. */}
      <p className="mb-3 text-xs text-muted-foreground">
        {t('review.queue.thresholdHint')}
      </p>

      <p
        role="status"
        className={aviso ? 'mb-3 text-sm text-muted-foreground' : 'sr-only'}
      >
        {aviso}
      </p>
      {resumoLote ? (
        <p role="status" className="mb-3 text-sm text-muted-foreground">
          {resumoLote}
        </p>
      ) : null}

      {cards.length > 0 ? (
        <>
          <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
            <div className="relative min-w-0 flex-1">
              <Search
                className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
                aria-hidden
              />
              <Input
                type="search"
                aria-label={t('review.filters.searchLabel')}
                placeholder={t('review.filters.searchPlaceholder')}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-8"
              />
            </div>
            <div className="flex min-w-0 gap-2">
              <Select
                aria-label={t('review.filters.kindLabel')}
                value={kind}
                onChange={(e) => setKind(e.target.value)}
                className="sm:w-40"
              >
                <option value="all">{t('review.filters.kindAll')}</option>
                {kinds.map((k) => (
                  <option key={k} value={k}>
                    {rotuloKind(k, t)}
                  </option>
                ))}
              </Select>
              <Select
                aria-label={t('review.filters.reasonLabel')}
                value={reason}
                onChange={(e) =>
                  setReason(e.target.value as ReviewReasonFilter)
                }
                className="sm:w-44"
              >
                <option value="all">{t('review.filters.reasonAll')}</option>
                <option value="not_identified">
                  {t('review.filters.reasonNotIdentified')}
                </option>
                <option value="low_confidence">
                  {t('review.filters.reasonLowConfidence')}
                </option>
              </Select>
            </div>
          </div>

          <div className="mb-3 flex flex-wrap items-center gap-2">
            <p className="text-xs text-muted-foreground">
              {filtrando
                ? t(pluralKey('review.queue.filteredOf', cards.length), {
                    shown: filtered.length,
                    total: cards.length,
                  })
                : t(pluralKey('common.label.countItems', filtered.length), {
                    n: filtered.length,
                  })}{' '}
              {t(pluralKey('review.queue.countInGroups', grupos.length), {
                n: grupos.length,
              })}
            </p>
            {filtrando && grupos.length > 0 ? (
              <Button
                size="sm"
                variant="ghost"
                onClick={limparFiltros}
                className="text-xs"
              >
                {t('review.filters.clear')}
              </Button>
            ) : null}
          </div>
        </>
      ) : null}

      {isPending ? (
        <LoadingState label={t('review.queue.loading')} />
      ) : isError ? (
        <ErrorState
          title={t('review.queue.loadError')}
          message={error instanceof Error ? error.message : undefined}
          onRetry={() => {
            void refetch()
          }}
        />
      ) : cards.length === 0 ? (
        <EmptyState
          title={t('review.queue.emptyTitle')}
          description={t('review.queue.emptyDescription')}
        />
      ) : grupos.length === 0 ? (
        <EmptyState
          title={t('review.filters.noResultsTitle')}
          description={t('review.filters.noResultsDescription')}
          action={
            <Button variant="outline" onClick={limparFiltros}>
              {t('review.filters.clear')}
            </Button>
          }
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {grupos.map((grupo) => (
            <SecaoRevisao key={grupo.key} grupo={grupo} onApplied={setAviso} />
          ))}
        </ul>
      )}

      <ConfirmDialog
        open={confirmingBatch}
        onOpenChange={setConfirmingBatch}
        title={t('review.queue.autoSyncConfirmTitle')}
        description={t(
          pluralKey('review.queue.autoSyncConfirm', cards.length),
          { n: cards.length },
        )}
        action={t('review.queue.autoSyncConfirmAction')}
        busy={autoMatchLote.isPending}
        onConfirm={() => {
          setConfirmingBatch(false)
          setResumoLote(null)
          autoMatchLote.mutate()
        }}
      />
    </main>
  )
}

// Confirmation shared by the actions that WRITE (keeping the identity, re-enriching the
// queue): the same `AlertDialog` of the destructive flows, with the verb on the action.
// It always asks — a single tap that changes the identity of hundreds of items cannot be
// silent.
function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  action,
  busy,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  action: string
  busy?: boolean
  onConfirm: () => void
}) {
  const t = useT()
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common.action.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            disabled={busy}
            onClick={onConfirm}
            className="bg-primary text-primary-foreground hover:bg-primary/80"
          >
            {action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

// ── Grouping ────────────────────────────────────────────────────────────────

// ORDEM_KINDS is only the order of the FILTER options now: the sections themselves come
// from `sortReviewGroups`, biggest first.
const ORDEM_KINDS = ['pasta', 'serie', 'album', 'sem_arquivo']

const CHAVE_KIND: Record<string, string> = {
  pasta: 'review.group.kind.pasta',
  serie: 'review.group.kind.serie',
  album: 'review.group.kind.album',
  sem_arquivo: 'review.group.kind.semArquivo',
}

// Label of the section kind; a kind the catalog does not know echoes raw.
function rotuloKind(kind: string, t: Translate): string {
  const chave = CHAVE_KIND[kind]
  return chave ? t(chave) : kind
}

// Reason for the review: draft = not identified; otherwise, low confidence
// with the percentage of the card (the backend only queues the ones < the threshold).
function motivoRevisao(card: EntityCard, t: Translate): string {
  if (reviewReason(card) === 'not_identified') {
    return t('review.row.notIdentified')
  }
  return t('review.row.lowConfidence', { score: card.match_confidence ?? 0 })
}

// resumoDoGrupo describes the closed section: number of items + the DOMINANT
// reason (the first item already comes ordered from the backend by worst confidence).
function resumoDoGrupo(grupo: ReviewGroup, t: Translate): string {
  const itens = grupo.items.length
  const contagem = t(pluralKey('common.label.countItems', itens), { n: itens })
  return `${contagem} · ${motivoRevisao(grupo.items[0], t)}`
}

// Invalidation after confirming/correcting: the queue (`['review']`) changes, the grid
// (`['entities']`) reflects the new state and the detail (`['entity', id]`, prefix)
// drops all the language variations.
function invalidarRevisao(qc: QueryClient, id: string) {
  qc.invalidateQueries({ queryKey: ['review'] })
  qc.invalidateQueries({ queryKey: ['entities'] })
  qc.invalidateQueries({ queryKey: ['entity', id] })
}

// Label of the source of the candidate: "local" came from the catalog already
// imported (it merges via `fundirEntities`), "external" came from the remote provider
// (same path as "Correct"). Used both by the warning of the batch auto-match
// and by the source badge of the preview (`PreviewBloco`).
function rotuloMetodo(
  method: 'local' | 'external' | 'none',
  t: Translate,
): string {
  return method === 'local'
    ? t('review.preview.methodLocal')
    : t('review.preview.methodExternal')
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

function SecaoRevisao({
  grupo,
  onApplied,
}: {
  grupo: ReviewGroup
  onApplied: (message: string) => void
}) {
  const t = useT()
  const qc = useQueryClient()
  const [aberto, setAberto] = useState(false)
  const [resumo, setResumo] = useState<string | null>(null)
  // An open group renders at most `REVIEW_PAGE_SIZE` rows: "No file" alone has 422
  // items and used to take the page to 42,206 px.
  const [shown, setShown] = useState(REVIEW_PAGE_SIZE)
  const [confirmingGroup, setConfirmingGroup] = useState(false)

  const autoMatchGrupo = useMutation({
    mutationFn: () => autoMatchBatch(grupo.items.map((item) => item.id)),
    onSuccess: (resultado: AutoMatchBatchResult) => {
      qc.invalidateQueries({ queryKey: ['review'] })
      qc.invalidateQueries({ queryKey: ['entities'] })
      setResumo(
        t('review.group.summary', {
          resolved: resultado.resolved_count,
          pending: resultado.pending_count,
        }),
      )
    },
  })

  const context = groupContext(grupo.path, grupo.label)
  const items = grupo.items.slice(0, shown)
  const remaining = grupo.items.length - items.length

  return (
    <li className="rounded-xl bg-card ring-1 ring-foreground/10">
      <button
        type="button"
        data-testid="review-grupo"
        aria-expanded={aberto}
        onClick={() => setAberto((v) => !v)}
        className="flex w-full min-w-0 items-center gap-2 rounded-xl p-3 text-left hover:bg-muted/40"
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
          {context ? (
            <span className="line-clamp-1 text-xs text-muted-foreground">
              {context}
            </span>
          ) : null}
          <span className="text-xs text-muted-foreground">
            {resumoDoGrupo(grupo, t)}
          </span>
        </span>
        <Badge variant="outline" className="shrink-0">
          {rotuloKind(grupo.kind, t)}
        </Badge>
      </button>

      {aberto ? (
        <div className="flex min-w-0 flex-col gap-2 border-t border-foreground/10 p-2">
          <div className="flex flex-wrap items-center gap-2 px-1">
            <Button
              size="sm"
              variant="outline"
              disabled={autoMatchGrupo.isPending}
              onClick={() => {
                setResumo(null)
                setConfirmingGroup(true)
              }}
            >
              {autoMatchGrupo.isPending ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Wand2 aria-hidden />
              )}
              {autoMatchGrupo.isPending
                ? t('review.queue.syncing')
                : t('review.group.autoSync')}
            </Button>
            <span className="text-xs text-muted-foreground">
              {t('review.group.shownOf', {
                shown: items.length,
                total: grupo.items.length,
              })}
            </span>
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
          <ul className="flex min-w-0 flex-col">
            {items.map((card) => (
              <ReviewRow key={card.id} card={card} onApplied={onApplied} />
            ))}
          </ul>
          {remaining > 0 ? (
            <Button
              size="sm"
              variant="outline"
              className="mx-1 self-start"
              onClick={() => setShown((v) => v + REVIEW_PAGE_SIZE)}
            >
              {t('review.group.showMore', {
                n: Math.min(remaining, REVIEW_PAGE_SIZE),
              })}
            </Button>
          ) : null}
        </div>
      ) : null}

      <ConfirmDialog
        open={confirmingGroup}
        onOpenChange={setConfirmingGroup}
        title={t('review.group.autoSyncConfirmTitle')}
        description={t(
          pluralKey('review.group.autoSyncConfirm', grupo.items.length),
          { n: grupo.items.length },
        )}
        action={t('review.queue.autoSyncConfirmAction')}
        busy={autoMatchGrupo.isPending}
        onConfirm={() => {
          setConfirmingGroup(false)
          setResumo(null)
          autoMatchGrupo.mutate()
        }}
      />
    </li>
  )
}

// ── Compact row + detail under demand ───────────────────────────────────────

function ReviewRow({
  card,
  onApplied,
}: {
  card: EntityCard
  onApplied: (message: string) => void
}) {
  const t = useT()
  const qc = useQueryClient()
  const [falhouArte, setFalhouArte] = useState(false)
  const [expandido, setExpandido] = useState(false)
  const [corrigindo, setCorrigindo] = useState(false)
  // "Keep as is" WRITES the current identity and re-enriches the item: it asks first,
  // like the other flows that write (AlertDialog with the verb on the action).
  const [confirmingKeep, setConfirmingKeep] = useState(false)
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
    onSuccess: () => {
      invalidarRevisao(qc, card.id)
      onApplied(t('review.row.appliedKeep', { title: card.title }))
    },
  })

  // Applies exactly the candidate shown by the preview — local (merges) or
  // external (writes the identity + re-enriches), even below the automatic
  // threshold (a human decision informed by the card, not a silent cut).
  const confirmarCandidato = useMutation({
    mutationFn: () => {
      const dadosPrevia = preview.data
      if (!candidatoValido(dadosPrevia)) {
        throw new Error('no candidate to confirm')
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
    onSuccess: () => {
      invalidarRevisao(qc, card.id)
      onApplied(t('review.row.appliedCandidate', { title: card.title }))
    },
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
    <li className="flex min-w-0 flex-col gap-2 rounded-lg px-1 py-1.5 hover:bg-muted/30">
      <div className="flex min-w-0 items-center gap-2">
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
              alt={t('review.row.posterAlt', { title: card.title })}
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
            {rotuloTipo(card.type, t)} · {motivoRevisao(card, t)}
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
          {expandido ? t('common.action.close') : t('review.row.showCandidate')}
        </Button>
      </div>

      {expandido ? (
        <div className="flex min-w-0 flex-col gap-2 sm:pl-12">
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
              onClick={() => {
                if (achouCandidato) {
                  confirmarCandidato.mutate()
                } else {
                  setConfirmingKeep(true)
                }
              }}
            >
              {acaoPendente ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Check aria-hidden />
              )}
              {achouCandidato
                ? acaoPendente
                  ? t('review.row.confirming')
                  : t('review.row.confirmCandidate')
                : acaoPendente
                  ? t('review.row.confirming')
                  : t('review.row.keepAsIs')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              aria-expanded={corrigindo}
              onClick={() => setCorrigindo((v) => !v)}
            >
              <PencilLine aria-hidden />
              {t('review.row.correct')}
            </Button>
            <Button asChild size="sm" variant="ghost">
              <Link to="/entities/$id" params={{ id: card.id }}>
                {t('review.row.viewDetail')}
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
              {t('review.row.mergeNotice')}
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

      <ConfirmDialog
        open={confirmingKeep}
        onOpenChange={setConfirmingKeep}
        title={t('review.row.keepConfirmTitle')}
        description={t('review.row.keepConfirmDescription')}
        action={t('review.row.keepConfirmAction')}
        busy={confirmar.isPending}
        onConfirm={() => {
          setConfirmingKeep(false)
          confirmar.mutate()
        }}
      />
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
  const t = useT()
  const locale = useLocale()
  const [falhouArte, setFalhouArte] = useState(false)

  if (isPending) {
    return (
      <div
        role="status"
        className="flex items-center gap-2 rounded-lg bg-muted/40 p-2 text-muted-foreground"
      >
        <LoaderCircle className="size-4 animate-spin" aria-hidden />
        <span className="text-xs">{t('review.preview.searching')}</span>
      </div>
    )
  }

  if (isError) {
    return (
      <p role="alert" className="text-xs font-medium text-destructive">
        {t('review.preview.searchError')}
      </p>
    )
  }

  if (!data || data.found === false || !data.target) {
    return (
      <p className="text-xs text-muted-foreground">
        {t('review.preview.none')}
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
            alt={t('review.row.posterAlt', { title: target.title })}
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
          <Badge variant="outline">
            {t('review.preview.confidence', {
              score: new Intl.NumberFormat(locale, {
                maximumFractionDigits: 1,
              }).format(data.score),
            })}
          </Badge>
          <Badge variant="secondary">{rotuloMetodo(data.method, t)}</Badge>
          {data.meets_auto_threshold ? (
            <Badge>{t('review.preview.strongMatch')}</Badge>
          ) : (
            <Badge variant="outline">
              {t('review.preview.checkBeforeConfirming')}
            </Badge>
          )}
        </div>
        {target.matched_title && target.matched_title !== target.title ? (
          <p className="line-clamp-1 text-xs text-muted-foreground">
            {t('review.preview.alternateTitle', {
              title: target.matched_title,
            })}
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
  const t = useT()
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
          aria-label={t('review.correction.searchLabel')}
          placeholder={t('review.correction.searchPlaceholder')}
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
  const t = useT()
  if (query === '') {
    return (
      <p className="text-sm text-muted-foreground">
        {t('review.correction.hint')}
      </p>
    )
  }
  if (isPending) {
    return <LoadingState label={t('review.correction.searching')} />
  }
  if (isError) {
    return (
      <ErrorState
        title={t('review.correction.searchFailed')}
        message={error instanceof Error ? error.message : undefined}
      />
    )
  }
  if (!disponivel) {
    return (
      <ErrorState
        title={t('review.correction.unavailableTitle')}
        message={t('review.correction.unavailableMessage')}
      />
    )
  }
  if (externos.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {t('review.correction.noResults')}
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
