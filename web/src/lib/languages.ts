// Language labels — single source for the language control, the settings admin and
// the fallback badge on the detail page.
//
// The names are ENDONYMS (each language written in itself): "English", "Português
// (Brasil)", "Español". That is what TMDB and every catalog does, and it means the
// list reads the same no matter which interface language is active (ADR-0032) —
// the alternative, translating "Inglês"/"English"/"Inglés", makes the reader hunt
// for their own language in a list that is already in a language they may not read.
// `rotuloIdioma` falls back to the code itself for a language the map does not
// know (never breaks, never hides the real code).

const NOMES: Record<string, string> = {
  'pt-BR': 'Português (Brasil)',
  pt: 'Português',
  en: 'English',
  es: 'Español',
  fr: 'Français',
  de: 'Deutsch',
  it: 'Italiano',
  ja: '日本語',
  ko: '한국어',
  zh: '中文',
  ru: 'Русский',
  nl: 'Nederlands',
  sv: 'Svenska',
  pl: 'Polski',
}

// Candidate codes offered in the admin multi-select (display order).
// The screen merges this list with the codes already allowed so it never loses an exotic
// code that is already saved in the settings.
export const IDIOMAS_DISPONIVEIS: string[] = Object.keys(NOMES)

// Human-readable name of the language; falls back to the code itself when unknown.
export function rotuloIdioma(codigo: string): string {
  return NOMES[codigo] ?? codigo
}
