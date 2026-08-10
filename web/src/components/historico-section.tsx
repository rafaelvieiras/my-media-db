import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleCheck, Trash2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ErrorState, LoadingState } from '@/components/query-states'
import {
  ApiError,
  deleteHistoricoEvento,
  type HistoricoEvento,
} from '@/lib/api'
import {
  formatarCarimboHistorico,
  KIND_ASSISTIR,
  rotuloOrigemHistorico,
  rotuloStatusHistorico,
  STATUS_COMPLETED,
} from '@/lib/historico'
import {
  historicoEstadoQueryOptions,
  historicoEventosQueryOptions,
} from '@/lib/queries'

// Histórico de consumo (briefing 0094, dá rosto ao 0092/0093/ADR-0022): selo de
// estado "Assistido" (`EstadoAssistidoBadge`) + timeline de eventos
// (`HistoricoSection`), companheiros da `RichDetailView` no mesmo padrão da
// `SimilaresRail` (vazio → a seção some por inteiro, sem "não assistido" ativo
// nem timeline vazia poluindo a tela). Os rótulos/constantes puros ficam em
// `lib/historico.ts` (módulo que exporta componente + const dispara
// `react-refresh/only-export-components`).

// EstadoAssistidoBadge é o selo "Assistido" (com data) do hero/card — lê
// `historicoEstadoQueryOptions` (kind="assistir") e só aparece quando o evento
// MAIS RECENTE desse kind é `completed` (assistir em andamento/pausado não
// acende o selo). Sem nenhum evento — ou erro/carregando — o selo simplesmente
// não aparece: nunca um selo ativo de "não assistido" (mesmo princípio de
// "sinalizar só o que precisa de atenção positiva" já usado no projeto).
export function EstadoAssistidoBadge({ entityId }: { entityId: string }) {
  const { data } = useQuery(historicoEstadoQueryOptions(entityId))
  const assistir = data?.estado?.[KIND_ASSISTIR]
  if (!assistir || assistir.status !== STATUS_COMPLETED) {
    return null
  }
  return (
    <Badge
      variant="secondary"
      className="gap-1"
      title={`Assistido em ${formatarCarimboHistorico(assistir.occurred_at)}`}
    >
      <CircleCheck className="size-3.5" aria-hidden />
      Assistido em {formatarCarimboHistorico(assistir.occurred_at)}
    </Badge>
  )
}

// invalidarHistorico invalida TODO o prefixo `['historico', id]` (eventos de
// qualquer kind + o estado derivado) — mesma convenção de invalidação por
// prefixo já usada no resto do projeto (`['entity', id]`, `['assets', id]`).
function invalidarHistorico(
  queryClient: ReturnType<typeof useQueryClient>,
  entityId: string,
) {
  queryClient.invalidateQueries({ queryKey: ['historico', entityId] })
}

// EventoRow é UMA linha da timeline: status + data + origem, com o controle de
// apagar em confirmação de 2 ETAPAS (× → "Apagar?"/"Cancelar") — o mesmo padrão
// já usado por `MembroEditavel` (collection-members.tsx) para remover um item de
// uma lista, mais leve que o modal `AlertDialog` reservado para ações
// destrutivas maiores (ex. "Excluir coleção").
function EventoRow({
  entityId,
  evento,
}: {
  entityId: string
  evento: HistoricoEvento
}) {
  const queryClient = useQueryClient()
  const [confirmando, setConfirmando] = useState(false)
  const apagar = useMutation({
    mutationFn: () => deleteHistoricoEvento(entityId, evento.id),
    onSuccess: () => invalidarHistorico(queryClient, entityId),
  })
  const erro =
    apagar.error instanceof ApiError
      ? apagar.error.message
      : apagar.isError
        ? 'Não foi possível apagar o evento.'
        : null

  return (
    <li className="flex flex-col gap-1 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <Badge variant="outline">
            {rotuloStatusHistorico(evento.status)}
          </Badge>
          <span className="text-muted-foreground">
            {formatarCarimboHistorico(evento.occurred_at)}
          </span>
          <span className="text-xs text-muted-foreground">
            · {rotuloOrigemHistorico(evento.source)}
          </span>
        </div>
        {confirmando ? (
          <div className="flex items-center gap-1">
            <Button
              variant="destructive"
              size="xs"
              disabled={apagar.isPending}
              onClick={() => apagar.mutate()}
            >
              {apagar.isPending ? 'Apagando…' : 'Apagar?'}
            </Button>
            <Button
              variant="ghost"
              size="xs"
              disabled={apagar.isPending}
              onClick={() => setConfirmando(false)}
            >
              Cancelar
            </Button>
          </div>
        ) : (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Apagar evento"
            onClick={() => setConfirmando(true)}
          >
            <Trash2 aria-hidden />
          </Button>
        )}
      </div>
      {erro ? (
        <span role="alert" className="text-xs font-medium text-destructive">
          {erro}
        </span>
      ) : null}
    </li>
  )
}

// HistoricoSection é a seção "Histórico" do detalhe do item (briefing 0094):
// lista TODOS os eventos (qualquer kind, agnóstico de vertical) em ordem
// cronológica reversa (occurred_at DESC, já assim do backend). Mesmo padrão da
// `SimilaresRail`: vazio → a seção some por inteiro (sem heading solto).
// Carregando/erro mostram a seção com o heading (transitório) — consistente com
// o resto do detalhe (`MediaInfoTable`/`ArtGallery`).
export function HistoricoSection({ entityId }: { entityId: string }) {
  const query = useQuery(historicoEventosQueryOptions(entityId))

  if (query.isPending) {
    return (
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-medium">Histórico</h2>
        <LoadingState label="Carregando histórico…" />
      </section>
    )
  }
  if (query.isError) {
    return (
      <section className="flex flex-col gap-3">
        <h2 className="font-heading text-lg font-medium">Histórico</h2>
        <ErrorState
          title="Não foi possível carregar o histórico"
          message={
            query.error instanceof Error ? query.error.message : undefined
          }
        />
      </section>
    )
  }
  if (query.data.eventos.length === 0) {
    return null
  }

  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-medium">Histórico</h2>
      <ul className="flex flex-col gap-2">
        {query.data.eventos.map((evento) => (
          <EventoRow key={evento.id} entityId={entityId} evento={evento} />
        ))}
      </ul>
    </section>
  )
}
