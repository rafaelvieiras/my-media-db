import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { useT } from '@/lib/i18n'
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
  const t = useT()
  return (
    <div className="flex flex-wrap gap-4">
      <label className="flex max-w-64 flex-col gap-1 text-sm">
        <span className="text-muted-foreground">
          {t('library.form.preferredLanguage')}
        </span>
        <Select
          value={preferredLanguage}
          onChange={(e) => onPreferredLanguageChange(e.target.value)}
          aria-label={t('library.form.preferredLanguageAriaLabel')}
        >
          <option value="">{t('library.form.useGlobalLanguage')}</option>
          {idiomasPermitidos.map((codigo) => (
            <option key={codigo} value={codigo}>
              {rotuloIdioma(codigo)}
            </option>
          ))}
        </Select>
      </label>

      <label className="flex max-w-64 flex-col gap-1 text-sm">
        <span className="text-muted-foreground">
          {t('library.form.retryDays')}
        </span>
        <Input
          type="number"
          min={1}
          max={365}
          value={retryDays}
          onChange={(e) => onRetryDaysChange(e.target.value)}
          placeholder={
            retryDaysGlobal !== undefined
              ? t('library.form.retryDaysPlaceholderWithDefault', {
                  n: retryDaysGlobal,
                })
              : t('library.form.retryDaysPlaceholder')
          }
          aria-label={t('library.form.retryDaysAriaLabel')}
        />
      </label>
    </div>
  )
}
