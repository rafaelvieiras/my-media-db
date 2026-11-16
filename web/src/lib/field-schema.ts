import { z } from 'zod'
import type {
  FieldDescriptor,
  OverrideBlob,
  OverridePatch,
  ResolvedEntity,
} from '@/lib/api'

// PURE core (no JSX) of the schema-driven form:
// it turns the descriptors served by the backend into (a) initial values, (b)
// a Zod schema for client-side validation and (c) the edit patch. A new field
// in the backend starts flowing through here without touching any component. Testable
// in isolation (field-schema.test.ts).

// Value of a field IN THE FORM: `string[]` for the tags widget; `string`
// for all the others (including `number`, which stores the text of the input to
// allow the empty state = "inherit/set empty"). The conversion into number/null
// only happens when building the patch.
export type ValorCampo = string | string[]
export type ValoresForm = Record<string, ValorCampo>

// `title` is the ONLY editorial field that the backend requires on creation — it is not a
// "list of fields" for rendering (the ratchet targets the JSX enumerations),
// it is the only requiredness rule. The server's 422 stays authoritative.
const OBRIGATORIO_NA_CRIACAO = new Set(['title'])

// ordenados returns the descriptors by increasing `order` (the backend already sorts,
// but we reinforce it so we do not depend on the arrival order).
export function ordenados(fields: FieldDescriptor[]): FieldDescriptor[] {
  return [...fields].sort((a, b) => a.order - b.order)
}

// criaveis cuts out the subset that appears when CREATING an item (flat body, without
// per-language operations), already ordered.
export function criaveis(fields: FieldDescriptor[]): FieldDescriptor[] {
  return ordenados(fields).filter((f) => f.creatable)
}

// ── Seeds (initial values) ───────────────────────────────────────────────────

// semearTraduzivel: the override of the current language when it is a string; otherwise empty (the
// placeholder shows the inherited/resolved value).
function semearTraduzivel(
  blob: OverrideBlob,
  campo: string,
  lang: string,
): string {
  const valor = blob[campo]
  if (valor && typeof valor === 'object' && !Array.isArray(valor)) {
    const doIdioma = (valor as Record<string, unknown>)[lang]
    return typeof doIdioma === 'string' ? doIdioma : ''
  }
  return ''
}

// semearNeutro: the stored scalar when it is a string; otherwise empty.
function semearNeutro(blob: OverrideBlob, campo: string): string {
  const valor = blob[campo]
  return typeof valor === 'string' ? valor : ''
}

// semearNumero: the stored number as text for the input; otherwise empty.
function semearNumero(blob: OverrideBlob, campo: string): string {
  const valor = blob[campo]
  return typeof valor === 'number' ? String(valor) : ''
}

// valorInicialEdicao returns the seed of ONE descriptor in the edit context,
// driven by `widget`/`translatable`: tags come from the resolved value (for the diff
// {add,remove}); number from the blob; translatable from the current language; neutral from the scalar.
function valorInicialEdicao(
  d: FieldDescriptor,
  blob: OverrideBlob,
  lang: string,
  entity: ResolvedEntity,
): ValorCampo {
  if (d.widget === 'tags') {
    return entity.tags
  }
  if (d.widget === 'number') {
    return semearNumero(blob, d.name)
  }
  return d.translatable
    ? semearTraduzivel(blob, d.name, lang)
    : semearNeutro(blob, d.name)
}

// valoresDeEdicao builds the seeds of ALL fields (the edit form shows the
// full set). Iterating the descriptors is what makes "new field ⇒ input"
// automatic.
export function valoresDeEdicao(
  fields: FieldDescriptor[],
  blob: OverrideBlob,
  lang: string,
  entity: ResolvedEntity,
): ValoresForm {
  const out: ValoresForm = {}
  for (const d of ordenados(fields)) {
    out[d.name] = valorInicialEdicao(d, blob, lang, entity)
  }
  return out
}

// valoresDeCriacao builds the seeds of the CREATION form: only the `creatable` ones, all
// empty (empty tags if a list field ever becomes creatable).
export function valoresDeCriacao(fields: FieldDescriptor[]): ValoresForm {
  const out: ValoresForm = {}
  for (const d of criaveis(fields)) {
    out[d.name] = d.widget === 'tags' ? [] : ''
  }
  return out
}

// ── Zod schema ───────────────────────────────────────────────────────────────

// regraCampo derives the Zod rule of ONE descriptor. `obrigatorio` only applies on
// creation (editing allows empty = set empty). The number validates the min/max
// range of the descriptor itself (it mirrors the backend's `formaNumero`) over the
// text of the input, accepting empty.
function regraCampo(d: FieldDescriptor, obrigatorio: boolean): z.ZodType {
  if (d.widget === 'tags') {
    return z.array(z.string())
  }
  if (d.widget === 'number') {
    const min = d.min ?? 0
    const max = d.max ?? 10
    return z.string().refine(
      (v) => {
        const t = v.trim()
        if (t === '') {
          return true
        }
        const n = Number(t)
        return Number.isFinite(n) && n >= min && n <= max
      },
      { message: `Informe um número entre ${min} e ${max}.` },
    )
  }
  // text | textarea | date
  if (obrigatorio) {
    return z.string().trim().min(1, `${d.label} é obrigatório`)
  }
  return z.string()
}

// schemaZod builds the `z.object` from the descriptors. `contexto` cuts out the
// set (creation uses only `creatable`) and turns on requiredness (only on creation).
export function schemaZod(
  fields: FieldDescriptor[],
  contexto: 'criacao' | 'edicao',
): z.ZodType<ValoresForm> {
  const lista = contexto === 'criacao' ? criaveis(fields) : ordenados(fields)
  const shape: Record<string, z.ZodType> = {}
  for (const d of lista) {
    const obrigatorio =
      contexto === 'criacao' && OBRIGATORIO_NA_CRIACAO.has(d.name)
    shape[d.name] = regraCampo(d, obrigatorio)
  }
  // The output of z.object is assignable to `ValoresForm` (every value is a string or
  // string[]); the cast narrows the type for the generic consumer (SchemaForm).
  return z.object(shape) as unknown as z.ZodType<ValoresForm>
}

// ── Edit patch ───────────────────────────────────────────────────────────────

// construirPatchEdicao compares the current values with the seeds and builds the sparse
// patch (only what CHANGED), iterating the descriptors:
//   - translatable/neutral scalar/date: empty → `null` (SET EMPTY, ≠ "Limpar");
//   - number: empty → `null`; otherwise the `Number`;
//   - tags: `{add, remove}` operations relative to the value resolved when opening.
// "Limpar" (go back to inheriting) is a separate DELETE, outside the patch.
export function construirPatchEdicao(
  fields: FieldDescriptor[],
  valores: ValoresForm,
  sementes: ValoresForm,
): OverridePatch {
  const patch: Record<string, unknown> = {}
  for (const d of ordenados(fields)) {
    const atual = valores[d.name]
    const semente = sementes[d.name]
    if (d.widget === 'tags') {
      const tags = (atual as string[]) ?? []
      const base = (semente as string[]) ?? []
      const add = tags.filter((t) => !base.includes(t))
      const remove = base.filter((t) => !tags.includes(t))
      if (add.length > 0 || remove.length > 0) {
        patch.tags = { add, remove }
      }
      continue
    }
    const texto = (atual as string) ?? ''
    if (texto === (semente as string)) {
      continue
    }
    if (d.widget === 'number') {
      const t = texto.trim()
      patch[d.name] = t === '' ? null : Number(t)
    } else {
      patch[d.name] = texto.trim() === '' ? null : texto
    }
  }
  return patch
}
