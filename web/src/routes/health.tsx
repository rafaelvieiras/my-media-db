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
  UsageCounts,
} from '@/lib/api'

// Tela de saúde (rota `/health`): consome `health` via TanStack Query e deriva
// três estados visíveis — carregando / erro / ok. Um erro de rede é ESTADO da
// UI, nunca uma exceção não-capturada: o Card sempre renderiza.
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

// SystemSection (0055): a telemetria de sistema — banco/uso/armazenamento. UMA
// única chamada a `systemHealthQueryOptions` aqui (o pai da seção); cada
// sub-bloco recebe o MESMO isPending/isError/data e decide o próprio
// Loading/ErrorState, mas todos derivam da mesma requisição (não há 3 fetches).
// É uma query SEPARADA de `healthQueryOptions` — uma falha aqui não afeta o
// card de status acima.
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
        data={data?.storage?.sources}
        isPending={isPending}
        isError={isError}
        message={mensagemErro}
      />
    </section>
  )
}

// Estado comum aos três sub-blocos: loading/erro derivados da mesma query pai.
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
}: BlockState & { data: SourceStorage[] | undefined }) {
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>Armazenamento</CardTitle>
      </CardHeader>
      <CardContent>
        {isPending ? (
          <LoadingState label="Carregando…" />
        ) : isError || !data ? (
          <ErrorState
            title="Não foi possível carregar a telemetria"
            message={message}
          />
        ) : data.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            Nenhuma fonte cadastrada.
          </p>
        ) : (
          <ul className="flex flex-col gap-3">
            {data.map((source) => (
              <StorageRow key={source.source_id} source={source} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

// StorageRow: uma fonte — barra "usado (mídia rastreada) / total do
// filesystem". Offline mostra um badge no lugar da barra, sem travar as
// demais linhas (`offline`/`filesystem: null` do contrato, ver 0055 §4).
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

// Barra de uso simples (`div` com `width%`): o projeto não tem um componente
// `Progress` em `web/src/components/ui/` ainda — criar um só para isto seria
// design system novo demais para uma barra de leitura.
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
