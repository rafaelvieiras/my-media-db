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

// Rota `/dashboard/library/new` (briefing 0071): substitui o antigo `DatasourceForm`
// sempre-visível no topo da lista por uma página própria — dá o espaço de tela
// que o navegador de pastas precisa (não cabia espremido numa linha de lista).
// O campo de caminho continua editável em paralelo ao `PathBrowser` (o navegador
// é um atalho visual, não o único jeito de preencher o caminho). Sucesso navega
// de volta para `/dashboard/library`; erro (`ApiError`, ex. 422 path inválido/duplicado)
// aparece inline, logo abaixo do campo — naturalmente perto dele nesta página
// (o achado #7 da auditoria, erro longe do campo na lista espremida, some de
// graça aqui).
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
    // Séries (type=tv) marca a biblioteca como content_type='series'; Filmes fica
    // 'movie'. É o valor por que o scanner ramifica o fluxo de match (0034).
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

      {/* noValidate: o min/max do campo de reentativa (0085) é só hint nativo;
          a validação de range é do backend (422 inline), sem depender do
          balão nativo do navegador bloqueando o submit antes de chegar lá. */}
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
