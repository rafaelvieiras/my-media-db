import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Clock,
  History,
  LoaderCircle,
  Play,
  CircleCheck,
  CircleX,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { LoadingState, ErrorState } from '@/components/query-states'
import { jobExecutionsQueryOptions } from '@/lib/queries'
import { ApiError, patchJob, runJob, type JobDefinition } from '@/lib/api'

// formatarCarimbo returns a short stamp (date+time) in the local format, or
// an em dash for null/absent. Without a date lib — only `Intl` via `toLocaleString`
// (no precedent for date formatting in this project yet, so it stays local
// and simple).
function formatarCarimbo(iso: string | null | undefined): string {
  if (!iso) {
    return '—'
  }
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) {
    return iso
  }
  return d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
}

function invalidarJobs(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ['jobs'] })
}

// JobCard: one `job_definition` — name/description, status badge
// (enabled/disabled/running), toggle, editable cron schedule (with
// inline 422 feedback), "Executar agora" button and the last/next run.
// The history (`JobHistoryList`) stays expandable inside the card itself, so
// it does not require a route of its own per job.
export function JobCard({ job }: { job: JobDefinition }) {
  const qc = useQueryClient()
  const [cron, setCron] = useState(job.schedule_cron ?? '')
  const [historicoAberto, setHistoricoAberto] = useState(false)

  const toggleEnabled = useMutation({
    mutationFn: (enabled: boolean) => patchJob(job.id, { enabled }),
    onSuccess: () => invalidarJobs(qc),
  })

  const salvarCron = useMutation({
    mutationFn: (valor: string) =>
      patchJob(job.id, { schedule_cron: valor.trim() === '' ? null : valor }),
    onSuccess: () => invalidarJobs(qc),
  })

  const executar = useMutation({
    mutationFn: () => runJob(job.id),
    onSuccess: () => {
      invalidarJobs(qc)
      qc.invalidateQueries({ queryKey: ['job', job.id, 'executions'] })
    },
  })

  const erroCron =
    salvarCron.error instanceof ApiError ? salvarCron.error.message : null
  const erroExecutar =
    executar.error instanceof ApiError ? executar.error.message : null

  return (
    <li className="flex flex-col gap-3 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex flex-col gap-0.5">
          <h2 className="font-heading text-base font-medium">{job.nome}</h2>
          <p className="text-sm text-muted-foreground">{job.descricao}</p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {job.running ? (
            <Badge variant="secondary" className="gap-1">
              <LoaderCircle className="size-3 animate-spin" aria-hidden />
              Rodando…
            </Badge>
          ) : (
            <Badge variant={job.enabled ? 'default' : 'outline'}>
              {job.enabled ? 'Habilitado' : 'Desabilitado'}
            </Badge>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-4">
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            role="switch"
            aria-checked={job.enabled}
            aria-label={`Agendamento automático de ${job.nome}`}
            className="size-4 accent-primary"
            checked={job.enabled}
            disabled={toggleEnabled.isPending}
            onChange={() => toggleEnabled.mutate(!job.enabled)}
          />
          <span>Agendamento automático</span>
        </label>

        <form
          className="flex items-center gap-1.5"
          onSubmit={(e) => {
            e.preventDefault()
            salvarCron.mutate(cron)
          }}
        >
          <label className="flex items-center gap-1.5 text-sm">
            <span className="text-muted-foreground">Cron</span>
            <Input
              value={cron}
              onChange={(e) => setCron(e.target.value)}
              placeholder="0 3 * * * (vazio = só manual)"
              aria-label={`Agendamento cron de ${job.nome}`}
              className="w-56"
            />
          </label>
          <Button
            type="submit"
            variant="outline"
            size="sm"
            disabled={salvarCron.isPending}
          >
            {salvarCron.isPending ? 'Salvando…' : 'Salvar agendamento'}
          </Button>
        </form>

        <Button
          type="button"
          size="sm"
          disabled={job.running || executar.isPending}
          onClick={() => executar.mutate()}
        >
          {job.running || executar.isPending ? (
            <LoaderCircle className="animate-spin" aria-hidden />
          ) : (
            <Play aria-hidden />
          )}
          {job.running || executar.isPending ? 'Executando…' : 'Executar agora'}
        </Button>
      </div>

      {erroCron ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {erroCron}
        </p>
      ) : null}
      {erroExecutar ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {erroExecutar}
        </p>
      ) : null}

      <div className="flex flex-wrap items-center gap-4 text-sm text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <Clock className="size-3.5" aria-hidden />
          Próxima execução: {formatarCarimbo(job.next_run_at)}
        </span>
        <span className="flex items-center gap-1.5">
          {job.last_status === 'success' ? (
            <CircleCheck className="size-3.5 text-primary" aria-hidden />
          ) : job.last_status === 'error' ? (
            <CircleX className="size-3.5 text-destructive" aria-hidden />
          ) : null}
          Última execução: {formatarCarimbo(job.last_run_at)}
          {job.last_message ? ` — ${job.last_message}` : ''}
        </span>
      </div>

      <div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setHistoricoAberto((v) => !v)}
        >
          <History aria-hidden />
          {historicoAberto ? 'Ocultar histórico' : 'Ver histórico'}
        </Button>
      </div>

      {historicoAberto ? <JobHistoryList jobId={job.id} /> : null}
    </li>
  )
}

// JobHistoryList: paginated list (most recent first) of the last
// runs of ONE job — stamp, trigger (scheduled/manual), status and message.
// Simple by design: no incremental pagination in this foundation (the backend already
// prunes to 100/job; the UI shows the `limit` most recent ones).
function JobHistoryList({ jobId }: { jobId: string }) {
  const { data, isPending, isError, error } = useQuery(
    jobExecutionsQueryOptions(jobId, { limit: 20 }),
  )

  if (isPending) {
    return <LoadingState label="Carregando histórico…" />
  }
  if (isError) {
    return (
      <ErrorState
        title="Não foi possível carregar o histórico"
        message={error instanceof Error ? error.message : undefined}
      />
    )
  }
  if (data.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Nenhuma execução registrada ainda.
      </p>
    )
  }

  return (
    <ul className="flex flex-col gap-1.5 rounded-lg bg-muted/50 p-2">
      {data.map((exec) => (
        <li key={exec.id} className="flex flex-wrap items-center gap-2 text-sm">
          <span className="text-muted-foreground">
            {formatarCarimbo(exec.started_at)}
          </span>
          <Badge variant="outline">
            {exec.trigger === 'schedule' ? 'Agendado' : 'Manual'}
          </Badge>
          <Badge
            variant={
              exec.status === 'success'
                ? 'default'
                : exec.status === 'error'
                  ? 'destructive'
                  : 'secondary'
            }
          >
            {exec.status === 'running'
              ? 'Rodando'
              : exec.status === 'success'
                ? 'Sucesso'
                : 'Erro'}
          </Badge>
          {exec.message ? (
            <span className="text-muted-foreground">{exec.message}</span>
          ) : null}
        </li>
      ))}
    </ul>
  )
}
