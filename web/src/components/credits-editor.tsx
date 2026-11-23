import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  Building2,
  Eraser,
  Plus,
  Trash2,
  Users,
  Wrench,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  ApiError,
  clearOverrideField,
  patchOverride,
  type CreditCast,
  type CreditCompany,
  type CreditCrew,
  type OverrideBlob,
  type OverridePatch,
  type ResolvedEntity,
} from '@/lib/api'
import { useT } from '@/lib/i18n'

// CreditsEditor edits CAST/CREW/PRODUCTION COMPANIES: outside the
// generic SchemaForm (the `credits` key was excluded from `descritoresDeCampo()`
// on purpose — it is not a simple form field), following the SAME pattern as
// ExternalIdsEditor (a section of its own, its own state/mutation/invalidation).
//
// Read: the RESOLVED list (`entity.cast/crew/companies`) already reflects any
// existing override — it is the base of the editable local state.
// Write: `PATCH /override` with the `credits` key (a NEUTRAL field, it ignores `lang`),
// segregated by kind (`cast`/`crew`/`companies`), each block with the operations
// `reorder`/`remove`/`edit`/`add`. Since the PATCH replaces the whole block of the kind
// (it does not merge operation by operation), we read the ALREADY STORED override (`blob`) to
// merge `remove`/`edit`/`add` from previous sessions underneath what changed now
// — that is the only way a new edit does not erase an old edit of the same kind.
// "Limpar todos os créditos" is `DELETE /override/credits` (it inherits from the source again).

// Business key of the credit (the SAME convention as the backend/provider): `source_id`
// for cast/companies; `source_id + "|" + job` for crew (a member may have two
// roles with the same `source_id`).
function chaveCast(sourceId: string): string {
  return sourceId
}
function chaveCrew(sourceId: string, job: string): string {
  return `${sourceId}|${job}`
}

interface LinhaBase {
  // React-only identity (never sent to the API).
  id: string
  // Business key in the source/override already stored; `null` = new credit of this
  // editing session (it does not exist on the server side yet).
  chaveOriginal: string | null
}

interface LinhaCast extends LinhaBase {
  source_id: string
  name: string
  character: string
  profile_url: string | null
}

interface LinhaCrew extends LinhaBase {
  source_id: string
  name: string
  job: string
  department: string
  profile_url: string | null
}

interface LinhaCompany extends LinhaBase {
  source_id: string
  name: string
  logo_url: string | null
}

function linhaDeCast(c: CreditCast): LinhaCast {
  const chave = chaveCast(c.source_id)
  return {
    id: chave,
    chaveOriginal: chave,
    source_id: c.source_id,
    name: c.name,
    character: c.character,
    profile_url: c.profile_url ?? null,
  }
}

function linhaDeCrew(c: CreditCrew): LinhaCrew {
  const chave = chaveCrew(c.source_id, c.job)
  return {
    id: chave,
    chaveOriginal: chave,
    source_id: c.source_id,
    name: c.name,
    job: c.job,
    department: c.department,
    profile_url: c.profile_url ?? null,
  }
}

function linhaDeCompany(c: CreditCompany): LinhaCompany {
  const chave = chaveCast(c.source_id)
  return {
    id: chave,
    chaveOriginal: chave,
    source_id: c.source_id,
    name: c.name,
    logo_url: c.logo_url ?? null,
  }
}

// Shape of a block per kind inside the `credits` patch (see OverridePatch in
// openapi.yaml). Typed here locally — the schema is `additionalProperties: true`
// (free shape), so the generated types do not describe it.
interface BlocoPatch {
  reorder?: string[]
  remove?: string[]
  edit?: Record<string, Record<string, string>>
  add?: Record<string, string>[]
}

interface CreditsPatch {
  cast?: BlocoPatch
  crew?: BlocoPatch
  companies?: BlocoPatch
}

function uniq(valores: string[]): string[] {
  return [...new Set(valores)]
}

interface ConfigDiff<L extends LinhaBase & { source_id: string }> {
  // Current editable fields of the row, as text (only the ones compared in the diff).
  camposEditaveis: (linha: L) => Record<string, string>
  // Business key computed from the CURRENT fields (new rows and the
  // `reorder`, which references new credits by the key they define themselves).
  chaveAtual: (linha: L) => string
  // Business key of an `add` item already stored (read from the blob), to know whether
  // it survives a `remove` of this session.
  chaveDoAdd: (item: Record<string, string>) => string
  // Payload of `add` for a new row.
  montarAdd: (linha: L) => Record<string, string>
}

// Result of comparing the initial state (resolved on open) with the current
// one (edited on screen) — the base of both `montarBlocoKind` (it builds the patch) and
// of the "dirty" state (it only needs `mudou`), so as not to duplicate the
// field-by-field comparison between the two needs.
interface DiffKind {
  removidosAgora: string[]
  editadosAgora: Record<string, Record<string, string>>
  adicionadosAgora: Record<string, string>[]
  chavesFinais: string[]
  mudou: boolean
}

function diffKind<L extends LinhaBase & { source_id: string }>(
  inicial: L[],
  atual: L[],
  cfg: ConfigDiff<L>,
): DiffKind {
  const porChaveInicial = new Map(
    inicial.map((l) => [l.chaveOriginal as string, l]),
  )
  const chavesPresentes = new Set(
    atual
      .filter((l) => l.chaveOriginal !== null)
      .map((l) => l.chaveOriginal as string),
  )
  const removidosAgora = [...porChaveInicial.keys()].filter(
    (k) => !chavesPresentes.has(k),
  )

  const editadosAgora: Record<string, Record<string, string>> = {}
  for (const linha of atual) {
    if (linha.chaveOriginal === null) continue
    const original = porChaveInicial.get(linha.chaveOriginal)
    if (!original) continue
    const camposAtuais = cfg.camposEditaveis(linha)
    const camposOriginais = cfg.camposEditaveis(original)
    const mudouCampos: Record<string, string> = {}
    for (const campo of Object.keys(camposAtuais)) {
      if (camposAtuais[campo] !== camposOriginais[campo]) {
        mudouCampos[campo] = camposAtuais[campo]
      }
    }
    if (Object.keys(mudouCampos).length > 0) {
      editadosAgora[linha.chaveOriginal] = mudouCampos
    }
  }

  const adicionadosAgora = atual
    .filter((l) => l.chaveOriginal === null)
    .map((l) => cfg.montarAdd(l))

  const chavesIniciais = inicial.map((l) => l.chaveOriginal as string)
  const chavesFinais = atual.map((l) => l.chaveOriginal ?? cfg.chaveAtual(l))
  const ordemMudou =
    JSON.stringify(chavesIniciais) !== JSON.stringify(chavesFinais)

  const mudou =
    removidosAgora.length > 0 ||
    Object.keys(editadosAgora).length > 0 ||
    adicionadosAgora.length > 0 ||
    ordemMudou

  return {
    removidosAgora,
    editadosAgora,
    adicionadosAgora,
    chavesFinais,
    mudou,
  }
}

// algoMudouKind is the "dirty" state of a kind: `true` if there is a pending reorder/edit/
// remove/add since the initial state.
function algoMudouKind<L extends LinhaBase & { source_id: string }>(
  inicial: L[],
  atual: L[],
  cfg: ConfigDiff<L>,
): boolean {
  return diffKind(inicial, atual, cfg).mudou
}

// montarBlocoKind builds the patch block of the kind from the diff, already merged
// with what was already stored (`anterior`, from the blob) — previous remove/edit/add
// that this session did not touch go forward; the `reorder` is always the
// complete final order (it needs no merging: it already reflects the inherited order plus
// what the owner changed now). `undefined` = nothing changed in that kind — the key
// stays out of the patch (it inherits).
function montarBlocoKind<L extends LinhaBase & { source_id: string }>(
  inicial: L[],
  atual: L[],
  anterior: BlocoPatch | null | undefined,
  cfg: ConfigDiff<L>,
): BlocoPatch | undefined {
  const diff = diffKind(inicial, atual, cfg)
  if (!diff.mudou) {
    return undefined
  }

  const removeFinal = uniq([
    ...(anterior?.remove ?? []),
    ...diff.removidosAgora,
  ])
  const chavesRemovidas = new Set(removeFinal)
  const addAnteriorVivo = (anterior?.add ?? []).filter(
    (item) => !chavesRemovidas.has(cfg.chaveDoAdd(item)),
  )
  const addFinal = [...addAnteriorVivo, ...diff.adicionadosAgora]
  const editFinal = { ...(anterior?.edit ?? {}), ...diff.editadosAgora }

  const bloco: BlocoPatch = { reorder: diff.chavesFinais }
  if (removeFinal.length > 0) bloco.remove = removeFinal
  if (Object.keys(editFinal).length > 0) bloco.edit = editFinal
  if (addFinal.length > 0) bloco.add = addFinal
  return bloco
}

const cfgCast: ConfigDiff<LinhaCast> = {
  camposEditaveis: (l) => ({ character: l.character }),
  chaveAtual: (l) => chaveCast(l.source_id),
  chaveDoAdd: (item) => chaveCast(item.source_id),
  montarAdd: (l) => ({
    source_id: l.source_id,
    name: l.name,
    character: l.character,
  }),
}

const cfgCrew: ConfigDiff<LinhaCrew> = {
  camposEditaveis: (l) => ({ job: l.job, department: l.department }),
  chaveAtual: (l) => chaveCrew(l.source_id, l.job),
  chaveDoAdd: (item) => chaveCrew(item.source_id, item.job ?? ''),
  montarAdd: (l) => ({
    source_id: l.source_id,
    name: l.name,
    job: l.job,
    department: l.department,
  }),
}

const cfgCompany: ConfigDiff<LinhaCompany> = {
  camposEditaveis: (l) => ({ name: l.name }),
  chaveAtual: (l) => chaveCast(l.source_id),
  chaveDoAdd: (item) => chaveCast(item.source_id),
  montarAdd: (l) => ({ source_id: l.source_id, name: l.name }),
}

function novoSourceId(): string {
  return `custom-${crypto.randomUUID()}`
}

function moverItem<T>(lista: T[], indice: number, direcao: -1 | 1): T[] {
  const destino = indice + direcao
  if (destino < 0 || destino >= lista.length) return lista
  const copia = [...lista]
  const [item] = copia.splice(indice, 1)
  copia.splice(destino, 0, item)
  return copia
}

// moverParaIndice reorders in BATCH: it removes the item from `indice` and
// reinserts it at `destino` (both 0-based), preserving the relative order of the others —
// the same mechanics as `moverItem`, generalized to any distance (not just one
// step). A `destino` outside the bounds is clamped, so "move to 1" or a number
// greater than the total always lands on a valid position.
function moverParaIndice<T>(lista: T[], indice: number, destino: number): T[] {
  const destinoValido = Math.max(0, Math.min(lista.length - 1, destino))
  if (destinoValido === indice) return lista
  const copia = [...lista]
  const [item] = copia.splice(indice, 1)
  copia.splice(destinoValido, 0, item)
  return copia
}

export function CreditsEditor({
  id,
  lang,
  entity,
  blob,
  onDirtyChange,
}: {
  id: string
  lang: string
  entity: Pick<ResolvedEntity, 'cast' | 'crew' | 'companies'>
  blob: OverrideBlob
  // "Leave without saving" guard: called only on the transition of the dirty state.
  onDirtyChange?: (sujo: boolean) => void
}) {
  const qc = useQueryClient()
  const t = useT()

  const inicialCast = useMemo(
    () => (entity.cast ?? []).map(linhaDeCast),
    [entity],
  )
  const inicialCrew = useMemo(
    () => (entity.crew ?? []).map(linhaDeCrew),
    [entity],
  )
  const inicialCompanies = useMemo(
    () => (entity.companies ?? []).map(linhaDeCompany),
    [entity],
  )

  const [cast, setCast] = useState<LinhaCast[]>(inicialCast)
  const [crew, setCrew] = useState<LinhaCrew[]>(inicialCrew)
  const [companies, setCompanies] = useState<LinhaCompany[]>(inicialCompanies)

  const [novoCastNome, setNovoCastNome] = useState('')
  const [novoCastPersonagem, setNovoCastPersonagem] = useState('')
  const [novoCrewNome, setNovoCrewNome] = useState('')
  const [novoCrewDepartamento, setNovoCrewDepartamento] = useState('')
  const [novoCrewCargo, setNovoCrewCargo] = useState('')
  const [novoCompanyNome, setNovoCompanyNome] = useState('')
  // "Clear all credits" is a DELETE of the whole block: it asks for confirmation
  // (it was a single tap 8 px away from "Save credits").
  const [confirmarLimpeza, setConfirmarLimpeza] = useState(false)

  // Dirty: the same rule used to build the patch of each kind — any
  // pending reorder/edit/removal/addition in cast, crew or production companies.
  const sujo = useMemo(
    () =>
      algoMudouKind(inicialCast, cast, cfgCast) ||
      algoMudouKind(inicialCrew, crew, cfgCrew) ||
      algoMudouKind(inicialCompanies, companies, cfgCompany),
    [inicialCast, cast, inicialCrew, crew, inicialCompanies, companies],
  )

  useEffect(() => {
    onDirtyChange?.(sujo)
  }, [sujo, onDirtyChange])

  function invalidar() {
    qc.invalidateQueries({ queryKey: ['entity', id] })
    qc.invalidateQueries({ queryKey: ['entities'] })
  }

  const anteriorCredits = (blob.credits ?? null) as CreditsPatch | null

  const salvar = useMutation({
    mutationFn: (patch: CreditsPatch) =>
      patchOverride(id, lang, { credits: patch } as OverridePatch),
    onSuccess: invalidar,
  })

  const limpar = useMutation({
    mutationFn: () => clearOverrideField(id, 'credits'),
    onSuccess: invalidar,
  })

  const ocupado = salvar.isPending || limpar.isPending

  function salvarClique() {
    const patch: CreditsPatch = {}
    const blocoCast = montarBlocoKind(
      inicialCast,
      cast,
      anteriorCredits?.cast,
      cfgCast,
    )
    const blocoCrew = montarBlocoKind(
      inicialCrew,
      crew,
      anteriorCredits?.crew,
      cfgCrew,
    )
    const blocoCompanies = montarBlocoKind(
      inicialCompanies,
      companies,
      anteriorCredits?.companies,
      cfgCompany,
    )
    if (blocoCast) patch.cast = blocoCast
    if (blocoCrew) patch.crew = blocoCrew
    if (blocoCompanies) patch.companies = blocoCompanies
    if (Object.keys(patch).length === 0) return
    salvar.mutate(patch)
  }

  const mensagemErro =
    salvar.error instanceof ApiError
      ? salvar.error.message
      : limpar.error instanceof ApiError
        ? limpar.error.message
        : salvar.isError || limpar.isError
          ? t('entity.edit.credits.saveError')
          : null

  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="flex items-center gap-2 font-heading text-lg font-medium">
          <Users className="size-4" aria-hidden />
          {t('entity.edit.credits.title')}
        </h2>
        <p className="text-sm text-muted-foreground">
          {t('entity.edit.credits.description')}
        </p>
      </div>

      <SecaoCreditos
        titulo={t('entity.section.cast')}
        icone={<Users className="size-4" aria-hidden />}
        linhas={cast}
        setLinhas={setCast}
        vazioTexto={t('entity.edit.credits.emptyCast')}
        mostrarNomeFixo
        removerAriaLabel={(l) =>
          t('entity.edit.credits.removeCast', { name: l.name })
        }
        camposEditaveis={[
          {
            label: t('entity.edit.credits.character'),
            valor: (l) => l.character,
            aoMudar: (l, v) => ({ ...l, character: v }),
            placeholder: t('entity.edit.credits.character'),
            ariaLabel: (l) =>
              t('entity.edit.credits.characterOf', { name: l.name }),
            className: 'sm:w-44',
          },
        ]}
      />
      <SecaoCreditos
        titulo={t('entity.section.crew')}
        icone={<Wrench className="size-4" aria-hidden />}
        linhas={crew}
        setLinhas={setCrew}
        vazioTexto={t('entity.edit.credits.emptyCrew')}
        mostrarNomeFixo
        removerAriaLabel={(l) =>
          t('entity.edit.credits.removeCrew', { name: l.name })
        }
        camposEditaveis={[
          {
            label: t('entity.edit.credits.department'),
            valor: (l) => l.department,
            aoMudar: (l, v) => ({ ...l, department: v }),
            placeholder: t('entity.edit.credits.department'),
            ariaLabel: (l) =>
              t('entity.edit.credits.departmentOf', { name: l.name }),
            className: 'sm:w-36',
          },
          {
            label: t('entity.edit.credits.job'),
            valor: (l) => l.job,
            aoMudar: (l, v) => ({ ...l, job: v }),
            placeholder: t('entity.edit.credits.job'),
            ariaLabel: (l) => t('entity.edit.credits.jobOf', { name: l.name }),
            className: 'sm:w-36',
          },
        ]}
      />
      <SecaoCreditos
        titulo={t('entity.section.companies')}
        icone={<Building2 className="size-4" aria-hidden />}
        linhas={companies}
        setLinhas={setCompanies}
        vazioTexto={t('entity.edit.credits.emptyCompanies')}
        mostrarNomeFixo={false}
        removerAriaLabel={(l) =>
          t('entity.edit.credits.removeCompany', { name: l.name })
        }
        camposEditaveis={[
          {
            label: t('entity.edit.credits.companyName'),
            valor: (l) => l.name,
            aoMudar: (l, v) => ({ ...l, name: v }),
            placeholder: t('entity.edit.credits.companyName'),
            ariaLabel: () => t('entity.edit.credits.companyName'),
            className: 'sm:w-56',
          },
        ]}
      />

      <div className="flex flex-col gap-2 border-t border-border pt-3">
        <FormularioAdicionar
          titulo={t('entity.edit.credits.addCastTitle')}
          textoBotao={t('entity.edit.credits.addCast')}
          podeAdicionar={!!novoCastNome.trim()}
          campos={[
            {
              label: t('common.label.name'),
              valor: novoCastNome,
              aoMudar: setNovoCastNome,
              placeholder: t('common.label.name'),
              ariaLabel: t('entity.edit.credits.newCastName'),
            },
            {
              label: t('entity.edit.credits.character'),
              valor: novoCastPersonagem,
              aoMudar: setNovoCastPersonagem,
              placeholder: t('entity.edit.credits.character'),
              ariaLabel: t('entity.edit.credits.newCastCharacter'),
            },
          ]}
          aoAdicionar={() => {
            const sourceId = novoSourceId()
            setCast((atual) => [
              ...atual,
              {
                id: sourceId,
                chaveOriginal: null,
                source_id: sourceId,
                name: novoCastNome.trim(),
                character: novoCastPersonagem.trim(),
                profile_url: null,
              },
            ])
            setNovoCastNome('')
            setNovoCastPersonagem('')
          }}
        />

        <FormularioAdicionar
          titulo={t('entity.edit.credits.addCrewTitle')}
          textoBotao={t('entity.edit.credits.addCrew')}
          podeAdicionar={!!novoCrewNome.trim()}
          campos={[
            {
              label: t('common.label.name'),
              valor: novoCrewNome,
              aoMudar: setNovoCrewNome,
              placeholder: t('common.label.name'),
              ariaLabel: t('entity.edit.credits.newCrewName'),
            },
            {
              label: t('entity.edit.credits.department'),
              valor: novoCrewDepartamento,
              aoMudar: setNovoCrewDepartamento,
              placeholder: t('entity.edit.credits.department'),
              ariaLabel: t('entity.edit.credits.newCrewDepartment'),
              className: 'sm:w-40',
            },
            {
              label: t('entity.edit.credits.job'),
              valor: novoCrewCargo,
              aoMudar: setNovoCrewCargo,
              placeholder: t('entity.edit.credits.job'),
              ariaLabel: t('entity.edit.credits.newCrewJob'),
              className: 'sm:w-40',
            },
          ]}
          aoAdicionar={() => {
            const sourceId = novoSourceId()
            setCrew((atual) => [
              ...atual,
              {
                id: sourceId,
                chaveOriginal: null,
                source_id: sourceId,
                name: novoCrewNome.trim(),
                department: novoCrewDepartamento.trim(),
                job: novoCrewCargo.trim(),
                profile_url: null,
              },
            ])
            setNovoCrewNome('')
            setNovoCrewDepartamento('')
            setNovoCrewCargo('')
          }}
        />

        <FormularioAdicionar
          titulo={t('entity.edit.credits.addCompany')}
          textoBotao={t('entity.edit.credits.addCompany')}
          podeAdicionar={!!novoCompanyNome.trim()}
          campos={[
            {
              label: t('common.label.name'),
              valor: novoCompanyNome,
              aoMudar: setNovoCompanyNome,
              placeholder: t('common.label.name'),
              ariaLabel: t('entity.edit.credits.newCompanyName'),
            },
          ]}
          aoAdicionar={() => {
            const sourceId = novoSourceId()
            setCompanies((atual) => [
              ...atual,
              {
                id: sourceId,
                chaveOriginal: null,
                source_id: sourceId,
                name: novoCompanyNome.trim(),
                logo_url: null,
              },
            ])
            setNovoCompanyNome('')
          }}
        />
      </div>

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      {/* Sticky action bar: on a long cast/crew list the primary action used to
          sit 5100 px below the fold. */}
      <div className="sticky bottom-0 z-10 -mx-1 flex flex-wrap items-center gap-2 rounded-lg border-t border-border bg-card/95 px-1 py-2 backdrop-blur supports-[backdrop-filter]:bg-card/80">
        <Button
          type="button"
          variant="outline"
          size="sm"
          // Without any unsaved change there is nothing to inherit again.
          disabled={ocupado || !sujo}
          onClick={() => setConfirmarLimpeza(true)}
        >
          <Eraser aria-hidden />
          {t('entity.edit.credits.clearAll')}
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={ocupado}
          onClick={salvarClique}
        >
          {salvar.isPending
            ? t('common.action.saving')
            : t('entity.edit.credits.save')}
        </Button>
      </div>

      <AlertDialog open={confirmarLimpeza} onOpenChange={setConfirmarLimpeza}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('entity.edit.credits.clearAllTitle')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('entity.edit.credits.clearAllDescription')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.action.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmarLimpeza(false)
                limpar.mutate()
              }}
            >
              {t('entity.edit.credits.clearAllConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}

function BotoesOrdem({
  indice,
  total,
  mover,
}: {
  indice: number
  total: number
  mover: (direcao: -1 | 1) => void
}) {
  const t = useT()
  return (
    <div className="flex flex-col">
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={t('entity.edit.credits.moveUp')}
        disabled={indice === 0}
        onClick={() => mover(-1)}
      >
        <ArrowUp className="size-3" aria-hidden />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={t('entity.edit.credits.moveDown')}
        disabled={indice === total - 1}
        onClick={() => mover(1)}
      >
        <ArrowDown className="size-3" aria-hidden />
      </Button>
    </div>
  )
}

// CampoPosicao is the BATCH reordering: a numeric "move
// to position N" field, next to the one-step arrows — moving from 50th to 1st requires a
// number + Enter, not 49 clicks. It keeps the editing text local (not controlled
// by the index on every keystroke) so the owner can erase/type freely; it only
// applies the reordering on Enter or when losing focus, and it always goes back to reflecting
// the current position (1-based) afterwards — including when the typed value is
// invalid or out of bounds (`moverParaIndice` clamps it).
function CampoPosicao({
  indice,
  total,
  rotulo,
  mover,
}: {
  indice: number
  total: number
  rotulo: string
  mover: (destino: number) => void
}) {
  const [texto, setTexto] = useState(String(indice + 1))
  // It resets the text during rendering (React pattern for "adjust state when
  // a prop changes", without useEffect) when the index changed from the outside — reordering
  // through the arrows, through another row, or through the reordering that this field
  // just fired.
  const [indiceRefletido, setIndiceRefletido] = useState(indice)
  if (indice !== indiceRefletido) {
    setIndiceRefletido(indice)
    setTexto(String(indice + 1))
  }

  function aplicar() {
    const posicao = Number.parseInt(texto, 10)
    if (Number.isNaN(posicao)) {
      setTexto(String(indice + 1))
      return
    }
    mover(posicao - 1)
  }

  return (
    <Input
      type="number"
      min={1}
      max={total}
      value={texto}
      onChange={(e) => setTexto(e.target.value)}
      onBlur={aplicar}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault()
          aplicar()
        }
      }}
      aria-label={rotulo}
      className="w-16 shrink-0 text-center"
    />
  )
}

// CampoEditavel describes ONE editable input of a credit row (used by
// SecaoCreditos, generic across cast/crew/production companies): `character` (cast),
// `department`+`job` (crew) or `name` (companies, which has no "fixed name").
// `label` is VISIBLE above the input (a placeholder disappears as soon as the owner
// types, and the screen reader only had the aria-label).
interface CampoEditavel<L> {
  label: string
  valor: (l: L) => string
  aoMudar: (l: L, v: string) => L
  placeholder: string
  ariaLabel: (l: L) => string
  className?: string
}

// SecaoCreditos renders ONE section (Elenco/Equipe/Produtoras): reorder
// (↑/↓), the editable fields of the kind and remove — generic enough for the
// 3 sections not to duplicate the same list structure. `mostrarNomeFixo` shows the
// name as a fixed label (cast/crew, whose `name` is not editable here) — for
// companies, the `name` itself is the only editable field, so it stays `false`.
function SecaoCreditos<
  L extends LinhaBase & { source_id: string; name: string },
>({
  titulo,
  icone,
  linhas,
  setLinhas,
  vazioTexto,
  mostrarNomeFixo,
  camposEditaveis,
  removerAriaLabel,
}: {
  titulo: string
  icone: ReactNode
  linhas: L[]
  setLinhas: (fn: (atual: L[]) => L[]) => void
  vazioTexto: string
  mostrarNomeFixo: boolean
  camposEditaveis: CampoEditavel<L>[]
  removerAriaLabel: (l: L) => string
}) {
  const t = useT()
  return (
    <section className="flex flex-col gap-2">
      <h3 className="flex items-center gap-2 text-sm font-medium">
        {icone}
        {titulo}
      </h3>
      {linhas.length === 0 ? (
        <p className="text-sm text-muted-foreground">{vazioTexto}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {linhas.map((l, i) => (
            // The row WRAPS on mobile: character/department/job used to collapse
            // into 35–78 px slivers squeezed next to the arrows and the name.
            <li
              key={l.id}
              className="flex flex-wrap items-end gap-2 sm:flex-nowrap"
            >
              <BotoesOrdem
                indice={i}
                total={linhas.length}
                mover={(d) => setLinhas((atual) => moverItem(atual, i, d))}
              />
              <label className="flex w-16 shrink-0 flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">
                  {t('entity.edit.credits.position')}
                </span>
                <CampoPosicao
                  indice={i}
                  total={linhas.length}
                  rotulo={t('entity.edit.credits.moveToPosition', {
                    name: l.name,
                    total: linhas.length,
                  })}
                  mover={(destino) =>
                    setLinhas((atual) => moverParaIndice(atual, i, destino))
                  }
                />
              </label>
              {mostrarNomeFixo ? (
                <span className="w-32 shrink-0 truncate text-sm" title={l.name}>
                  {l.name}
                </span>
              ) : null}
              {camposEditaveis.map((campo) => (
                <label
                  key={campo.label}
                  className="flex min-w-0 basis-full flex-col gap-0.5 sm:basis-auto"
                >
                  <span className="text-xs text-muted-foreground">
                    {campo.label}
                  </span>
                  <Input
                    value={campo.valor(l)}
                    onChange={(e) =>
                      setLinhas((atual) =>
                        atual.map((x) =>
                          x.id === l.id ? campo.aoMudar(x, e.target.value) : x,
                        ),
                      )
                    }
                    placeholder={campo.placeholder}
                    aria-label={campo.ariaLabel(l)}
                    className={campo.className}
                  />
                </label>
              ))}
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                className="sm:ml-auto"
                aria-label={removerAriaLabel(l)}
                onClick={() =>
                  setLinhas((atual) => atual.filter((x) => x.id !== l.id))
                }
              >
                <Trash2 aria-hidden />
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

// CampoNovo describes ONE input of the "Adicionar X" form (generic across the 3
// sections — only the amount/label of the fields changes). `label` is visible above
// the input, like in `LinhaCampo` of the schema-form.
interface CampoNovo {
  label: string
  valor: string
  aoMudar: (v: string) => void
  placeholder: string
  ariaLabel: string
  className?: string
}

// FormularioAdicionar is the small form at the end of each section that adds
// a CUSTOM credit (without a provider) to the local list.
function FormularioAdicionar({
  titulo,
  campos,
  podeAdicionar,
  aoAdicionar,
  textoBotao,
}: {
  titulo: string
  campos: CampoNovo[]
  podeAdicionar: boolean
  aoAdicionar: () => void
  textoBotao: string
}) {
  return (
    <>
      <h3 className="text-sm font-medium">{titulo}</h3>
      <div className="flex flex-wrap items-end gap-2">
        {campos.map((c) => (
          <label
            key={c.ariaLabel}
            className="flex min-w-0 basis-full flex-col gap-0.5 sm:basis-auto"
          >
            <span className="text-xs text-muted-foreground">{c.label}</span>
            <Input
              value={c.valor}
              onChange={(e) => c.aoMudar(e.target.value)}
              placeholder={c.placeholder}
              aria-label={c.ariaLabel}
              className={c.className ?? 'sm:w-48'}
            />
          </label>
        ))}
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!podeAdicionar}
          onClick={aoAdicionar}
        >
          <Plus aria-hidden />
          {textoBotao}
        </Button>
      </div>
    </>
  )
}
