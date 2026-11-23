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
import { useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
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
  'flex min-h-32 w-full rounded-lg border border-border bg-background px-2.5 py-1.5 text-sm shadow-xs outline-none',
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
  // Fields that the backend requires IN THIS CONTEXT (creation): they render the
  // asterisk and `aria-required`. The server 422 keeps being authoritative.
  obrigatorios?: ReadonlySet<string>
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
  obrigatorios,
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
  const t = useT()
  const traduziveis = lista.filter((f) => f.group === 'traduzivel')
  const neutros = lista.filter((f) => f.group === 'neutro')
  // The summary appears only after a submit attempt with problems; it disappears
  // by itself as soon as the values become valid (the `form.Subscribe` below
  // recomputes it on every change).
  const [mostrarResumo, setMostrarResumo] = useState(false)

  // resumoDeErros names the fields that fail the SAME schema the form validates
  // with, so the top of the form says what is missing before the owner hunts for
  // the red text in a long form.
  function resumoDeErros(): string[] {
    const resultado = schema.safeParse(form.state.values)
    if (resultado.success) return []
    const nomes = new Set<string>()
    for (const issue of resultado.error.issues) {
      nomes.add(String(issue.path[0] ?? ''))
    }
    return [...nomes]
      .map((nome) => lista.find((d) => d.name === nome)?.label ?? nome)
      .filter((rotulo) => rotulo !== '')
  }

  // The submit button is ALWAYS enabled on purpose: a disabled primary action with
  // no explanation is a dead end (the owner cannot discover what is missing).
  // Submitting marks every field as touched, so the inline errors stop being silent.
  async function enviar(e: React.FormEvent) {
    e.preventDefault()
    e.stopPropagation()
    for (const d of lista) {
      form.setFieldMeta(d.name, (prev) => ({ ...prev, isTouched: true }))
    }
    void form.validateAllFields('submit')
    setMostrarResumo(true)
    await form.handleSubmit()
  }

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
            obrigatorio={obrigatorios?.has(d.name) ?? false}
          />
        )}
      />
    )
  }

  return (
    <form
      onSubmit={(e) => {
        void enviar(e)
      }}
      className="mt-2 flex flex-col gap-5 rounded-xl bg-card p-4 ring-1 ring-foreground/10"
    >
      {header}

      {mostrarResumo ? (
        <form.Subscribe
          selector={(s) => s.values}
          children={() => {
            const problemas = resumoDeErros()
            return problemas.length > 0 ? (
              <div
                role="alert"
                className="rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2 text-sm text-destructive"
              >
                <p className="font-medium">
                  {t('entity.edit.requiredSummary')}
                </p>
                <ul className="list-disc pl-5">
                  {problemas.map((problema) => (
                    <li key={problema}>{problema}</li>
                  ))}
                </ul>
              </div>
            ) : null
          }}
        />
      ) : null}

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
              {t('entity.edit.editingIn')}{' '}
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
              {t('entity.edit.neutralFieldsHint')}
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

      <div className="sticky bottom-0 z-10 -mx-4 flex items-center gap-2 border-t border-border bg-card/95 px-4 py-2 backdrop-blur supports-[backdrop-filter]:bg-card/80">
        <form.Subscribe
          selector={(s) => ({ isSubmitting: s.isSubmitting })}
          children={({ isSubmitting }) => (
            <Button type="submit" disabled={isPending || isSubmitting}>
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
          {t('common.action.cancel')}
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
  obrigatorio,
}: {
  d: FieldDescriptor
  field: AnyFieldApi
  placeholder?: string
  onClear?: (campo: string, traduzivel: boolean) => void
  clearDisabled?: boolean
  obrigatorio?: boolean
}) {
  const t = useT()
  const erro =
    field.state.meta.isTouched && field.state.meta.errors.length > 0
      ? mensagemDoErro(field.state.meta.errors[0], t)
      : null
  // Stable ids so the error is announced by the screen reader together with the
  // control it belongs to (`aria-describedby`), not only painted red.
  const inputId = `campo-${d.name}`
  const erroId = `${inputId}-erro`

  const limpar = onClear ? (
    <BotaoLimpar
      rotulo={t('entity.edit.clearField', { field: d.label.toLowerCase() })}
      onClick={() => onClear(d.name, d.translatable)}
      disabled={clearDisabled}
    />
  ) : null

  if (d.widget === 'tags') {
    return (
      <CampoTags
        d={d}
        field={field}
        limpar={limpar}
        erro={erro}
        obrigatorio={obrigatorio}
      />
    )
  }

  // Attributes common to both widget branches (textarea × text/number input):
  // controlled value, handlers and accessibility. Extracted so as not to repeat the
  // same list in each branch (the specific part — type, min/max/step, class — stays
  // per branch).
  const comum = {
    id: inputId,
    value: field.state.value as string,
    onChange: (e: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
      field.handleChange(e.target.value),
    onBlur: field.handleBlur,
    placeholder,
    'aria-label': d.label,
    'aria-invalid': erro !== null || undefined,
    'aria-required': obrigatorio || undefined,
    'aria-describedby': erro !== null ? erroId : undefined,
  }

  const controle =
    d.widget === 'textarea' ? (
      <textarea {...comum} className={CLASSE_TEXTAREA} />
    ) : (
      <Input
        {...comum}
        type={
          d.widget === 'number'
            ? 'number'
            : d.widget === 'date'
              ? 'date'
              : 'text'
        }
        min={d.widget === 'number' ? d.min : undefined}
        max={d.widget === 'number' ? d.max : undefined}
        step={d.widget === 'number' ? d.step : undefined}
        className={d.widget === 'number' ? 'max-w-32' : undefined}
      />
    )

  return (
    <label className="flex flex-col gap-1.5 text-sm" htmlFor={inputId}>
      <RotuloDoCampo d={d} obrigatorio={obrigatorio} />
      <div className="flex items-center gap-2">
        {controle}
        {limpar}
      </div>
      <TextoDoErro id={erroId} erro={erro} />
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
  obrigatorio,
}: {
  d: FieldDescriptor
  field: AnyFieldApi
  limpar: ReactNode
  erro: string | null
  obrigatorio?: boolean
}) {
  const [tagInput, setTagInput] = useState('')
  const t = useT()
  const tags = (field.state.value as string[]) ?? []
  const erroId = `campo-${d.name}-erro`

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
      <RotuloDoCampo d={d} obrigatorio={obrigatorio} />
      <div className="flex flex-wrap items-center gap-1.5">
        {tags.map((tag) => (
          <Badge key={tag} variant="outline" className="gap-1 pr-1">
            {tag}
            <button
              type="button"
              aria-label={t('entity.edit.tags.remove', { tag })}
              onClick={() =>
                field.handleChange(tags.filter((outra) => outra !== tag))
              }
              className="rounded-full text-muted-foreground hover:text-foreground"
            >
              <X className="size-3" aria-hidden />
            </button>
          </Badge>
        ))}
        {tags.length === 0 ? (
          <span className="text-xs text-muted-foreground">
            {t('entity.edit.tags.empty')}
          </span>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={tagInput}
          onChange={(e) => setTagInput(e.target.value)}
          onKeyDown={onTagKeyDown}
          placeholder={t('entity.edit.tags.placeholder')}
          aria-label={t('entity.edit.tags.inputLabel')}
          aria-describedby={erro !== null ? erroId : undefined}
          className="min-w-0 basis-full sm:basis-auto sm:flex-1"
        />
        <Button type="button" variant="outline" size="sm" onClick={adicionar}>
          <Plus aria-hidden />
          {t('common.action.add')}
        </Button>
        {limpar}
      </div>
      <TextoDoErro id={erroId} erro={erro} />
    </div>
  )
}

// RotuloDoCampo is the label of a descriptor plus the required asterisk: the `*` is
// `aria-hidden` because the meaning travels on `aria-required` of the control (the
// screen reader announces the requirement once, not twice).
function RotuloDoCampo({
  d,
  obrigatorio,
}: {
  d: FieldDescriptor
  obrigatorio?: boolean
}) {
  return (
    <span className="font-medium">
      {d.label}
      {obrigatorio ? (
        <span aria-hidden className="text-destructive">
          {' '}
          *
        </span>
      ) : null}
    </span>
  )
}

// TextoDoErro is the inline error of the field, linked by `aria-describedby` to the
// control (`id`) and announced as an alert when it appears.
function TextoDoErro({ id, erro }: { id: string; erro: string | null }) {
  if (erro === null) {
    return null
  }
  return (
    <span id={id} role="alert" className="text-xs text-destructive">
      {erro}
    </span>
  )
}

// mensagemDoErro extracts the localized `message` of the Standard Schema (Zod) issue. The
// item may be the issue object or, defensively, a string.
function mensagemDoErro(issue: unknown, t: Translate): string {
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
  return t('entity.edit.invalidValue')
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
  const t = useT()
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
      {t('entity.edit.clear')}
    </Button>
  )
}
