import { Check, Languages } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useLanguage } from '@/lib/language-context'
import { rotuloIdioma } from '@/lib/languages'
import {
  UI_LANGUAGES,
  UI_LANGUAGE_LABELS,
  useT,
  useUiLanguage,
} from '@/lib/i18n'

// Single language control of the AppHeader, with two sections (ADR-0032):
//
// - **Interface** — the language of the chrome. A browser preference, stored in
//   `localStorage`, defaulting to English. It changes nothing in the catalog.
// - **Metadata language** — the READING language, which is content: it comes from
//   `GET /settings/languages` and re-fetches the reads on change (ADR-0019).
//
// They are one control because they answer the same question ("in which language
// do I read this?"), and two settings because one is a device preference and the
// other belongs to the catalog. The metadata section disappears when the server
// allows a single language — there is no choice to offer.
export function LanguageSelector({
  container,
}: {
  /** Portal target. Inside the mobile `Sheet` the default (body) made the menu float at x=0,
   *  over the darkened backdrop and on top of the sheet's own links. */
  container?: HTMLElement | null
} = {}) {
  const { lang: idiomaLeitura, setLang: setLeitura, allowed } = useLanguage()
  const { lang: idiomaUi, setLang: setUi } = useUiLanguage()
  const t = useT()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="gap-1.5"
          aria-label={t('nav.language.button')}
        >
          <Languages className="size-4" aria-hidden />
          <span className="text-xs font-medium">
            {idiomaUi === 'pt-BR' ? 'PT' : idiomaUi.toUpperCase()}
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="min-w-52"
        container={container ?? undefined}
      >
        <div className="px-2 py-1 text-xs font-medium text-muted-foreground">
          {t('nav.language.interface')}
        </div>
        {UI_LANGUAGES.map((codigo) => (
          <DropdownMenuItem
            key={codigo}
            onSelect={() => setUi(codigo)}
            aria-checked={codigo === idiomaUi}
            role="menuitemradio"
          >
            <span className="flex-1">{UI_LANGUAGE_LABELS[codigo]}</span>
            {codigo === idiomaUi ? (
              <Check className="size-4" aria-hidden />
            ) : null}
          </DropdownMenuItem>
        ))}
        {allowed.length > 1 ? (
          <>
            <DropdownMenuSeparator />
            <div className="px-2 py-1 text-xs font-medium text-muted-foreground">
              {t('nav.language.metadata')}
            </div>
            {allowed.map((codigo) => (
              <DropdownMenuItem
                key={codigo}
                onSelect={() => setLeitura(codigo)}
                aria-checked={codigo === idiomaLeitura}
                role="menuitemradio"
              >
                <span className="flex-1">{rotuloIdioma(codigo)}</span>
                {codigo === idiomaLeitura ? (
                  <Check className="size-4" aria-hidden />
                ) : null}
              </DropdownMenuItem>
            ))}
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
