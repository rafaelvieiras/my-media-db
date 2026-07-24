import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { LoaderCircle, Save, Trash2, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { PathBrowser } from '@/components/path-browser'
import { LibraryFormHeader } from '@/components/library-form-header'
import { ErrorState, LoadingState } from '@/components/query-states'
import { ApiError, deleteLibrary, patchLibrary, type Source } from '@/lib/api'
import { libraryQueryOptions } from '@/lib/queries'

// Rota `/dashboard/library/$id/edit` (briefing 0071): reaponta o caminho do datasource
// (PATCH, recasco em cascata do 0070) e concentra a remoção — antes um ícone de
// lixeira sem confirmação colado ao botão "Escanear" na lista (achado de risco
// de clique acidental da auditoria de UX) — numa "zona de perigo" com
// confirmação explícita em 2 passos, no mesmo padrão de `/orphans`.
//
// Não existe um `GET /library/{id}` dedicado — a fonte é encontrada na mesma
// lista que a tela `/dashboard/library` já consome (`libraryQueryOptions`).
export function LibraryEditScreen() {
  const { id } = useParams({ from: '/dashboard/library/$id/edit' })
  const query = useQuery(libraryQueryOptions)
  const source = query.data?.items.find((s) => s.id === id)

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <LibraryFormHeader titulo="Editar datasource" />

      {query.isPending ? (
        <LoadingState label="Carregando datasource…" />
      ) : query.isError ? (
        <ErrorState
          title="Não foi possível carregar o datasource"
          message={
            query.error instanceof Error ? query.error.message : undefined
          }
        />
      ) : !source ? (
        <ErrorState title="Datasource não encontrado" />
      ) : (
        <LibraryEditForm id={id} source={source} />
      )}
    </main>
  )
}

function LibraryEditForm({ id, source }: { id: string; source: Source }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [path, setPath] = useState(source.path)

  function invalidar() {
    queryClient.invalidateQueries({ queryKey: ['library'] })
  }

  const salvar = useMutation({
    mutationFn: (novoPath: string) => patchLibrary(id, { path: novoPath }),
    onSuccess: () => {
      invalidar()
      navigate({ to: '/dashboard/library' })
    },
  })

  const remover = useMutation({
    mutationFn: () => deleteLibrary(id),
    onSuccess: () => {
      invalidar()
      navigate({ to: '/dashboard/library' })
    },
  })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    const limpo = path.trim()
    if (!limpo) {
      return
    }
    salvar.mutate(limpo)
  }

  const mensagemErroSalvar =
    salvar.error instanceof ApiError
      ? salvar.error.message
      : salvar.isError
        ? 'Não foi possível salvar o novo caminho.'
        : null

  const mensagemErroRemover =
    remover.error instanceof ApiError
      ? remover.error.message
      : remover.isError
        ? 'Não foi possível remover o datasource.'
        : null

  return (
    <div className="flex flex-col gap-8">
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Caminho</span>
          <Input
            value={path}
            onChange={(e) => setPath(e.target.value)}
            aria-label="Caminho do datasource"
            aria-invalid={mensagemErroSalvar !== null || undefined}
          />
        </label>

        <PathBrowser value={path} onChange={setPath} />

        {mensagemErroSalvar ? (
          <p role="alert" className="text-sm font-medium text-destructive">
            {mensagemErroSalvar}
          </p>
        ) : null}

        <div>
          <Button type="submit" disabled={salvar.isPending}>
            <Save aria-hidden />
            {salvar.isPending ? 'Salvando…' : 'Salvar'}
          </Button>
        </div>
      </form>

      <DangerZone
        ocupado={remover.isPending}
        erro={mensagemErroRemover}
        onConfirmar={() => remover.mutate()}
      />
    </div>
  )
}

// "Zona de perigo": a remoção do datasource (briefing 0071) — antes um clique
// direto sem confirmação, agora exige um segundo passo explícito na própria UI
// (sem `window.confirm`), o mesmo padrão já usado na purga de órfãs.
function DangerZone({
  ocupado,
  erro,
  onConfirmar,
}: {
  ocupado: boolean
  erro: string | null
  onConfirmar: () => void
}) {
  const [confirmando, setConfirmando] = useState(false)

  return (
    <section className="flex flex-col gap-2 rounded-xl border border-destructive/30 bg-destructive/5 p-4">
      <h2 className="text-sm font-semibold text-destructive">Zona de perigo</h2>
      <p className="text-sm text-muted-foreground">
        Remover o datasource não apaga os arquivos no disco, mas desliga o
        acompanhamento desta pasta e some com o catálogo detectado a partir
        dela.
      </p>
      {confirmando ? (
        <div
          role="alertdialog"
          aria-label="Confirmar remoção do datasource"
          className="flex flex-wrap items-center gap-2"
        >
          <span className="flex items-center gap-1.5 text-sm font-medium text-destructive">
            <TriangleAlert className="size-4" aria-hidden />
            Tem certeza? Isso remove o datasource e não pode ser desfeito.
          </span>
          <Button
            variant="destructive"
            size="sm"
            disabled={ocupado}
            onClick={onConfirmar}
          >
            {ocupado ? (
              <LoaderCircle className="animate-spin" aria-hidden />
            ) : (
              <Trash2 aria-hidden />
            )}
            {ocupado ? 'Removendo…' : 'Confirmar remoção'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={ocupado}
            onClick={() => setConfirmando(false)}
          >
            Cancelar
          </Button>
        </div>
      ) : (
        <div>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setConfirmando(true)}
          >
            <Trash2 aria-hidden />
            Remover datasource
          </Button>
        </div>
      )}
      {erro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {erro}
        </p>
      ) : null}
    </section>
  )
}
