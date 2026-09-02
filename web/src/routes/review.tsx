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

// Rota `/review` (0018): a fila de curadoria. Lista os itens que o scanner
// SINALIZOU — `draft` (não identificados) e baixa confiança — e dá a AÇÃO que a
// Fase 2 não tinha: confirmar o palpite, corrigir a identidade (re-enriquece) ou
// abrir o detalhe. Nada aqui é destrutivo; corrigir/confirmar apenas re-resolvem.
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

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-col gap-1">
            <h1 className="font-heading text-xl font-semibold">Revisão</h1>
            <p className="text-sm text-muted-foreground">
              Itens casados com baixa confiança e não identificados. Confirme o
              palpite ou corrija a identidade.
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
      </div>

      {isPending ? (
        <LoadingState label="Carregando a fila de revisão…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar a revisão"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : data.length === 0 ? (
        <EmptyState
          title="Nada para revisar"
          description="Nenhum item pendente de curadoria — tudo casado com confiança."
        />
      ) : (
        <ul className="flex flex-col gap-3">
          {data.map((card) => (
            <ReviewCard key={card.id} card={card} />
          ))}
        </ul>
      )}
    </main>
  )
}

// Motivo da revisão, em pt-BR: draft = não identificado; senão, baixa confiança
// com o percentual do card (o backend só põe na fila os < limiar).
function motivoRevisao(card: EntityCard): string {
  if (ehDraft(card.status)) {
    return 'não identificado'
  }
  return `baixa confiança (${card.match_confidence}%)`
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
// principal (`ReviewCard`), garantindo que nunca divirjam: se o card MOSTRA um
// candidato, o botão principal SEMPRE aplica exatamente esse candidato.
function candidatoValido(
  data: MatchPreview | undefined,
): data is MatchPreview & { target: MatchPreviewTarget } {
  return !!data && data.found === true && data.target !== null
}

function ReviewCard({ card }: { card: EntityCard }) {
  const qc = useQueryClient()
  const [falhouArte, setFalhouArte] = useState(false)
  const [corrigindo, setCorrigindo] = useState(false)
  // Avisa quando "Corrigir" fundiu uma duplicata automaticamente (0051): a
  // entity sem investimento do dono some sozinha da fila/biblioteca.
  const [avisoFusao, setAvisoFusao] = useState(false)
  const src = posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte

  // Prévia de candidato (0060): busca no MOUNT do card (não lazy) — mostra
  // ANTES de qualquer clique o que o botão principal aplicaria.
  const preview = useQuery(matchPreviewQueryOptions(card.id))
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
    <li className="flex flex-col gap-3 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex gap-3">
        <div
          className={`relative ${aspectPorTipo(card.type)} w-20 shrink-0 overflow-hidden rounded-lg bg-muted`}
        >
          {mostrarPlaceholder ? (
            <div
              data-testid="review-poster-placeholder"
              className="flex h-full w-full items-center justify-center text-muted-foreground"
            >
              <ImageOff className="size-6" aria-hidden />
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
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <Link
              to="/entities/$id"
              params={{ id: card.id }}
              className="line-clamp-1 font-medium hover:underline"
            >
              {card.title}
            </Link>
            <span className="text-xs text-muted-foreground">
              {rotuloTipo(card.type)}
            </span>
          </div>
          <div>
            <Badge variant={ehDraft(card.status) ? 'secondary' : 'destructive'}>
              {motivoRevisao(card)}
            </Badge>
          </div>
        </div>
      </div>

      <PreviewBloco
        tipo={card.type}
        isPending={preview.isPending}
        isError={preview.isError}
        data={preview.data}
      />

      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          disabled={acaoPendente}
          onClick={() =>
            achouCandidato ? confirmarCandidato.mutate() : confirmar.mutate()
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
          <span role="alert" className="text-xs font-medium text-destructive">
            {mensagemErro}
          </span>
        ) : null}
      </div>

      {avisoFusao ? (
        <p role="status" className="text-xs text-muted-foreground">
          Duplicata mesclada automaticamente — o item já importado foi combinado
          com este.
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
    </li>
  )
}

// Bloco de prévia do candidato (0060): entre o cabeçalho (pôster do item +
// título + badge de motivo) e a linha de botões. Quatro estados — carregando
// (skeleton discreto), achou candidato (pôster + título + ano + selos),
// não achou (mensagem neutra) e falha na busca (mensagem curta de erro, NÃO
// bloqueia o resto do card — `ReviewCard` já trata isso como "sem candidato"
// via `candidatoValido`, que só é `true` com `data` presente). `tipo` é o
// tipo do ITEM sendo revisado (`card.type` do `ReviewCard` pai) — o candidato
// (`MatchPreviewTarget`) não carrega o próprio tipo no contrato da API, mas o
// match nunca sugere um candidato de tipo diferente do alvo (álbum só casa
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
