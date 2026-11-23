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
import { formatarCarimbo, formatarDuracaoMS } from '@/lib/format'
import { useLocale, useT } from '@/lib/i18n'

function invalidarJobs(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ['jobs'] })
}

// JobCard: one `job_definition` — name/description, status badge
// (enabled/disabled/running), toggle, editable cron schedule (with
// inline 422 feedback), "Executar agora" button and the last/next run.
// The history (`JobHistoryList`) stays expandable inside the card itself, so
// it does not require a route of its own per job.
export function JobCard({ job }: { job: JobDefinition }) {
  const t = useT()
  const locale = useLocale()
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
  // Duration of the last execution and the progress of the active execution (ADR-0040): the
  // two things that were missing to know "how long it took" and "where it stands".
  const levou = formatarDuracaoMS(job.last_duration_ms)
  const progresso = job.progress
  const corrido = formatarDuracaoMS(progresso?.elapsed_ms)

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
              {t('jobs.card.runningBadge')}
            </Badge>
          ) : (
            <Badge variant={job.enabled ? 'default' : 'outline'}>
              {job.enabled ? t('jobs.card.enabled') : t('jobs.card.disabled')}
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
            aria-label={t('jobs.card.autoScheduleToggle', { name: job.nome })}
            className="size-4 accent-primary"
            checked={job.enabled}
            disabled={toggleEnabled.isPending}
            onChange={() => toggleEnabled.mutate(!job.enabled)}
          />
          <span>{t('jobs.card.autoSchedule')}</span>
        </label>

        <form
          className="flex items-center gap-1.5"
          onSubmit={(e) => {
            e.preventDefault()
            salvarCron.mutate(cron)
          }}
        >
          <label className="flex items-center gap-1.5 text-sm">
            <span className="text-muted-foreground">{t('jobs.card.cron')}</span>
            <Input
              value={cron}
              onChange={(e) => setCron(e.target.value)}
              placeholder={t('jobs.card.cronPlaceholder')}
              aria-label={t('jobs.card.cronSchedule', { name: job.nome })}
              className="w-56"
            />
          </label>
          <Button
            type="submit"
            variant="outline"
            size="sm"
            disabled={salvarCron.isPending}
          >
            {salvarCron.isPending
              ? t('common.action.saving')
              : t('jobs.card.saveSchedule')}
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
          {job.running || executar.isPending
            ? t('jobs.card.starting')
            : t('jobs.card.runNow')}
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
          {t('jobs.card.nextRun', {
            when: formatarCarimbo(job.next_run_at, locale),
          })}
        </span>
        <span className="flex items-center gap-1.5">
          {job.last_status === 'success' ? (
            <CircleCheck className="size-3.5 text-primary" aria-hidden />
          ) : job.last_status === 'error' ? (
            <CircleX className="size-3.5 text-destructive" aria-hidden />
          ) : null}
          {job.last_message
            ? t('jobs.card.lastRunWithMessage', {
                when: formatarCarimbo(job.last_run_at, locale),
                message: job.last_message,
              })
            : t('jobs.card.lastRun', {
                when: formatarCarimbo(job.last_run_at, locale),
              })}
          {levou ? ` · ${t('jobs.card.took', { duration: levou })}` : null}
        </span>
      </div>

      {progresso ? (
        <div className="flex flex-col gap-1 rounded-lg bg-muted/50 p-2 text-sm">
          {progresso.step_total > 0 && progresso.step_label ? (
            <span>
              {t('jobs.card.step', {
                index: progresso.step_index,
                total: progresso.step_total,
                label: progresso.step_label,
              })}
            </span>
          ) : null}
          <span className="flex flex-wrap items-center gap-3 text-muted-foreground">
            {progresso.total > 0 ? (
              <span>
                {t('jobs.card.progressCount', {
                  done: progresso.done,
                  total: progresso.total,
                })}
              </span>
            ) : null}
            <span>
              {t('jobs.card.okFailed', {
                ok: progresso.ok,
                failed: progresso.failed,
              })}
            </span>
            {corrido ? (
              <span>{t('jobs.card.elapsed', { duration: corrido })}</span>
            ) : null}
          </span>
          {progresso.current_item ? (
            <span
              className="truncate text-muted-foreground"
              title={progresso.current_item}
            >
              {t('jobs.card.currentItem', { item: progresso.current_item })}
            </span>
          ) : null}
        </div>
      ) : null}

      <div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setHistoricoAberto((v) => !v)}
        >
          <History aria-hidden />
          {historicoAberto
            ? t('jobs.card.hideHistory')
            : t('jobs.card.showHistory')}
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
  const t = useT()
  const locale = useLocale()
  const { data, isPending, isError, error } = useQuery(
    jobExecutionsQueryOptions(jobId, { limit: 20 }),
  )

  if (isPending) {
    return <LoadingState label={t('jobs.history.loading')} />
  }
  if (isError) {
    return (
      <ErrorState
        title={t('jobs.history.loadError')}
        message={error instanceof Error ? error.message : undefined}
      />
    )
  }
  if (data.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">{t('jobs.history.empty')}</p>
    )
  }

  return (
    <ul className="flex flex-col gap-1.5 rounded-lg bg-muted/50 p-2">
      {data.map((exec) => {
        const duracao = formatarDuracaoMS(exec.duration_ms)
        return (
          <li
            key={exec.id}
            className="flex flex-wrap items-center gap-2 text-sm"
          >
            <span className="text-muted-foreground">
              {formatarCarimbo(exec.started_at, locale)}
            </span>
            <Badge variant="outline">
              {exec.trigger === 'schedule'
                ? t('jobs.history.trigger.schedule')
                : t('jobs.history.trigger.manual')}
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
                ? t('jobs.history.status.running')
                : exec.status === 'success'
                  ? t('jobs.history.status.success')
                  : t('jobs.history.status.error')}
            </Badge>
            {duracao ? (
              <span className="text-muted-foreground">
                {t('jobs.card.took', { duration: duracao })}
              </span>
            ) : null}
            {exec.ok > 0 || exec.failed > 0 ? (
              <span className="text-muted-foreground">
                {t('jobs.card.okFailed', { ok: exec.ok, failed: exec.failed })}
              </span>
            ) : null}
            {exec.message ? (
              <span className="text-muted-foreground">{exec.message}</span>
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}
