import type { Ref } from 'react'
import { Input } from '@/components/ui/input'
import { useT } from '@/lib/i18n'

// id of the inline error of the path field: `aria-describedby` points at it, so the
// message is announced together with the input that failed.
const PATH_ERROR_ID = 'library-path-error'

// Path field shared by `/dashboard/library/new` and `/dashboard/library/$id/edit`:
// the required label, the `aria-label` of the input, the `aria-invalid` that only
// turns on when the field (or the submit) failed and the inline error right below
// it — the two routes differ only in the placeholder, since the edit screen
// already opens with a value.
export function LibraryPathField({
  valor,
  onChange,
  inputRef,
  placeholder,
  invalido,
  erroCaminho,
}: {
  valor: string
  onChange: (valor: string) => void
  inputRef: Ref<HTMLInputElement>
  placeholder?: string
  invalido: boolean
  erroCaminho: string | null
}) {
  const t = useT()

  return (
    <>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">
          {t('library.form.path')}
          <span aria-hidden className="ml-0.5 text-destructive">
            *
          </span>
        </span>
        <Input
          ref={inputRef}
          value={valor}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          aria-label={t('library.form.pathAriaLabel')}
          required
          aria-invalid={invalido || undefined}
          aria-describedby={erroCaminho !== null ? PATH_ERROR_ID : undefined}
        />
      </label>
      {erroCaminho !== null ? (
        <p
          id={PATH_ERROR_ID}
          role="alert"
          className="text-sm font-medium text-destructive"
        >
          {erroCaminho}
        </p>
      ) : null}
    </>
  )
}
