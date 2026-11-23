import { useQuery } from '@tanstack/react-query'
import { ScreenHeader } from '@/components/screen-header'
import { QueueItemCard } from '@/components/queue-item-card'
import { EmptyState, LoadingState, ErrorState } from '@/components/query-states'
import { queueQueryOptions, useFreshnessSeconds } from '@/lib/queries'
import { useT } from '@/lib/i18n'

// Route `/dashboard/queue`: the execution queue in one screen (ADR-0040, issue
// #62) — what runs now, what waits in FIFO order and what finished recently, all
// from a single request. It is the answer to "is it stuck, and on what": the
// running card carries the step, the counters, the item being processed and how
// long it has been running.
//
// It lives next to `/dashboard/jobs` because it shows the SAME runs the jobs
// screen triggers (plus the scans, which have no screen of their own).
export function QueueScreen() {
  const t = useT()
  const { data, isPending, isError, error, refetch, dataUpdatedAt } =
    useQuery(queueQueryOptions)
  // The screen polls every 4s while something runs; without this line the counters
  // moved on their own and nothing said the page was alive.
  const idade = useFreshnessSeconds(dataUpdatedAt)

  const vazia =
    !data ||
    (!data.running && data.queued.length === 0 && data.recent.length === 0)

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <ScreenHeader
        title={t('queue.screen.title')}
        subtitle={t('queue.screen.subtitle')}
      />

      {idade !== null ? (
        <p className="-mt-2 mb-4 text-xs text-muted-foreground">
          {idade <= 1
            ? t('common.live.updatedJustNow')
            : t('common.live.updatedAgo', { seconds: idade })}
        </p>
      ) : null}

      {isPending ? (
        <LoadingState label={t('queue.screen.loading')} />
      ) : isError ? (
        <ErrorState
          title={t('queue.screen.loadError')}
          message={error instanceof Error ? error.message : undefined}
          onRetry={() => void refetch()}
        />
      ) : vazia ? (
        <EmptyState
          title={t('queue.screen.emptyTitle')}
          description={t('queue.screen.emptyDescription')}
        />
      ) : (
        <div className="flex flex-col gap-6">
          {data.running ? (
            <section className="flex flex-col gap-2">
              <h2 className="font-heading text-lg font-medium">
                {t('queue.sections.running')}
              </h2>
              <ul className="flex flex-col gap-3">
                <QueueItemCard item={data.running} />
              </ul>
            </section>
          ) : null}

          {data.queued.length > 0 ? (
            <section className="flex flex-col gap-2">
              <h2 className="font-heading text-lg font-medium">
                {t('queue.sections.queued')}
              </h2>
              <ul className="flex flex-col gap-3">
                {data.queued.map((item) => (
                  <QueueItemCard key={item.id} item={item} />
                ))}
              </ul>
            </section>
          ) : null}

          {data.recent.length > 0 ? (
            <section className="flex flex-col gap-2">
              <h2 className="font-heading text-lg font-medium">
                {t('queue.sections.recent')}{' '}
                <span className="text-sm font-normal text-muted-foreground">
                  {t('queue.sections.recentCount', {
                    count: data.recent.length,
                  })}
                </span>
              </h2>
              <ul className="flex flex-col gap-3">
                {data.recent.map((item) => (
                  <QueueItemCard key={item.id} item={item} />
                ))}
              </ul>
            </section>
          ) : null}
        </div>
      )}
    </main>
  )
}
