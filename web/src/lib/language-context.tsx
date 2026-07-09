import {
  createContext,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useQuery } from '@tanstack/react-query'
import { languageSettingsQueryOptions } from '@/lib/queries'

// Estado GLOBAL LEVE do idioma de leitura (ADR-0019 / briefing 0021): não é um
// search param por rota, e sim um context semeado pelo `primary` das settings e
// persistido em localStorage. O seletor do AppHeader o altera; as queries de
// leitura passam o `lang` no `?lang=` e na queryKey — trocar o idioma re-busca
// e recompõe sem recarregar a página.

// Default seguro enquanto as settings carregam (nunca quebra).
const IDIOMA_PADRAO = 'pt-BR'
const CHAVE_STORAGE = 'mmdb.lang'

export interface LanguageContextValue {
  // Idioma efetivo de leitura (sempre ∈ allowed, ou o principal).
  lang: string
  // Troca o idioma e persiste em localStorage.
  setLang: (lang: string) => void
  // Idiomas permitidos (fonte: GET /settings/languages).
  allowed: string[]
  // Idioma principal das settings (semente/queda).
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

  // `escolhido` guarda o que o usuário salvou (ou tinha salvo); `null` = seguir
  // a semente (principal). É lido do localStorage uma vez, na montagem.
  const [escolhido, setEscolhido] = useState<string | null>(() => lerSalvo())

  const value = useMemo<LanguageContextValue>(() => {
    const primary = data?.primary ?? IDIOMA_PADRAO
    const allowed = data?.allowed ?? [IDIOMA_PADRAO]
    // Idioma efetivo: o escolhido só vale se ainda ∈ allowed; senão cai no
    // principal (critério: "cai no principal quando o salvo não é permitido").
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
          // localStorage indisponível (ex.: modo privado) — o estado em memória
          // ainda funciona nesta sessão.
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

// Hook de consumo. Convive com o Provider neste arquivo (a regra de fast-refresh
// só afeta HMR em dev; o par Provider+hook num só módulo é o padrão idiomático).
// eslint-disable-next-line react-refresh/only-export-components
export function useLanguage(): LanguageContextValue {
  const ctx = useContext(LanguageContext)
  if (!ctx) {
    throw new Error('useLanguage precisa estar dentro de <LanguageProvider>')
  }
  return ctx
}
