// Types of the UI i18n layer (ADR-0032).
//
// The catalog is nested objects per domain, and the English one is the source of
// truth: `defineDomain` forces the three locales of a domain to have exactly the
// same shape, so a missing or extra key is a compile error, not a runtime
// fallback that ships a blank label.

export const UI_LANGUAGES = ['en', 'pt-BR', 'es'] as const

export type UiLanguage = (typeof UI_LANGUAGES)[number]

// A domain file holds the three locales side by side. Inference takes the shape
// from `en`, and the excess property check rejects an extra key in `pt-BR`/`es`.
export type Domain<T> = Record<UiLanguage, T>

export function defineDomain<T extends object>(domain: Domain<T>): Domain<T> {
  return domain
}

// `leaf` is a path to a string leaf, e.g. `nav.library.search`.
type Leafs<T> = T extends string
  ? ''
  : {
      [K in keyof T & string]: T[K] extends string ? K : `${K}.${Leafs<T[K]>}`
    }[keyof T & string]

export type MessageKey<T> = Leafs<T>

export type MessageParams = Record<string, string | number>

export type Translate = (key: string, params?: MessageParams) => string
