import { useQuery } from '@tanstack/react-query'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { JobCard } from '@/components/job-card'
import { jobsQueryOptions } from '@/lib/queries'

// Rota `/jobs` (0052): a tela de administração dos jobs internos (agendador
// embutido — backup por VACUUM INTO, e futuros tipos registrados em código).
// Agrupada com `/settings` sob "Administração" no `AppHeader` — não é
// navegação do dia a dia. Lista os `job_definition` com estado atual; cada
// card cuida de habilitar/desabilitar, agendar, disparar e ver o histórico.
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
