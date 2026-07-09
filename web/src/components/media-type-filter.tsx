import { Select } from '@/components/ui/select'
import { cn } from '@/lib/utils'

// Filtro de tipo de mídia (Todos/Filmes/Séries), compartilhado entre o grid do
// catálogo e a busca — centraliza o rótulo e as opções num só lugar (evita
// duplicação de markup medida pelo jscpd).
export type MediaTypeFilterValue = 'all' | 'movie' | 'tv'

export function MediaTypeFilter({
  value,
  onChange,
  className,
}: {
  value: MediaTypeFilterValue
  onChange: (value: MediaTypeFilterValue) => void
  className?: string
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Tipo</span>
      <Select
        aria-label="Filtrar por tipo"
        value={value}
        onChange={(e) => onChange(e.target.value as MediaTypeFilterValue)}
        className={cn('w-36', className)}
      >
        <option value="all">Todos</option>
        <option value="movie">Filmes</option>
        <option value="tv">Séries</option>
      </Select>
    </label>
  )
}
