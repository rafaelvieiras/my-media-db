import { useQuery } from '@tanstack/react-query'
import { DataScreen } from '@/components/data-screen'
import { QueueItemCard } from '@/components/queue-item-card'
import type { QueueView } from '@/lib/api'
import { queueQueryOptions } from '@/lib/queries'
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
  const query = useQuery(queueQueryOptions)
  const data = query.data

  // The screen polls every 4s while something runs; without the shell's line the
  // counters moved on their own and nothing said the page was alive.
  const vazia =
    !data ||
    (!data.running && data.queued.length === 0 && data.recent.length === 0)

  return (
    <DataScreen
      title={t('queue.screen.title')}
      subtitle={t('queue.screen.subtitle')}
      updatedAt={query.dataUpdatedAt}
      query={query}
      empty={vazia}
      copy={{
        loading: t('queue.screen.loading'),
        loadError: t('queue.screen.loadError'),
        emptyTitle: t('queue.screen.emptyTitle'),
        emptyDescription: t('queue.screen.emptyDescription'),
      }}
    >
      {data ? <QueueSections data={data} /> : null}
    </DataScreen>
  )
}

// QueueSections is the content of the screen once the query answered: the running
// run, the FIFO and the recent ones. It is a separate component so the empty
// branch of the shell stays simple and `data` is narrowed once.
function QueueSections({ data }: { data: QueueView }) {
  const t = useT()

  return (
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
  )
}
