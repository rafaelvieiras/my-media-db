import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { rotuloIdioma } from '@/lib/languages'

// Pair of fields shared by `/dashboard/library/new` and
// `/dashboard/library/$id/edit`: preferred language and
// datasource enrichment retry (days) — the two routes only
// differ in how they handle the change (creation keeps the raw value; editing also
// marks it as "touched" to decide what enters the partial PATCH).
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
