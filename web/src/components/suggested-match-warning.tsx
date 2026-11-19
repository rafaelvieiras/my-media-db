import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CircleHelp } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  confirmCandidate,
  dismissMatchSuggestion,
  type MatchSuggestion,
} from '@/lib/api'
import { useT } from '@/lib/i18n'

// The "did you mean this?" warning of an item the scanner could not identify.
//
// WHY a warning and not an automatic fix: the backend found this candidate with a CHEAPER
// VARIANT of the name on disk (a misspelled folder name finds nothing in the sources, drops
// or cuts a token and finds something). That is a guess, and a wrong cataloged series is
// worse than a draft -- so it is shown, never applied. The owner looks at the poster, and
// one click applies it through the SAME endpoint the review card uses (confirm-candidate,
// no threshold), or dismisses the question for good.
//
// It renders in the hero, right under the synopsis, which is where the "this item is not
// right" information belongs.
//
// TWO MORE REASONS (migration 0029) share this box, because they share its shape: a
// question the backend refuses to answer alone. `empty_tree` says the matched entry has no
// episodes in the source (so no file can ever be placed), and `tree_mismatch` says the tree
// exists but does not fit the files (the counts are the evidence). In both the box is only
// a WARNING: the correction itself belongs to "Fix match" in the item menu, which already
// exists -- this component does not duplicate that entry point.

export function SuggestedMatchWarning({
  entityId,
  suggestion,
}: {
  entityId: string
  suggestion: MatchSuggestion
}) {
  const t = useT()
  const qc = useQueryClient()

  const aplicar = useMutation({
    mutationFn: () =>
      confirmCandidate(entityId, {
        method: 'external',
        source: suggestion.source,
        source_id: suggestion.source_id,
      }),
    onSuccess: async () => {
      // The item changed identity: everything about it is refetched.
      await qc.invalidateQueries({ queryKey: ['entity', entityId] })
    },
  })

  const descartar = useMutation({
    mutationFn: () => dismissMatchSuggestion(entityId),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['entity', entityId] })
    },
  })

  const ocupado = aplicar.isPending || descartar.isPending
  const ano = suggestion.year ? ` (${suggestion.year})` : ''
  // The reason picks the sentence, nothing else: the shape is the same box with the same
  // two ways out. Unknown values (an older backend) fall back to the name hint.
  const arvoreVazia = suggestion.reason === 'empty_tree'
  const arvoreIncompativel = suggestion.reason === 'tree_mismatch'
  const titulo =
    arvoreVazia || arvoreIncompativel
      ? t(
          arvoreVazia
            ? 'entity.suggestedMatch.emptyTreeTitle'
            : 'entity.suggestedMatch.treeMismatchTitle',
        )
      : t('entity.suggestedMatch.title')
  const motivo = arvoreVazia
    ? t('entity.suggestedMatch.emptyTreeReason')
    : arvoreIncompativel
      ? t('entity.suggestedMatch.treeMismatchReason', {
          unmatched: String(suggestion.files_unmatched),
          total: String(suggestion.files_total),
        })
      : t('entity.suggestedMatch.reason', { query: suggestion.query })
  // Applying only makes sense when the candidate is NOT what the item already is.
  const podeAplicar = suggestion.can_apply

  return (
    <div
      data-testid="suggested-match-warning"
      className="flex max-w-3xl flex-col gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 sm:flex-row sm:items-center"
    >
      {suggestion.poster_url ? (
        // The poster is the fastest way to recognize the right item.
        <img
          src={suggestion.poster_url}
          alt=""
          data-testid="suggested-match-poster"
          className="h-24 w-16 shrink-0 rounded object-cover"
        />
      ) : (
        <div className="flex h-24 w-16 shrink-0 items-center justify-center rounded bg-muted">
          <CircleHelp className="size-6 text-muted-foreground" aria-hidden />
        </div>
      )}

      <div className="flex min-w-0 flex-col gap-2">
        <div className="flex items-center gap-2">
          <CircleHelp className="size-4 shrink-0 text-amber-600" aria-hidden />
          <p className="text-sm font-medium">{titulo}</p>
        </div>
        <p className="text-sm text-foreground/90">
          {arvoreIncompativel ? (
            <span className="text-muted-foreground">
              {t('entity.suggestedMatch.currentMatch')}{' '}
            </span>
          ) : null}
          <span className="font-semibold">{suggestion.title}</span>
          {ano} · {suggestion.source.toUpperCase()}
        </p>
        <p className="text-xs text-muted-foreground">{motivo}</p>
        <div className="flex flex-wrap gap-2">
          {podeAplicar ? (
            <Button
              size="sm"
              onClick={() => aplicar.mutate()}
              disabled={ocupado}
              data-testid="suggested-match-apply"
            >
              {aplicar.isPending
                ? t('entity.suggestedMatch.applying')
                : t('entity.suggestedMatch.apply')}
            </Button>
          ) : null}
          <Button
            size="sm"
            variant="ghost"
            onClick={() => descartar.mutate()}
            disabled={ocupado}
            data-testid="suggested-match-dismiss"
          >
            {t('entity.suggestedMatch.dismiss')}
          </Button>
        </div>
        {arvoreVazia || arvoreIncompativel ? (
          <p className="text-xs text-muted-foreground">
            {t('entity.suggestedMatch.fixHint')}
          </p>
        ) : null}
        {aplicar.isError || descartar.isError ? (
          <p role="alert" className="text-xs text-destructive">
            {t('entity.suggestedMatch.error')}
          </p>
        ) : null}
      </div>
    </div>
  )
}
