import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, RefreshCw, Trash2, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  deleteLibrary,
  patchLibrary,
  startScan,
  type Source,
  ApiError,
} from '@/lib/api'
import { libraryQueryOptions, scanStatusQueryOptions } from '@/lib/queries'
import { DatasourceForm } from '@/components/datasource-form'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'

// Tela de datasources: formulário de adição + lista com estado (online/offline),
// disparo de scan com progresso por polling e remoção.
export function DatasourceList() {
  const { data, isPending, isError, error } = useQuery(libraryQueryOptions)

  return (
    <div className="flex flex-col gap-6">
      <DatasourceForm />
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
          description="Adicione um caminho acima para começar a escanear sua mídia."
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
  const [editandoPath, setEditandoPath] = useState(false)
  const [novoPath, setNovoPath] = useState(source.path)

  // Ao concluir um scan (running: true → false), invalida catálogo e lista para
  // refletir os itens recém-detectados e o novo `last_scan`.
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

  const deleteMutation = useMutation({
    mutationFn: () => deleteLibrary(source.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['library'] })
    },
  })

  // Liga/desliga o watcher de filesystem desta fonte (briefing 0064). Aplicado
  // a quente pelo servidor — sem confirmação, é reversível e de baixo risco.
  const watchMutation = useMutation({
    mutationFn: (watch_enabled: boolean) =>
      patchLibrary(source.id, { watch_enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['library'] })
    },
  })

  // Reaponta o datasource para um novo caminho (briefing 0070): recasca em
  // cascata media_file/asset/entity no backend, sem re-scan. Erro (409
  // duplicata, 422 path inválido/não-diretório/ilegível, 400 vazio) aparece
  // INLINE no formulário, igual ao DatasourceForm.
  const pathMutation = useMutation({
    mutationFn: (path: string) => patchLibrary(source.id, { path }),
    onSuccess: () => {
      setEditandoPath(false)
      queryClient.invalidateQueries({ queryKey: ['library'] })
    },
  })

  function onSubmitPath(e: FormEvent) {
    e.preventDefault()
    const limpo = novoPath.trim()
    if (!limpo) {
      return
    }
    pathMutation.mutate(limpo)
  }

  function abrirEdicaoPath() {
    setNovoPath(source.path)
    pathMutation.reset()
    setEditandoPath(true)
  }

  function cancelarEdicaoPath() {
    pathMutation.reset()
    setEditandoPath(false)
  }

  const mensagemErroPath =
    pathMutation.error instanceof ApiError
      ? pathMutation.error.message
      : pathMutation.isError
        ? 'Não foi possível atualizar o caminho.'
        : null

  const rodando = status?.running ?? false

  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-4 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          {editandoPath ? (
            <form
              onSubmit={onSubmitPath}
              className="flex flex-col gap-2 sm:flex-row sm:items-center"
            >
              <Input
                value={novoPath}
                onChange={(e) => setNovoPath(e.target.value)}
                aria-label="Novo caminho do datasource"
                aria-invalid={mensagemErroPath !== null || undefined}
                autoFocus
                className="min-w-0 flex-1"
              />
              <div className="flex shrink-0 gap-2">
                <Button
                  type="submit"
                  size="sm"
                  disabled={pathMutation.isPending}
                >
                  {pathMutation.isPending ? 'Salvando…' : 'Salvar'}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label="Cancelar edição do caminho"
                  disabled={pathMutation.isPending}
                  onClick={cancelarEdicaoPath}
                >
                  <X aria-hidden />
                </Button>
              </div>
            </form>
          ) : (
            <div className="flex min-w-0 items-center gap-1">
              <span className="truncate font-medium">{source.path}</span>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Editar caminho"
                onClick={abrirEdicaoPath}
              >
                <Pencil aria-hidden />
              </Button>
            </div>
          )}
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span>{source.type === 'tv' ? 'Séries' : 'Filmes'}</span>
            <EstadoBadge state={source.state} />
          </div>
          {mensagemErroPath ? (
            <p role="alert" className="text-xs font-medium text-destructive">
              {mensagemErroPath}
            </p>
          ) : null}
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
            variant="destructive"
            size="icon-sm"
            aria-label="Remover datasource"
            disabled={deleteMutation.isPending}
            onClick={() => deleteMutation.mutate()}
          >
            <Trash2 aria-hidden />
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

// Indicador de progresso: enquanto `running`, mostra arquivos vistos/total; erros
// (do último scan ou do disparo) aparecem como texto destacado.
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
