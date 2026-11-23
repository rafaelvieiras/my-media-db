import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  CircleCheck,
  CircleX,
  ChevronDown,
  LoaderCircle,
  MinusCircle,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { LoadingState, ErrorState } from '@/components/query-states'
import { queueItemQueryOptions } from '@/lib/queries'
import { formatarCarimbo, formatarDuracaoMS } from '@/lib/format'
import { useLocale, useT } from '@/lib/i18n'
import type { QueueEvent, QueueItem } from '@/lib/api'

// QueueItemCard renders ONE run of the execution queue (ADR-0040): what it is
// (scan or job, and of which reference), how far it got (step, counters, current
// item), how long it took (or has been taking) and — on demand — the trail of what
// it already did to each item.
//
// The same card serves the three lists of the screen: running, waiting and
// recently finished. What differs between them is only what the numbers mean
// (`running for`, `waiting`, `took`) and the position in line.

// Trail rows rendered at once. The server caps the trail at 200 rows, which used to
// land in the DOM in one block (a 13 000 px card): the rest arrives on demand.
const RASTRO_PASSO = 20

// numero writes a count in the interface locale ("36,599" in English, "36.599" in
// pt-BR): raw five-digit numbers next to locale-formatted bytes read as noise.
function numero(valor: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(valor)
}

// etiquetaDoStatus maps the queue status to its catalog label.
function etiquetaDoStatus(status: QueueItem['status']): string {
  switch (status) {
    case 'queued':
      return 'queue.status.queued'
    case 'running':
      return 'queue.status.running'
    case 'success':
      return 'queue.status.success'
    default:
      return 'queue.status.error'
  }
}

// corDoStatus: finished well is green, failure is destructive, the rest is muted.
function corDoStatus(status: QueueItem['status']): string {
  switch (status) {
    case 'success':
      return 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
    case 'error':
      return 'bg-destructive/10 text-destructive'
    case 'running':
      return 'bg-sky-500/15 text-sky-700 dark:text-sky-400'
    default:
      return 'bg-muted text-muted-foreground'
  }
}

// corDaBarra: the progress bar used to be sky for every state — a finished run with
// an error looked exactly like one still going. Running is sky, done is emerald,
// failure is destructive; a waiting run has nothing to show yet.
function corDaBarra(status: QueueItem['status']): string {
  switch (status) {
    case 'success':
      return 'bg-emerald-600'
    case 'error':
      return 'bg-destructive'
    case 'running':
      return 'bg-sky-600'
    default:
      return 'bg-muted-foreground/40'
  }
}

export function QueueItemCard({ item }: { item: QueueItem }) {
  const t = useT()
  const locale = useLocale()
  const [rastroAberto, setRastroAberto] = useState(false)

  const corrido = formatarDuracaoMS(item.elapsed_ms)
  const levou = formatarDuracaoMS(item.duration_ms)
  const esperou = formatarDuracaoMS(item.wait_ms)

  const referencia = item.ref_label ?? item.ref_id
  const tipo =
    item.kind === 'scan' ? t('queue.item.kindScan') : t('queue.item.kindJob')

  // Running and waiting runs get the "it is happening" line; finished ones get
  // how long they took.
  let linhaDeTempo: string | null = null
  if (item.status === 'running' && corrido) {
    linhaDeTempo = t('queue.item.running', { duration: corrido })
  } else if (item.status === 'queued') {
    linhaDeTempo = esperou
      ? t('queue.item.waitingFor', { duration: esperou })
      : null
  } else if (levou) {
    linhaDeTempo = t('queue.item.took', { duration: levou })
  }

  const percentual =
    item.total > 0
      ? Math.min(100, Math.round((item.done / item.total) * 100))
      : 0
  const temEtapa = item.step_total > 0 && item.step_label

  // The two counters of a run measure DIFFERENT things (the scan counts files seen
  // of files found, the counters count reported items), so each one carries its
  // noun and `ok` hides when nothing succeeded.
  const contagem =
    item.ok > 0
      ? t('queue.item.okFailedItems', {
          ok: numero(item.ok, locale),
          failed: numero(item.failed, locale),
        })
      : item.failed > 0
        ? t('queue.item.failedItems', {
            failed: numero(item.failed, locale),
          })
        : null
  const arquivos = t('queue.item.progressFiles', {
    done: numero(item.done, locale),
    total: numero(item.total, locale),
  })

  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-0.5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
              {tipo}
            </span>
            <span
              className="line-clamp-2 font-heading text-base font-medium break-words"
              title={referencia}
            >
              {referencia}
            </span>
            {item.position ? (
              <Badge variant="outline" className="shrink-0">
                {t('queue.item.position', { position: item.position })}
              </Badge>
            ) : null}
          </div>
          <span className="text-xs text-muted-foreground">
            {t(`queue.trigger.${item.trigger}`)}
            {item.started_at
              ? ` · ${t('queue.item.startedAt', { when: formatarCarimbo(item.started_at, locale) })}`
              : null}
            {item.finished_at
              ? ` · ${t('queue.item.finishedAt', { when: formatarCarimbo(item.finished_at, locale) })}`
              : null}
          </span>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {item.status === 'running' ? (
            <LoaderCircle
              className="size-3.5 animate-spin text-sky-600"
              aria-hidden
            />
          ) : null}
          <span
            className={`rounded-full px-2 py-0.5 text-xs font-medium ${corDoStatus(item.status)}`}
          >
            {t(etiquetaDoStatus(item.status))}
          </span>
        </div>
      </div>

      {linhaDeTempo ? (
        <p className="text-sm text-muted-foreground">{linhaDeTempo}</p>
      ) : null}

      {temEtapa ? (
        <p className="text-sm">
          {t('queue.item.step', {
            index: item.step_index,
            total: item.step_total,
            label: item.step_label ?? '',
          })}
        </p>
      ) : null}

      {item.total > 0 ? (
        <div className="flex flex-col gap-1">
          <div
            role="status"
            aria-live="polite"
            className="flex flex-wrap items-center justify-between gap-x-2 text-xs text-muted-foreground"
          >
            <span>{arquivos}</span>
            {contagem ? <span>{contagem}</span> : null}
          </div>
          <div
            className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
            role="progressbar"
            aria-label={t('queue.item.progressbarLabel')}
            aria-valuemin={0}
            aria-valuemax={item.total}
            aria-valuenow={item.done}
            aria-valuetext={`${arquivos} (${percentual}%)`}
          >
            <div
              className={`h-full rounded-full transition-all ${corDaBarra(item.status)}`}
              style={{ width: `${percentual}%` }}
            />
          </div>
        </div>
      ) : null}

      <p className="text-sm break-words text-muted-foreground">
        {item.current_item
          ? t('queue.item.currentItem', { item: item.current_item })
          : t('queue.item.unknownItem')}
      </p>

      {item.message ? (
        <p className="text-sm text-muted-foreground">{item.message}</p>
      ) : null}

      <div>
        <Button
          variant="ghost"
          size="sm"
          className="gap-1 px-0 text-muted-foreground hover:bg-transparent hover:text-foreground"
          onClick={() => setRastroAberto((aberto) => !aberto)}
          aria-expanded={rastroAberto}
        >
          <ChevronDown
            className={`size-3.5 transition-transform ${rastroAberto ? 'rotate-180' : ''}`}
            aria-hidden
          />
          {rastroAberto ? t('queue.item.hideTrail') : t('queue.item.showTrail')}
        </Button>
        {rastroAberto ? <RastroDoItem id={item.id} /> : null}
      </div>
    </li>
  )
}

// RastroDoItem is the trail of ONE run, fetched only when the owner expands it
// (up to 200 rows per run — not something to load for every card of the list) and
// rendered RASTRO_PASSO rows at a time inside a scroll box: 200 rows in the page
// flow made the card 13 000 px tall and pushed the next one out of reach.
function RastroDoItem({ id }: { id: string }) {
  const t = useT()
  const locale = useLocale()
  const [visiveis, setVisiveis] = useState(RASTRO_PASSO)
  const { data, isPending, isError, error, refetch } = useQuery(
    queueItemQueryOptions(id),
  )

  if (isPending) {
    return <LoadingState label={t('queue.item.trailLoading')} />
  }
  if (isError) {
    return (
      <ErrorState
        title={t('queue.item.trailError')}
        message={error instanceof Error ? error.message : undefined}
        onRetry={() => void refetch()}
      />
    )
  }
  if (!data || data.events.length === 0) {
    return (
      <p className="pt-2 text-sm text-muted-foreground">
        {t('queue.item.trailEmpty')}
      </p>
    )
  }

  const total = data.events.length
  const mostrados = data.events.slice(0, visiveis)

  return (
    <div className="flex flex-col gap-1 pt-1">
      <p className="text-xs text-muted-foreground">
        {t('queue.item.trailCount', {
          shown: numero(mostrados.length, locale),
          total: numero(total, locale),
        })}
      </p>
      <ul className="flex max-h-72 flex-col divide-y divide-border/60 overflow-y-auto">
        {mostrados.map((evento) => (
          <EventoDoRastro key={evento.seq} evento={evento} locale={locale} />
        ))}
      </ul>
      {visiveis < total ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="self-start"
          onClick={() => setVisiveis((n) => n + RASTRO_PASSO)}
        >
          {t('queue.item.trailMore')}
        </Button>
      ) : null}
    </div>
  )
}

function EventoDoRastro({
  evento,
  locale,
}: {
  evento: QueueEvent
  locale: string
}) {
  const t = useT()
  const Icone =
    evento.status === 'ok'
      ? CircleCheck
      : evento.status === 'error'
        ? CircleX
        : MinusCircle
  const cor =
    evento.status === 'ok'
      ? 'text-emerald-600 dark:text-emerald-400'
      : evento.status === 'error'
        ? 'text-destructive'
        : 'text-muted-foreground'

  return (
    <li className="flex items-start gap-2 py-1.5">
      <Icone className={`mt-0.5 size-3.5 shrink-0 ${cor}`} aria-hidden />
      <div className="flex min-w-0 flex-col">
        <span className="text-sm break-words">{evento.label}</span>
        <span className="text-xs text-muted-foreground">
          {t(`queue.event.${evento.status}`)} ·{' '}
          {formatarCarimbo(evento.at, locale)}
          {evento.detail ? ` · ${evento.detail}` : null}
        </span>
      </div>
    </li>
  )
}
