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
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { LoadingState, ErrorState } from '@/components/query-states'
import { jobExecutionsQueryOptions } from '@/lib/queries'
import { ApiError, patchJob, runJob, type JobDefinition } from '@/lib/api'
import { formatarCarimbo, formatarDuracaoMS } from '@/lib/format'
import { useLocale, useT } from '@/lib/i18n'

// numero writes a count in the interface locale ("36,599" in English, "36.599" in
// pt-BR): the counters arrive raw from the server while the bytes next to them are
// already localized.
function numero(valor: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(valor)
}

function invalidarJobs(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ['jobs'] })
}

// JobCard: one `job_definition` — name/description, status badge
// (enabled/disabled/running), toggle, editable cron schedule (with
// inline 422 feedback), "Executar agora" button and the last/next run.
// The history (`JobHistoryList`) stays expandable inside the card itself, so
// it does not require a route of its own per job.
//
// "Executar agora" asks first: the click fires 20–30 min of server work, so the
// AlertDialog states the verb and how long the last run took.
export function JobCard({ job }: { job: JobDefinition }) {
  const t = useT()
  const locale = useLocale()
  const qc = useQueryClient()
  const [cron, setCron] = useState(job.schedule_cron ?? '')
  const [historicoAberto, setHistoricoAberto] = useState(false)
  const [confirmarExecucao, setConfirmarExecucao] = useState(false)

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

  // The two counters of the progress measure different things — `done/total` counts
  // files of the current step, `ok/failed` counts reported items — so each carries
  // its noun and `ok` hides when nothing succeeded.
  const contagemDoProgresso = progresso
    ? progresso.ok > 0
      ? t('jobs.card.okFailedItems', {
          ok: numero(progresso.ok, locale),
          failed: numero(progresso.failed, locale),
        })
      : progresso.failed > 0
        ? t('jobs.card.failedItems', {
            failed: numero(progresso.failed, locale),
          })
        : null
    : null

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

      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:gap-4">
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
          className="flex min-w-0 flex-col gap-1.5 sm:flex-row sm:items-center"
          onSubmit={(e) => {
            e.preventDefault()
            salvarCron.mutate(cron)
          }}
        >
          <label className="flex min-w-0 flex-col gap-1 text-sm sm:flex-row sm:items-center sm:gap-1.5">
            <span className="text-muted-foreground">{t('jobs.card.cron')}</span>
            <Input
              value={cron}
              onChange={(e) => setCron(e.target.value)}
              placeholder={t('jobs.card.cronPlaceholder')}
              aria-label={t('jobs.card.cronSchedule', { name: job.nome })}
              className="w-full sm:w-56"
            />
          </label>
          <Button
            type="submit"
            variant="outline"
            size="sm"
            className="sm:w-auto"
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
          className="sm:w-auto"
          disabled={job.running || executar.isPending}
          onClick={() => setConfirmarExecucao(true)}
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

      <AlertDialog open={confirmarExecucao} onOpenChange={setConfirmarExecucao}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('jobs.card.runNowConfirmTitle', { name: job.nome })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {levou
                ? t('jobs.card.runNowConfirmDescription', { duration: levou })
                : t('jobs.card.runNowConfirmDescriptionUnknown')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.action.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              // The action is not destructive: it starts a background run. The
              // default look of `AlertDialogAction` is the destructive pair.
              className={buttonVariants({ variant: 'default' })}
              onClick={() => {
                setConfirmarExecucao(false)
                executar.mutate()
              }}
            >
              {t('jobs.card.runNowConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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
          <span
            role="status"
            aria-live="polite"
            className="flex flex-wrap items-center gap-x-3 gap-y-1 text-muted-foreground"
          >
            {progresso.total > 0 ? (
              <span>
                {t('jobs.card.progressFiles', {
                  done: numero(progresso.done, locale),
                  total: numero(progresso.total, locale),
                })}
              </span>
            ) : null}
            {contagemDoProgresso ? <span>{contagemDoProgresso}</span> : null}
            {corrido ? (
              <span>{t('jobs.card.elapsed', { duration: corrido })}</span>
            ) : null}
          </span>
          {progresso.current_item ? (
            <span className="break-words text-muted-foreground">
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
  const { data, isPending, isError, error, refetch } = useQuery(
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
        onRetry={() => void refetch()}
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
                {/* Own key: `jobs.card.took` says "Last run took", which is wrong for
                    every older row of the history. */}
                {t('jobs.history.took', { duration: duracao })}
              </span>
            ) : null}
            {exec.ok > 0 || exec.failed > 0 ? (
              <span className="text-muted-foreground">
                {exec.ok > 0
                  ? t('jobs.card.okFailedItems', {
                      ok: numero(exec.ok, locale),
                      failed: numero(exec.failed, locale),
                    })
                  : t('jobs.card.failedItems', {
                      failed: numero(exec.failed, locale),
                    })}
              </span>
            ) : null}
            {exec.message ? (
              <span className="min-w-0 break-words text-muted-foreground">
                {exec.message}
              </span>
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}
