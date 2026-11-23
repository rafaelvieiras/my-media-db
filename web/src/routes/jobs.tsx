import { useQuery } from '@tanstack/react-query'
import { ScreenHeader } from '@/components/screen-header'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { JobCard } from '@/components/job-card'
import { jobsQueryOptions, useFreshnessSeconds } from '@/lib/queries'
import { useT } from '@/lib/i18n'

// Route `/jobs`: the administration screen of the internal jobs (embedded
// scheduler — backup through VACUUM INTO, and future types registered in code).
// Grouped with `/settings` under "Administração" in the `AppHeader` — it is not
// everyday navigation. It lists the `job_definition` with the current state; each
// card takes care of enabling/disabling, scheduling, triggering and showing the history.
//
// The screen polls while a job runs, so it also carries the "updated Xs ago" line:
// a live value that changes by itself needs to say it is alive.
export function JobsScreen() {
  const t = useT()
  const { data, isPending, isError, error, refetch, dataUpdatedAt } =
    useQuery(jobsQueryOptions)
  const idade = useFreshnessSeconds(dataUpdatedAt)
  const jobs = data ?? []

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <ScreenHeader
        title={t('jobs.screen.title')}
        subtitle={t('jobs.screen.subtitle')}
      />

      {idade !== null ? (
        <p className="-mt-2 mb-4 text-xs text-muted-foreground">
          {idade <= 1
            ? t('common.live.updatedJustNow')
            : t('common.live.updatedAgo', { seconds: idade })}
        </p>
      ) : null}

      {isPending ? (
        <LoadingState label={t('jobs.screen.loading')} />
      ) : isError ? (
        <ErrorState
          title={t('jobs.screen.loadError')}
          message={error instanceof Error ? error.message : undefined}
          onRetry={() => void refetch()}
        />
      ) : jobs.length === 0 ? (
        <EmptyState
          title={t('jobs.screen.emptyTitle')}
          description={t('jobs.screen.emptyDescription')}
        />
      ) : (
        <ul className="flex flex-col gap-3">
          {jobs.map((job) => (
            <JobCard key={job.id} job={job} />
          ))}
        </ul>
      )}
    </main>
  )
}
