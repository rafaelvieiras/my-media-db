import {
  createContext,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useQuery } from '@tanstack/react-query'
import { languageSettingsQueryOptions } from '@/lib/queries'

// LIGHTWEIGHT GLOBAL state of the reading language (ADR-0019): it is not a
// per-route search param, but a context seeded by the `primary` from the settings and
// persisted in localStorage. The AppHeader selector changes it; the read
// queries pass the `lang` in the `?lang=` and in the queryKey — switching the language re-fetches
// and recomposes without reloading the page.

// Safe default while the settings load (it never breaks).
const IDIOMA_PADRAO = 'pt-BR'
const CHAVE_STORAGE = 'mmdb.lang'

export interface LanguageContextValue {
  // Effective reading language (always ∈ allowed, or the primary one).
  lang: string
  // Switches the language and persists it in localStorage.
  setLang: (lang: string) => void
  // Allowed languages (source: GET /settings/languages).
  allowed: string[]
  // Primary language from the settings (seed/fallback).
  primary: string
}

const LanguageContext = createContext<LanguageContextValue | null>(null)

function lerSalvo(): string | null {
  try {
    return localStorage.getItem(CHAVE_STORAGE)
  } catch {
    return null
  }
}

export function LanguageProvider({ children }: { children: ReactNode }) {
  const { data } = useQuery(languageSettingsQueryOptions)

  // `escolhido` holds what the user saved (or had saved); `null` = follow
  // the seed (primary). It is read from localStorage once, on mount.
  const [escolhido, setEscolhido] = useState<string | null>(() => lerSalvo())

  const value = useMemo<LanguageContextValue>(() => {
    const primary = data?.primary ?? IDIOMA_PADRAO
    const allowed = data?.allowed ?? [IDIOMA_PADRAO]
    // Effective language: the chosen one only counts if it is still ∈ allowed; otherwise it falls
    // back to the primary (criterion: "falls back to the primary when the saved one is not allowed").
    const lang = escolhido && allowed.includes(escolhido) ? escolhido : primary
    return {
      lang,
      allowed,
      primary,
      setLang: (novo: string) => {
        setEscolhido(novo)
        try {
          localStorage.setItem(CHAVE_STORAGE, novo)
        } catch {
          // localStorage unavailable (e.g. private mode) — the in-memory state
          // still works for this session.
        }
      },
    }
  }, [data, escolhido])

  return (
    <LanguageContext.Provider value={value}>
      {children}
    </LanguageContext.Provider>
  )
}

// Consumption hook. It lives together with the Provider in this file (the fast-refresh
// rule only affects HMR in dev; the Provider+hook pair in a single module is the idiomatic
// pattern).
// eslint-disable-next-line react-refresh/only-export-components
export function useLanguage(): LanguageContextValue {
  const ctx = useContext(LanguageContext)
  if (!ctx) {
    throw new Error('useLanguage precisa estar dentro de <LanguageProvider>')
  }
  return ctx
}
