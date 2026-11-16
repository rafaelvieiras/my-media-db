// Language labels — single source for the selector, the settings admin and the
// fallback badge on the detail page. The code→human-readable name map (pt-BR) translates the
// most common BCP-47/TMDB codes; `rotuloIdioma` falls back to the code itself when
// it does not know the language (never breaks, never hides the real code).

const NOMES: Record<string, string> = {
  'pt-BR': 'Português (BR)',
  pt: 'Português',
  en: 'Inglês',
  es: 'Espanhol',
  fr: 'Francês',
  de: 'Alemão',
  it: 'Italiano',
  ja: 'Japonês',
  ko: 'Coreano',
  zh: 'Chinês',
  ru: 'Russo',
  nl: 'Holandês',
  sv: 'Sueco',
  pl: 'Polonês',
}

// Candidate codes offered in the admin multi-select (display order).
// The screen merges this list with the codes already allowed so it never loses an exotic
// code that is already saved in the settings.
export const IDIOMAS_DISPONIVEIS: string[] = Object.keys(NOMES)

// Human-readable name of the language; falls back to the code itself when unknown.
export function rotuloIdioma(codigo: string): string {
  return NOMES[codigo] ?? codigo
}
