// The catalog and everything that does not need React (ADR-0032): the three
// locales of every domain, the pure lookup, the language resolution and the types
// the pure helpers share.
//
// It lives in its own module (not in the provider `.tsx`) so the component file
// exports only a component and hooks — `react-refresh/only-export-components`
// refuses a file that mixes a component with constants and plain functions.
import { collections } from './collections'
import { common } from './common'
import { entity } from './entity'
import { jobs } from './jobs'
import { library } from './library'
import { media } from './media'
import { nav } from './nav'
import { queue } from './queue'
import { review } from './review'
import { settings } from './settings'
import {
  UI_LANGUAGES,
  type MessageKey,
  type MessageParams,
  type UiLanguage,
} from '../types'

export { UI_LANGUAGES, type MessageParams, type UiLanguage }

// One entry per locale, each a tree of domains. `pt-BR` and `es` are typed against
// `en` inside every domain file (`defineDomain`), so the shapes cannot drift.
export const CATALOGS = {
  en: {
    common: common.en,
    nav: nav.en,
    library: library.en,
    entity: entity.en,
    review: review.en,
    settings: settings.en,
    jobs: jobs.en,
    queue: queue.en,
    collections: collections.en,
    media: media.en,
  },
  'pt-BR': {
    common: common['pt-BR'],
    nav: nav['pt-BR'],
    library: library['pt-BR'],
    entity: entity['pt-BR'],
    review: review['pt-BR'],
    settings: settings['pt-BR'],
    jobs: jobs['pt-BR'],
    queue: queue['pt-BR'],
    collections: collections['pt-BR'],
    media: media['pt-BR'],
  },
  es: {
    common: common.es,
    nav: nav.es,
    library: library.es,
    entity: entity.es,
    review: review.es,
    settings: settings.es,
    jobs: jobs.es,
    queue: queue.es,
    collections: collections.es,
    media: media.es,
  },
} as const

export type Messages = (typeof CATALOGS)['en']
export type Key = MessageKey<Messages>

// Overloaded on purpose: the literal `Key` signature gives autocomplete for the
// keys of the catalog, and the `string` one keeps the function assignable to the
// `Translate` type the pure helpers (`lib/entity-badges.ts` and friends) receive.
export type TypedTranslate = {
  (key: Key, params?: MessageParams): string
  (key: string, params?: MessageParams): string
}

// Endonyms: the name of a language is written the same way in every interface
// language, so this map is not translated.
export const UI_LANGUAGE_LABELS: Record<UiLanguage, string> = {
  en: 'English',
  'pt-BR': 'Português (Brasil)',
  es: 'Español',
}

export const STORAGE_KEY = 'mmdb.ui-lang'
export const FALLBACK_LANGUAGE: UiLanguage = 'en'

// Maps any BCP-47 tag to one of the supported locales (`pt-PT` and `es-419`
// included), or `null` when there is no match.
export function matchUiLanguage(
  tag: string | null | undefined,
): UiLanguage | null {
  if (!tag) {
    return null
  }
  const normalized = tag.toLowerCase()
  if (normalized.startsWith('pt')) {
    return 'pt-BR'
  }
  if (normalized.startsWith('es')) {
    return 'es'
  }
  if (normalized.startsWith('en')) {
    return 'en'
  }
  return null
}

// Pure resolution of the initial language, exported so a test can walk the whole
// order without touching `localStorage`.
export function resolveUiLanguage(
  stored: string | null | undefined,
  browser: string | null | undefined,
): UiLanguage {
  return (
    matchUiLanguage(stored) ?? matchUiLanguage(browser) ?? FALLBACK_LANGUAGE
  )
}

// Reads the stored choice, tolerating a browser that blocks storage (private mode).
export function readStoredLanguage(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

function lookup(messages: unknown, key: string): string | undefined {
  const value = key.split('.').reduce<unknown>((node, part) => {
    if (typeof node !== 'object' || node === null) {
      return undefined
    }
    return (node as Record<string, unknown>)[part]
  }, messages)
  return typeof value === 'string' ? value : undefined
}

// `{name}` placeholders are replaced by the params; an unknown placeholder stays
// visible instead of turning into "undefined".
export function interpolate(template: string, params?: MessageParams): string {
  if (!params) {
    return template
  }
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in params ? String(params[name]) : match,
  )
}

// Translation without React, with English as the last resort so a half-filled
// catalog never renders an empty label.
export function translate(
  lang: UiLanguage,
  key: string,
  params?: MessageParams,
): string {
  const template =
    lookup(CATALOGS[lang], key) ?? lookup(CATALOGS[FALLBACK_LANGUAGE], key)
  return template === undefined ? key : interpolate(template, params)
}
