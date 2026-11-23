import { useQuery } from '@tanstack/react-query'
import { DataScreen } from '@/components/data-screen'
import { JobCard } from '@/components/job-card'
import { jobsQueryOptions } from '@/lib/queries'
import { useT } from '@/lib/i18n'

// Route `/jobs`: the administration screen of the internal jobs (embedded
// scheduler — backup through VACUUM INTO, and future types registered in code).
// Grouped with `/settings` under "Administração" in the `AppHeader` — it is not
// everyday navigation. It lists the `job_definition` with the current state; each
// card takes care of enabling/disabling, scheduling, triggering and showing the history.
//
// The screen polls while a job runs, so it also carries the "updated Xs ago" line
// (the `DataScreen` shell owns it): a live value that changes by itself needs to
// say it is alive.
export function JobsScreen() {
  const t = useT()
  const query = useQuery(jobsQueryOptions)
  const jobs = query.data ?? []

  return (
    <DataScreen
      title={t('jobs.screen.title')}
      subtitle={t('jobs.screen.subtitle')}
      updatedAt={query.dataUpdatedAt}
      query={query}
      empty={jobs.length === 0}
      copy={{
        loading: t('jobs.screen.loading'),
        loadError: t('jobs.screen.loadError'),
        emptyTitle: t('jobs.screen.emptyTitle'),
        emptyDescription: t('jobs.screen.emptyDescription'),
      }}
    >
      <ul className="flex flex-col gap-3">
        {jobs.map((job) => (
          <JobCard key={job.id} job={job} />
        ))}
      </ul>
    </DataScreen>
  )
}
