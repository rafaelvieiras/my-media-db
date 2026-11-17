import { useState } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ImageOff, LoaderCircle, Trash2, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { orphansQueryOptions } from '@/lib/queries'
import {
  ApiError,
  posterSrc,
  purgeBatch,
  purgeEntity,
  type EntityCard,
} from '@/lib/api'
import { aspectPorTipo, rotuloTipo } from '@/lib/entity-badges'
import { useT } from '@/lib/i18n'
import { ScreenHeader } from '@/components/screen-header'

// Invalidation after purging (individual or batch): the orphan queue changes and the grid
// (`['entities']`) loses the purged cards. There is no `['entity', id]` — the entity
// no longer exists.
function invalidarOrfas(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: ['orphans'] })
  qc.invalidateQueries({ queryKey: ['entities'] })
}

// The catalog has no ICU plural rules, so the caller picks the `_one`/`_other` suffix.
function pluralKey(base: string, n: number): string {
  return `${base}_${n === 1 ? 'one' : 'other'}`
}

// Route `/orphans`: the cleanup queue. Auto-created entities whose file
// disappeared and with no investment from the owner. The ONLY action is the PURGE — destructive, which is why it
// always has an EXPLICIT confirmation (a second step in the UI itself, without
// `window.confirm`): individual per card or in batch through the selection.
export function OrphansScreen() {
  const t = useT()
  const qc = useQueryClient()
  const { data, isPending, isError, error } = useQuery(orphansQueryOptions)
  const [selecionadas, setSelecionadas] = useState<Set<string>>(new Set())
  const [confirmandoLote, setConfirmandoLote] = useState(false)
  const [confirmandoId, setConfirmandoId] = useState<string | null>(null)

  const purgarLote = useMutation({
    mutationFn: (ids: string[]) => purgeBatch(ids),
    onSuccess: () => {
      invalidarOrfas(qc)
      setSelecionadas(new Set())
      setConfirmandoLote(false)
    },
  })

  const purgarUma = useMutation({
    mutationFn: (id: string) => purgeEntity(id),
    onSuccess: (_res, id) => {
      invalidarOrfas(qc)
      setSelecionadas((prev) => {
        const proximo = new Set(prev)
        proximo.delete(id)
        return proximo
      })
      setConfirmandoId(null)
    },
  })

  function alternar(id: string) {
    setSelecionadas((prev) => {
      const proximo = new Set(prev)
      if (proximo.has(id)) {
        proximo.delete(id)
      } else {
        proximo.add(id)
      }
      return proximo
    })
  }

  const cards = data ?? []
  const todasSelecionadas =
    cards.length > 0 && cards.every((c) => selecionadas.has(c.id))

  function alternarTodas() {
    setSelecionadas(
      todasSelecionadas ? new Set() : new Set(cards.map((c) => c.id)),
    )
  }

  const erroLote =
    purgarLote.error instanceof ApiError ? purgarLote.error.message : null

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <ScreenHeader
        title={t('review.orphans.title')}
        subtitle={t('review.orphans.subtitle')}
      />

      {isPending ? (
        <LoadingState label={t('review.orphans.loading')} />
      ) : isError ? (
        <ErrorState
          title={t('review.orphans.loadError')}
          message={error instanceof Error ? error.message : undefined}
        />
      ) : cards.length === 0 ? (
        <EmptyState
          title={t('review.orphans.emptyTitle')}
          description={t('review.orphans.emptyDescription')}
        />
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-3 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-primary"
                aria-label={t('review.orphans.selectAll')}
                checked={todasSelecionadas}
                onChange={alternarTodas}
              />
              <span>{t('review.orphans.selectAll')}</span>
            </label>
            <span className="text-sm text-muted-foreground">
              {t(pluralKey('review.orphans.selected', selecionadas.size), {
                n: selecionadas.size,
              })}
            </span>

            <div className="ml-auto flex flex-wrap items-center gap-2">
              {confirmandoLote ? (
                <ConfirmacaoInline
                  mensagem={t(
                    pluralKey('review.orphans.confirmBatch', selecionadas.size),
                    { n: selecionadas.size },
                  )}
                  ocupado={purgarLote.isPending}
                  onConfirmar={() => purgarLote.mutate([...selecionadas])}
                  onCancelar={() => setConfirmandoLote(false)}
                />
              ) : (
                <Button
                  variant="destructive"
                  size="sm"
                  disabled={selecionadas.size === 0}
                  onClick={() => setConfirmandoLote(true)}
                >
                  <Trash2 aria-hidden />
                  {t('review.orphans.purgeSelected')}
                </Button>
              )}
            </div>
          </div>

          {erroLote ? (
            <p role="alert" className="text-sm font-medium text-destructive">
              {erroLote}
            </p>
          ) : null}

          <ul className="flex flex-col gap-3">
            {cards.map((card) => (
              <OrphanCard
                key={card.id}
                card={card}
                selecionada={selecionadas.has(card.id)}
                onAlternar={() => alternar(card.id)}
                confirmando={confirmandoId === card.id}
                onPedirPurga={() => setConfirmandoId(card.id)}
                onCancelar={() => setConfirmandoId(null)}
                onConfirmar={() => purgarUma.mutate(card.id)}
                ocupado={purgarUma.isPending && confirmandoId === card.id}
                erro={
                  confirmandoId === card.id &&
                  purgarUma.error instanceof ApiError
                    ? purgarUma.error.message
                    : null
                }
              />
            ))}
          </ul>
        </div>
      )}
    </main>
  )
}

// Reusable explicit confirmation (the second step): the warning message plus
// the "Confirm purge" (destructive) and "Cancel" buttons. WITHOUT `window.confirm` —
// it is state of the UI itself, testable and cancelable.
function ConfirmacaoInline({
  mensagem,
  ocupado,
  onConfirmar,
  onCancelar,
}: {
  mensagem: string
  ocupado: boolean
  onConfirmar: () => void
  onCancelar: () => void
}) {
  const t = useT()
  return (
    <div
      role="alertdialog"
      aria-label={t('review.orphans.confirmTitle')}
      className="flex flex-wrap items-center gap-2"
    >
      <span className="flex items-center gap-1.5 text-sm font-medium text-destructive">
        <TriangleAlert className="size-4" aria-hidden />
        {mensagem}
      </span>
      <Button
        variant="destructive"
        size="sm"
        disabled={ocupado}
        onClick={onConfirmar}
      >
        {ocupado ? (
          <LoaderCircle className="animate-spin" aria-hidden />
        ) : (
          <Trash2 aria-hidden />
        )}
        {ocupado
          ? t('review.orphans.purging')
          : t('review.orphans.confirmPurge')}
      </Button>
      <Button variant="ghost" size="sm" disabled={ocupado} onClick={onCancelar}>
        {t('common.action.cancel')}
      </Button>
    </div>
  )
}

function OrphanCard({
  card,
  selecionada,
  onAlternar,
  confirmando,
  onPedirPurga,
  onCancelar,
  onConfirmar,
  ocupado,
  erro,
}: {
  card: EntityCard
  selecionada: boolean
  onAlternar: () => void
  confirmando: boolean
  onPedirPurga: () => void
  onCancelar: () => void
  onConfirmar: () => void
  ocupado: boolean
  erro: string | null
}) {
  const t = useT()
  const [falhouArte, setFalhouArte] = useState(false)
  const src = posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte

  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex items-center gap-3">
        <input
          type="checkbox"
          className="size-4 shrink-0 accent-primary"
          aria-label={t('review.orphans.selectItem', { title: card.title })}
          checked={selecionada}
          onChange={onAlternar}
        />
        <div
          className={`relative ${aspectPorTipo(card.type)} w-14 shrink-0 overflow-hidden rounded-lg bg-muted`}
        >
          {mostrarPlaceholder ? (
            <div
              data-testid="orphan-poster-placeholder"
              className="flex h-full w-full items-center justify-center text-muted-foreground"
            >
              <ImageOff className="size-5" aria-hidden />
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
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <Link
            to="/entities/$id"
            params={{ id: card.id }}
            className="line-clamp-1 font-medium hover:underline"
          >
            {card.title}
          </Link>
          <span className="text-xs text-muted-foreground">
            {rotuloTipo(card.type, t)}
          </span>
        </div>
        <div className="shrink-0">
          {confirmando ? (
            <ConfirmacaoInline
              mensagem={t('review.orphans.confirmOne')}
              ocupado={ocupado}
              onConfirmar={onConfirmar}
              onCancelar={onCancelar}
            />
          ) : (
            <Button
              variant="outline"
              size="sm"
              aria-label={t('review.orphans.purgeItem', { title: card.title })}
              onClick={onPedirPurga}
            >
              <Trash2 aria-hidden />
              {t('review.orphans.purge')}
            </Button>
          )}
        </div>
      </div>
      {erro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {erro}
        </p>
      ) : null}
    </li>
  )
}
