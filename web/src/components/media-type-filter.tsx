import { useQuery } from '@tanstack/react-query'
import { Select } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { entityTypesQueryOptions } from '@/lib/queries'
import { rotuloTipoPlural } from '@/lib/entity-badges'

// Filtro de tipo de mídia, compartilhado entre o grid do catálogo
// (`PosterGrid`) e a busca (`SearchPanel`) — centraliza o rótulo e as opções
// num só lugar (evita duplicação de markup medida pelo jscpd).
//
// Desde o briefing 0113/ADR-0030 as opções são DINÂMICAS: em vez do antigo
// enum fixo `'all' | 'movie' | 'tv'`, o componente busca sozinho os tipos de
// TOPO com dado catalogado via `GET /entities/types` (mesmo
// `entityTypesQueryOptions` que a navbar/home já consultam, briefings
// 0109/0110) — um schema novo (ex. "artista") aparece aqui sem tocar este
// arquivo. `value`/`onChange` carregam o `type` real da API; `'all'` é o
// sentinela de "sem filtro" (nunca um `type` de verdade, então não colide com
// nenhum schema futuro). Enquanto a lista ainda não chegou (pending) ou falha,
// o Select degrada mostrando só "Todos" — nunca quebra a tela.
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
