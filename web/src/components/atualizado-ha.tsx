import { useT } from '@/lib/i18n'

// The "updated Xs ago" line: the live signal the polling screens (`/queue`,
// `/jobs`) show while the data refetches by itself. The elapsed seconds come from
// `useFreshnessSeconds` (the hook keeps ticking); this component owns only the copy
// and the layout, so both screens stop repeating the same markup (jscpd).
//
// `seconds === null` means "the query has not answered yet" (or the tab is hidden),
// and then there is nothing to say: the line stays out of the screen.
export function AtualizadoHa({ seconds }: { seconds: number | null }) {
  const t = useT()

  if (seconds === null) return null

  return (
    <p className="-mt-2 mb-4 text-xs text-muted-foreground">
      {seconds <= 1
        ? t('common.live.updatedJustNow')
        : t('common.live.updatedAgo', { seconds })}
    </p>
  )
}
