import { useState, type KeyboardEvent, type ReactNode } from 'react'
import {
  useForm,
  type AnyFieldApi,
  type StandardSchemaV1,
} from '@tanstack/react-form'
import { Eraser, Plus, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import type { FieldDescriptor } from '@/lib/api'
import { ordenados, type ValoresForm } from '@/lib/field-schema'
import { rotuloIdioma } from '@/lib/languages'

// Renderizador GENÉRICO do formulário editorial (briefing 0030): monta os inputs
// e liga a validação Zod a partir dos DESCRITORES (não de JSX por campo). Agrupa
// por `group` (traduzível × neutro) e renderiza por `widget`. As rotas de criação
// e edição só fornecem os dados (descritores, sementes, schema) e o `onSubmit` —
// a forma do submit (POST plano × PATCH por idioma) fica na rota.

const CLASSE_TEXTAREA = cn(
  'flex min-h-20 w-full rounded-lg border border-border bg-background px-2.5 py-1.5 text-sm shadow-xs outline-none',
  'transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30',
)

export interface SchemaFormProps {
  // Descritores a renderizar, já recortados para o contexto (criáveis na criação,
  // conjunto completo na edição).
  fields: FieldDescriptor[]
  // Sementes e schema derivados dos mesmos descritores (via lib/field-schema).
  defaultValues: ValoresForm
  schema: import('zod').ZodType<ValoresForm>
  // A rota recebe os valores atuais e decide a forma do submit.
  onSubmit: (valores: ValoresForm) => void
  onCancel: () => void
  submitLabel: string
  submitPendingLabel: string
  submitIcon?: ReactNode
  isPending: boolean
  // Erro do servidor (422 etc.) — a validação de cliente COMPLEMENTA, não substitui.
  mensagemErro: string | null
  // Placeholders herdados por campo (edição mostra o valor resolvido por baixo).
  placeholders?: Record<string, string>
  // Edição: botão "Limpar" por campo (DELETE por idioma/da chave) — distinto de
  // esvaziar o input (que ao Salvar define vazio). Ausente na criação.
  onClear?: (campo: string, traduzivel: boolean) => void
  clearDisabled?: boolean
  // Edição: idioma corrente (rótulo da seção traduzível). Ausente na criação.
  lang?: string
  // Conteúdo extra no topo (ex.: o seletor de Tipo na criação de item — não é
  // campo editorial, então a rota o injeta).
  header?: ReactNode
}

export function SchemaForm({
  fields,
  defaultValues,
  schema,
  onSubmit,
  onCancel,
  submitLabel,
  submitPendingLabel,
  submitIcon,
  isPending,
  mensagemErro,
  placeholders,
  onClear,
  clearDisabled,
  lang,
  header,
}: SchemaFormProps) {
  // Zod é Standard Schema (aceito direto pelo TanStack Form v1). O `input` do
  // schema derivado é `unknown` (efeito do refine do número), então estreitamos
  // para o Standard Schema sobre os valores do form no ponto de ligação.
  const validador = schema as unknown as StandardSchemaV1<
    ValoresForm,
    ValoresForm
  >
  const form = useForm({
    defaultValues,
    // onMount deixa o botão desabilitado enquanto um obrigatório (título) está
    // vazio; onChange valida a cada digitação.
    validators: { onMount: validador, onChange: validador },
    onSubmit: async ({ value }) => {
      onSubmit(value)
    },
  })

  const lista = ordenados(fields)
  const traduziveis = lista.filter((f) => f.group === 'traduzivel')
  const neutros = lista.filter((f) => f.group === 'neutro')

  function renderCampo(d: FieldDescriptor) {
    return (
      <form.Field
        key={d.name}
        name={d.name}
        children={(field) => (
          <LinhaCampo
            d={d}
            field={field}
            placeholder={placeholders?.[d.name] ?? d.placeholder ?? undefined}
            onClear={onClear}
            clearDisabled={clearDisabled}
          />
        )}
      />
    )
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        e.stopPropagation()
        void form.handleSubmit()
      }}
      className="mt-2 flex flex-col gap-5 rounded-xl bg-card p-4 ring-1 ring-foreground/10"
    >
      {header}

      {traduziveis.length > 0 ? (
        <div className="flex flex-col gap-4">
          {lang ? (
            <p className="text-sm font-medium">
              Editando em{' '}
              <Badge variant="secondary">{rotuloIdioma(lang)}</Badge>
            </p>
          ) : null}
          {traduziveis.map(renderCampo)}
        </div>
      ) : null}

      {neutros.length > 0 ? (
        <div
          className={cn(
            'flex flex-col gap-4',
            traduziveis.length > 0 ? 'border-t border-border pt-4' : undefined,
          )}
        >
          {lang ? (
            <p className="text-sm text-muted-foreground">
              Campos neutros — valem para todos os idiomas.
            </p>
          ) : null}
          {neutros.map(renderCampo)}
        </div>
      ) : null}

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      <div className="flex items-center gap-2">
        <form.Subscribe
          selector={(s) => ({
            canSubmit: s.canSubmit,
            isSubmitting: s.isSubmitting,
          })}
          children={({ canSubmit, isSubmitting }) => (
            <Button
              type="submit"
              disabled={isPending || isSubmitting || !canSubmit}
            >
              {submitIcon}
              {isPending ? submitPendingLabel : submitLabel}
            </Button>
          )}
        />
        <Button
          type="button"
          variant="ghost"
          onClick={onCancel}
          disabled={isPending}
        >
          Cancelar
        </Button>
      </div>
    </form>
  )
}

// LinhaCampo renderiza UM descritor pelo `widget`, com o rótulo (= aria-label), o
// erro de cliente (só depois de tocado, para não gritar no estado pristino) e,
// na edição, o botão "Limpar".
function LinhaCampo({
  d,
  field,
  placeholder,
  onClear,
  clearDisabled,
}: {
  d: FieldDescriptor
  field: AnyFieldApi
  placeholder?: string
  onClear?: (campo: string, traduzivel: boolean) => void
  clearDisabled?: boolean
}) {
  const erro =
    field.state.meta.isTouched && field.state.meta.errors.length > 0
      ? mensagemDoErro(field.state.meta.errors[0])
      : null

  const limpar = onClear ? (
    <BotaoLimpar
      rotulo={`Limpar ${d.label.toLowerCase()}`}
      onClick={() => onClear(d.name, d.translatable)}
      disabled={clearDisabled}
    />
  ) : null

  if (d.widget === 'tags') {
    return <CampoTags d={d} field={field} limpar={limpar} erro={erro} />
  }

  const controle =
    d.widget === 'textarea' ? (
      <textarea
        value={field.state.value as string}
        onChange={(e) => field.handleChange(e.target.value)}
        onBlur={field.handleBlur}
        placeholder={placeholder}
        aria-label={d.label}
        aria-invalid={erro !== null || undefined}
        className={CLASSE_TEXTAREA}
      />
    ) : (
      <Input
        type={d.widget === 'number' ? 'number' : 'text'}
        min={d.widget === 'number' ? d.min : undefined}
        max={d.widget === 'number' ? d.max : undefined}
        step={d.widget === 'number' ? d.step : undefined}
        value={field.state.value as string}
        onChange={(e) => field.handleChange(e.target.value)}
        onBlur={field.handleBlur}
        placeholder={placeholder}
        aria-label={d.label}
        aria-invalid={erro !== null || undefined}
        className={d.widget === 'number' ? 'max-w-32' : undefined}
      />
    )

  return (
    <label className="flex flex-col gap-1.5 text-sm">
      <span className="font-medium">{d.label}</span>
      <div className="flex items-center gap-2">
        {controle}
        {limpar}
      </div>
      {erro ? <span className="text-xs text-destructive">{erro}</span> : null}
    </label>
  )
}

// CampoTags: chips removíveis + input "adicionar e Enter". O valor é o `string[]`
// resolvido ao abrir; o diff {add,remove} acontece ao montar o patch (rota).
function CampoTags({
  d,
  field,
  limpar,
  erro,
}: {
  d: FieldDescriptor
  field: AnyFieldApi
  limpar: ReactNode
  erro: string | null
}) {
  const [tagInput, setTagInput] = useState('')
  const tags = (field.state.value as string[]) ?? []

  function adicionar() {
    const limpo = tagInput.trim()
    if (limpo !== '' && !tags.includes(limpo)) {
      field.handleChange([...tags, limpo])
    }
    setTagInput('')
  }

  function onTagKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      adicionar()
    }
  }

  return (
    <div className="flex flex-col gap-1.5 text-sm">
      <span className="font-medium">{d.label}</span>
      <div className="flex flex-wrap items-center gap-1.5">
        {tags.map((tag) => (
          <Badge key={tag} variant="outline" className="gap-1 pr-1">
            {tag}
            <button
              type="button"
              aria-label={`Remover tag ${tag}`}
              onClick={() => field.handleChange(tags.filter((t) => t !== tag))}
              className="rounded-full text-muted-foreground hover:text-foreground"
            >
              <X className="size-3" aria-hidden />
            </button>
          </Badge>
        ))}
        {tags.length === 0 ? (
          <span className="text-xs text-muted-foreground">Nenhuma tag.</span>
        ) : null}
      </div>
      <div className="flex items-center gap-2">
        <Input
          value={tagInput}
          onChange={(e) => setTagInput(e.target.value)}
          onKeyDown={onTagKeyDown}
          placeholder="Adicionar tag e Enter"
          aria-label="Adicionar tag"
        />
        <Button type="button" variant="outline" size="sm" onClick={adicionar}>
          <Plus aria-hidden />
          Adicionar
        </Button>
        {limpar}
      </div>
      {erro ? <span className="text-xs text-destructive">{erro}</span> : null}
    </div>
  )
}

// mensagemDoErro extrai a `message` pt-BR do issue do Standard Schema (Zod). O
// item pode ser o objeto do issue ou, defensivamente, uma string.
function mensagemDoErro(issue: unknown): string {
  if (typeof issue === 'string') {
    return issue
  }
  if (
    issue &&
    typeof issue === 'object' &&
    'message' in issue &&
    typeof (issue as { message: unknown }).message === 'string'
  ) {
    return (issue as { message: string }).message
  }
  return 'Valor inválido.'
}

// BotaoLimpar (volta a herdar). Distinto de esvaziar o input (que ao Salvar
// define vazio). O ícone de borracha reforça a intenção "apagar a edição".
function BotaoLimpar({
  rotulo,
  onClick,
  disabled,
}: {
  rotulo: string
  onClick: () => void
  disabled?: boolean
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      aria-label={rotulo}
      onClick={onClick}
      disabled={disabled}
    >
      <Eraser aria-hidden />
      Limpar
    </Button>
  )
}
