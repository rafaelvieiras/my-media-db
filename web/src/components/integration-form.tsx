import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { FormSaveFooter } from '@/components/query-states'
import { ApiError, putIntegration, type IntegrationField } from '@/lib/api'

// IntegrationForm (briefing 0096): formulário GERADO A PARTIR DO SCHEMA de
// UMA integração (`GET /integrations/{slug}`), no mesmo espírito do
// `SchemaForm` do 0030 (descritor → widget), mas com um vocabulário PRÓPRIO
// (`integration.Campo`, não `FieldDescriptor` de entity) e um widget que o
// `SchemaForm` não tem: `secret` (toggle mostrar/ocultar, nunca reidrata do
// servidor). Reaproveitado é só o PADRÃO ("form dirigido por descritor"), não
// o componente — os dois vocabulários e semânticas de submit são distintos o
// bastante (PUT parcial com `null` = limpar, aqui; PATCH por idioma lá) para
// não valer forçar um componente único.

// EstadoCampo é o rascunho local de UM campo: `valor` é o que o dono digitou/
// selecionou nesta sessão de edição (nunca semeado com o valor efetivo de um
// campo secreto — só os não-secretos pré-preenchem, ver `estadoInicial`);
// `limpar` marca que o botão "Limpar" foi clicado (grava `null` no PUT,
// distinto de deixar o campo vazio sem clicar em Limpar — que não entra no
// PUT, mesma distinção do `SchemaForm`/editor de entity).
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

// ENV_VAR_HINTS é um mapa best-effort nome-da-env-var por slug+campo, usado
// SÓ para a legenda de `origem:"env"` (briefing 0096 §2, item 3: "Definido
// pela variável de ambiente MYMEDIADB_TMDB_TOKEN..."). O contrato do 0095
// (`IntegrationField`, openapi.yaml) DELIBERADAMENTE não devolve o nome da
// env var — só `origem`/`definido` — então isso é um hint de UI conhecido
// hoje só para o TMDB; qualquer campo fora deste mapa (ex.: uma integração de
// plugin futura) cai no texto genérico abaixo, nunca inventa um nome. Decisão
// tomada aqui (não no briefing/backend) por ser puramente de apresentação.
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

  // `campos` é ESTADO PRÓPRIO (semeado da prop, não a prop direto): depois de
  // um PUT bem-sucedido, precisa refletir o `definido`/`origem` frescos SEM
  // desmontar o formulário — desmontar (ex.: via `key` no refetch do pai)
  // resetaria `mutation.isSuccess` e faria a mensagem de sucesso ("Salvo.
  // Reinicie o servidor...") sumir antes do dono conseguir ler. Por isso o
  // reseed acontece aqui, a partir do ECO do próprio PUT (`onSuccess` abaixo),
  // e o componente permanece montado com a mesma instância de `useMutation`.
  const [campos, setCampos] = useState(camposIniciais)
  const [estados, setEstados] = useState<Record<string, EstadoCampo>>(() =>
    Object.fromEntries(camposIniciais.map((c) => [c.nome, estadoInicial(c)])),
  )

  const mutation = useMutation({
    mutationFn: (valores: Record<string, string | null>) =>
      putIntegration(slug, valores),
    onSuccess: (detalhe) => {
      // Invalida a lista (badge `configurada`) para quando o dono voltar a
      // `/dashboard/integrations`. O PRÓPRIO formulário reseeda a partir do
      // eco do PUT (`detalhe.campos`) — nunca do valor de um campo secreto
      // (que o eco também nunca carrega, mesma regra do GET): cada campo
      // secreto volta a ficar vazio/mascarado, com `origem`/`definido` já
      // atualizados para a legenda e o botão "Limpar" refletirem na hora.
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
    // Nada tocado (nenhum campo preenchido/limpo): o 422 do backend por
    // `valores` vazio não agrega nada — simplesmente não há o que salvar.
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

// CampoSegredo NUNCA pré-preenche (mesmo com `definido:true` — a API não
// devolve `valor` de um campo secreto em nenhuma resposta). O placeholder
// sinaliza "há um valor efetivo, mascarado" sem revelar nada.
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
