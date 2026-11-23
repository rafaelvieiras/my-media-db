import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Select } from '@/components/ui/select'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
import {
  ErrorState,
  FormSaveFooter,
  LoadingState,
  CorpoComCarga,
} from '@/components/query-states'
import {
  languageSettingsQueryOptions,
  retroactiveEnrichmentSettingsQueryOptions,
  autoMaterializeArtworkSettingsQueryOptions,
  autoDownloadTrailerTemaSettingsQueryOptions,
  ytdlpSettingsQueryOptions,
} from '@/lib/queries'
import {
  putLanguageSettings,
  putRetroactiveEnrichmentSettings,
  putAutoMaterializeArtworkSettings,
  putAutoDownloadTrailerTemaSettings,
  putYtdlpSettings,
  type LanguageSettings,
  type RetroactiveEnrichmentSettings,
  type AutoMaterializeArtworkSettings,
  type AutoDownloadTrailerTemaSettings,
  type YtdlpSettings,
  ApiError,
} from '@/lib/api'
import { IDIOMAS_DISPONIVEIS, rotuloIdioma } from '@/lib/languages'
import { useT } from '@/lib/i18n'
import { useUnsavedGuard } from '@/lib/use-unsaved-guard'
import { CatalogCompletenessForm } from '@/components/catalog-completeness-form'
import { ScreenHeader } from '@/components/screen-header'

// Route `/settings`: language admin (primary, fallback, allowed). It reads the
// settings, lets you edit them and saves via PUT. The client-side validation mirrors the
// backend (primary and fallback ∈ allowed, non-empty allowed); the server 422 is
// the final guard and its pt-BR message appears inline.
export function SettingsScreen() {
  const t = useT()
  const { data, isPending, isError, error } = useQuery(
    languageSettingsQueryOptions,
  )
  const retroativo = useQuery(retroactiveEnrichmentSettingsQueryOptions)
  const autoMaterializar = useQuery(autoMaterializeArtworkSettingsQueryOptions)
  const autoDownloadTrailerTema = useQuery(
    autoDownloadTrailerTemaSettingsQueryOptions,
  )
  const ytdlp = useQuery(ytdlpSettingsQueryOptions)

  // "Leave without saving" guard: the screen used to navigate away with a section
  // half edited — the manual sections report their dirty state up.
  const [sujos, setSujos] = useState<Record<string, boolean>>({})
  useUnsavedGuard(Object.values(sujos).some(Boolean))

  function reportarSujo(secao: string, sujo: boolean) {
    setSujos((prev) =>
      prev[secao] === sujo ? prev : { ...prev, [secao]: sujo },
    )
  }

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <section className="mb-10">
        <ScreenHeader
          title={t('settings.screen.title')}
          subtitle={t('settings.screen.subtitle')}
        />

        <CorpoComCarga
          dados={data}
          carregando={isPending}
          falhou={isError}
          erro={error}
          textos={{
            loading: t('settings.screen.loading'),
            loadError: t('settings.screen.loadFailed'),
          }}
        >
          {(dados) => (
            <LanguageSettingsForm
              inicial={dados}
              onDirtyChange={(sujo) => reportarSujo('languages', sujo)}
            />
          )}
        </CorpoComCarga>
      </section>

      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            {t('settings.retroactive.title')}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t('settings.retroactive.subtitle')}
          </p>
        </div>

        {retroativo.isPending ? (
          <LoadingState label={t('settings.retroactive.loading')} />
        ) : retroativo.isError ? (
          <ErrorState
            title={t('settings.screen.loadFailed')}
            message={
              retroativo.error instanceof Error
                ? retroativo.error.message
                : undefined
            }
            onRetry={() => void retroativo.refetch()}
          />
        ) : (
          <RetroactiveEnrichmentSettingsForm
            inicial={retroativo.data}
            onDirtyChange={(sujo) => reportarSujo('retroactive', sujo)}
          />
        )}
      </section>

      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            {t('settings.artwork.title')}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t('settings.artwork.subtitle')}
          </p>
        </div>

        {autoMaterializar.isPending ? (
          <LoadingState label={t('settings.artwork.loading')} />
        ) : autoMaterializar.isError ? (
          <ErrorState
            title={t('settings.screen.loadFailed')}
            message={
              autoMaterializar.error instanceof Error
                ? autoMaterializar.error.message
                : undefined
            }
            onRetry={() => void autoMaterializar.refetch()}
          />
        ) : (
          <AutoMaterializeArtworkSwitch inicial={autoMaterializar.data} />
        )}
      </section>

      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            {t('settings.trailerTheme.title')}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t('settings.trailerTheme.subtitle')}
          </p>
        </div>

        {autoDownloadTrailerTema.isPending ? (
          <LoadingState label={t('settings.trailerTheme.loading')} />
        ) : autoDownloadTrailerTema.isError ? (
          <ErrorState
            title={t('settings.screen.loadFailed')}
            message={
              autoDownloadTrailerTema.error instanceof Error
                ? autoDownloadTrailerTema.error.message
                : undefined
            }
            onRetry={() => void autoDownloadTrailerTema.refetch()}
          />
        ) : (
          <AutoDownloadTrailerTemaSwitch
            inicial={autoDownloadTrailerTema.data}
          />
        )}
      </section>

      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            {t('settings.completeness.title')}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t('settings.completeness.subtitle')}
          </p>
        </div>

        <CatalogCompletenessForm />
      </section>

      <section>
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">yt-dlp</h2>
          <p className="text-sm text-muted-foreground">
            {t('settings.ytdlp.subtitle')}
          </p>
        </div>

        {ytdlp.isPending ? (
          <LoadingState label={t('settings.ytdlp.loading')} />
        ) : ytdlp.isError ? (
          <ErrorState
            title={t('settings.ytdlp.loadFailed')}
            message={
              ytdlp.error instanceof Error ? ytdlp.error.message : undefined
            }
            onRetry={() => void ytdlp.refetch()}
          />
        ) : (
          <YtdlpSettingsForm
            inicial={ytdlp.data}
            onDirtyChange={(sujo) => reportarSujo('ytdlp', sujo)}
          />
        )}
      </section>
    </main>
  )
}

// Form seeded with the current config. It receives `inicial` already resolved to
// seed the state a single time (no synchronization useEffect). `onDirtyChange`
// feeds the leave guard of the route, and the button only enables with a change.
function LanguageSettingsForm({
  inicial,
  onDirtyChange,
}: {
  inicial: LanguageSettings
  onDirtyChange?: (sujo: boolean) => void
}) {
  const t = useT()
  const queryClient = useQueryClient()
  const [primary, setPrimary] = useState(inicial.primary)
  const [fallback, setFallback] = useState(inicial.fallback)
  const [allowed, setAllowed] = useState<string[]>(inicial.allowed)
  const [erroValidacao, setErroValidacao] = useState<string | null>(null)

  // The same set, in any order, is not a change worth a PUT.
  const sujo =
    primary !== inicial.primary ||
    fallback !== inicial.fallback ||
    allowed.length !== inicial.allowed.length ||
    allowed.some((codigo) => !inicial.allowed.includes(codigo))

  useEffect(() => {
    onDirtyChange?.(sujo)
  }, [sujo, onDirtyChange])

  // Candidates of the multi-select: the known ones + the already allowed ones (so as not to
  // lose an exotic code that is already saved). It also feeds the primary/fallback
  // selects — so it is possible (and the validation catches it) to choose a
  // value outside the allowed ones.
  const candidatos = useMemo(() => {
    const set = new Set<string>([...IDIOMAS_DISPONIVEIS, ...allowed])
    return [...set]
  }, [allowed])

  const mutation = useMutation({
    mutationFn: (body: LanguageSettings) => putLanguageSettings(body),
    onSuccess: () => {
      // Changing the allowed ones may invalidate the current `lang` of the reads: it revalidates
      // the settings and the dependent reads (avoids serving the cache of a newly
      // disabled language).
      queryClient.invalidateQueries({ queryKey: ['settings', 'languages'] })
      queryClient.invalidateQueries({ queryKey: ['entity'] })
      queryClient.invalidateQueries({ queryKey: ['entities'] })
    },
  })

  function alternarPermitido(codigo: string) {
    setAllowed((prev) =>
      prev.includes(codigo)
        ? prev.filter((c) => c !== codigo)
        : [...prev, codigo],
    )
  }

  // Mirrors the backend. It returns the message of the 1st problem, or null if valid.
  function validar(): string | null {
    if (allowed.length === 0) {
      return t('settings.language.emptyAllowed')
    }
    if (!allowed.includes(primary)) {
      return t('settings.language.primaryNotAllowed')
    }
    if (!allowed.includes(fallback)) {
      return t('settings.language.fallbackNotAllowed')
    }
    return null
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    const problema = validar()
    setErroValidacao(problema)
    if (problema) {
      return
    }
    mutation.mutate({ primary, fallback, allowed })
  }

  // Error shown inline: the client-side validation has priority; otherwise, the
  // pt-BR message of the ApiError (422) of the server response.
  const erroServidor =
    mutation.isError && mutation.error instanceof Error
      ? mutation.error.message
      : null
  const mensagemErro = erroValidacao ?? erroServidor

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-5">
      <label className="flex flex-col gap-1.5">
        <span className="text-sm font-medium">
          {t('settings.language.primary')}
        </span>
        <Select value={primary} onChange={(e) => setPrimary(e.target.value)}>
          {candidatos.map((codigo) => (
            <option key={codigo} value={codigo}>
              {rotuloIdioma(codigo)}
            </option>
          ))}
        </Select>
      </label>

      <label className="flex flex-col gap-1.5">
        <span className="text-sm font-medium">
          {t('settings.language.fallback')}
        </span>
        <Select value={fallback} onChange={(e) => setFallback(e.target.value)}>
          {candidatos.map((codigo) => (
            <option key={codigo} value={codigo}>
              {rotuloIdioma(codigo)}
            </option>
          ))}
        </Select>
      </label>

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-medium">
          {t('settings.language.allowed')}
        </legend>
        <div className="flex flex-col gap-1.5">
          {candidatos.map((codigo) => (
            <label key={codigo} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-primary"
                checked={allowed.includes(codigo)}
                onChange={() => alternarPermitido(codigo)}
              />
              <span>{rotuloIdioma(codigo)}</span>
              <span className="text-xs text-muted-foreground">{codigo}</span>
            </label>
          ))}
        </div>
      </fieldset>

      <FormSaveFooter
        erro={mensagemErro}
        sucesso={mutation.isSuccess}
        mensagemSucesso={t('settings.form.saved')}
        salvando={mutation.isPending}
        desabilitado={!sujo}
      />
    </form>
  )
}

// Form seeded with the current throttle. Same pattern as the
// LanguageSettingsForm: controlled input, client-side validation mirroring the
// backend ([1, 365]) and PUT via TanStack Query.
function RetroactiveEnrichmentSettingsForm({
  inicial,
  onDirtyChange,
}: {
  inicial: RetroactiveEnrichmentSettings
  onDirtyChange?: (sujo: boolean) => void
}) {
  const t = useT()
  const queryClient = useQueryClient()
  const [retryDays, setRetryDays] = useState(String(inicial.retry_days))
  const [erroValidacao, setErroValidacao] = useState<string | null>(null)

  const sujo = retryDays !== String(inicial.retry_days)

  useEffect(() => {
    onDirtyChange?.(sujo)
  }, [sujo, onDirtyChange])

  const mutation = useMutation({
    mutationFn: (body: RetroactiveEnrichmentSettings) =>
      putRetroactiveEnrichmentSettings(body),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['settings', 'enrichment-retroativo'],
      })
    },
  })

  // Mirrors the backend (retry_days between 1 and 365). It returns the message of the 1st
  // problem, or null if valid.
  function validar(valor: number): string | null {
    if (!Number.isInteger(valor) || valor < 1 || valor > 365) {
      return t('settings.retroactive.retryDaysInvalid')
    }
    return null
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    const valor = Number(retryDays)
    const problema = validar(valor)
    setErroValidacao(problema)
    if (problema) {
      return
    }
    mutation.mutate({ retry_days: valor })
  }

  const erroServidor =
    mutation.isError && mutation.error instanceof Error
      ? mutation.error.message
      : null
  const mensagemErro = erroValidacao ?? erroServidor

  return (
    // noValidate: the min/max attributes below are only a native hint (spinner/
    // numeric keypad); the real validation is the `validar()` above —
    // same pt-BR message as the backend, without depending on the native browser
    // balloon (which would block the submit BEFORE onSubmit runs).
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-5">
      <label className="flex max-w-64 flex-col gap-1.5">
        <span className="text-sm font-medium">
          {t('settings.retroactive.retryDays')}
        </span>
        <Input
          type="number"
          min={1}
          max={365}
          value={retryDays}
          onChange={(e) => setRetryDays(e.target.value)}
          aria-label={t('settings.retroactive.retryDays')}
          aria-invalid={mensagemErro !== null || undefined}
        />
      </label>

      <FormSaveFooter
        erro={mensagemErro}
        sucesso={mutation.isSuccess}
        mensagemSucesso={t('settings.form.saved')}
        salvando={mutation.isPending}
        desabilitado={!sujo}
      />
    </form>
  )
}

// Both automatic toggles below are the same widget: a Switch seeded with the
// current setting that saves on every change (no save button, the same pattern as
// the "Watch this folder for changes" switch of the datasource list — low risk,
// reversible at any moment). The difference is only the endpoint, the query key
// and the copy, so the plumbing lives here once.
function SwitchAutomatico<T extends { enabled: boolean }>({
  inicial,
  salvar,
  invalidar,
  texto,
}: {
  inicial: T
  salvar: (body: T) => Promise<unknown>
  invalidar: string[]
  texto: { toggle: string; hint: string }
}) {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (body: T) => salvar(body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: invalidar })
    },
  })

  const ligado = mutation.isSuccess
    ? mutation.variables.enabled
    : inicial.enabled
  const mensagemErro =
    mutation.isError && mutation.error instanceof ApiError
      ? mutation.error.message
      : null

  return (
    <label className="flex items-center gap-2 text-sm">
      <Switch
        checked={ligado}
        disabled={mutation.isPending}
        onCheckedChange={(checked) =>
          mutation.mutate({ ...inicial, enabled: checked })
        }
        aria-label={texto.toggle}
        aria-invalid={mensagemErro !== null || undefined}
        // The off track (--input: oklch 0.922) is nearly invisible against the
        // card: the extra contrast makes "off" readable as a state.
        className="data-unchecked:bg-foreground/35"
      />
      <span className="flex flex-col gap-0.5">
        <span>{texto.toggle}</span>
        <span className="text-xs text-muted-foreground">{texto.hint}</span>
        {mensagemErro ? (
          <span role="alert" className="text-xs font-medium text-destructive">
            {mensagemErro}
          </span>
        ) : null}
      </span>
    </label>
  )
}

function AutoMaterializeArtworkSwitch({
  inicial,
}: {
  inicial: AutoMaterializeArtworkSettings
}) {
  const t = useT()
  return (
    <SwitchAutomatico
      inicial={inicial}
      salvar={putAutoMaterializeArtworkSettings}
      invalidar={['settings', 'auto-materialize-artwork']}
      texto={{
        toggle: t('settings.artwork.toggle'),
        hint: t('settings.artwork.hint'),
      }}
    />
  )
}

function AutoDownloadTrailerTemaSwitch({
  inicial,
}: {
  inicial: AutoDownloadTrailerTemaSettings
}) {
  const t = useT()
  return (
    <SwitchAutomatico
      inicial={inicial}
      salvar={putAutoDownloadTrailerTemaSettings}
      invalidar={['settings', 'auto-download-trailer-tema']}
      texto={{
        toggle: t('settings.trailerTheme.toggle'),
        hint: t('settings.trailerTheme.hint'),
      }}
    />
  )
}

// Form seeded with the current status (ADR-0025): a single
// custom path field (empty string == clear the override, the same
// contract as the backend) + a status badge. Without a "baixar agora" button
// on purpose — the download is lazy, triggered by the first real use in
// trailer/theme; this card only reflects and configures, it never
// triggers a download.
function YtdlpSettingsForm({
  inicial,
  onDirtyChange,
}: {
  inicial: YtdlpSettings
  onDirtyChange?: (sujo: boolean) => void
}) {
  const t = useT()
  const queryClient = useQueryClient()
  const [customPath, setCustomPath] = useState(inicial.custom_binary_path ?? '')

  const sujo = customPath !== (inicial.custom_binary_path ?? '')

  useEffect(() => {
    onDirtyChange?.(sujo)
  }, [sujo, onDirtyChange])

  const mutation = useMutation({
    mutationFn: (path: string) =>
      putYtdlpSettings({ custom_binary_path: path.trim() || null }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings', 'ytdlp'] })
    },
  })

  const dados = mutation.isSuccess ? mutation.data : inicial
  const mensagemErro =
    mutation.isError && mutation.error instanceof ApiError
      ? mutation.error.message
      : null

  function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    mutation.mutate(customPath)
  }

  return (
    <div className="flex flex-col gap-4">
      <YtdlpStatusBadge dados={dados} />

      <form onSubmit={onSubmit} className="flex flex-col gap-5">
        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">
            {t('settings.ytdlp.customPath')}
          </span>
          <Input
            type="text"
            value={customPath}
            onChange={(e) => setCustomPath(e.target.value)}
            placeholder={t('settings.ytdlp.customPathPlaceholder')}
            aria-label={t('settings.ytdlp.customPathAria')}
            aria-invalid={mensagemErro !== null || undefined}
          />
          <span className="text-xs text-muted-foreground">
            {t('settings.ytdlp.hint')}
          </span>
        </label>

        <p className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-400">
          {t('settings.ytdlp.termsWarning')}
        </p>

        <FormSaveFooter
          erro={mensagemErro}
          sucesso={mutation.isSuccess}
          mensagemSucesso={t('settings.form.saved')}
          salvando={mutation.isPending}
          desabilitado={!sujo}
        />
      </form>
    </div>
  )
}

// Status badge of yt-dlp: "available (downloaded automatically)",
// "available (custom path)" or "unavailable" + the reason (user journey).
function YtdlpStatusBadge({ dados }: { dados: YtdlpSettings }) {
  const t = useT()

  if (!dados.disponivel) {
    return (
      <div className="flex flex-col gap-1">
        <Badge variant="destructive" className="w-fit">
          {t('settings.ytdlp.badgeUnavailable')}
        </Badge>
        {dados.detalhe ? (
          <span className="text-xs text-muted-foreground">{dados.detalhe}</span>
        ) : null}
      </div>
    )
  }

  const rotulo =
    dados.origem === 'custom'
      ? t('settings.ytdlp.badgeAvailableCustom')
      : t('settings.ytdlp.badgeAvailableDownloaded')

  return (
    <Badge className="w-fit border-transparent bg-emerald-700 text-white">
      {rotulo}
    </Badge>
  )
}
