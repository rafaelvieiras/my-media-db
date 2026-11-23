import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { CorrectMatchResultCard } from '@/components/external-result-card'
import { ErrorState, LoadingState } from '@/components/query-states'
import { Input } from '@/components/ui/input'
import { ApiError, correctMatch, type ExternalResult } from '@/lib/api'
import { useLanguage } from '@/lib/language-context'
import { searchQueryOptions } from '@/lib/queries'
import { useT } from '@/lib/i18n'

const DEBOUNCE_MS = 350

// ManualMatchPanel is the owner's escape hatch for a WRONG identity: it searches the
// external source by hand and applies the chosen candidate with the same `correctMatch`
// of the review queue (write the identity + re-enrich, and for a series rebuild the
// season/episode tree).
//
// Why it exists as an action of the entity detail, and not only in the review queue:
// the auto-match can pick the WRONG item ABOVE the threshold, and then the entity is
// `cataloged` -- it never shows up in the review queue, so there was no place where the
// owner could say "no, it is this one". Measured case: a folder matched a longer same-name
// title of another year with score 94 and stayed cataloged.
function ManualMatchPanel({
  entityId,
  initialQuery = '',
  onApplied,
}: {
  entityId: string
  initialQuery?: string
  onApplied: () => void
}) {
  const t = useT()
  const qc = useQueryClient()
  const { lang } = useLanguage()
  const [termo, setTermo] = useState(initialQuery)
  const [debounced, setDebounced] = useState(initialQuery.trim())

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(termo.trim()), DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [termo])

  const busca = useQuery(searchQueryOptions(debounced, undefined, lang))

  const corrigir = useMutation({
    mutationFn: (escolhido: ExternalResult) =>
      correctMatch(entityId, escolhido.source, escolhido.source_id),
    onSuccess: () => {
      // The identity changed: every cached view of this entity is stale (title,
      // artwork, seasons/episodes).
      void qc.invalidateQueries({ queryKey: ['entity', entityId] })
      void qc.invalidateQueries({ queryKey: ['entities'] })
      onApplied()
    },
  })

  const mensagemErro =
    corrigir.error instanceof ApiError
      ? corrigir.error.message
      : corrigir.isError
        ? t('review.correction.searchFailed')
        : null

  const externos = busca.data?.external ?? []
  const disponivel = busca.data?.external_available ?? true

  return (
    <div className="flex flex-col gap-3">
      <div className="relative">
        <Search
          className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
          aria-hidden
        />
        <Input
          type="search"
          aria-label={t('review.correction.searchLabel')}
          placeholder={t('review.correction.searchPlaceholder')}
          value={termo}
          onChange={(e) => setTermo(e.target.value)}
          className="h-9 pl-8"
          autoFocus
        />
      </div>

      {mensagemErro ? (
        <p role="alert" className="text-sm font-medium text-destructive">
          {mensagemErro}
        </p>
      ) : null}

      {debounced === '' ? (
        <p className="text-sm text-muted-foreground">
          {t('review.correction.hint')}
        </p>
      ) : busca.isPending ? (
        <LoadingState label={t('review.correction.searching')} />
      ) : busca.isError ? (
        <ErrorState
          title={t('review.correction.searchFailed')}
          message={
            busca.error instanceof Error ? busca.error.message : undefined
          }
        />
      ) : !disponivel ? (
        <ErrorState
          title={t('review.correction.unavailableTitle')}
          message={t('review.correction.unavailableMessage')}
        />
      ) : externos.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {t('review.correction.noResults')}
        </p>
      ) : (
        <ul className="flex flex-col gap-3">
          {externos.map((r) => (
            <CorrectMatchResultCard
              key={`${r.source}:${r.source_id}`}
              result={r}
              pendente={corrigir.isPending}
              onEscolher={(escolhido) => corrigir.mutate(escolhido)}
            />
          ))}
        </ul>
      )}
    </div>
  )
}

export { ManualMatchPanel }
