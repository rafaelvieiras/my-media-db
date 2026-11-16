import {
  useEffect,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
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

// GENERIC renderer of the editorial form: it builds the inputs
// and wires the Zod validation from the DESCRIPTORS (not from per-field JSX). It groups
// by `group` (translatable × neutral) and renders by `widget`. The creation and
// edit routes only provide the data (descriptors, seeds, schema) and the `onSubmit` —
// the shape of the submit (flat POST × per-language PATCH) stays in the route.

const CLASSE_TEXTAREA = cn(
  'flex min-h-20 w-full rounded-lg border border-border bg-background px-2.5 py-1.5 text-sm shadow-xs outline-none',
  'transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30',
)

export interface SchemaFormProps {
  // Descriptors to render, already cut for the context (creatable ones on creation,
  // the full set on edit).
  fields: FieldDescriptor[]
  // Seeds and schema derived from the same descriptors (via lib/field-schema).
  defaultValues: ValoresForm
  schema: import('zod').ZodType<ValoresForm>
  // The route receives the current values and decides the shape of the submit.
  onSubmit: (valores: ValoresForm) => void
  onCancel: () => void
  submitLabel: string
  submitPendingLabel: string
  submitIcon?: ReactNode
  isPending: boolean
  // Server error (422 etc.) — the client validation COMPLEMENTS it, it does not replace it.
  mensagemErro: string | null
  // Placeholders inherited per field (the edit form shows the resolved value underneath).
  placeholders?: Record<string, string>
  // Edit: "Limpar" button per field (DELETE per language/of the key) — distinct from
  // emptying the input (which on Save sets empty). Absent on creation.
  onClear?: (campo: string, traduzivel: boolean) => void
  clearDisabled?: boolean
  // Edit: current language (label of the translatable section). Absent on creation.
  lang?: string
  // Extra content at the top (e.g. the Type selector on item creation — it is not an
  // editorial field, so the route injects it).
  header?: ReactNode
  // "Leave without saving" guard: called only on the TRANSITION of the dirty
  // state (unsaved changes since the last load/save), never on
  // every keystroke — the route combines it with the one of the other edit blocks.
  onDirtyChange?: (sujo: boolean) => void
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
  onDirtyChange,
}: SchemaFormProps) {
  // Zod is Standard Schema (accepted directly by TanStack Form v1). The `input` of the
  // derived schema is `unknown` (effect of the number refine), so we narrow it
  // to the Standard Schema over the form values at the binding point.
  const validador = schema as unknown as StandardSchemaV1<
    ValoresForm,
    ValoresForm
  >
  const form = useForm({
    defaultValues,
    // onMount keeps the button disabled while a required field (title) is
    // empty; onChange validates on every keystroke.
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

      {onDirtyChange ? (
        <form.Subscribe
          selector={(s) => s.isDirty}
          children={(sujo) => (
            <NotificarSujo sujo={sujo} onDirtyChange={onDirtyChange} />
          )}
        />
      ) : null}

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

// NotificarSujo forwards `form.state.isDirty` to the parent via `onDirtyChange`
// only on the TRANSITION (the dependency array of the effect is the boolean itself) —
// it renders nothing, it is just the bridge between form.Subscribe and the callback.
function NotificarSujo({
  sujo,
  onDirtyChange,
}: {
  sujo: boolean
  onDirtyChange: (sujo: boolean) => void
}) {
  useEffect(() => {
    onDirtyChange(sujo)
  }, [sujo, onDirtyChange])
  return null
}

// LinhaCampo renders ONE descriptor through the `widget`, with the label (= aria-label), the
// client error (only after it is touched, so as not to shout in the pristine state) and,
// on edit, the "Limpar" button.
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

  // Attributes common to both widget branches (textarea × text/number input):
  // controlled value, handlers and accessibility. Extracted so as not to repeat the
  // same list in each branch (the specific part — type, min/max/step, class — stays
  // per branch).
  const comum = {
    value: field.state.value as string,
    onChange: (e: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
      field.handleChange(e.target.value),
    onBlur: field.handleBlur,
    placeholder,
    'aria-label': d.label,
    'aria-invalid': erro !== null || undefined,
  }

  const controle =
    d.widget === 'textarea' ? (
      <textarea {...comum} className={CLASSE_TEXTAREA} />
    ) : (
      <Input
        {...comum}
        type={d.widget === 'number' ? 'number' : 'text'}
        min={d.widget === 'number' ? d.min : undefined}
        max={d.widget === 'number' ? d.max : undefined}
        step={d.widget === 'number' ? d.step : undefined}
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

// CampoTags: removable chips + "add and Enter" input. The value is the `string[]`
// resolved on open; the {add,remove} diff happens when building the patch (route).
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

// mensagemDoErro extracts the pt-BR `message` of the Standard Schema (Zod) issue. The
// item may be the issue object or, defensively, a string.
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

// BotaoLimpar (go back to inheriting). Distinct from emptying the input (which on Save
// sets empty). The eraser icon reinforces the intention "erase the edit".
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
