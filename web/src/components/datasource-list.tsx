import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { FolderPlus, Pencil, RefreshCw, Wrench } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  patchLibrary,
  startScan,
  startReprocess,
  type Source,
  type ReprocessReport,
  ApiError,
} from '@/lib/api'
import {
  libraryQueryOptions,
  scanStatusQueryOptions,
  reprocessStatusQueryOptions,
} from '@/lib/queries'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'

// Datasources screen: the creation and the editing/removal of a path
// moved out to dedicated routes (`/dashboard/library/new`,
// `/dashboard/library/$id/edit`) — this list
// only shows path + state, with the transient/binary low-risk actions that
// stay inline: "Escanear" (+ progress) and the watch-for-changes switch.
export function DatasourceList() {
  const { data, isPending, isError, error } = useQuery(libraryQueryOptions)

  return (
    <div className="flex flex-col gap-4">
      <div>
        <Button asChild size="sm">
          <Link to="/dashboard/library/new">
            <FolderPlus aria-hidden />
            Adicionar datasource
          </Link>
        </Button>
      </div>

      {isPending ? (
        <LoadingState label="Carregando datasources…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar os datasources"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : data.items.length === 0 ? (
        <EmptyState
          title="Nenhum datasource cadastrado"
          description="Adicione um caminho para começar a escanear sua mídia."
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {data.items.map((source) => (
            <DatasourceRow key={source.id} source={source} />
          ))}
        </ul>
      )}
    </div>
  )
}

function DatasourceRow({ source }: { source: Source }) {
  const queryClient = useQueryClient()
  const { data: status } = useQuery(scanStatusQueryOptions(source.id))
  const rodavaAntes = useRef(false)

  // When a scan finishes (running: true → false), it invalidates the catalog and the list to
  // reflect the newly detected items and the new `last_scan`.
  useEffect(() => {
    const rodando = status?.running ?? false
    if (rodavaAntes.current && !rodando) {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      queryClient.invalidateQueries({ queryKey: ['library'] })
    }
    rodavaAntes.current = rodando
  }, [status?.running, queryClient])

  const scanMutation = useMutation({
    mutationFn: () => startScan(source.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['scan', source.id] })
    },
  })

  // Turns the filesystem watcher of this source on/off. Applied
  // live by the server — without confirmation, it is reversible and low risk.
  const watchMutation = useMutation({
    mutationFn: (watch_enabled: boolean) =>
      patchLibrary(source.id, { watch_enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['library'] })
    },
  })

  // Reprocessamento: o dry-run é o passo 1 (obrigatório) e mostra o que mudaria;
  // só depois o botão de aplicar aparece. O progresso vem do próprio endpoint.
  const [previa, setPrevia] = useState<ReprocessReport | null>(null)
  const { data: reproc } = useQuery(reprocessStatusQueryOptions(source.id))
  const reprocRodando = reproc?.running ?? false

  const previaMutation = useMutation({
    mutationFn: () => startReprocess(source.id, { dry_run: true }),
    onSuccess: (r) => setPrevia(r as ReprocessReport),
  })
  const aplicarMutation = useMutation({
    mutationFn: () =>
      startReprocess(source.id, { dry_run: false, delete_orphans: true }),
    onSuccess: () => {
      setPrevia(null)
      queryClient.invalidateQueries({ queryKey: ['reprocess', source.id] })
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['entities'] })
      queryClient.invalidateQueries({ queryKey: ['library'] })
    },
  })

  const rodando = status?.running ?? false

  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="truncate font-medium">{source.path}</span>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span>{source.type === 'tv' ? 'Séries' : 'Filmes'}</span>
            <EstadoBadge state={source.state} />
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={rodando || scanMutation.isPending}
            onClick={() => scanMutation.mutate()}
          >
            <RefreshCw
              aria-hidden
              className={rodando ? 'animate-spin' : undefined}
            />
            {rodando ? 'Escaneando…' : 'Escanear'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={rodando || reprocRodando || previaMutation.isPending}
            onClick={() => previaMutation.mutate()}
            title="Corrige o catálogo já existente: retenta os itens em revisão e limpa os rascunhos vazios"
          >
            <Wrench aria-hidden />
            {reprocRodando ? 'Reprocessando…' : 'Reprocessar'}
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to="/dashboard/library/$id/edit" params={{ id: source.id }}>
              <Pencil aria-hidden />
              Editar
            </Link>
          </Button>
        </div>
      </div>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <Switch
          size="sm"
          checked={source.watch_enabled}
          disabled={watchMutation.isPending}
          onCheckedChange={(checked) => watchMutation.mutate(checked)}
          aria-label="Observar mudanças nesta pasta"
        />
        <span>
          Observar mudanças nesta pasta
          <span className="hidden sm:inline">
            {' '}
            — acelera a detecção de arquivos novos, mas não substitui o scan
            periódico
          </span>
        </span>
      </label>
      <PainelReprocess
        previa={previa}
        reproc={reproc}
        onAplicar={() => aplicarMutation.mutate()}
        onCancelar={() => setPrevia(null)}
        erro={previaMutation.error ?? aplicarMutation.error}
        aplicando={aplicarMutation.isPending}
      />
      <ProgressoScan status={status} erroScan={scanMutation.error} />
    </li>
  )
}

function EstadoBadge({ state }: { state: Source['state'] }) {
  if (state === 'online') {
    return (
      <Badge className="border-transparent bg-emerald-600 text-white">
        online
      </Badge>
    )
  }
  return <Badge variant="destructive">offline</Badge>
}

// Progress indicator: while `running`, it shows files seen/total; errors
// (from the last scan or from the trigger) appear as highlighted text.
function ProgressoScan({
  status,
  erroScan,
}: {
  status:
    | {
        running: boolean
        files_seen: number
        files_total: number
        last_error?: string
      }
    | undefined
  erroScan: unknown
}) {
  const mensagemDisparo = erroScan instanceof ApiError ? erroScan.message : null

  if (status?.running) {
    const total = status.files_total > 0 ? ` de ${status.files_total}` : ''
    return (
      <p className="text-xs text-muted-foreground">
        Escaneando… {status.files_seen}
        {total} arquivos vistos
      </p>
    )
  }
  if (mensagemDisparo) {
    return (
      <p role="alert" className="text-xs font-medium text-destructive">
        {mensagemDisparo}
      </p>
    )
  }
  if (status?.last_error) {
    return (
      <p role="alert" className="text-xs font-medium text-destructive">
        Último scan falhou: {status.last_error}
      </p>
    )
  }
  return null
}

// PainelReprocess shows the MANDATORY dry-run before anything is written: the counts
// of what the reprocess would touch, with "Aplicar" only after the preview. While it
// runs, the same block becomes the progress of the phase.
function PainelReprocess({
  previa,
  reproc,
  onAplicar,
  onCancelar,
  erro,
  aplicando,
}: {
  previa: ReprocessReport | null
  reproc?: {
    running: boolean
    phase?: string
    done: number
    total: number
    report?: ReprocessReport
    last_error?: string
  }
  onAplicar: () => void
  onCancelar: () => void
  erro: unknown
  aplicando: boolean
}) {
  if (erro) {
    return (
      <p className="text-xs text-destructive">
        {erro instanceof Error ? erro.message : 'Falha ao reprocessar'}
      </p>
    )
  }

  if (reproc?.running) {
    return (
      <p className="text-xs text-muted-foreground">
        Reprocessando ({reproc.phase ?? 'reconcile'}): {reproc.done}/
        {reproc.total} itens…
      </p>
    )
  }

  if (reproc?.report && !previa) {
    const r = reproc.report
    return (
      <p className="text-xs text-muted-foreground">
        Último reprocessamento: {r.promoted} promovidos, {r.orphans_deleted}{' '}
        rascunhos vazios apagados.
      </p>
    )
  }

  if (!previa) return null

  return (
    <div className="flex flex-col gap-2 rounded-lg bg-muted/40 p-3 text-xs">
      <span className="font-medium">Prévia — nada foi escrito ainda</span>
      <ul className="flex flex-col gap-1 text-muted-foreground">
        <li>{previa.series_draft} série(s) em revisão a retentar</li>
        <li>{previa.movies_pending} filme(s) pendente(s) de match</li>
        <li>{previa.draft_episodes} episódio(s) em revisão nesta biblioteca</li>
        <li>{previa.orphans} rascunho(s) vazio(s) a apagar</li>
      </ul>
      <p className="text-muted-foreground">
        Modo {previa.mode}. Sem orçamento de tempo e sem throttle: resolve tudo
        que está pendente agora.
      </p>
      <div className="flex gap-2">
        <Button size="sm" disabled={aplicando} onClick={onAplicar}>
          {aplicando ? 'Aplicando…' : 'Aplicar'}
        </Button>
        <Button size="sm" variant="ghost" onClick={onCancelar}>
          Cancelar
        </Button>
      </div>
    </div>
  )
}
