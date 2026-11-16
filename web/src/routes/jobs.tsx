import { useQuery } from '@tanstack/react-query'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { JobCard } from '@/components/job-card'
import { jobsQueryOptions } from '@/lib/queries'

// Route `/jobs`: the administration screen of the internal jobs (embedded
// scheduler — backup through VACUUM INTO, and future types registered in code).
// Grouped with `/settings` under "Administração" in the `AppHeader` — it is not
// everyday navigation. It lists the `job_definition` with the current state; each
// card takes care of enabling/disabling, scheduling, triggering and showing the history.
export function JobsScreen() {
  const { data, isPending, isError, error } = useQuery(jobsQueryOptions)

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">Jobs</h1>
        <p className="text-sm text-muted-foreground">
          Tarefas periódicas rodadas dentro do próprio binário: habilite, agende
          ou dispare manualmente.
        </p>
      </div>

      {isPending ? (
        <LoadingState label="Carregando os jobs…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar os jobs"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : data.length === 0 ? (
        <EmptyState
          title="Nenhum job registrado"
          description="Nenhum tipo de job foi registrado no binário ainda."
        />
      ) : (
        <ul className="flex flex-col gap-3">
          {data.map((job) => (
            <JobCard key={job.id} job={job} />
          ))}
        </ul>
      )}
    </main>
  )
}
