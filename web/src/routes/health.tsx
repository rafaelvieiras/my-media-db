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
  const { data, isPending, isError } = useQuery(healthQueryOptions)

  return (
    <main className="flex flex-col items-center gap-8 p-6">
      <Card className="w-full max-w-md" size="default">
        <CardHeader>
          <CardTitle className="flex items-center justify-between gap-3">
            Estado da API
            <StatusBadge isPending={isPending} isError={isError} />
          </CardTitle>
          <CardDescription>Saúde do serviço MyMediaDB</CardDescription>
        </CardHeader>
        <CardContent>
          {isPending ? (
            <p className="text-muted-foreground">Conectando…</p>
          ) : isError ? (
            <p className="font-medium text-destructive">API indisponível</p>
          ) : (
            <p className="font-medium text-emerald-600 dark:text-emerald-400">
              API online — v{data.version}
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
  if (isPending) {
    return <Badge variant="secondary">conectando</Badge>
  }
  if (isError) {
    return <Badge variant="destructive">offline</Badge>
  }
  return (
    <Badge className="border-transparent bg-emerald-600 text-white">
      online
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
  const { data, isPending, isError, error } = useQuery(systemHealthQueryOptions)
  const mensagemErro = error instanceof Error ? error.message : undefined

  return (
    <section className="flex w-full max-w-3xl flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="font-heading text-lg font-semibold">Sistema</h2>
        <p className="text-sm text-muted-foreground">
          Retrato local do processo — banco, uso do catálogo e disco das fontes.
          Snapshot cacheado por 60s (recarregue a página para atualizar).
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

function DatabaseBlock({
  data,
  isPending,
  isError,
  message,
}: BlockState & { data: DatabaseHealth | undefined }) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>Banco de dados</CardTitle>
      </CardHeader>
      <CardContent>
        {isPending ? (
          <LoadingState label="Carregando…" />
        ) : isError || !data ? (
          <ErrorState
            title="Não foi possível carregar a telemetria"
            message={message}
          />
        ) : (
          <dl className="flex flex-col gap-1.5 text-sm">
            <BlockRow label="Tamanho" value={formatBytes(data.size_bytes)} />
            <BlockRow
              label="Versão de schema"
              value={String(data.schema_version)}
            />
          </dl>
        )}
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
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>Uso</CardTitle>
      </CardHeader>
      <CardContent>
        {isPending ? (
          <LoadingState label="Carregando…" />
        ) : isError || !data ? (
          <ErrorState
            title="Não foi possível carregar a telemetria"
            message={message}
          />
        ) : (
          <dl className="flex flex-col gap-1.5 text-sm">
            <BlockRow label="Entities" value={String(data.entities.total)} />
            {Object.entries(data.entities.by_type).map(([tipo, total]) => (
              <BlockRow key={tipo} label={`— ${tipo}`} value={String(total)} />
            ))}
            <BlockRow
              label="Arquivos de mídia"
              value={formatMediaFiles(data.media_files)}
            />
            <BlockRow
              label="Fontes cadastradas"
              value={String(data.library_sources)}
            />
            <BlockRow
              label="Fila de revisão"
              value={String(data.review_queue_size)}
            />
            <BlockRow label="Órfãs" value={String(data.orphans_count)} />
            <BlockRow
              label="Execuções de job"
              value={String(data.job_executions_total)}
            />
            <BlockRow
              label="Uptime"
              value={formatDuration(data.uptime_seconds)}
            />
          </dl>
        )}
      </CardContent>
    </Card>
  )
}

function formatMediaFiles(mediaFiles: MediaFilesUsage): string {
  return `${mediaFiles.total} (${mediaFiles.present} presentes, ${mediaFiles.missing} ausentes)`
}

function StorageBlock({
  data,
  isPending,
  isError,
  message,
}: BlockState & { data: StorageSnapshot | undefined }) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>Armazenamento</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {isPending ? (
          <LoadingState label="Carregando…" />
        ) : isError || !data ? (
          <ErrorState
            title="Não foi possível carregar a telemetria"
            message={message}
          />
        ) : (
          <>
            {/* Disk distribution used by the tracked folders —
                managed media (video + NFO/artwork with a canonical name),
                internal cache of the app (assetsDir) and the unidentified rest.
                Read-only: no action on "não identificado" here. */}
            <dl className="flex flex-col gap-1.5 text-sm">
              <BlockRow
                label="Mídia gerenciada"
                value={formatBytes(data.managed_media_bytes)}
              />
              <BlockRow
                label="Cache da aplicação"
                value={formatBytes(data.app_cache_bytes)}
              />
              <BlockRow
                label="Não identificado"
                value={formatBytes(data.unmanaged_bytes)}
              />
            </dl>

            <div className="flex flex-col gap-3 border-t border-border pt-3">
              <p className="text-xs font-medium text-muted-foreground">
                Por fonte
              </p>
              {data.sources.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  Nenhuma fonte cadastrada.
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
      </CardContent>
    </Card>
  )
}

// StorageRow: one source — a "used (tracked media) / filesystem total"
// bar. Offline shows a badge in place of the bar, without freezing the
// other rows (`offline`/`filesystem: null` of the contract, see §4).
function StorageRow({ source }: { source: SourceStorage }) {
  return (
    <li className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-sm">
        <span className="truncate font-medium" title={source.path}>
          {source.path}
        </span>
        {source.offline ? (
          <Badge variant="destructive">indisponível</Badge>
        ) : (
          <span className="shrink-0 text-muted-foreground">
            {formatBytes(source.used_bytes)}
            {source.filesystem
              ? ` de ${formatBytes(source.filesystem.total_bytes)}`
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
