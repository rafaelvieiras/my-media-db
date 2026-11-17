import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleCheck, Trash2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ErrorState, LoadingState } from '@/components/query-states'
import {
  ApiError,
  deleteHistoricoEvento,
  type HistoricoEvento,
} from '@/lib/api'
import {
  formatarCarimboHistorico,
  KIND_ASSISTIR,
  rotuloOrigemHistorico,
  rotuloStatusHistorico,
  STATUS_COMPLETED,
} from '@/lib/historico'
import { useLocale, useT } from '@/lib/i18n'
import {
  historicoEstadoQueryOptions,
  historicoEventosQueryOptions,
} from '@/lib/queries'

// Consumption history (it gives a face to ADR-0022): the "watched"
// state badge (`EstadoAssistidoBadge`) + event timeline
// (`HistoricoSection`), companions of the `RichDetailView` in the same pattern as the
// `SimilaresRail` (empty → the section disappears entirely, without an active "not
// watched" nor an empty timeline polluting the screen). The pure labels/constants live in
// `lib/historico.ts` (a module that exports a component + a const triggers
// `react-refresh/only-export-components`).

// EstadoAssistidoBadge is the "watched" badge (with a date) of the hero/card — it reads
// `historicoEstadoQueryOptions` (kind="assistir") and only appears when the MOST RECENT
// event of that kind is `completed` (watching in progress/paused does not
// light the badge). With no event at all — or error/loading — the badge simply
// does not appear: never an active "not watched" badge (same principle of
// "signal only what needs positive attention" already used in the project).
export function EstadoAssistidoBadge({ entityId }: { entityId: string }) {
  const t = useT()
  const locale = useLocale()
  const { data } = useQuery(historicoEstadoQueryOptions(entityId))
  const assistir = data?.estado?.[KIND_ASSISTIR]
  if (!assistir || assistir.status !== STATUS_COMPLETED) {
    return null
  }
  const rotulo = t('review.history.badge', {
    date: formatarCarimboHistorico(assistir.occurred_at, locale),
  })
  return (
    <Badge variant="secondary" className="gap-1" title={rotulo}>
      <CircleCheck className="size-3.5" aria-hidden />
      {rotulo}
    </Badge>
  )
}

// invalidarHistorico invalidates the WHOLE `['historico', id]` prefix (events of
// any kind + the derived state) — the same prefix invalidation convention
// already used in the rest of the project (`['entity', id]`, `['assets', id]`).
function invalidarHistorico(
  queryClient: ReturnType<typeof useQueryClient>,
  entityId: string,
) {
  queryClient.invalidateQueries({ queryKey: ['historico', entityId] })
}

// EventoRow is ONE row of the timeline: status + date + origin, with the
// delete control in a 2-STEP confirmation (× → "Delete?"/"Cancel") — the same pattern
// already used by `MembroEditavel` (collection-members.tsx) to remove an item from
// a list, lighter than the `AlertDialog` modal reserved for bigger
// destructive actions (e.g. "Delete collection").
function EventoRow({
  entityId,
  evento,
}: {
  entityId: string
  evento: HistoricoEvento
}) {
  const t = useT()
  const locale = useLocale()
  const queryClient = useQueryClient()
  const [confirmando, setConfirmando] = useState(false)
  const apagar = useMutation({
    mutationFn: () => deleteHistoricoEvento(entityId, evento.id),
    onSuccess: () => invalidarHistorico(queryClient, entityId),
  })
  const erro =
    apagar.error instanceof ApiError
      ? apagar.error.message
      : apagar.isError
        ? t('review.history.deleteFailed')
        : null

  return (
    <li className="flex flex-col gap-1 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <Badge variant="outline">
            {rotuloStatusHistorico(evento.status, t)}
          </Badge>
          <span className="text-muted-foreground">
            {formatarCarimboHistorico(evento.occurred_at, locale)}
          </span>
          <span className="text-xs text-muted-foreground">
            · {rotuloOrigemHistorico(evento.source, t)}
          </span>
        </div>
        {confirmando ? (
          <div className="flex items-center gap-1">
            <Button
              variant="destructive"
              size="xs"
              disabled={apagar.isPending}
              onClick={() => apagar.mutate()}
            >
              {apagar.isPending
                ? t('review.history.deleting')
                : t('review.history.confirmDelete')}
            </Button>
            <Button
              variant="ghost"
              size="xs"
              disabled={apagar.isPending}
              onClick={() => setConfirmando(false)}
            >
              {t('common.action.cancel')}
            </Button>
          </div>
        ) : (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t('review.history.deleteLabel')}
            onClick={() => setConfirmando(true)}
          >
            <Trash2 aria-hidden />
          </Button>
        )}
      </div>
      {erro ? (
        <span role="alert" className="text-xs font-medium text-destructive">
          {erro}
        </span>
      ) : null}
    </li>
  )
}

// HistoricoSection is the "History" section of the item detail:
// it lists ALL the events (any kind, agnostic of vertical) in reverse
// chronological order (occurred_at DESC, already like that from the backend). Same pattern as the
// `SimilaresRail`: empty → the section disappears entirely (no loose heading).
// Loading/error show the section with the heading (transient) — consistent with
// the rest of the detail (`MediaInfoTable`/`ArtGallery`).
export function HistoricoSection({ entityId }: { entityId: string }) {
  const t = useT()
  const query = useQuery(historicoEventosQueryOptions(entityId))

  if (query.isPending) {
    return (
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-medium">
          {t('review.history.sectionTitle')}
        </h2>
        <LoadingState label={t('review.history.loading')} />
      </section>
    )
  }
  if (query.isError) {
    return (
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-medium">
          {t('review.history.sectionTitle')}
        </h2>
        <ErrorState
          title={t('review.history.loadError')}
          message={
            query.error instanceof Error ? query.error.message : undefined
          }
        />
      </section>
    )
  }
  if (query.data.eventos.length === 0) {
    return null
  }

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">
        {t('review.history.sectionTitle')}
      </h2>
      <ul className="flex flex-col gap-2">
        {query.data.eventos.map((evento) => (
          <EventoRow key={evento.id} entityId={entityId} evento={evento} />
        ))}
      </ul>
    </section>
  )
}
