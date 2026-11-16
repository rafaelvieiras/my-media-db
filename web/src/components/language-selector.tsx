import { Select } from '@/components/ui/select'
import { useLanguage } from '@/lib/language-context'
import { rotuloIdioma } from '@/lib/languages'

// Reading-language selector in the AppHeader: it lists the `allowed` ones and switches via
// `setLang` (which persists and re-fetches the reads). It hides itself when there is only
// one allowed language — there is no choice to offer.
export function LanguageSelector() {
  const { lang, setLang, allowed } = useLanguage()

  if (allowed.length <= 1) {
    return null
  }

  return (
    <Select
      aria-label="Idioma de leitura"
      value={lang}
      onChange={(e) => setLang(e.target.value)}
      className="w-auto"
    >
      {allowed.map((codigo) => (
        <option key={codigo} value={codigo}>
          {rotuloIdioma(codigo)}
        </option>
      ))}
    </Select>
  )
}
