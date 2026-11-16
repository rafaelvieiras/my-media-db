import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { FolderPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { PathBrowser } from '@/components/path-browser'
import { LibraryFormHeader } from '@/components/library-form-header'
import { LibraryLanguageAndRetryFields } from '@/components/library-language-retry-fields'
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
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [path, setPath] = useState('')
  const [type, setType] = useState<CreateSource['type']>('movie')
  const [preferredLanguage, setPreferredLanguage] = useState('')
  const [retryDays, setRetryDays] = useState('')

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
        ? 'Não foi possível adicionar o datasource.'
        : null

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <LibraryFormHeader titulo="Adicionar datasource" />

      {/* noValidate: the min/max of the retry field is only a native hint;
          the range validation belongs to the backend (422 inline), without depending on the
          native browser balloon blocking the submit before it gets there. */}
      <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Caminho</span>
          <Input
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="/mnt/midia/filmes"
            aria-label="Caminho do datasource"
            aria-invalid={mensagemErro !== null || undefined}
          />
        </label>

        <PathBrowser value={path} onChange={setPath} />

        <label className="flex max-w-40 flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Tipo</span>
          <Select
            value={type}
            onChange={(e) => setType(e.target.value as CreateSource['type'])}
            aria-label="Tipo do datasource"
          >
            <option value="movie">Filmes</option>
            <option value="tv">Séries</option>
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
            {mutation.isPending ? 'Adicionando…' : 'Adicionar datasource'}
          </Button>
        </div>
      </form>
    </main>
  )
}
