import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { historicoOrfaosQueryOptions } from '@/lib/queries'
import type { HistoricoEventoOrfao } from '@/lib/api'
import {
  formatarCarimboHistorico,
  rotuloOrigemHistorico,
  rotuloStatusHistorico,
} from '@/lib/historico'

// Rota `/dashboard/historico-orfaos` (briefing 0099): lista GLOBAL (fora de
// /entities/{id}) dos eventos de webhook (hoje só Jellyfin, 0093) que não
// bateram com nenhuma entity local no momento em que chegaram — em vez de
// serem descartados (princípio 2 do AGENTS.md), ficam registrados com os IDs
// externos/título/ano BRUTOS do payload. Tela SOMENTE LEITURA: o backend
// vincula esses eventos automaticamente (backfill retroativo) quando o dono
// importa ou corrige a identidade da entity correspondente — nenhuma ação
// manual de vincular aqui (fora de escopo, ver §3 do briefing 0099).
const LIMITE_PAGINA = 20

export function HistoricoOrfaosScreen() {
  const [offset, setOffset] = useState(0)
  const { data, isPending, isError, error } = useQuery(
    historicoOrfaosQueryOptions({ limit: LIMITE_PAGINA, offset }),
  )

  const itens = data?.items ?? []
  const total = data?.total ?? 0
  const temAnterior = offset > 0
  const temProxima = offset + LIMITE_PAGINA < total

  return (
    <main className="mx-auto max-w-3xl px-4 py-6">
      <div className="mb-4 flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">
          Eventos sem item relacionado
        </h1>
        <p className="text-sm text-muted-foreground">
          Eventos de reprodução reportados por um webhook que não bateram com
          nenhum item catalogado. Quando você importar ou corrigir a identidade
          (IDs externos) do item correspondente, o evento é vinculado
          automaticamente — não é preciso fazer nada aqui.
        </p>
      </div>

      {isPending ? (
        <LoadingState label="Carregando eventos…" />
      ) : isError ? (
        <ErrorState
          title="Não foi possível carregar os eventos sem item relacionado"
          message={error instanceof Error ? error.message : undefined}
        />
      ) : itens.length === 0 ? (
        <EmptyState
          title="Nenhum evento sem item relacionado"
          description="Todo evento de reprodução recebido até agora casou com algum item catalogado."
        />
      ) : (
        <div className="flex flex-col gap-3">
          <ul className="flex flex-col gap-2">
            {itens.map((evento) => (
              <EventoOrfaoRow key={evento.id} evento={evento} />
            ))}
          </ul>

          <div className="flex items-center justify-between gap-2 pt-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!temAnterior}
              onClick={() => setOffset((o) => Math.max(0, o - LIMITE_PAGINA))}
            >
              Anterior
            </Button>
            <span className="text-xs text-muted-foreground">
              {offset + 1}–{offset + itens.length} de {total}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!temProxima}
              onClick={() => setOffset((o) => o + LIMITE_PAGINA)}
            >
              Próxima
            </Button>
          </div>
        </div>
      )}
    </main>
  )
}

// tituloOrfao devolve `raw_title` quando disponível, senão monta um rótulo a
// partir dos IDs externos brutos que o evento trouxe (pode não ter nenhum, se
// o payload não informou — nesse caso sobra só o kind/data).
function tituloOrfao(evento: HistoricoEventoOrfao): string {
  if (evento.raw_title) {
    return evento.raw_title
  }
  const ids = evento.raw_external_ids
  const partes = [
    ids.tmdb ? `tmdb=${ids.tmdb}` : null,
    ids.imdb ? `imdb=${ids.imdb}` : null,
    ids.tvdb ? `tvdb=${ids.tvdb}` : null,
  ].filter((p): p is string => p !== null)
  return partes.length > 0
    ? `Item sem título (${partes.join(' / ')})`
    : 'Item sem título'
}

function EventoOrfaoRow({ evento }: { evento: HistoricoEventoOrfao }) {
  return (
    <li className="flex flex-col gap-1 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{tituloOrfao(evento)}</span>
        {evento.raw_year ? (
          <span className="text-sm text-muted-foreground">
            ({evento.raw_year})
          </span>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge variant="outline">{rotuloStatusHistorico(evento.status)}</Badge>
        <span className="text-xs text-muted-foreground">{evento.kind}</span>
        <span className="text-muted-foreground">
          {formatarCarimboHistorico(evento.occurred_at)}
        </span>
        <span className="text-xs text-muted-foreground">
          · {rotuloOrigemHistorico(evento.source)}
        </span>
      </div>
    </li>
  )
}
