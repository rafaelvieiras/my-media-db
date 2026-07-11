import { z } from 'zod'
import type {
  FieldDescriptor,
  OverrideBlob,
  OverridePatch,
  ResolvedEntity,
} from '@/lib/api'

// Núcleo PURO (sem JSX) do formulário dirigido por schema (briefing 0030):
// transforma os descritores servidos pelo backend em (a) valores iniciais, (b)
// um schema Zod de validação de cliente e (c) o patch de edição. Um campo novo
// no backend passa a fluir por aqui sem tocar em nenhum componente. Testável
// isolado (field-schema.test.ts).

// Valor de um campo NO FORMULÁRIO: `string[]` para o widget de tags; `string`
// para todos os demais (inclusive `number`, que guarda o texto do input para
// permitir o estado vazio = "herdar/definir vazio"). A conversão para número/null
// acontece só ao montar o patch.
export type ValorCampo = string | string[]
export type ValoresForm = Record<string, ValorCampo>

// `title` é o ÚNICO campo editorial que o backend exige na criação — não é uma
// "lista de campos" de render (a catraca do 0030 mira as enumerações de JSX),
// é a única regra de obrigatoriedade. O 422 do servidor segue autoritativo.
const OBRIGATORIO_NA_CRIACAO = new Set(['title'])

// ordenados devolve os descritores por `order` crescente (o backend já ordena,
// mas reforçamos para não depender da ordem de chegada).
export function ordenados(fields: FieldDescriptor[]): FieldDescriptor[] {
  return [...fields].sort((a, b) => a.order - b.order)
}

// criaveis recorta o subconjunto que aparece ao CRIAR um item (corpo plano, sem
// operações por idioma), já ordenado.
export function criaveis(fields: FieldDescriptor[]): FieldDescriptor[] {
  return ordenados(fields).filter((f) => f.creatable)
}

// ── Sementes (valores iniciais) ──────────────────────────────────────────────

// semearTraduzivel: o override do idioma atual se for string; senão vazio (o
// placeholder mostra o herdado/resolvido).
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

// semearNeutro: o escalar guardado se for string; senão vazio.
function semearNeutro(blob: OverrideBlob, campo: string): string {
  const valor = blob[campo]
  return typeof valor === 'string' ? valor : ''
}

// semearNumero: o número guardado como texto para o input; senão vazio.
function semearNumero(blob: OverrideBlob, campo: string): string {
  const valor = blob[campo]
  return typeof valor === 'number' ? String(valor) : ''
}

// valorInicialEdicao devolve a semente de UM descritor no contexto de edição,
// dirigida por `widget`/`translatable`: tags saem do resolvido (para o diff
// {add,remove}); número do blob; traduzível do idioma corrente; neutro do escalar.
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

// valoresDeEdicao monta as sementes de TODOS os campos (o form de edição mostra o
// conjunto completo). Iterar os descritores é o que torna "campo novo ⇒ input"
// automático.
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

// valoresDeCriacao monta as sementes do form de CRIAÇÃO: só os `creatable`, todos
// vazios (tags vazia se algum dia um campo de lista virar creatable).
export function valoresDeCriacao(fields: FieldDescriptor[]): ValoresForm {
  const out: ValoresForm = {}
  for (const d of criaveis(fields)) {
    out[d.name] = d.widget === 'tags' ? [] : ''
  }
  return out
}

// ── Schema Zod ───────────────────────────────────────────────────────────────

// regraCampo deriva a regra Zod de UM descritor. `obrigatorio` só vale na
// criação (edição permite vazio = definir vazio). O número valida a faixa
// min/max do próprio descritor (espelha o `formaNumero` do backend) sobre o
// texto do input, aceitando vazio.
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

// schemaZod monta o `z.object` a partir dos descritores. `contexto` recorta o
// conjunto (criação usa só `creatable`) e liga a obrigatoriedade (só na criação).
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
  // O output do z.object é atribuível a `ValoresForm` (todo valor é string ou
  // string[]); o cast estreita o tipo para o consumidor genérico (SchemaForm).
  return z.object(shape) as unknown as z.ZodType<ValoresForm>
}

// ── Patch de edição ──────────────────────────────────────────────────────────

// construirPatchEdicao compara os valores atuais com as sementes e monta o patch
// esparso (só o que MUDOU), iterando os descritores:
//   - traduzível/neutro escalar/data: vazio → `null` (DEFINIR VAZIO, ≠ "Limpar");
//   - número: vazio → `null`; senão o `Number`;
//   - tags: operações `{add, remove}` relativas ao resolvido ao abrir.
// "Limpar" (voltar a herdar) é um DELETE separado, fora do patch.
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
