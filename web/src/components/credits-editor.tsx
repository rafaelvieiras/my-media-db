import { useMemo, useState, type ReactNode } from 'react'
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

// CreditsEditor edita ELENCO/EQUIPE/PRODUTORAS (briefing 0029 Fase C): fora do
// SchemaForm genérico (a chave `credits` foi excluída de `descritoresDeCampo()` de
// propósito — não é um campo de formulário simples), seguindo o MESMO padrão do
// ExternalIdsEditor (seção própria, seu próprio estado/mutação/invalidação).
//
// Leitura: a lista RESOLVIDA (`entity.cast/crew/companies`) já reflete qualquer
// override existente — é a base do estado local editável.
// Escrita: `PATCH /override` com a chave `credits` (campo NEUTRO, ignora `lang`),
// segregada por kind (`cast`/`crew`/`companies`), cada bloco com as operações
// `reorder`/`remove`/`edit`/`add`. Como o PATCH substitui o bloco do kind inteiro
// (não funde operação a operação), lemos o override JÁ GRAVADO (`blob`) para
// mesclar `remove`/`edit`/`add` de sessões anteriores por baixo do que mudou agora
// — só assim uma edição nova não apaga uma edição antiga do mesmo kind.
// "Limpar todos os créditos" é `DELETE /override/credits` (volta a herdar da fonte).

// Chave de negócio do crédito (a MESMA convenção do backend/provider): `source_id`
// para cast/companies; `source_id + "|" + job` para crew (um membro pode ter dois
// cargos com o mesmo `source_id`).
function chaveCast(sourceId: string): string {
  return sourceId
}
function chaveCrew(sourceId: string, job: string): string {
  return `${sourceId}|${job}`
}

interface LinhaBase {
  // Identidade só do React (nunca enviada à API).
  id: string
  // Chave de negócio na fonte/override já gravado; `null` = crédito novo desta
  // sessão de edição (ainda não existe no lado do servidor).
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

// Forma de um bloco por kind dentro do patch `credits` (ver OverridePatch no
// openapi.yaml). Tipada aqui localmente — o schema é `additionalProperties: true`
// (forma livre), então os tipos gerados não a descrevem.
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
  // Campos editáveis atuais da linha, como texto (só os comparados no diff).
  camposEditaveis: (linha: L) => Record<string, string>
  // Chave de negócio calculada a partir dos campos ATUAIS (linhas novas e o
  // `reorder`, que referencia créditos novos pela chave que eles próprios definem).
  chaveAtual: (linha: L) => string
  // Chave de negócio de um item de `add` já gravado (lido do blob), para saber se
  // ele sobrevive a um `remove` desta sessão.
  chaveDoAdd: (item: Record<string, string>) => string
  // Payload de `add` para uma linha nova.
  montarAdd: (linha: L) => Record<string, string>
}

// montarBlocoKind compara o estado inicial (resolvido ao abrir) com o estado atual
// (editado na tela) e monta o bloco de patch do kind, já mesclado com o que já
// estava gravado (`anterior`, do blob) — remove/edit/add anteriores que esta
// sessão não tocou seguem adiante; o `reorder` é sempre a ordem final completa
// (não precisa mesclar: já reflete a ordem herdada mais o que o dono mudou agora).
// `undefined` = nada mudou nesse kind — a chave fica de fora do patch (herda).
function montarBlocoKind<L extends LinhaBase & { source_id: string }>(
  inicial: L[],
  atual: L[],
  anterior: BlocoPatch | null | undefined,
  cfg: ConfigDiff<L>,
): BlocoPatch | undefined {
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
    const mudou: Record<string, string> = {}
    for (const campo of Object.keys(camposAtuais)) {
      if (camposAtuais[campo] !== camposOriginais[campo]) {
        mudou[campo] = camposAtuais[campo]
      }
    }
    if (Object.keys(mudou).length > 0) {
      editadosAgora[linha.chaveOriginal] = mudou
    }
  }

  const adicionadosAgora = atual
    .filter((l) => l.chaveOriginal === null)
    .map((l) => cfg.montarAdd(l))

  const chavesIniciais = inicial.map((l) => l.chaveOriginal as string)
  const chavesFinais = atual.map((l) => l.chaveOriginal ?? cfg.chaveAtual(l))
  const ordemMudou =
    JSON.stringify(chavesIniciais) !== JSON.stringify(chavesFinais)

  const algoMudouAgora =
    removidosAgora.length > 0 ||
    Object.keys(editadosAgora).length > 0 ||
    adicionadosAgora.length > 0 ||
    ordemMudou

  if (!algoMudouAgora) {
    return undefined
  }

  const removeFinal = uniq([...(anterior?.remove ?? []), ...removidosAgora])
  const chavesRemovidas = new Set(removeFinal)
  const addAnteriorVivo = (anterior?.add ?? []).filter(
    (item) => !chavesRemovidas.has(cfg.chaveDoAdd(item)),
  )
  const addFinal = [...addAnteriorVivo, ...adicionadosAgora]
  const editFinal = { ...(anterior?.edit ?? {}), ...editadosAgora }

  const bloco: BlocoPatch = { reorder: chavesFinais }
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

export function CreditsEditor({
  id,
  lang,
  entity,
  blob,
}: {
  id: string
  lang: string
  entity: Pick<ResolvedEntity, 'cast' | 'crew' | 'companies'>
  blob: OverrideBlob
}) {
  const qc = useQueryClient()

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
          ? 'Não foi possível salvar os créditos.'
          : null

  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="flex items-center gap-2 font-heading text-lg font-medium">
          <Users className="size-4" aria-hidden />
          Créditos
        </h2>
        <p className="text-sm text-muted-foreground">
          Reordene, edite, remova ou adicione elenco, equipe e produtoras
          exibidos para este item.
        </p>
      </div>

      <SecaoCreditos
        titulo="Elenco"
        icone={<Users className="size-4" aria-hidden />}
        linhas={cast}
        setLinhas={setCast}
        vazioTexto="Sem elenco."
        mostrarNomeFixo
        removerAriaLabel={(l) => `Remover ${l.name} do elenco`}
        camposEditaveis={[
          {
            valor: (l) => l.character,
            aoMudar: (l, v) => ({ ...l, character: v }),
            placeholder: 'Personagem',
            ariaLabel: (l) => `Personagem de ${l.name}`,
          },
        ]}
      />
      <SecaoCreditos
        titulo="Equipe"
        icone={<Wrench className="size-4" aria-hidden />}
        linhas={crew}
        setLinhas={setCrew}
        vazioTexto="Sem equipe."
        mostrarNomeFixo
        removerAriaLabel={(l) => `Remover ${l.name} da equipe`}
        camposEditaveis={[
          {
            valor: (l) => l.department,
            aoMudar: (l, v) => ({ ...l, department: v }),
            placeholder: 'Departamento',
            ariaLabel: (l) => `Departamento de ${l.name}`,
            className: 'max-w-[9rem]',
          },
          {
            valor: (l) => l.job,
            aoMudar: (l, v) => ({ ...l, job: v }),
            placeholder: 'Cargo',
            ariaLabel: (l) => `Cargo de ${l.name}`,
            className: 'max-w-[9rem]',
          },
        ]}
      />
      <SecaoCreditos
        titulo="Produtoras"
        icone={<Building2 className="size-4" aria-hidden />}
        linhas={companies}
        setLinhas={setCompanies}
        vazioTexto="Sem produtoras."
        mostrarNomeFixo={false}
        removerAriaLabel={(l) => `Remover ${l.name}`}
        camposEditaveis={[
          {
            valor: (l) => l.name,
            aoMudar: (l, v) => ({ ...l, name: v }),
            placeholder: 'Nome da produtora',
            ariaLabel: () => 'Nome da produtora',
          },
        ]}
      />

      <div className="flex flex-col gap-2 border-t border-border pt-3">
        <FormularioAdicionar
          titulo="Adicionar elenco"
          textoBotao="Adicionar ao elenco"
          podeAdicionar={!!novoCastNome.trim()}
          campos={[
            {
              valor: novoCastNome,
              aoMudar: setNovoCastNome,
              placeholder: 'Nome',
              ariaLabel: 'Nome do novo membro do elenco',
            },
            {
              valor: novoCastPersonagem,
              aoMudar: setNovoCastPersonagem,
              placeholder: 'Personagem',
              ariaLabel: 'Personagem do novo membro do elenco',
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
          titulo="Adicionar equipe"
          textoBotao="Adicionar à equipe"
          podeAdicionar={!!novoCrewNome.trim()}
          campos={[
            {
              valor: novoCrewNome,
              aoMudar: setNovoCrewNome,
              placeholder: 'Nome',
              ariaLabel: 'Nome do novo membro da equipe',
            },
            {
              valor: novoCrewDepartamento,
              aoMudar: setNovoCrewDepartamento,
              placeholder: 'Departamento',
              ariaLabel: 'Departamento do novo membro da equipe',
              className: 'max-w-[10rem]',
            },
            {
              valor: novoCrewCargo,
              aoMudar: setNovoCrewCargo,
              placeholder: 'Cargo',
              ariaLabel: 'Cargo do novo membro da equipe',
              className: 'max-w-[10rem]',
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
          titulo="Adicionar produtora"
          textoBotao="Adicionar produtora"
          podeAdicionar={!!novoCompanyNome.trim()}
          campos={[
            {
              valor: novoCompanyNome,
              aoMudar: setNovoCompanyNome,
              placeholder: 'Nome',
              ariaLabel: 'Nome da nova produtora',
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

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={ocupado}
          onClick={() => limpar.mutate()}
        >
          <Eraser aria-hidden />
          Limpar todos os créditos
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={ocupado}
          onClick={salvarClique}
        >
          {salvar.isPending ? 'Salvando…' : 'Salvar créditos'}
        </Button>
      </div>
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
  return (
    <div className="flex flex-col">
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-6"
        aria-label="Mover para cima"
        disabled={indice === 0}
        onClick={() => mover(-1)}
      >
        <ArrowUp className="size-3" aria-hidden />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-6"
        aria-label="Mover para baixo"
        disabled={indice === total - 1}
        onClick={() => mover(1)}
      >
        <ArrowDown className="size-3" aria-hidden />
      </Button>
    </div>
  )
}

// CampoEditavel descreve UM input editável de uma linha de crédito (usado por
// SecaoCreditos, genérico entre elenco/equipe/produtoras): `character` (cast),
// `department`+`job` (crew) ou `name` (companies, que não tem "nome fixo").
interface CampoEditavel<L> {
  valor: (l: L) => string
  aoMudar: (l: L, v: string) => L
  placeholder: string
  ariaLabel: (l: L) => string
  className?: string
}

// SecaoCreditos renderiza UMA seção (Elenco/Equipe/Produtoras): reordenar
// (↑/↓), os campos editáveis do kind e remover — genérico o suficiente para as
// 3 seções não duplicarem a mesma estrutura de lista. `mostrarNomeFixo` exibe o
// nome como rótulo fixo (cast/crew, cujo `name` não é editável aqui) — para
// companies, o próprio `name` é o único campo editável, então fica `false`.
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
            <li key={l.id} className="flex items-center gap-2">
              <BotoesOrdem
                indice={i}
                total={linhas.length}
                mover={(d) => setLinhas((atual) => moverItem(atual, i, d))}
              />
              {mostrarNomeFixo ? (
                <span className="w-32 shrink-0 truncate text-sm" title={l.name}>
                  {l.name}
                </span>
              ) : null}
              {camposEditaveis.map((campo) => (
                <Input
                  key={campo.placeholder}
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
              ))}
              <Button
                type="button"
                variant="ghost"
                size="icon"
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

// CampoNovo descreve UM input do formulário "Adicionar X" (genérico entre as 3
// seções — só muda a quantidade/rótulo dos campos).
interface CampoNovo {
  valor: string
  aoMudar: (v: string) => void
  placeholder: string
  ariaLabel: string
  className?: string
}

// FormularioAdicionar é o formulário pequeno no fim de cada seção que acrescenta
// um crédito CUSTOM (sem provider) à lista local.
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
      <div className="flex flex-wrap items-center gap-2">
        {campos.map((c) => (
          <Input
            key={c.ariaLabel}
            value={c.valor}
            onChange={(e) => c.aoMudar(e.target.value)}
            placeholder={c.placeholder}
            aria-label={c.ariaLabel}
            className={c.className ?? 'max-w-[12rem]'}
          />
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
