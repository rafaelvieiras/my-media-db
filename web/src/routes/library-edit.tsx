import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { LoaderCircle, Save, Trash2, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { PathBrowser } from '@/components/path-browser'
import { LibraryFormHeader } from '@/components/library-form-header'
import { LibraryLanguageAndRetryFields } from '@/components/library-language-retry-fields'
import { ErrorState, LoadingState } from '@/components/query-states'
import {
  ApiError,
  deleteLibrary,
  patchLibrary,
  type PatchSource,
  type Source,
} from '@/lib/api'
import {
  libraryQueryOptions,
  languageSettingsQueryOptions,
  retroactiveEnrichmentSettingsQueryOptions,
} from '@/lib/queries'

// Route `/dashboard/library/$id/edit`: it re-points the datasource path
// (PATCH, cascading re-basing) and concentrates the removal — previously a
// trash icon without confirmation glued to the "Escanear" button in the list (an accidental-click
// risk finding from the UX audit) — in a "danger zone" with an
// explicit 2-step confirmation, in the same pattern as `/orphans`.
//
// There is no dedicated `GET /library/{id}` — the source is found in the same
// list that the `/dashboard/library` screen already consumes (`libraryQueryOptions`).
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
  const [preferredLanguage, setPreferredLanguage] = useState(
    source.preferred_language ?? '',
  )
  const [preferredLanguageTocado, setPreferredLanguageTocado] = useState(false)
  const [retryDays, setRetryDays] = useState(
    source.retroactive_enrichment_days != null
      ? String(source.retroactive_enrichment_days)
      : '',
  )
  const [retryDaysTocado, setRetryDaysTocado] = useState(false)

  const languagesQuery = useQuery(languageSettingsQueryOptions)
  const idiomasPermitidos = languagesQuery.data?.allowed ?? []
  const retroativoQuery = useQuery(retroactiveEnrichmentSettingsQueryOptions)
  const retryDaysGlobal = retroativoQuery.data?.retry_days

  function invalidar() {
    queryClient.invalidateQueries({ queryKey: ['library'] })
  }

  const salvar = useMutation({
    mutationFn: (body: PatchSource) => patchLibrary(id, body),
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
    const body: PatchSource = { path: limpo }
    // Partial PATCH: it only sends `preferred_language` if the owner actually touched the
    // select (same spirit as `path`/`watch_enabled`) — a field absent from the
    // body keeps the current value in the backend. Going back to "Usar idioma global"
    // sends an explicit `null`, which clears the preference.
    if (preferredLanguageTocado) {
      body.preferred_language = preferredLanguage || null
    }
    // Partial PATCH (same spirit as `preferred_language`): it only sends
    // `retroactive_enrichment_days` if the owner touched the field. Going back to empty
    // sends an explicit `null`, which clears the override (it inherits the global one again).
    if (retryDaysTocado) {
      const retryDaysLimpo = retryDays.trim()
      body.retroactive_enrichment_days = retryDaysLimpo
        ? Number(retryDaysLimpo)
        : null
    }
    salvar.mutate(body)
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
      {/* noValidate: the min/max of the retry field is only a native hint;
          the range validation belongs to the backend (422 inline), without depending on the
          native browser balloon blocking the submit before it gets there. */}
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
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

        <LibraryLanguageAndRetryFields
          idiomasPermitidos={idiomasPermitidos}
          preferredLanguage={preferredLanguage}
          onPreferredLanguageChange={(valor) => {
            setPreferredLanguage(valor)
            setPreferredLanguageTocado(true)
          }}
          retryDays={retryDays}
          onRetryDaysChange={(valor) => {
            setRetryDays(valor)
            setRetryDaysTocado(true)
          }}
          retryDaysGlobal={retryDaysGlobal}
        />

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

// "Danger zone": the removal of the datasource — previously a click
// straight without confirmation, now it requires an explicit second step in the UI itself
// (without `window.confirm`), the same pattern already used in the orphan purge.
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
