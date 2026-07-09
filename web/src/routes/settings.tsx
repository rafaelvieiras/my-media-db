import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { ErrorState, LoadingState } from '@/components/query-states'
import { languageSettingsQueryOptions } from '@/lib/queries'
import { putLanguageSettings, type LanguageSettings } from '@/lib/api'
import { IDIOMAS_DISPONIVEIS, rotuloIdioma } from '@/lib/languages'

// Rota `/settings`: admin de idiomas (principal, fallback, permitidos). Lê as
// settings, deixa editar e grava via PUT. A validação client-side espelha o
// backend (principal e fallback ∈ permitidos, permitidos não-vazio); o 422 do
// servidor é o guarda final e sua mensagem pt-BR aparece inline.
export function SettingsScreen() {
  const { data, isPending, isError, error } = useQuery(
    languageSettingsQueryOptions,
  )

  return (
    <main className="mx-auto max-w-2xl px-4 py-6">
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

      {mensagemErro ? (
        <p role="alert" className="text-sm text-destructive">
          {mensagemErro}
        </p>
      ) : null}
      {mutation.isSuccess && !mensagemErro ? (
        <p role="status" className="text-sm text-muted-foreground">
          Configurações salvas.
        </p>
      ) : null}

      <div>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? 'Salvando…' : 'Salvar'}
        </Button>
      </div>
    </form>
  )
}
