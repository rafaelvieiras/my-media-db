import { useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { FolderPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { PathBrowser } from '@/components/path-browser'
import { LibraryFormHeader } from '@/components/library-form-header'
import { LibraryLanguageAndRetryFields } from '@/components/library-language-retry-fields'
import { useT } from '@/lib/i18n'
import { ApiError, createLibrary, type CreateSource } from '@/lib/api'
import {
  languageSettingsQueryOptions,
  retroactiveEnrichmentSettingsQueryOptions,
} from '@/lib/queries'

// Route `/dashboard/library/new`: it replaces the old always-visible `DatasourceForm`
// at the top of the list with a page of its own — it gives the screen room
// that the folder browser needs (it did not fit squeezed into one row of the list).
// The path field stays editable in parallel with the `PathBrowser` (the browser
// is a visual shortcut, not the only way of filling in the path). Success navigates
// back to `/dashboard/library`; an error (`ApiError`, e.g. 422 invalid/duplicate path)
// appears inline, right below the field — naturally close to it on this page
// (finding #7 of the audit, an error far from the field in the squeezed list, disappears
// for free here).
export function LibraryNewScreen() {
  const t = useT()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [path, setPath] = useState('')
  const [type, setType] = useState<CreateSource['type']>('movie')
  const [preferredLanguage, setPreferredLanguage] = useState('')
  const [retryDays, setRetryDays] = useState('')
  // The empty path used to return in silence: the screen looked broken because
  // nothing happened at all. Now the first submit marks the field and moves the focus.
  const [tentouEnviar, setTentouEnviar] = useState(false)
  const campoCaminho = useRef<HTMLInputElement>(null)

  const languagesQuery = useQuery(languageSettingsQueryOptions)
  const idiomasPermitidos = languagesQuery.data?.allowed ?? []
  const retroativoQuery = useQuery(retroactiveEnrichmentSettingsQueryOptions)
  const retryDaysGlobal = retroativoQuery.data?.retry_days

  const mutation = useMutation({
    mutationFn: (body: CreateSource) => createLibrary(body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['library'] })
      navigate({ to: '/dashboard/library' })
    },
  })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    const limpo = path.trim()
    if (!limpo) {
      setTentouEnviar(true)
      campoCaminho.current?.focus()
      return
    }
    // Series (type=tv) marks the library as content_type='series'; Movies stay
    // 'movie'. It is the value by which the scanner branches the match flow.
    const content_type = type === 'tv' ? 'series' : 'movie'
    const retryDaysLimpo = retryDays.trim()
    mutation.mutate({
      path: limpo,
      type,
      content_type,
      preferred_language: preferredLanguage || undefined,
      retroactive_enrichment_days: retryDaysLimpo
        ? Number(retryDaysLimpo)
        : undefined,
    })
  }

  const mensagemErro =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.isError
        ? t('library.create.failed')
        : null

  // Live on purpose: the message disappears as soon as the owner types, without
  // another submit to clear the state.
  const erroCaminho =
    tentouEnviar && path.trim() === '' ? t('library.form.pathRequired') : null
  const caminhoInvalido = erroCaminho !== null || mensagemErro !== null

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <LibraryFormHeader
        titulo={t('library.create.title')}
        dirty={
          path !== '' ||
          type !== 'movie' ||
          preferredLanguage !== '' ||
          retryDays !== ''
        }
      />

      {/* noValidate: the min/max of the retry field is only a native hint;
          the range validation belongs to the backend (422 inline), without depending on the
          native browser balloon blocking the submit before it gets there. */}
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">
            {t('library.form.path')}
            <span aria-hidden className="ml-0.5 text-destructive">
              *
            </span>
          </span>
          <Input
            ref={campoCaminho}
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder={t('library.form.pathPlaceholder')}
            aria-label={t('library.form.pathAriaLabel')}
            required
            aria-invalid={caminhoInvalido || undefined}
            aria-describedby={
              erroCaminho !== null ? 'library-path-error' : undefined
            }
          />
        </label>
        {erroCaminho !== null ? (
          <p
            id="library-path-error"
            role="alert"
            className="text-sm font-medium text-destructive"
          >
            {erroCaminho}
          </p>
        ) : null}

        <PathBrowser value={path} onChange={setPath} />

        <label className="flex max-w-40 flex-col gap-1 text-sm">
          <span className="text-muted-foreground">
            {t('common.label.type')}
          </span>
          <Select
            value={type}
            onChange={(e) => setType(e.target.value as CreateSource['type'])}
            aria-label={t('library.create.typeAriaLabel')}
          >
            <option value="movie">{t('common.entityTypePlural.movie')}</option>
            <option value="tv">{t('common.entityTypePlural.tv')}</option>
          </Select>
        </label>

        <LibraryLanguageAndRetryFields
          idiomasPermitidos={idiomasPermitidos}
          preferredLanguage={preferredLanguage}
          onPreferredLanguageChange={setPreferredLanguage}
          retryDays={retryDays}
          onRetryDaysChange={setRetryDays}
          retryDaysGlobal={retryDaysGlobal}
        />

        {mensagemErro ? (
          <p role="alert" className="text-sm font-medium text-destructive">
            {mensagemErro}
          </p>
        ) : null}

        <div>
          <Button type="submit" disabled={mutation.isPending}>
            <FolderPlus aria-hidden />
            {mutation.isPending
              ? t('library.create.submitting')
              : t('library.create.submit')}
          </Button>
        </div>
      </form>
    </main>
  )
}
