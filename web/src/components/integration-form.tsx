import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { FormSaveFooter } from '@/components/query-states'
import { ApiError, putIntegration, type IntegrationField } from '@/lib/api'

// IntegrationForm: form GENERATED FROM THE SCHEMA of
// ONE integration (`GET /integrations/{slug}`), in the same spirit as the
// `SchemaForm`, but with a vocabulary OF ITS OWN
// (`integration.Campo`, not the entity `FieldDescriptor`) and a widget that the
// `SchemaForm` does not have: `secret` (show/hide toggle, never rehydrated from
// the server). Only the PATTERN is reused ("form driven by descriptor"), not
// the component — the two vocabularies and submit semantics are different enough
// (partial PUT with `null` = clear, here; PATCH per language there) that
// forcing a single component is not worth it.

// EstadoCampo is the local draft of ONE field: `valor` is what the owner typed/
// selected in this editing session (never seeded with the effective value of a
// secret field — only the non-secret ones pre-fill, see `estadoInicial`);
// `limpar` marks that the "Limpar" button was clicked (it writes `null` in the PUT,
// distinct from leaving the field empty without clicking Limpar — which does not enter the
// PUT, the same distinction as the `SchemaForm`/entity editor).
interface EstadoCampo {
  valor: string
  limpar: boolean
}

function estadoInicial(campo: IntegrationField): EstadoCampo {
  if (campo.tipo === 'secret') {
    return { valor: '', limpar: false }
  }
  if (campo.tipo === 'bool') {
    return { valor: campo.valor === 'true' ? 'true' : 'false', limpar: false }
  }
  return { valor: campo.valor ?? '', limpar: false }
}

// ENV_VAR_HINTS is a best-effort map of env-var name per slug+field, used
// ONLY for the caption of `origem:"env"` (§2, item 3: "Definido
// pela variável de ambiente MYMEDIADB_TMDB_TOKEN..."). The contract
// (`IntegrationField`, openapi.yaml) DELIBERATELY does not return the name of the
// env var — only `origem`/`definido` — so this is a UI hint known
// today only for TMDB; any field outside this map (e.g. a future plugin
// integration) falls into the generic text below, it never invents a name. A decision
// made here (not in the spec/backend) because it is purely presentational.
const ENV_VAR_HINTS: Record<string, Record<string, string>> = {
  tmdb: { token: 'MYMEDIADB_TMDB_TOKEN', api_key: 'MYMEDIADB_TMDB_API_KEY' },
}

function legendaOrigem(slug: string, campo: IntegrationField): string | null {
  if (campo.origem === 'banco') {
    return 'Definido no painel.'
  }
  if (campo.origem === 'env') {
    const envVar = ENV_VAR_HINTS[slug]?.[campo.nome]
    return envVar
      ? `Definido pela variável de ambiente ${envVar} — salve aqui para sobrepor.`
      : 'Definido por uma variável de ambiente do servidor — salve aqui para sobrepor.'
  }
  return null
}

export interface IntegrationFormProps {
  slug: string
  campos: IntegrationField[]
}

export function IntegrationForm({
  slug,
  campos: camposIniciais,
}: IntegrationFormProps) {
  const queryClient = useQueryClient()

  // `campos` is ITS OWN STATE (seeded from the prop, not the prop directly): after
  // a successful PUT, it needs to reflect the fresh `definido`/`origem` WITHOUT
  // unmounting the form — unmounting (e.g. via a `key` on the parent refetch)
  // would reset `mutation.isSuccess` and make the success message ("Salvo.
  // Reinicie o servidor...") vanish before the owner manages to read it. That is why the
  // reseed happens here, from the ECHO of the PUT itself (`onSuccess` below),
  // and the component stays mounted with the same `useMutation` instance.
  const [campos, setCampos] = useState(camposIniciais)
  const [estados, setEstados] = useState<Record<string, EstadoCampo>>(() =>
    Object.fromEntries(camposIniciais.map((c) => [c.nome, estadoInicial(c)])),
  )

  const mutation = useMutation({
    mutationFn: (valores: Record<string, string | null>) =>
      putIntegration(slug, valores),
    onSuccess: (detalhe) => {
      // Invalidates the list (`configurada` badge) for when the owner comes back to
      // `/dashboard/integrations`. The FORM ITSELF reseeds from the
      // echo of the PUT (`detalhe.campos`) — never from the value of a secret field
      // (which the echo never carries either, same rule as the GET): each secret
      // field becomes empty/masked again, with `origem`/`definido` already
      // updated so that the caption and the "Limpar" button reflect it right away.
      void queryClient.invalidateQueries({ queryKey: ['integrations'] })
      void queryClient.invalidateQueries({ queryKey: ['integrations', slug] })
      setCampos(detalhe.campos)
      setEstados(
        Object.fromEntries(
          detalhe.campos.map((c) => [c.nome, estadoInicial(c)]),
        ),
      )
    },
  })

  function atualizar(nome: string, valor: string) {
    setEstados((prev) => ({ ...prev, [nome]: { valor, limpar: false } }))
  }

  function limpar(nome: string) {
    setEstados((prev) => ({ ...prev, [nome]: { valor: '', limpar: true } }))
  }

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    const valores: Record<string, string | null> = {}
    for (const campo of campos) {
      const estado = estados[campo.nome]
      if (estado.limpar) {
        valores[campo.nome] = null
      } else if (estado.valor.trim() !== '') {
        valores[campo.nome] = estado.valor
      }
    }
    // Nothing touched (no field filled/cleared): the backend 422 for an empty
    // `valores` adds nothing — there is simply nothing to save.
    if (Object.keys(valores).length === 0) {
      return
    }
    mutation.mutate(valores)
  }

  const mensagemErro =
    mutation.isError && mutation.error instanceof ApiError
      ? mutation.error.message
      : null

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-5">
      {campos.map((campo) => (
        <CampoIntegracao
          key={campo.nome}
          slug={slug}
          campo={campo}
          estado={estados[campo.nome]}
          onChange={(valor) => atualizar(campo.nome, valor)}
          onClear={() => limpar(campo.nome)}
          disabled={mutation.isPending}
        />
      ))}

      <FormSaveFooter
        erro={mensagemErro}
        sucesso={mutation.isSuccess}
        mensagemSucesso="Salvo. Reinicie o servidor (docker restart) para aplicar."
        salvando={mutation.isPending}
      />
    </form>
  )
}

function CampoIntegracao({
  slug,
  campo,
  estado,
  onChange,
  onClear,
  disabled,
}: {
  slug: string
  campo: IntegrationField
  estado: EstadoCampo
  onChange: (valor: string) => void
  onClear: () => void
  disabled: boolean
}) {
  const legenda = legendaOrigem(slug, campo)
  const mostrarLimpar = campo.origem === 'banco'

  return (
    <div className="flex flex-col gap-1.5 text-sm">
      <span className="font-medium">
        {campo.label}
        {campo.obrigatorio ? (
          <span aria-hidden className="text-destructive">
            {' '}
            *
          </span>
        ) : null}
      </span>

      <div className="flex items-center gap-2">
        <CampoControle
          campo={campo}
          estado={estado}
          onChange={onChange}
          disabled={disabled}
        />
      </div>

      {campo.ajuda ? (
        <span className="text-xs text-muted-foreground">{campo.ajuda}</span>
      ) : null}

      {legenda ? (
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{legenda}</span>
          {mostrarLimpar ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onClear}
              disabled={disabled}
            >
              Limpar
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function CampoControle({
  campo,
  estado,
  onChange,
  disabled,
}: {
  campo: IntegrationField
  estado: EstadoCampo
  onChange: (valor: string) => void
  disabled: boolean
}) {
  if (campo.tipo === 'secret') {
    return (
      <CampoSegredo
        campo={campo}
        estado={estado}
        onChange={onChange}
        disabled={disabled}
      />
    )
  }

  if (campo.tipo === 'bool') {
    return (
      <Switch
        checked={estado.valor === 'true'}
        onCheckedChange={(checked) => onChange(checked ? 'true' : 'false')}
        aria-label={campo.label}
        disabled={disabled}
      />
    )
  }

  if (campo.tipo === 'select') {
    return (
      <Select
        value={estado.valor}
        onChange={(e) => onChange(e.target.value)}
        aria-label={campo.label}
        disabled={disabled}
      >
        <option value="">Selecione…</option>
        {(campo.opcoes ?? []).map((op) => (
          <option key={op.valor} value={op.valor}>
            {op.label}
          </option>
        ))}
      </Select>
    )
  }

  return (
    <Input
      type={campo.tipo === 'number' ? 'number' : 'text'}
      value={estado.valor}
      onChange={(e) => onChange(e.target.value)}
      placeholder={campo.placeholder}
      aria-label={campo.label}
      disabled={disabled}
    />
  )
}

// CampoSegredo NEVER pre-fills (even with `definido:true` — the API does not
// return the `valor` of a secret field in any response). The placeholder
// signals "there is an effective, masked value" without revealing anything.
function CampoSegredo({
  campo,
  estado,
  onChange,
  disabled,
}: {
  campo: IntegrationField
  estado: EstadoCampo
  onChange: (valor: string) => void
  disabled: boolean
}) {
  const [mostrar, setMostrar] = useState(false)

  return (
    <>
      <Input
        type={mostrar ? 'text' : 'password'}
        value={estado.valor}
        onChange={(e) => onChange(e.target.value)}
        placeholder={
          campo.placeholder ?? (campo.definido ? '••••••••' : undefined)
        }
        aria-label={campo.label}
        disabled={disabled}
        autoComplete="off"
      />
      <Button
        type="button"
        variant="outline"
        size="icon"
        onClick={() => setMostrar((v) => !v)}
        aria-label={
          mostrar ? `Ocultar ${campo.label}` : `Mostrar ${campo.label}`
        }
        disabled={disabled}
      >
        {mostrar ? (
          <EyeOff className="size-4" aria-hidden />
        ) : (
          <Eye className="size-4" aria-hidden />
        )}
      </Button>
    </>
  )
}
