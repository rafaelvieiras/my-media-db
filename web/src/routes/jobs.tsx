import { useQuery } from '@tanstack/react-query'
import { ListaScreen } from '@/components/query-states'
import { JobCard } from '@/components/job-card'
import { jobsQueryOptions } from '@/lib/queries'
import { useT } from '@/lib/i18n'

// Route `/jobs`: the administration screen of the internal jobs (embedded
// scheduler — backup through VACUUM INTO, and future types registered in code).
// Grouped with `/settings` under "Administração" in the `AppHeader` — it is not
// everyday navigation. It lists the `job_definition` with the current state; each
// card takes care of enabling/disabling, scheduling, triggering and showing the history.
export function JobsScreen() {
  const t = useT()
  const { data, isPending, isError, error } = useQuery(jobsQueryOptions)

  return (
    <ListaScreen
      title={t('jobs.screen.title')}
      subtitle={t('jobs.screen.subtitle')}
      isPending={isPending}
      isError={isError}
      error={error}
      itens={data ?? []}
      textos={{
        loading: t('jobs.screen.loading'),
        loadError: t('jobs.screen.loadError'),
        emptyTitle: t('jobs.screen.emptyTitle'),
        emptyDescription: t('jobs.screen.emptyDescription'),
      }}
      listaClassName="flex flex-col gap-3"
      renderItem={(job) => <JobCard key={job.id} job={job} />}
    />
  )
}
