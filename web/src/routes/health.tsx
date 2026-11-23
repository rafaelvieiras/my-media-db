import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { ScreenHeader } from '@/components/screen-header'
import { healthQueryOptions, systemHealthQueryOptions } from '@/lib/queries'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { LoadingState, ErrorState } from '@/components/query-states'
import { formatBytes, formatDuration } from '@/lib/format'
import { useLocale, useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
import type {
  DatabaseHealth,
  MediaFilesUsage,
  SourceStorage,
  StorageSnapshot,
  SystemHealth,
  UsageCounts,
} from '@/lib/api'

// numero writes a count in the interface locale ("36,599" in English, "36.599" in
// pt-BR): raw counts used to sit next to locale-formatted bytes.
function numero(valor: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(valor)
}

// Health screen (route `/health`): it consumes `health` via TanStack Query and derives
// three visible states — loading / error / ok. A network error is UI
// STATE, never an uncaught exception: the Card always renders.
//
// It is also the only operational screen of its own (no list to manage), so it owns
// the `ScreenHeader` (it used to start straight at a card, with no h1) and the
// Refresh action that asks for a new telemetry snapshot — the copy used to tell the
// owner to reload the page.
export function HealthScreen() {
  const t = useT()
  const locale = useLocale()
  const health = useQuery(healthQueryOptions)
  const system = useQuery(systemHealthQueryOptions)

  const geradoEm = system.data
    ? new Date(system.data.computed_at).toLocaleTimeString(locale, {
        hour: '2-digit',
        minute: '2-digit',
      })
    : null
  const atualizando = health.isFetching || system.isFetching

  const atualizar = () => {
    void health.refetch()
    void system.refetch()
  }

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <ScreenHeader
        title={t('jobs.health.title')}
        subtitle={t('jobs.health.subtitle')}
        actions={
          <Button variant="outline" onClick={atualizar} disabled={atualizando}>
            <RefreshCw aria-hidden />
            {t('common.action.refresh')}
          </Button>
        }
      />

      {geradoEm ? (
        <p className="-mt-2 mb-4 text-xs text-muted-foreground">
          {t('jobs.health.generatedAt', { time: geradoEm })}
        </p>
      ) : null}

      <div className="flex flex-col gap-6">
        <Card size="default">
          <CardHeader>
            <CardTitle className="flex items-center justify-between gap-3">
              {t('jobs.health.apiTitle')}
              <StatusBadge
                isPending={health.isPending}
                isError={health.isError}
              />
            </CardTitle>
            <CardDescription>{t('jobs.health.apiDescription')}</CardDescription>
          </CardHeader>
          <CardContent>
            {health.isPending ? (
              <p className="text-muted-foreground">
                {t('jobs.health.connecting')}
              </p>
            ) : health.isError ? (
              <div className="flex flex-wrap items-center gap-3">
                <p role="alert" className="font-medium text-destructive">
                  {t('jobs.health.apiUnavailable')}
                </p>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void health.refetch()}
                >
                  {t('common.action.retry')}
                </Button>
              </div>
            ) : (
              <p className="font-medium">
                {t('jobs.health.apiVersion', { version: health.data.version })}
              </p>
            )}
          </CardContent>
        </Card>

        <SystemSection
          data={system.data}
          isPending={system.isPending}
          isError={system.isError}
          error={system.error}
          onRetry={atualizar}
        />
      </div>
    </main>
  )
}

function StatusBadge({
  isPending,
  isError,
}: {
  isPending: boolean
  isError: boolean
}) {
  const t = useT()
  if (isPending) {
    return (
      <Badge variant="secondary">{t('jobs.health.status.connecting')}</Badge>
    )
  }
  if (isError) {
    return (
      <Badge variant="destructive">{t('jobs.health.status.offline')}</Badge>
    )
  }
  return (
    <Badge className="border-transparent bg-emerald-700 text-white">
      {t('jobs.health.status.online')}
    </Badge>
  )
}

// SystemSection: the system telemetry — database/usage/storage. It receives the
// SINGLE `systemHealthQueryOptions` result owned by `HealthScreen` (the header needs
// the same request to offer Refresh and to show when the snapshot was generated) and
// hands it to the three blocks, which decide their own Loading/ErrorState. It is a
// query SEPARATE from `healthQueryOptions` — a failure here does not affect the
// status card above.
function SystemSection({
  data,
  isPending,
  isError,
  error,
  onRetry,
}: {
  data: SystemHealth | undefined
  isPending: boolean
  isError: boolean
  error: unknown
  onRetry?: () => void
}) {
  const t = useT()
  const mensagemErro = error instanceof Error ? error.message : undefined

  return (
    <section className="flex w-full flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="font-heading text-lg font-semibold">
          {t('jobs.health.system.title')}
        </h2>
        <p className="text-sm text-muted-foreground">
          {t('jobs.health.system.description')}
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <DatabaseBlock
          data={data?.database}
          isPending={isPending}
          isError={isError}
          message={mensagemErro}
          onRetry={onRetry}
        />
        <UsageBlock
          data={data?.usage}
          isPending={isPending}
          isError={isError}
          message={mensagemErro}
          onRetry={onRetry}
        />
      </div>

      <StorageBlock
        data={data?.storage}
        isPending={isPending}
        isError={isError}
        message={mensagemErro}
        onRetry={onRetry}
      />
    </section>
  )
}

// State common to the three sub-blocks: loading/error derived from the same parent query.
interface BlockState {
  isPending: boolean
  isError: boolean
  message?: string
  onRetry?: () => void
}

// The three telemetry cards share the same body contract: spinner while
// pending, the inline error when the block failed, the content otherwise. The
// content arrives as a render prop, so the narrowed (non-undefined) data type
// reaches the card without a cast — and the copy stays identical across the cards.
function CorpoDoBloco<T>({
  dados,
  isPending,
  message,
  onRetry,
  children,
}: {
  dados: T | undefined
  isPending: boolean
  message?: string
  onRetry?: () => void
  children: (dados: T) => React.ReactNode
}) {
  const t = useT()
  if (isPending) {
    return <LoadingState label={t('common.state.loading')} />
  }
  if (dados === undefined) {
    return (
      <ErrorState
        title={t('jobs.health.telemetryError')}
        message={message}
        onRetry={onRetry}
      />
    )
  }
  return <>{children(dados)}</>
}

function DatabaseBlock({
  data,
  isPending,
  isError,
  message,
  onRetry,
}: BlockState & { data: DatabaseHealth | undefined }) {
  const t = useT()
  const locale = useLocale()
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>{t('jobs.health.database.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <CorpoDoBloco
          isPending={isPending}
          dados={isError ? undefined : data}
          message={message}
          onRetry={onRetry}
        >
          {(data) => (
            <dl className="flex flex-col gap-1.5 text-sm">
              <BlockRow
                label={t('common.label.size')}
                value={formatBytes(data.size_bytes, locale)}
              />
              <BlockRow
                label={t('jobs.health.database.schemaVersion')}
                value={String(data.schema_version)}
              />
            </dl>
          )}
        </CorpoDoBloco>
      </CardContent>
    </Card>
  )
}

function UsageBlock({
  data,
  isPending,
  isError,
  message,
  onRetry,
}: BlockState & { data: UsageCounts | undefined }) {
  const t = useT()
  const locale = useLocale()
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>{t('jobs.health.usage.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <CorpoDoBloco
          isPending={isPending}
          dados={isError ? undefined : data}
          message={message}
          onRetry={onRetry}
        >
          {(data) => (
            <dl className="flex flex-col gap-1.5 text-sm">
              <BlockRow
                label={t('jobs.health.usage.entities')}
                value={numero(data.entities.total, locale)}
              />
              {Object.entries(data.entities.by_type).map(([tipo, total]) => (
                <BlockRow
                  key={tipo}
                  label={`— ${tipo}`}
                  value={numero(total, locale)}
                />
              ))}
              <BlockRow
                label={t('jobs.health.usage.mediaFiles')}
                value={formatMediaFiles(data.media_files, t, locale)}
              />
              <BlockRow
                label={t('jobs.health.usage.librarySources')}
                value={numero(data.library_sources, locale)}
              />
              <BlockRow
                label={t('jobs.health.usage.reviewQueue')}
                value={numero(data.review_queue_size, locale)}
              />
              <BlockRow
                label={t('jobs.health.usage.orphans')}
                value={numero(data.orphans_count, locale)}
              />
              <BlockRow
                label={t('jobs.health.usage.jobExecutions')}
                value={numero(data.job_executions_total, locale)}
              />
              <BlockRow
                label={t('jobs.health.usage.uptime')}
                value={formatDuration(data.uptime_seconds)}
              />
            </dl>
          )}
        </CorpoDoBloco>
      </CardContent>
    </Card>
  )
}

function formatMediaFiles(
  mediaFiles: MediaFilesUsage,
  t: Translate,
  locale: string,
): string {
  return t('jobs.health.usage.mediaFilesValue', {
    total: numero(mediaFiles.total, locale),
    present: numero(mediaFiles.present, locale),
    missing: numero(mediaFiles.missing, locale),
  })
}

function StorageBlock({
  data,
  isPending,
  isError,
  message,
  onRetry,
}: BlockState & { data: StorageSnapshot | undefined }) {
  const t = useT()
  const locale = useLocale()
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>{t('jobs.health.storage.title')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <CorpoDoBloco
          isPending={isPending}
          dados={isError ? undefined : data}
          message={message}
          onRetry={onRetry}
        >
          {(data) => (
            <>
              {/* Disk distribution used by the tracked folders —
                managed media (video + NFO/artwork with a canonical name),
                internal cache of the app (assetsDir) and the unidentified rest.
                Read-only: no action on "Unidentified" here. */}
              <dl className="flex flex-col gap-1.5 text-sm">
                <BlockRow
                  label={t('jobs.health.storage.managedMedia')}
                  value={formatBytes(data.managed_media_bytes, locale)}
                />
                <BlockRow
                  label={t('jobs.health.storage.appCache')}
                  value={formatBytes(data.app_cache_bytes, locale)}
                />
                <BlockRow
                  label={t('jobs.health.storage.unidentified')}
                  value={formatBytes(data.unmanaged_bytes, locale)}
                />
              </dl>

              <div className="flex flex-col gap-3 border-t border-border pt-3">
                <p className="text-xs font-medium text-muted-foreground">
                  {t('jobs.health.storage.bySource')}
                </p>
                {data.sources.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    {t('jobs.health.storage.noSources')}
                  </p>
                ) : (
                  <ul className="flex flex-col gap-3">
                    {data.sources.map((source) => (
                      <StorageRow key={source.source_id} source={source} />
                    ))}
                  </ul>
                )}
              </div>
            </>
          )}
        </CorpoDoBloco>
      </CardContent>
    </Card>
  )
}

// StorageRow: one source — a "used (tracked media) / filesystem total"
// bar. Offline shows a badge in place of the bar, without freezing the
// other rows (`offline`/`filesystem: null` of the contract, see §4).
function StorageRow({ source }: { source: SourceStorage }) {
  const t = useT()
  const locale = useLocale()
  return (
    <li className="flex min-w-0 flex-col gap-1">
      {/* The path is what tells two sources apart, so it wraps instead of being
          truncated: `title` is the only copy of the full value and there is no
          hover on a phone. */}
      <div className="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-0.5 text-sm">
        <span className="min-w-0 font-medium break-all">{source.path}</span>
        {source.offline ? (
          <Badge variant="destructive">
            {t('jobs.health.storage.offlineBadge')}
          </Badge>
        ) : (
          <span className="shrink-0 text-muted-foreground">
            {formatBytes(source.used_bytes, locale)}
            {source.filesystem
              ? ` ${t('jobs.health.storage.ofTotal', {
                  total: formatBytes(source.filesystem.total_bytes, locale),
                })}`
              : ''}
          </span>
        )}
      </div>
      {source.offline ? (
        source.error ? (
          <p className="text-xs break-words text-muted-foreground">
            {source.error}
          </p>
        ) : null
      ) : source.filesystem ? (
        <StorageBar
          usedBytes={source.used_bytes}
          totalBytes={source.filesystem.total_bytes}
        />
      ) : null}
    </li>
  )
}

// Simple usage bar (`div` with `width%`): the project does not have a `Progress`
// component in `web/src/components/ui/` yet — creating one just for this would
// be too much design system for a read-only bar.
function StorageBar({
  usedBytes,
  totalBytes,
}: {
  usedBytes: number
  totalBytes: number
}) {
  const t = useT()
  const locale = useLocale()
  const percentual =
    totalBytes > 0 ? Math.min(100, (usedBytes / totalBytes) * 100) : 0
  return (
    <div
      role="progressbar"
      aria-label={t('jobs.health.storage.barLabel')}
      aria-valuetext={t('jobs.health.storage.usedOfTotal', {
        used: formatBytes(usedBytes, locale),
        total: formatBytes(totalBytes, locale),
      })}
      aria-valuenow={Math.round(percentual)}
      aria-valuemin={0}
      aria-valuemax={100}
      className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
    >
      <div
        className="h-full rounded-full bg-primary"
        style={{ width: `${percentual}%` }}
      />
    </div>
  )
}

function BlockRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{value}</dd>
    </div>
  )
}
