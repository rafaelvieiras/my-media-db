import { Select } from '@/components/ui/select'
import { useLanguage } from '@/lib/language-context'
import { rotuloIdioma } from '@/lib/languages'

// Seletor de idioma de leitura no AppHeader: lista os `allowed` e troca via
// `setLang` (que persiste e re-busca as leituras). Esconde-se quando há apenas
// um idioma permitido — não há escolha a oferecer.
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
