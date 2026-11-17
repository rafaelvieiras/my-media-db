import { useQuery } from '@tanstack/react-query'
import { healthQueryOptions, systemHealthQueryOptions } from '@/lib/queries'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { LoadingState, ErrorState } from '@/components/query-states'
import { formatBytes, formatDuration } from '@/lib/format'
import { useLocale, useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
import type {
  DatabaseHealth,
  MediaFilesUsage,
  SourceStorage,
  StorageSnapshot,
  UsageCounts,
} from '@/lib/api'

// Health screen (route `/health`): it consumes `health` via TanStack Query and derives
// three visible states — loading / error / ok. A network error is UI
// STATE, never an uncaught exception: the Card always renders.
export function HealthScreen() {
  const t = useT()
  const { data, isPending, isError } = useQuery(healthQueryOptions)

  return (
    <main className="flex flex-col items-center gap-8 p-6">
      <Card className="w-full max-w-md" size="default">
        <CardHeader>
          <CardTitle className="flex items-center justify-between gap-3">
            {t('jobs.health.apiTitle')}
            <StatusBadge isPending={isPending} isError={isError} />
          </CardTitle>
          <CardDescription>{t('jobs.health.apiDescription')}</CardDescription>
        </CardHeader>
        <CardContent>
          {isPending ? (
            <p className="text-muted-foreground">
              {t('jobs.health.connecting')}
            </p>
          ) : isError ? (
            <p className="font-medium text-destructive">
              {t('jobs.health.apiUnavailable')}
            </p>
          ) : (
            <p className="font-medium text-emerald-600 dark:text-emerald-400">
              {t('jobs.health.apiOnline', { version: data.version })}
            </p>
          )}
        </CardContent>
      </Card>

      <SystemSection />
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
    <Badge className="border-transparent bg-emerald-600 text-white">
      {t('jobs.health.status.online')}
    </Badge>
  )
}

// SystemSection: the system telemetry — database/usage/storage. A SINGLE
// call to `systemHealthQueryOptions` here (the parent of the section); each
// sub-block receives the SAME isPending/isError/data and decides its own
// Loading/ErrorState, but all of them derive from the same request (there are no 3 fetches).
// It is a query SEPARATE from `healthQueryOptions` — a failure here does not affect the
// status card above.
function SystemSection() {
  const t = useT()
  const { data, isPending, isError, error } = useQuery(systemHealthQueryOptions)
  const mensagemErro = error instanceof Error ? error.message : undefined

  return (
    <section className="flex w-full max-w-3xl flex-col gap-4">
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
        />
        <UsageBlock
          data={data?.usage}
          isPending={isPending}
          isError={isError}
          message={mensagemErro}
        />
      </div>

      <StorageBlock
        data={data?.storage}
        isPending={isPending}
        isError={isError}
        message={mensagemErro}
      />
    </section>
  )
}

// State common to the three sub-blocks: loading/error derived from the same parent query.
interface BlockState {
  isPending: boolean
  isError: boolean
  message?: string
}

// The three telemetry cards share the same body contract: spinner while
// pending, the inline error when the block failed, the content otherwise. The
// content arrives as a render prop, so the narrowed (non-undefined) data type
// reaches the card without a cast — and the copy stays identical across the cards.
function CorpoDoBloco<T>({
  dados,
  isPending,
  message,
  children,
}: {
  dados: T | undefined
  isPending: boolean
  message?: string
  children: (dados: T) => React.ReactNode
}) {
  const t = useT()
  if (isPending) {
    return <LoadingState label={t('common.state.loading')} />
  }
  if (dados === undefined) {
    return (
      <ErrorState title={t('jobs.health.telemetryError')} message={message} />
    )
  }
  return <>{children(dados)}</>
}

function DatabaseBlock({
  data,
  isPending,
  isError,
  message,
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
}: BlockState & { data: UsageCounts | undefined }) {
  const t = useT()
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
        >
          {(data) => (
            <dl className="flex flex-col gap-1.5 text-sm">
              <BlockRow
                label={t('jobs.health.usage.entities')}
                value={String(data.entities.total)}
              />
              {Object.entries(data.entities.by_type).map(([tipo, total]) => (
                <BlockRow
                  key={tipo}
                  label={`— ${tipo}`}
                  value={String(total)}
                />
              ))}
              <BlockRow
                label={t('jobs.health.usage.mediaFiles')}
                value={formatMediaFiles(data.media_files, t)}
              />
              <BlockRow
                label={t('jobs.health.usage.librarySources')}
                value={String(data.library_sources)}
              />
              <BlockRow
                label={t('jobs.health.usage.reviewQueue')}
                value={String(data.review_queue_size)}
              />
              <BlockRow
                label={t('jobs.health.usage.orphans')}
                value={String(data.orphans_count)}
              />
              <BlockRow
                label={t('jobs.health.usage.jobExecutions')}
                value={String(data.job_executions_total)}
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

function formatMediaFiles(mediaFiles: MediaFilesUsage, t: Translate): string {
  return t('jobs.health.usage.mediaFilesValue', {
    total: mediaFiles.total,
    present: mediaFiles.present,
    missing: mediaFiles.missing,
  })
}

function StorageBlock({
  data,
  isPending,
  isError,
  message,
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
    <li className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="truncate font-medium" title={source.path}>
          {source.path}
        </span>
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
          <p className="text-xs text-muted-foreground">{source.error}</p>
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
  const percentual =
    totalBytes > 0 ? Math.min(100, (usedBytes / totalBytes) * 100) : 0
  return (
    <div
      role="progressbar"
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
