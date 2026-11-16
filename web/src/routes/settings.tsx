import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Select } from '@/components/ui/select'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
import {
  ErrorState,
  FormSaveFooter,
  LoadingState,
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
import { CatalogCompletenessForm } from '@/components/catalog-completeness-form'

// Route `/settings`: language admin (primary, fallback, allowed). It reads the
// settings, lets you edit them and saves via PUT. The client-side validation mirrors the
// backend (primary and fallback ∈ allowed, non-empty allowed); the server 422 is
// the final guard and its pt-BR message appears inline.
export function SettingsScreen() {
  const { data, isPending, isError, error } = useQuery(
    languageSettingsQueryOptions,
  )
  const retroativo = useQuery(retroactiveEnrichmentSettingsQueryOptions)
  const autoMaterializar = useQuery(autoMaterializeArtworkSettingsQueryOptions)
  const autoDownloadTrailerTema = useQuery(
    autoDownloadTrailerTemaSettingsQueryOptions,
  )
  const ytdlp = useQuery(ytdlpSettingsQueryOptions)

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h1 className="font-heading text-xl font-semibold">Configurações</h1>
          <p className="text-sm text-muted-foreground">
            Idiomas do catálogo: principal, fallback e os permitidos.
          </p>
        </div>

        {isPending ? (
          <LoadingState label="Carregando configurações…" />
        ) : isError ? (
          <ErrorState
            title="Não foi possível carregar as configurações"
            message={error instanceof Error ? error.message : undefined}
          />
        ) : (
          <LanguageSettingsForm inicial={data} />
        )}
      </section>

      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            Enriquecimento retroativo
          </h2>
          <p className="text-sm text-muted-foreground">
            Intervalo mínimo entre duas tentativas de reenriquecer o MESMO item,
            para bibliotecas sem override próprio.
          </p>
        </div>

        {retroativo.isPending ? (
          <LoadingState label="Carregando configurações…" />
        ) : retroativo.isError ? (
          <ErrorState
            title="Não foi possível carregar as configurações"
            message={
              retroativo.error instanceof Error
                ? retroativo.error.message
                : undefined
            }
          />
        ) : (
          <RetroactiveEnrichmentSettingsForm inicial={retroativo.data} />
        )}
      </section>

      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">Artes</h2>
          <p className="text-sm text-muted-foreground">
            Comportamento da materialização de pôsteres/fundos na pasta real da
            biblioteca.
          </p>
        </div>

        {autoMaterializar.isPending ? (
          <LoadingState label="Carregando configurações…" />
        ) : autoMaterializar.isError ? (
          <ErrorState
            title="Não foi possível carregar as configurações"
            message={
              autoMaterializar.error instanceof Error
                ? autoMaterializar.error.message
                : undefined
            }
          />
        ) : (
          <AutoMaterializeArtworkSwitch inicial={autoMaterializar.data} />
        )}
      </section>

      <section className="mb-10">
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">
            Trailer e tema musical
          </h2>
          <p className="text-sm text-muted-foreground">
            Comportamento do download automático de trailer e música-tema via
            yt-dlp (briefings 0102/0103).
          </p>
        </div>

        {autoDownloadTrailerTema.isPending ? (
          <LoadingState label="Carregando configurações…" />
        ) : autoDownloadTrailerTema.isError ? (
          <ErrorState
            title="Não foi possível carregar as configurações"
            message={
              autoDownloadTrailerTema.error instanceof Error
                ? autoDownloadTrailerTema.error.message
                : undefined
            }
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
            Completude de catalogação
          </h2>
          <p className="text-sm text-muted-foreground">
            Por tipo de mídia, quais campos e artes são desejáveis para
            considerar um item bem catalogado.
          </p>
        </div>

        <CatalogCompletenessForm />
      </section>

      <section>
        <div className="mb-4 flex flex-col gap-1">
          <h2 className="font-heading text-lg font-semibold">yt-dlp</h2>
          <p className="text-sm text-muted-foreground">
            Downloader usado para baixar trailer (briefing 0102) e música-tema
            (briefing 0103) do YouTube. Nenhuma ação é necessária: o próprio
            MyMediaDB baixa o binário sozinho, dentro do container, na primeira
            vez que precisar dele.
          </p>
        </div>

        {ytdlp.isPending ? (
          <LoadingState label="Carregando status do yt-dlp…" />
        ) : ytdlp.isError ? (
          <ErrorState
            title="Não foi possível carregar o status do yt-dlp"
            message={
              ytdlp.error instanceof Error ? ytdlp.error.message : undefined
            }
          />
        ) : (
          <YtdlpSettingsForm inicial={ytdlp.data} />
        )}
      </section>
    </main>
  )
}

// Form seeded with the current config. It receives `inicial` already resolved to
// seed the state a single time (no synchronization useEffect).
function LanguageSettingsForm({ inicial }: { inicial: LanguageSettings }) {
  const queryClient = useQueryClient()
  const [primary, setPrimary] = useState(inicial.primary)
  const [fallback, setFallback] = useState(inicial.fallback)
  const [allowed, setAllowed] = useState<string[]>(inicial.allowed)
  const [erroValidacao, setErroValidacao] = useState<string | null>(null)

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
      return 'Selecione ao menos um idioma permitido.'
    }
    if (!allowed.includes(primary)) {
      return 'O idioma principal precisa estar entre os permitidos.'
    }
    if (!allowed.includes(fallback)) {
      return 'O idioma de fallback precisa estar entre os permitidos.'
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
        <span className="text-sm font-medium">Idioma principal</span>
        <Select value={primary} onChange={(e) => setPrimary(e.target.value)}>
          {candidatos.map((codigo) => (
            <option key={codigo} value={codigo}>
              {rotuloIdioma(codigo)}
            </option>
          ))}
        </Select>
      </label>

      <label className="flex flex-col gap-1.5">
        <span className="text-sm font-medium">Idioma de fallback</span>
        <Select value={fallback} onChange={(e) => setFallback(e.target.value)}>
          {candidatos.map((codigo) => (
            <option key={codigo} value={codigo}>
              {rotuloIdioma(codigo)}
            </option>
          ))}
        </Select>
      </label>

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-medium">Idiomas permitidos</legend>
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
        salvando={mutation.isPending}
      />
    </form>
  )
}

// Form seeded with the current throttle. Same pattern as the
// LanguageSettingsForm: controlled input, client-side validation mirroring the
// backend ([1, 365]) and PUT via TanStack Query.
function RetroactiveEnrichmentSettingsForm({
  inicial,
}: {
  inicial: RetroactiveEnrichmentSettings
}) {
  const queryClient = useQueryClient()
  const [retryDays, setRetryDays] = useState(String(inicial.retry_days))
  const [erroValidacao, setErroValidacao] = useState<string | null>(null)

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
      return 'Informe um número inteiro entre 1 e 365 dias.'
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
          Repetir tentativa após (dias)
        </span>
        <Input
          type="number"
          min={1}
          max={365}
          value={retryDays}
          onChange={(e) => setRetryDays(e.target.value)}
          aria-label="Repetir tentativa após (dias)"
          aria-invalid={mensagemErro !== null || undefined}
        />
      </label>

      <FormSaveFooter
        erro={mensagemErro}
        sucesso={mutation.isSuccess}
        salvando={mutation.isPending}
      />
    </form>
  )
}

// Switch seeded with the current toggle: it saves on every change —
// without a save button, the same pattern as the "Observar mudanças" switch of the
// datasource list (low risk, reversible at any moment).
function AutoMaterializeArtworkSwitch({
  inicial,
}: {
  inicial: AutoMaterializeArtworkSettings
}) {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (body: AutoMaterializeArtworkSettings) =>
      putAutoMaterializeArtworkSettings(body),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['settings', 'auto-materialize-artwork'],
      })
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
        onCheckedChange={(checked) => mutation.mutate({ enabled: checked })}
        aria-label="Baixar artes automaticamente após o scan"
        aria-invalid={mensagemErro !== null || undefined}
      />
      <span className="flex flex-col gap-0.5">
        <span>Baixar artes automaticamente após o scan</span>
        <span className="text-xs text-muted-foreground">
          Quando um item novo é auto-importado durante o scan, baixa e copia as
          artes encontradas para a pasta real da biblioteca na hora, sem esperar
          ação manual ou o job periódico.
        </span>
        {mensagemErro ? (
          <span role="alert" className="text-xs font-medium text-destructive">
            {mensagemErro}
          </span>
        ) : null}
      </span>
    </label>
  )
}

// Switch seeded with the current toggle: same pattern as the
// AutoMaterializeArtworkSwitch — it saves on every change, without a save
// button.
function AutoDownloadTrailerTemaSwitch({
  inicial,
}: {
  inicial: AutoDownloadTrailerTemaSettings
}) {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (body: AutoDownloadTrailerTemaSettings) =>
      putAutoDownloadTrailerTemaSettings(body),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['settings', 'auto-download-trailer-tema'],
      })
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
        onCheckedChange={(checked) => mutation.mutate({ enabled: checked })}
        aria-label="Baixar trailer e tema automaticamente"
        aria-invalid={mensagemErro !== null || undefined}
      />
      <span className="flex flex-col gap-0.5">
        <span>Baixar trailer e tema automaticamente</span>
        <span className="text-xs text-muted-foreground">
          Ao importar um item novo no scan, tenta baixar trailer e música-tema
          automaticamente em segundo plano (não bloqueia o scan). Desligado por
          padrão.
        </span>
        {mensagemErro ? (
          <span role="alert" className="text-xs font-medium text-destructive">
            {mensagemErro}
          </span>
        ) : null}
      </span>
    </label>
  )
}

// Form seeded with the current status (ADR-0025): a single
// custom path field (empty string == clear the override, the same
// contract as the backend) + a status badge. Without a "baixar agora" button
// on purpose — the download is lazy, triggered by the first real use in
// trailer/theme; this card only reflects and configures, it never
// triggers a download.
function YtdlpSettingsForm({ inicial }: { inicial: YtdlpSettings }) {
  const queryClient = useQueryClient()
  const [customPath, setCustomPath] = useState(inicial.custom_binary_path ?? '')

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
            Path customizado do binário
          </span>
          <Input
            type="text"
            value={customPath}
            onChange={(e) => setCustomPath(e.target.value)}
            placeholder="/usr/local/bin/yt-dlp (opcional)"
            aria-label="Path customizado do binário yt-dlp"
            aria-invalid={mensagemErro !== null || undefined}
          />
          <span className="text-xs text-muted-foreground">
            Deixe em branco para usar o binário baixado automaticamente (ou a
            variável de ambiente MYMEDIADB_YTDLP_PATH, quando configurada).
            Preencha só se você já gerencia sua própria instalação do yt-dlp.
          </span>
        </label>

        <p className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-400">
          Atenção: baixar vídeos do YouTube via yt-dlp contraria os Termos de
          Serviço do YouTube — prática tolerada de facto no ecossistema
          Kodi/Jellyfin/Plex/tinyMediaManager, mas um risco que você assume ao
          usar este recurso.
        </p>

        <FormSaveFooter
          erro={mensagemErro}
          sucesso={mutation.isSuccess}
          salvando={mutation.isPending}
        />
      </form>
    </div>
  )
}

// Status badge of yt-dlp: "disponível (baixado automaticamente)",
// "disponível (path customizado)" or "indisponível" + the reason (user
// journey).
function YtdlpStatusBadge({ dados }: { dados: YtdlpSettings }) {
  if (!dados.disponivel) {
    return (
      <div className="flex flex-col gap-1">
        <Badge variant="destructive" className="w-fit">
          indisponível
        </Badge>
        {dados.detalhe ? (
          <span className="text-xs text-muted-foreground">{dados.detalhe}</span>
        ) : null}
      </div>
    )
  }

  const rotulo =
    dados.origem === 'custom'
      ? 'disponível (path customizado)'
      : 'disponível (baixado automaticamente)'

  return (
    <Badge className="w-fit border-transparent bg-emerald-600 text-white">
      {rotulo}
    </Badge>
  )
}
