import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { rotuloIdioma } from '@/lib/languages'

// Par de campos compartilhado por `/dashboard/library/new` e
// `/dashboard/library/$id/edit` (briefing 0085): idioma preferido (0071) e
// reentativa de enriquecimento (dias) do datasource — as duas rotas só
// diferem em como tratam a mudança (criar guarda o valor puro; editar também
// marca "tocado" pra decidir o que entra no PATCH parcial).
export function LibraryLanguageAndRetryFields({
  idiomasPermitidos,
  preferredLanguage,
  onPreferredLanguageChange,
  retryDays,
  onRetryDaysChange,
  retryDaysGlobal,
}: {
  idiomasPermitidos: string[]
  preferredLanguage: string
  onPreferredLanguageChange: (valor: string) => void
  retryDays: string
  onRetryDaysChange: (valor: string) => void
  retryDaysGlobal: number | undefined
}) {
  return (
    <div className="flex flex-wrap gap-4">
      <label className="flex max-w-64 flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Idioma preferido</span>
        <Select
          value={preferredLanguage}
          onChange={(e) => onPreferredLanguageChange(e.target.value)}
          aria-label="Idioma preferido do datasource"
        >
          <option value="">Usar idioma global</option>
          {idiomasPermitidos.map((codigo) => (
            <option key={codigo} value={codigo}>
              {rotuloIdioma(codigo)}
            </option>
          ))}
        </Select>
      </label>

      <label className="flex max-w-64 flex-col gap-1 text-sm">
        <span className="text-muted-foreground">
          Reentativa de enriquecimento (dias)
        </span>
        <Input
          type="number"
          min={1}
          max={365}
          value={retryDays}
          onChange={(e) => onRetryDaysChange(e.target.value)}
          placeholder={
            retryDaysGlobal !== undefined
              ? `Usar padrão global (${retryDaysGlobal} dias)`
              : 'Usar padrão global'
          }
          aria-label="Reentativa de enriquecimento (dias) do datasource"
        />
      </label>
    </div>
  )
}
