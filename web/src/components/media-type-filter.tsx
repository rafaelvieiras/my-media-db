import { useQuery } from '@tanstack/react-query'
import { Select } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { entityTypesQueryOptions } from '@/lib/queries'
import { rotuloTipoPlural } from '@/lib/entity-badges'

// Media type filter, shared between the catalog grid
// (`PosterGrid`) and the search (`SearchPanel`) — it centralizes the label and the options
// in a single place (avoids markup duplication measured by jscpd).
//
// Since ADR-0030 the options are DYNAMIC: instead of the old
// fixed enum `'all' | 'movie' | 'tv'`, the component fetches by itself the TOP-LEVEL
// types with cataloged data via `GET /entities/types` (the same
// `entityTypesQueryOptions` that the navbar/home already query) — a new schema (e.g. "artista") appears here without touching this
// file. `value`/`onChange` carry the REAL `type` from the API; `'all'` is the
// sentinel for "no filter" (never a real `type`, so it does not collide with
// any future schema). While the list has not arrived yet (pending) or fails,
// the Select degrades by showing only "Todos" — it never breaks the screen.
export type MediaTypeFilterValue = string

export function MediaTypeFilter({
  value,
  onChange,
  className,
}: {
  value: MediaTypeFilterValue
  onChange: (value: MediaTypeFilterValue) => void
  className?: string
}) {
  const { data: tipos } = useQuery(entityTypesQueryOptions)

  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Tipo</span>
      <Select
        aria-label="Filtrar por tipo"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={cn('w-36', className)}
      >
        <option value="all">Todos</option>
        {(tipos ?? []).map((tipo) => (
          <option key={tipo.type} value={tipo.type}>
            {rotuloTipoPlural(tipo.type)}
          </option>
        ))}
      </Select>
    </label>
  )
}
