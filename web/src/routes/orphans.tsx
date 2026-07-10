import { useState } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ImageOff, LoaderCircle, Trash2, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { orphansQueryOptions } from '@/lib/queries'
import {
  ApiError,
  posterSrc,
  purgeBatch,
  purgeEntity,
  type EntityCard,
} from '@/lib/api'
import { rotuloTipo } from '@/lib/entity-badges'

// Invalidação após purgar (individual ou lote): a fila de órfãs muda e o grid
// (`['entities']`) perde os cards purgados. Não há `['entity', id]` — a entity
// deixou de existir.
function invalidarOrfas(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: ['orphans'] })
  qc.invalidateQueries({ queryKey: ['entities'] })
}

// Rota `/orphans` (0018): a fila de limpeza. Entidades auto-criadas cujo arquivo
// sumiu e sem investimento do dono. A ÚNICA ação é a PURGA — destrutiva, por isso
// sempre com confirmação EXPLÍCITA (um segundo passo na própria UI, sem
// `window.confirm`): individual por card ou em lote pela seleção.
export function OrphansScreen() {
  const qc = useQueryClient()
  const { data, isPending, isError, error } = useQuery(orphansQueryOptions)
  const [selecionadas, setSelecionadas] = useState<Set<string>>(new Set())
  const [confirmandoLote, setConfirmandoLote] = useState(false)
  const [confirmandoId, setConfirmandoId] = useState<string | null>(null)

  const purgarLote = useMutation({
    mutationFn: (ids: string[]) => purgeBatch(ids),
    onSuccess: () => {
      invalidarOrfas(qc)
      setSelecionadas(new Set())
      setConfirmandoLote(false)
    },
  })

  const purgarUma = useMutation({
    mutationFn: (id: string) => purgeEntity(id),
    onSuccess: (_res, id) => {
      invalidarOrfas(qc)
      setSelecionadas((prev) => {
        const proximo = new Set(prev)
        proximo.delete(id)
        return proximo
      })
      setConfirmandoId(null)
    },
  })

  function alternar(id: string) {
    setSelecionadas((prev) => {
      const proximo = new Set(prev)
      if (proximo.has(id)) {
        proximo.delete(id)
      } else {
        proximo.add(id)
      }
      return proximo
    })
  }

  const cards = data ?? []
  const todasSelecionadas =
    cards.length > 0 && cards.every((c) => selecionadas.has(c.id))

  function alternarTodas() {
    setSelecionadas(
      todasSelecionadas ? new Set() : new Set(cards.map((c) => c.id)),
    )
  }

  const erroLote =
    purgarLote.error instanceof ApiError ? purgarLote.error.message : null

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">Órfãs</h1>
        <p className="text-sm text-muted-foreground">
          Itens auto-criados cujo arquivo sumiu e sem edição sua. Nada é apagado
          sozinho — a purga é sempre sua e explícita.
        </p>
      </div>

      {isPending ? (
        <LoadingState label="Carregando as órfãs…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar as órfãs"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : cards.length === 0 ? (
        <EmptyState
          title="Nenhuma órfã"
          description="Nenhum item auto-criado ficou sem arquivo — nada a limpar."
        />
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-3 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-primary"
                aria-label="Selecionar todas"
                checked={todasSelecionadas}
                onChange={alternarTodas}
              />
              <span>Selecionar todas</span>
            </label>
            <span className="text-sm text-muted-foreground">
              {selecionadas.size} selecionada(s)
            </span>

            <div className="ml-auto flex flex-wrap items-center gap-2">
              {confirmandoLote ? (
                <ConfirmacaoInline
                  mensagem={`Tem certeza? Isso remove ${selecionadas.size} itens e não pode ser desfeito.`}
                  ocupado={purgarLote.isPending}
                  onConfirmar={() => purgarLote.mutate([...selecionadas])}
                  onCancelar={() => setConfirmandoLote(false)}
                />
              ) : (
                <Button
                  variant="destructive"
                  size="sm"
                  disabled={selecionadas.size === 0}
                  onClick={() => setConfirmandoLote(true)}
                >
                  <Trash2 aria-hidden />
                  Purgar selecionadas
                </Button>
              )}
            </div>
          </div>

          {erroLote ? (
            <p role="alert" className="text-sm font-medium text-destructive">
              {erroLote}
            </p>
          ) : null}

          <ul className="flex flex-col gap-3">
            {cards.map((card) => (
              <OrphanCard
                key={card.id}
                card={card}
                selecionada={selecionadas.has(card.id)}
                onAlternar={() => alternar(card.id)}
                confirmando={confirmandoId === card.id}
                onPedirPurga={() => setConfirmandoId(card.id)}
                onCancelar={() => setConfirmandoId(null)}
                onConfirmar={() => purgarUma.mutate(card.id)}
                ocupado={purgarUma.isPending && confirmandoId === card.id}
                erro={
                  confirmandoId === card.id &&
                  purgarUma.error instanceof ApiError
                    ? purgarUma.error.message
                    : null
                }
              />
            ))}
          </ul>
        </div>
      )}
    </main>
  )
}

// Confirmação explícita reutilizável (o segundo passo): a mensagem de aviso mais
// os botões "Confirmar purga" (destrutivo) e "Cancelar". SEM `window.confirm` —
// é estado da própria UI, testável e cancelável.
function ConfirmacaoInline({
  mensagem,
  ocupado,
  onConfirmar,
  onCancelar,
}: {
  mensagem: string
  ocupado: boolean
  onConfirmar: () => void
  onCancelar: () => void
}) {
  return (
    <div
      role="alertdialog"
      aria-label="Confirmar purga"
      className="flex flex-wrap items-center gap-2"
    >
      <span className="flex items-center gap-1.5 text-sm font-medium text-destructive">
        <TriangleAlert className="size-4" aria-hidden />
        {mensagem}
      </span>
      <Button
        variant="destructive"
        size="sm"
        disabled={ocupado}
        onClick={onConfirmar}
      >
        {ocupado ? (
          <LoaderCircle className="animate-spin" aria-hidden />
        ) : (
          <Trash2 aria-hidden />
        )}
        {ocupado ? 'Purgando…' : 'Confirmar purga'}
      </Button>
      <Button variant="ghost" size="sm" disabled={ocupado} onClick={onCancelar}>
        Cancelar
      </Button>
    </div>
  )
}

function OrphanCard({
  card,
  selecionada,
  onAlternar,
  confirmando,
  onPedirPurga,
  onCancelar,
  onConfirmar,
  ocupado,
  erro,
}: {
  card: EntityCard
  selecionada: boolean
  onAlternar: () => void
  confirmando: boolean
  onPedirPurga: () => void
  onCancelar: () => void
  onConfirmar: () => void
  ocupado: boolean
  erro: string | null
}) {
  const [falhouArte, setFalhouArte] = useState(false)
  const src = posterSrc(card.poster_url)
  const mostrarPlaceholder = src === null || falhouArte

  return (
    <li className="flex flex-col gap-2 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex items-center gap-3">
        <input
          type="checkbox"
          className="size-4 shrink-0 accent-primary"
          aria-label={`Selecionar ${card.title}`}
          checked={selecionada}
          onChange={onAlternar}
        />
        <div className="relative aspect-[2/3] w-14 shrink-0 overflow-hidden rounded-lg bg-muted">
          {mostrarPlaceholder ? (
            <div
              data-testid="orphan-poster-placeholder"
              className="flex h-full w-full items-center justify-center text-muted-foreground"
            >
              <ImageOff className="size-5" aria-hidden />
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
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
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
        <div className="shrink-0">
          {confirmando ? (
            <ConfirmacaoInline
              mensagem="Remove este item e não pode ser desfeito."
              ocupado={ocupado}
              onConfirmar={onConfirmar}
              onCancelar={onCancelar}
            />
          ) : (
            <Button
              variant="outline"
              size="sm"
              aria-label={`Purgar ${card.title}`}
              onClick={onPedirPurga}
            >
              <Trash2 aria-hidden />
              Purgar
            </Button>
          )}
        </div>
      </div>
      {erro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {erro}
        </p>
      ) : null}
    </li>
  )
}
