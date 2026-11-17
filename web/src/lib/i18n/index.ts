// Public entry point of the UI i18n layer (ADR-0032), so a screen imports
// everything from one place: `import { useT, useLocale } from '@/lib/i18n'`.
export {
  CATALOGS,
  FALLBACK_LANGUAGE,
  STORAGE_KEY,
  UI_LANGUAGES,
  UI_LANGUAGE_LABELS,
  interpolate,
  matchUiLanguage,
  readStoredLanguage,
  resolveUiLanguage,
  translate,
} from './catalog'
export type {
  Key,
  MessageParams,
  Messages,
  TypedTranslate,
  UiLanguage,
} from './catalog'
export {
  I18nProvider,
  useI18n,
  useLocale,
  useT,
  useUiLanguage,
} from './provider'
