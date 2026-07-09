// Rótulos de idioma — fonte única para o seletor, o admin de configurações e o
// selo de fallback do detalhe. O mapa código→nome legível (pt-BR) traduz os
// códigos BCP-47/TMDB mais comuns; `rotuloIdioma` cai no próprio código quando
// não conhece o idioma (nunca quebra, nunca esconde o código real).

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

// Códigos candidatos oferecidos no multi-select do admin (ordem de exibição).
// A tela une esta lista aos já permitidos para nunca perder um código exótico
// que já esteja salvo nas settings.
export const IDIOMAS_DISPONIVEIS: string[] = Object.keys(NOMES)

// Nome legível do idioma; fallback ao próprio código quando desconhecido.
export function rotuloIdioma(codigo: string): string {
  return NOMES[codigo] ?? codigo
}
