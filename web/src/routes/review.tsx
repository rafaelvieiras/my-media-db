import { useEffect, useState } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Check,
  ChevronDown,
  ChevronRight,
  ImageOff,
  LoaderCircle,
  PencilLine,
  Search,
  Wand2,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { CorrectMatchResultCard } from '@/components/external-result-card'
import {
  matchPreviewQueryOptions,
  reviewQueueQueryOptions,
  searchQueryOptions,
} from '@/lib/queries'
import {
  ApiError,
  arteSrc,
  autoMatchBatch,
  confirmCandidate,
  confirmMatch,
  correctMatch,
  posterSrc,
  type AutoMatchBatchResult,
  type ConfirmCandidateBody,
  type EntityCard,
  type ExternalResult,
  type MatchPreview,
  type MatchPreviewTarget,
} from '@/lib/api'
import { aspectPorTipo, ehDraft, rotuloTipo } from '@/lib/entity-badges'
import { internalIdDe } from '@/lib/internal-id'
import { useLanguage } from '@/lib/language-context'

const DEBOUNCE_MS = 350

// Rota `/review` (0018, reagrupada no 0137): a fila de curadoria. Lista os itens
// que o scanner SINALIZOU — `draft` (não identificados) e baixa confiança — e dá a
// AÇÃO que a Fase 2 não tinha: confirmar o palpite, corrigir a identidade
// (re-enriquece) ou abrir o detalhe. Nada aqui é destrutivo; corrigir/confirmar
// apenas re-resolvem.
//
// Desde o 0137 a fila é AGRUPADA pelo contexto do arquivo (álbum da faixa, série
// do episódio, pasta do filme) e abre FECHADA: 43 álbuns de faixas viram 43
// seções em vez de 185 cards, e a prévia de candidato só é buscada quando o dono
// abre a linha (antes, um request por card ao montar — N+1).
export function ReviewScreen() {
  const qc = useQueryClient()
  const { data, isPending, isError, error } = useQuery(reviewQueueQueryOptions)
  const [resumoLote, setResumoLote] = useState<string | null>(null)

  const autoMatchLote = useMutation({
    mutationFn: () => autoMatchBatch(),
    onSuccess: (resultado: AutoMatchBatchResult) => {
      qc.invalidateQueries({ queryKey: ['review'] })
      qc.invalidateQueries({ queryKey: ['entities'] })
      setResumoLote(
        `${resultado.resolved_count} resolvidos automaticamente, ${resultado.pending_count} continuam para revisão manual.`,
      )
    },
  })

  const grupos = agruparRevisao(data ?? [])

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            <h1 className="font-heading text-xl font-semibold">Revisão</h1>
            <p className="text-sm text-muted-foreground">
              Itens casados com baixa confiança e não identificados, agrupados
              pelo álbum, série ou pasta de origem.
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            disabled={autoMatchLote.isPending || !data || data.length === 0}
            onClick={() => {
              setResumoLote(null)
              autoMatchLote.mutate()
            }}
          >
            {autoMatchLote.isPending ? (
              <LoaderCircle className="animate-spin" aria-hidden />
            ) : (
              <Wand2 aria-hidden />
            )}
            {autoMatchLote.isPending
              ? 'Sincronizando…'
              : 'Auto-sincronizar elegíveis'}
          </Button>
        </div>
        {resumoLote ? (
          <p role="status" className="text-sm text-muted-foreground">
            {resumoLote}
          </p>
        ) : null}
        {data && data.length > 0 ? (
          <p className="text-xs text-muted-foreground">
            {data.length} {data.length === 1 ? 'item' : 'itens'} em{' '}
            {grupos.length} {grupos.length === 1 ? 'grupo' : 'grupos'}
          </p>
        ) : null}
      </div>

      {isPending ? (
        <LoadingState label="Carregando a fila de revisão…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar a revisão"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : grupos.length === 0 ? (
        <EmptyState
          title="Nada para revisar"
          description="Nenhum item pendente de curadoria — tudo casado com confiança."
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {grupos.map((grupo) => (
            <SecaoRevisao key={grupo.key} grupo={grupo} />
          ))}
        </ul>
      )}
    </main>
  )
}

// ── Agrupamento (0137) ──────────────────────────────────────────────────────

type GrupoRevisao = {
  key: string
  kind: string
  label: string
  items: EntityCard[]
}

// ORDEM_KINDS decide a ordem das seções na tela: pastas de filme, depois séries,
// depois álbuns, e por último o que não tem arquivo. Dentro do mesmo kind, o
// maior grupo vem primeiro (é o que mais pesa na fila).
const ORDEM_KINDS = ['pasta', 'serie', 'album', 'sem_arquivo']

const ROTULO_KIND: Record<string, string> = {
  pasta: 'pasta',
  serie: 'série',
  album: 'álbum',
  sem_arquivo: 'sem arquivo',
}

// agruparRevisao monta as seções a partir do `review_group` anotado pelo backend.
// Item sem grupo (backend antigo) cai numa seção "sem arquivo" própria — a tela
// nunca esconde um item da fila.
function agruparRevisao(cards: EntityCard[]): GrupoRevisao[] {
  const porChave = new Map<string, GrupoRevisao>()
  for (const card of cards) {
    const grupo = card.review_group
    const key = grupo?.key ?? 'sem_arquivo:'
    const existente = porChave.get(key)
    if (existente) {
      existente.items.push(card)
      continue
    }
    porChave.set(key, {
      key,
      kind: grupo?.kind ?? 'sem_arquivo',
      label: grupo?.label ?? 'Sem arquivo',
      items: [card],
    })
  }
  return [...porChave.values()].sort((a, b) => {
    const ka = ORDEM_KINDS.indexOf(a.kind)
    const kb = ORDEM_KINDS.indexOf(b.kind)
    const pa = ka === -1 ? ORDEM_KINDS.length : ka
    const pb = kb === -1 ? ORDEM_KINDS.length : kb
    if (pa !== pb) {
      return pa - pb
    }
    if (a.items.length !== b.items.length) {
      return b.items.length - a.items.length
    }
    return a.label.localeCompare(b.label, 'pt-BR')
  })
}

// Motivo da revisão, em pt-BR: draft = não identificado; senão, baixa confiança
// com o percentual do card (o backend só põe na fila os < limiar).
function motivoRevisao(card: EntityCard): string {
  if (ehDraft(card.status)) {
    return 'não identificado'
  }
  return `baixa confiança (${card.match_confidence}%)`
}

// resumoDoGrupo descreve a seção fechada: quantidade de itens + o motivo
// DOMINANTE (o primeiro item já vem ordenado do backend por pior confiança).
function resumoDoGrupo(grupo: GrupoRevisao): string {
  const itens = grupo.items.length
  const unidade = itens === 1 ? 'item' : 'itens'
  return `${itens} ${unidade} · ${motivoRevisao(grupo.items[0])}`
}

// Invalidação após confirmar/corrigir: a fila (`['review']`) muda, o grid
// (`['entities']`) reflete o novo estado e o detalhe (`['entity', id]`, prefixo)
// derruba todas as variações de idioma.
function invalidarRevisao(qc: QueryClient, id: string) {
  qc.invalidateQueries({ queryKey: ['review'] })
  qc.invalidateQueries({ queryKey: ['entities'] })
  qc.invalidateQueries({ queryKey: ['entity', id] })
}

// Rótulo pt-BR da fonte do candidato (0050/0060): "local" veio do catálogo já
// importado (funde via `fundirEntities`), "external" veio do provedor remoto
// (mesmo caminho do "Corrigir"). Usado tanto pelo aviso do auto-match em lote
// quanto pelo selo de fonte da prévia (`PreviewBloco`).
function rotuloMetodo(method: 'local' | 'external' | 'none'): string {
  return method === 'local' ? 'catálogo local' : 'provedor externo'
}

// Candidato "aplicável" pela prévia (0060): `found=true` E `target` não-nulo —
// a MESMA checagem usada pelo bloco visual (`PreviewBloco`) e pelo botão
// principal (`ReviewRow`), garantindo que nunca divirjam: se a linha MOSTRA um
// candidato, o botão principal SEMPRE aplica exatamente esse candidato.
function candidatoValido(
  data: MatchPreview | undefined,
): data is MatchPreview & { target: MatchPreviewTarget } {
  return !!data && data.found === true && data.target !== null
}

// ── Seção (grupo) ───────────────────────────────────────────────────────────

function SecaoRevisao({ grupo }: { grupo: GrupoRevisao }) {
  const qc = useQueryClient()
  const [aberto, setAberto] = useState(false)
  const [resumo, setResumo] = useState<string | null>(null)

  const autoMatchGrupo = useMutation({
    mutationFn: () => autoMatchBatch(grupo.items.map((item) => item.id)),
    onSuccess: (resultado: AutoMatchBatchResult) => {
      qc.invalidateQueries({ queryKey: ['review'] })
      qc.invalidateQueries({ queryKey: ['entities'] })
      setResumo(
        `${resultado.resolved_count} resolvido(s) · ${resultado.pending_count} ainda pendente(s)`,
      )
    },
  })

  return (
    <li className="overflow-hidden rounded-xl bg-card ring-1 ring-foreground/10">
      <button
        type="button"
        aria-expanded={aberto}
        onClick={() => setAberto((v) => !v)}
        className="flex w-full items-center gap-2 p-3 text-left hover:bg-muted/40"
      >
        {aberto ? (
          <ChevronDown className="size-4 shrink-0" aria-hidden />
        ) : (
          <ChevronRight className="size-4 shrink-0" aria-hidden />
        )}
        <span className="min-w-0 flex-1">
          <span className="line-clamp-1 text-sm font-medium">
            {grupo.label}
          </span>
          <span className="text-xs text-muted-foreground">
            {resumoDoGrupo(grupo)}
          </span>
        </span>
        <Badge variant="outline" className="shrink-0">
          {ROTULO_KIND[grupo.kind] ?? grupo.kind}
        </Badge>
      </button>

      {aberto ? (
        <div className="flex flex-col gap-2 border-t border-foreground/10 p-2">
          <div className="flex flex-wrap items-center gap-2 px-1">
            <Button
              size="sm"
              variant="outline"
              disabled={autoMatchGrupo.isPending}
              onClick={() => {
                setResumo(null)
                autoMatchGrupo.mutate()
              }}
            >
              {autoMatchGrupo.isPending ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Wand2 aria-hidden />
              )}
              {autoMatchGrupo.isPending
                ? 'Sincronizando…'
                : 'Auto-sincronizar este grupo'}
            </Button>
            {resumo ? (
              <span role="status" className="text-xs text-muted-foreground">
                {resumo}
              </span>
            ) : null}
            {autoMatchGrupo.error instanceof ApiError ? (
              <span
                role="alert"
                className="text-xs font-medium text-destructive"
              >
                {autoMatchGrupo.error.message}
              </span>
            ) : null}
          </div>
          <ul className="flex flex-col">
            {grupo.items.map((card) => (
              <ReviewRow key={card.id} card={card} />
            ))}
          </ul>
        </div>
      ) : null}
    </li>
  )
}

// ── Linha compacta + detalhe sob demanda ────────────────────────────────────

function ReviewRow({ card }: { card: EntityCard }) {
  const qc = useQueryClient()
  const [falhouArte, setFalhouArte] = useState(false)
  const [expandido, setExpandido] = useState(false)
  const [corrigindo, setCorrigindo] = useState(false)
  // Avisa quando "Corrigir" fundiu uma duplicata automaticamente (0051): a
  // entity sem investimento do dono some sozinha da fila/biblioteca.
  const [avisoFusao, setAvisoFusao] = useState(false)
  const src = posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte

  // Prévia de candidato (0060) LAZY (0137): só busca quando o dono abre a linha.
  // `enabled` evita um request por card na montagem da fila (o N+1 antigo).
  const preview = useQuery({
    ...matchPreviewQueryOptions(card.id),
    enabled: expandido,
  })
  const achouCandidato = candidatoValido(preview.data)

  const confirmar = useMutation({
    mutationFn: () => confirmMatch(card.id),
    onSuccess: () => invalidarRevisao(qc, card.id),
  })

  // Aplica exatamente o candidato mostrado pela prévia — local (funde) ou
  // externo (grava identidade + re-enriquece), mesmo abaixo do limiar
  // automático (decisão humana informada pelo card, não um corte silencioso).
  const confirmarCandidato = useMutation({
    mutationFn: () => {
      const dadosPrevia = preview.data
      if (!candidatoValido(dadosPrevia)) {
        throw new Error('sem candidato para confirmar')
      }
      const { target } = dadosPrevia
      const body: ConfirmCandidateBody =
        dadosPrevia.method === 'local'
          ? { method: 'local', local_id: target.local_id ?? '' }
          : {
              method: 'external',
              source: target.source ?? '',
              source_id: target.source_id ?? '',
            }
      return confirmCandidate(card.id, body)
    },
    onSuccess: () => invalidarRevisao(qc, card.id),
  })

  const acaoPendente = achouCandidato
    ? confirmarCandidato.isPending
    : confirmar.isPending

  const mensagemErro =
    confirmar.error instanceof ApiError
      ? confirmar.error.message
      : confirmarCandidato.error instanceof ApiError
        ? confirmarCandidato.error.message
        : null

  return (
    <li className="flex flex-col gap-2 rounded-lg px-1 py-1.5 hover:bg-muted/30">
      <div className="flex items-center gap-2">
        <div
          className={`relative ${aspectPorTipo(card.type)} w-10 shrink-0 overflow-hidden rounded bg-muted`}
        >
          {mostrarPlaceholder ? (
            <div
              data-testid="review-poster-placeholder"
              className="flex h-full w-full items-center justify-center text-muted-foreground"
            >
              <ImageOff className="size-4" aria-hidden />
            </div>
          ) : (
            <img
              src={src}
              alt={`Pôster de ${card.title}`}
              loading="lazy"
              onError={() => setFalhouArte(true)}
              className="h-full w-full object-cover"
            />
          )}
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <Link
            to="/entities/$id"
            params={{ id: card.id }}
            className="line-clamp-1 text-sm font-medium hover:underline"
          >
            {card.title}
          </Link>
          <span className="text-xs text-muted-foreground">
            {rotuloTipo(card.type)} · {motivoRevisao(card)}
          </span>
        </div>
        <Button
          size="sm"
          variant="ghost"
          aria-expanded={expandido}
          onClick={() => setExpandido((v) => !v)}
        >
          {expandido ? (
            <ChevronDown className="size-4" aria-hidden />
          ) : (
            <ChevronRight className="size-4" aria-hidden />
          )}
          {expandido ? 'Fechar' : 'Ver candidato'}
        </Button>
      </div>

      {expandido ? (
        <div className="flex flex-col gap-2 pl-12">
          <PreviewBloco
            tipo={card.type}
            isPending={preview.isPending}
            isError={preview.isError}
            data={preview.data}
          />

          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              disabled={acaoPendente || preview.isPending}
              onClick={() =>
                achouCandidato
                  ? confirmarCandidato.mutate()
                  : confirmar.mutate()
              }
            >
              {acaoPendente ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Check aria-hidden />
              )}
              {achouCandidato
                ? acaoPendente
                  ? 'Confirmando…'
                  : 'Confirmar candidato'
                : acaoPendente
                  ? 'Confirmando…'
                  : 'Manter como está (sem metadados externos)'}
            </Button>
            <Button
              size="sm"
              variant="outline"
              aria-expanded={corrigindo}
              onClick={() => setCorrigindo((v) => !v)}
            >
              <PencilLine aria-hidden />
              Corrigir
            </Button>
            <Button asChild size="sm" variant="ghost">
              <Link to="/entities/$id" params={{ id: card.id }}>
                Ver detalhe
              </Link>
            </Button>
            {mensagemErro ? (
              <span
                role="alert"
                className="text-xs font-medium text-destructive"
              >
                {mensagemErro}
              </span>
            ) : null}
          </div>

          {avisoFusao ? (
            <p role="status" className="text-xs text-muted-foreground">
              Duplicata mesclada automaticamente — o item já importado foi
              combinado com este.
            </p>
          ) : null}

          {corrigindo ? (
            <CorrigirPainel
              entityId={card.id}
              onCorrigido={(duplicataMesclada) => {
                setCorrigindo(false)
                setAvisoFusao(duplicataMesclada)
              }}
            />
          ) : null}
        </div>
      ) : null}
    </li>
  )
}

// Bloco de prévia do candidato (0060): entre o cabeçalho do item e a linha de
// botões. Quatro estados — carregando (skeleton discreto), achou candidato
// (pôster + título + ano + selos), não achou (mensagem neutra) e falha na busca
// (mensagem curta de erro, NÃO bloqueia o resto — `ReviewRow` já trata isso como
// "sem candidato" via `candidatoValido`, que só é `true` com `data` presente).
// `tipo` é o tipo do ITEM sendo revisado (`card.type` do `ReviewRow` pai) — o
// candidato (`MatchPreviewTarget`) não carrega o próprio tipo no contrato da API,
// mas o match nunca sugere um candidato de tipo diferente do alvo (álbum só casa
// com álbum), então reaproveitar o tipo do pai para o frame da capa é seguro.
function PreviewBloco({
  tipo,
  isPending,
  isError,
  data,
}: {
  tipo: string
  isPending: boolean
  isError: boolean
  data: MatchPreview | undefined
}) {
  const [falhouArte, setFalhouArte] = useState(false)

  if (isPending) {
    return (
      <div
        role="status"
        className="flex items-center gap-2 rounded-lg bg-muted/40 p-2 text-muted-foreground"
      >
        <LoaderCircle className="size-4 animate-spin" aria-hidden />
        <span className="text-xs">Buscando candidato…</span>
      </div>
    )
  }

  if (isError) {
    return (
      <p role="alert" className="text-xs font-medium text-destructive">
        Não foi possível buscar um candidato — tente novamente mais tarde.
      </p>
    )
  }

  if (!data || data.found === false || !data.target) {
    return (
      <p className="text-xs text-muted-foreground">
        nenhum candidato encontrado — nem no catálogo local, nem na fonte
        externa
      </p>
    )
  }

  const { target } = data
  const candidatoSrc =
    data.method === 'local'
      ? posterSrc(target.poster_url)
      : arteSrc(target.poster_url)
  const mostrarPlaceholder = candidatoSrc === null || falhouArte

  return (
    <div className="flex items-center gap-3 rounded-lg bg-muted/40 p-2">
      <div
        className={`relative ${aspectPorTipo(tipo)} w-12 shrink-0 overflow-hidden rounded bg-muted`}
      >
        {mostrarPlaceholder ? (
          <div className="flex h-full w-full items-center justify-center text-muted-foreground">
            <ImageOff className="size-4" aria-hidden />
          </div>
        ) : (
          <img
            src={candidatoSrc}
            alt={`Pôster de ${target.title}`}
            loading="lazy"
            onError={() => setFalhouArte(true)}
            className="h-full w-full object-cover"
          />
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline gap-2">
          <span className="line-clamp-1 text-sm font-medium">
            {target.title}
          </span>
          {target.year ? (
            <span className="shrink-0 text-xs text-muted-foreground">
              {target.year}
            </span>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline">score {data.score.toFixed(1)}</Badge>
          <Badge variant="secondary">{rotuloMetodo(data.method)}</Badge>
          {data.meets_auto_threshold ? (
            <Badge>match forte</Badge>
          ) : (
            <Badge variant="outline">confira antes de confirmar</Badge>
          )}
        </div>
        {target.matched_title && target.matched_title !== target.title ? (
          <p className="line-clamp-1 text-xs text-muted-foreground">
            via título alternativo: &quot;{target.matched_title}&quot;
          </p>
        ) : null}
      </div>
    </div>
  )
}

// Painel inline de correção: o dono busca o título certo na fonte externa e
// escolhe o resultado; a UI grava a identidade escolhida com `correctMatch`
// (source/source_id do item), o backend re-enriquece e re-resolve. Reusa o
// `searchQueryOptions` (mesma busca do 0015), mas a ação é CORRIGIR, não importar.
function CorrigirPainel({
  entityId,
  onCorrigido,
}: {
  entityId: string
  onCorrigido: (duplicataMesclada: boolean) => void
}) {
  const qc = useQueryClient()
  const { lang } = useLanguage()
  const [termo, setTermo] = useState('')
  const [debounced, setDebounced] = useState('')

  useEffect(() => {
    const t = setTimeout(() => setDebounced(termo.trim()), DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [termo])

  const busca = useQuery(searchQueryOptions(debounced, undefined, lang))

  const corrigir = useMutation({
    mutationFn: (escolhido: ExternalResult) =>
      correctMatch(entityId, escolhido.source, escolhido.source_id),
    onSuccess: (resultado) => {
      invalidarRevisao(qc, entityId)
      onCorrigido(resultado.duplicataMesclada)
    },
  })

  const mensagemErro =
    corrigir.error instanceof ApiError ? corrigir.error.message : null

  return (
    <div className="flex flex-col gap-3 rounded-lg bg-muted/40 p-3">
      <div className="relative">
        <Search
          className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
          aria-hidden
        />
        <Input
          type="search"
          aria-label="Buscar o título correto"
          placeholder="Buscar o título correto…"
          value={termo}
          onChange={(e) => setTermo(e.target.value)}
          className="h-9 pl-8"
        />
      </div>

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      <ResultadosCorrecao
        query={debounced}
        isPending={busca.isPending}
        isError={busca.isError}
        error={busca.error}
        externos={busca.data?.external ?? []}
        disponivel={busca.data?.external_available ?? true}
        escolhendo={corrigir.isPending}
        onEscolher={(r) => corrigir.mutate(r)}
      />
    </div>
  )
}

// Lista os resultados EXTERNOS da busca como o mesmo card rico de `/search`
// (0074): pôster, sinopse truncada e selo "Na biblioteca" quando o resultado já é
// uma entity interna — mas aqui o botão é "Usar este" (não Importar), e o selo
// NÃO esconde a ação (é o caso de fundir duplicata, 0051). A escolha vira a
// identidade correta. Só os externos importam aqui (a identidade que o dono
// informa vem de uma fonte, não do catálogo local).
function ResultadosCorrecao({
  query,
  isPending,
  isError,
  error,
  externos,
  disponivel,
  escolhendo,
  onEscolher,
}: {
  query: string
  isPending: boolean
  isError: boolean
  error: unknown
  externos: ExternalResult[]
  disponivel: boolean
  escolhendo: boolean
  onEscolher: (r: ExternalResult) => void
}) {
  if (query === '') {
    return (
      <p className="text-sm text-muted-foreground">
        Digite o título correto para escolher a identidade certa.
      </p>
    )
  }
  if (isPending) {
    return <LoadingState label="Buscando…" />
  }
  if (isError) {
    return (
      <ErrorState
        title="Não foi possível buscar"
        message={error instanceof Error ? error.message : undefined}
      />
    )
  }
  if (!disponivel) {
    return (
      <ErrorState
        title="Busca externa indisponível"
        message="A fonte externa não respondeu ou não está configurada."
      />
    )
  }
  if (externos.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Nenhum resultado externo para esta busca.
      </p>
    )
  }
  return (
    <ul className="flex flex-col gap-3">
      {externos.map((r) => (
        <CorrectMatchResultCard
          key={`${r.source}:${r.source_id}`}
          result={r}
          internalId={internalIdDe(r)}
          pendente={escolhendo}
          onEscolher={onEscolher}
        />
      ))}
    </ul>
  )
}
