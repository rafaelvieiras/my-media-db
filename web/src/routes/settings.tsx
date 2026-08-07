import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Select } from '@/components/ui/select'
import { Input } from '@/components/ui/input'
import {
  ErrorState,
  FormSaveFooter,
  LoadingState,
} from '@/components/query-states'
import {
  languageSettingsQueryOptions,
  retroactiveEnrichmentSettingsQueryOptions,
} from '@/lib/queries'
import {
  putLanguageSettings,
  putRetroactiveEnrichmentSettings,
  type LanguageSettings,
  type RetroactiveEnrichmentSettings,
} from '@/lib/api'
import { IDIOMAS_DISPONIVEIS, rotuloIdioma } from '@/lib/languages'
import { CatalogCompletenessForm } from '@/components/catalog-completeness-form'

// Rota `/settings`: admin de idiomas (principal, fallback, permitidos). Lê as
// settings, deixa editar e grava via PUT. A validação client-side espelha o
// backend (principal e fallback ∈ permitidos, permitidos não-vazio); o 422 do
// servidor é o guarda final e sua mensagem pt-BR aparece inline.
export function SettingsScreen() {
  const { data, isPending, isError, error } = useQuery(
    languageSettingsQueryOptions,
  )
  const retroativo = useQuery(retroactiveEnrichmentSettingsQueryOptions)

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

      <section>
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
    </main>
  )
}

// Formulário semeado pela config vigente. Recebe `inicial` já resolvido para
// semear o estado uma única vez (sem useEffect de sincronização).
function LanguageSettingsForm({ inicial }: { inicial: LanguageSettings }) {
  const queryClient = useQueryClient()
  const [primary, setPrimary] = useState(inicial.primary)
  const [fallback, setFallback] = useState(inicial.fallback)
  const [allowed, setAllowed] = useState<string[]>(inicial.allowed)
  const [erroValidacao, setErroValidacao] = useState<string | null>(null)

  // Candidatos do multi-select: os conhecidos + os já permitidos (para não
  // perder um código exótico que já esteja salvo). Também alimenta os selects
  // de principal/fallback — assim é possível (e a validação captura) escolher um
  // valor fora dos permitidos.
  const candidatos = useMemo(() => {
    const set = new Set<string>([...IDIOMAS_DISPONIVEIS, ...allowed])
    return [...set]
  }, [allowed])

  const mutation = useMutation({
    mutationFn: (body: LanguageSettings) => putLanguageSettings(body),
    onSuccess: () => {
      // Mudar os permitidos pode invalidar o `lang` atual das leituras: revalida
      // as settings e as leituras dependentes (evita servir cache de um idioma
      // recém-desabilitado).
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

  // Espelha o backend. Devolve a mensagem do 1º problema, ou null se válido.
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

  // Erro exibido inline: a validação client-side tem prioridade; senão, a
  // mensagem pt-BR do ApiError (422) da resposta do servidor.
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

// Formulário semeado pelo throttle vigente (briefing 0085). Mesmo padrão do
// LanguageSettingsForm: input controlado, validação client-side espelhando o
// backend ([1, 365]) e PUT via TanStack Query.
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

  // Espelha o backend (retry_days entre 1 e 365). Devolve a mensagem do 1º
  // problema, ou null se válido.
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
    // noValidate: os atributos min/max abaixo são só um hint nativo (spinner/
    // teclado numérico); a validação de verdade é a `validar()` acima —
    // mesma mensagem pt-BR do backend, sem depender do balão nativo do
    // navegador (que bloquearia o submit ANTES do onSubmit rodar).
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
