import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { historicoOrfaosQueryOptions } from '@/lib/queries'
import type { HistoricoEventoOrfao } from '@/lib/api'
import { useLocale, useT } from '@/lib/i18n'
import type { Translate } from '@/lib/i18n/types'
import { ScreenHeader } from '@/components/screen-header'
import {
  formatarCarimboHistorico,
  rotuloOrigemHistorico,
  rotuloStatusHistorico,
} from '@/lib/historico'

// Route `/dashboard/historico-orfaos`: GLOBAL list (outside
// /entities/{id}) of the webhook events (today only Jellyfin) that did not
// match any local entity at the moment they arrived — instead of
// being discarded (principle 2 of AGENTS.md), they stay recorded with the RAW
// external ids/title/year of the payload. READ-ONLY screen: the backend
// links those events automatically (retroactive backfill) when the owner
// imports or corrects the identity of the corresponding entity — no manual
// linking action here (out of scope, see §3).
const LIMITE_PAGINA = 20

// numero writes a count in the interface locale ("1,361" in English, "1.361" in
// pt-BR): the range line mixed raw digits with the localized dates around it.
function numero(valor: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(valor)
}

export function HistoricoOrfaosScreen() {
  const t = useT()
  const locale = useLocale()
  const [offset, setOffset] = useState(0)
  const { data, isPending, isError, error, refetch } = useQuery(
    historicoOrfaosQueryOptions({ limit: LIMITE_PAGINA, offset }),
  )

  const itens = data?.items ?? []
  const total = data?.total ?? 0
  const temAnterior = offset > 0
  const temProxima = offset + LIMITE_PAGINA < total

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <ScreenHeader
        title={t('review.orphanEvents.title')}
        subtitle={t('review.orphanEvents.subtitle')}
      />

      {isPending ? (
        <LoadingState label={t('review.orphanEvents.loading')} />
      ) : isError ? (
        <ErrorState
          title={t('review.orphanEvents.loadError')}
          message={error instanceof Error ? error.message : undefined}
          onRetry={() => void refetch()}
        />
      ) : itens.length === 0 ? (
        <EmptyState
          title={t('review.orphanEvents.emptyTitle')}
          description={t('review.orphanEvents.emptyDescription')}
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
              {t('common.action.previous')}
            </Button>
            <span className="text-xs text-muted-foreground">
              {t('review.orphanEvents.range', {
                from: numero(offset + 1, locale),
                to: numero(offset + itens.length, locale),
                total: numero(total, locale),
              })}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!temProxima}
              onClick={() => setOffset((o) => o + LIMITE_PAGINA)}
            >
              {t('common.action.next')}
            </Button>
          </div>
        </div>
      )}
    </main>
  )
}

// tituloOrfao returns `raw_title` when available, otherwise it builds a label
// from the raw external ids that the event brought (there may be none, if
// the payload did not inform them — in that case only the kind/date is left).
// The ids themselves are data and stay as they are.
function tituloOrfao(evento: HistoricoEventoOrfao, t: Translate): string {
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
    ? t('review.orphanEvents.untitledWithIds', { ids: partes.join(' / ') })
    : t('review.orphanEvents.untitled')
}

function EventoOrfaoRow({ evento }: { evento: HistoricoEventoOrfao }) {
  const t = useT()
  const locale = useLocale()
  return (
    <li className="flex flex-col gap-1 rounded-xl bg-card p-3 ring-1 ring-foreground/10">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{tituloOrfao(evento, t)}</span>
        {evento.raw_year ? (
          <span className="text-sm text-muted-foreground">
            ({evento.raw_year})
          </span>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge variant="outline">
          {rotuloStatusHistorico(evento.status, t)}
        </Badge>
        <span className="text-xs text-muted-foreground">{evento.kind}</span>
        <span className="text-muted-foreground">
          {formatarCarimboHistorico(evento.occurred_at, locale)}
        </span>
        <span className="text-xs text-muted-foreground">
          · {rotuloOrigemHistorico(evento.source, t)}
        </span>
      </div>
    </li>
  )
}
