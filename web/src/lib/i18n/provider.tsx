// UI i18n, React side (ADR-0032): the interface language is a browser
// preference, separate from the metadata reading language (`language-context.tsx`,
// which is content and comes from the server).
//
// Resolution order: localStorage → `navigator.language` when it matches a
// supported locale → `en`. The catalogs and the pure lookup live in
// `./catalog`; this file only holds the context, the provider and the hooks.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  FALLBACK_LANGUAGE,
  readStoredLanguage,
  resolveUiLanguage,
  STORAGE_KEY,
  translate,
  type Key,
  type MessageParams,
  type TypedTranslate,
  type UiLanguage,
} from './catalog'

interface I18nContextValue {
  lang: UiLanguage
  setLang: (lang: UiLanguage) => void
  t: TypedTranslate
}

// The default value is the English catalog, so a component rendered without the
// provider (isolated unit tests) shows real English copy instead of throwing.
const I18nContext = createContext<I18nContextValue>({
  lang: FALLBACK_LANGUAGE,
  setLang: () => {},
  t: (key: string, params?: MessageParams) =>
    translate(FALLBACK_LANGUAGE, key, params),
})

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<UiLanguage>(() =>
    resolveUiLanguage(
      readStoredLanguage(),
      typeof navigator === 'undefined' ? null : navigator.language,
    ),
  )

  useEffect(() => {
    document.documentElement.lang = lang
  }, [lang])

  const setLang = useCallback((novo: UiLanguage) => {
    setLangState(novo)
    try {
      localStorage.setItem(STORAGE_KEY, novo)
    } catch {
      // Private mode / storage disabled: the choice lives only for this session.
    }
  }, [])

  const value = useMemo<I18nContextValue>(
    () => ({
      lang,
      setLang,
      t: (key: string, params?: MessageParams) => translate(lang, key, params),
    }),
    [lang, setLang],
  )

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components
export function useI18n(): I18nContextValue {
  return useContext(I18nContext)
}

// The translation function typed against the catalog: `t('nav.header.library')`
// autocompletes and a typo is a compile error.
// eslint-disable-next-line react-refresh/only-export-components
export function useT(): TypedTranslate {
  return useI18n().t
}

// eslint-disable-next-line react-refresh/only-export-components
export function useUiLanguage(): {
  lang: UiLanguage
  setLang: (lang: UiLanguage) => void
} {
  const { lang, setLang } = useI18n()
  return { lang, setLang }
}

// The locale to hand to `Intl.*` — the same `pt-BR`/`en`/`es` strings already are
// valid BCP-47 tags.
// eslint-disable-next-line react-refresh/only-export-components
export function useLocale(): UiLanguage {
  return useI18n().lang
}

export type { Key, MessageParams }
