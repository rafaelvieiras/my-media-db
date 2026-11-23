import type { ReactNode } from 'react'
import { AtualizadoHa } from '@/components/atualizado-ha'
import { EmptyState, ErrorState, LoadingState } from '@/components/query-states'
import { ScreenHeader } from '@/components/screen-header'
import { useFreshnessSeconds } from '@/lib/queries'

// The skeleton the read-only data screens repeat: the header, the "updated Xs ago"
// line that the polling screens carry and the pending → error → empty → content
// branch. Extracted after jscpd flagged `/jobs` and `/queue` as the same 18 lines:
// only the copy changed, the state machine was identical. The route keeps its data
// and its content; this shell keeps the layout.
export function DataScreen({
  title,
  subtitle,
  updatedAt,
  query,
  copy,
  empty,
  children,
}: {
  title: string
  subtitle?: string
  /** `dataUpdatedAt` of the screen query: the clock behind "updated Xs ago". */
  updatedAt: number
  /** The query state, passed whole so the route does not repeat the destructuring. */
  query: {
    isPending: boolean
    isError: boolean
    error: unknown
    refetch: () => unknown
  }
  copy: {
    loading: string
    loadError: string
    emptyTitle: string
    emptyDescription: string
  }
  /** Whether the query answered with nothing to list. */
  empty: boolean
  children: ReactNode
}) {
  const idade = useFreshnessSeconds(updatedAt)

  return (
    <main className="mx-auto max-w-4xl px-4 py-6">
      <ScreenHeader title={title} subtitle={subtitle} />
      <AtualizadoHa seconds={idade} />

      {query.isPending ? (
        <LoadingState label={copy.loading} />
      ) : query.isError ? (
        <ErrorState
          title={copy.loadError}
          message={
            query.error instanceof Error ? query.error.message : undefined
          }
          onRetry={() => void query.refetch()}
        />
      ) : empty ? (
        <EmptyState
          title={copy.emptyTitle}
          description={copy.emptyDescription}
        />
      ) : (
        children
      )}
    </main>
  )
}
