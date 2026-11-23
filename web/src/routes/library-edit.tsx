import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { LoaderCircle, Save, Trash2, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
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
import { PathBrowser } from '@/components/path-browser'
import { LibraryFormHeader } from '@/components/library-form-header'
import { LibraryLanguageAndRetryFields } from '@/components/library-language-retry-fields'
import { LibraryPathField } from '@/components/library-path-field'
import { LibrarySubmitButton } from '@/components/library-submit-button'
import { ErrorState, LoadingState } from '@/components/query-states'
import { useT } from '@/lib/i18n'
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
  const t = useT()
  const { id } = useParams({ from: '/dashboard/library/$id/edit' })
  const query = useQuery(libraryQueryOptions)
  const source = query.data?.items.find((s) => s.id === id)
  // The guard of "leave without saving" lives in the header, but the unsaved state
  // belongs to the form below it — the screen only relays it.
  const [sujo, setSujo] = useState(false)

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <LibraryFormHeader titulo={t('library.edit.title')} dirty={sujo} />

      {query.isPending ? (
        <LoadingState label={t('library.edit.loading')} />
      ) : query.isError ? (
        <ErrorState
          title={t('library.edit.loadFailed')}
          message={
            query.error instanceof Error ? query.error.message : undefined
          }
          onRetry={() => void query.refetch()}
        />
      ) : !source ? (
        <ErrorState title={t('library.edit.notFound')} />
      ) : (
        <LibraryEditForm id={id} source={source} onSujoChange={setSujo} />
      )}
    </main>
  )
}

function LibraryEditForm({
  id,
  source,
  onSujoChange,
}: {
  id: string
  source: Source
  onSujoChange: (sujo: boolean) => void
}) {
  const t = useT()
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
  // The empty path used to return in silence (the same defect of the creation
  // screen): the submit now marks the field and moves the focus.
  const [tentouSalvar, setTentouSalvar] = useState(false)
  const campoCaminho = useRef<HTMLInputElement>(null)

  // "Leave without saving" guard: the header asks before navigating when one of the
  // three fields holds a value different from the one loaded from the API.
  const sujo =
    path !== source.path ||
    (preferredLanguageTocado &&
      preferredLanguage !== (source.preferred_language ?? '')) ||
    (retryDaysTocado &&
      retryDays.trim() !==
        (source.retroactive_enrichment_days != null
          ? String(source.retroactive_enrichment_days)
          : ''))

  useEffect(() => {
    onSujoChange(sujo)
  }, [sujo, onSujoChange])

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
      setTentouSalvar(true)
      campoCaminho.current?.focus()
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
        ? t('library.edit.saveFailed')
        : null

  const mensagemErroRemover =
    remover.error instanceof ApiError
      ? remover.error.message
      : remover.isError
        ? t('library.edit.removeFailed')
        : null

  // Live on purpose: the message disappears as soon as the owner types.
  const erroCaminho =
    tentouSalvar && path.trim() === '' ? t('library.form.pathRequired') : null
  const caminhoInvalido = erroCaminho !== null || mensagemErroSalvar !== null

  return (
    <div className="flex flex-col gap-8">
      {/* noValidate: the min/max of the retry field is only a native hint;
          the range validation belongs to the backend (422 inline), without depending on the
          native browser balloon blocking the submit before it gets there. */}
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
        <LibraryPathField
          valor={path}
          onChange={setPath}
          inputRef={campoCaminho}
          invalido={caminhoInvalido}
          erroCaminho={erroCaminho}
        />

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

        <LibrarySubmitButton
          Icone={Save}
          pendente={salvar.isPending}
          rotulo={t('common.action.save')}
          rotuloPendente={t('common.action.saving')}
        />
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
// straight without confirmation, now it requires an explicit second step. The
// confirmation is the shared `AlertDialog` (not a hand-rolled `role="alertdialog"`):
// it moves the focus into the dialog, closes on Esc and gives the focus back to the
// button that opened it — the three things the old inline block promised and did not do.
function DangerZone({
  ocupado,
  erro,
  onConfirmar,
}: {
  ocupado: boolean
  erro: string | null
  onConfirmar: () => void
}) {
  const t = useT()
  const [confirmando, setConfirmando] = useState(false)
  const gatilho = useRef<HTMLButtonElement | null>(null)

  return (
    <section className="flex flex-col gap-2 rounded-xl border border-destructive/30 bg-destructive/5 p-4">
      <h2 className="text-sm font-semibold text-destructive">
        {t('library.edit.danger.title')}
      </h2>
      <p className="text-sm text-muted-foreground">
        {t('library.edit.danger.description')}
      </p>
      <div>
        <Button
          ref={gatilho}
          variant="destructive"
          size="sm"
          onClick={() => setConfirmando(true)}
        >
          <Trash2 aria-hidden />
          {t('library.edit.danger.remove')}
        </Button>
      </div>
      {erro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {erro}
        </p>
      ) : null}

      <AlertDialog open={confirmando} onOpenChange={setConfirmando}>
        <AlertDialogContent
          onCloseAutoFocus={(e) => {
            // Esc/cancel: the focus goes back to "Remove datasource" instead of
            // falling into `<body>`.
            e.preventDefault()
            gatilho.current?.focus()
          }}
        >
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('library.edit.danger.remove')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('library.edit.danger.question')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {/* The dialog stays open on error (the mutation above does not close it):
              the message has to live inside it, otherwise it lands in the region the
              dialog marks as `aria-hidden`. */}
          {erro ? (
            <p role="alert" className="text-sm font-medium text-destructive">
              {erro}
            </p>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={ocupado}>
              {t('common.action.cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={ocupado}
              onClick={(e) => {
                // preventDefault: the automatic close of `AlertDialogAction` would
                // close the dialog BEFORE the mutation resolves — the `open` state is
                // ours (it stays open on error, showing the message; on success the
                // `navigate` unmounts the screen) — same pattern as the collection
                // removal in `entity-manage-menu.tsx`.
                e.preventDefault()
                onConfirmar()
              }}
            >
              {ocupado ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <TriangleAlert aria-hidden />
              )}
              {ocupado
                ? t('library.edit.danger.removing')
                : t('library.edit.danger.confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
