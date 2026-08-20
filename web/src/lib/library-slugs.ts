// Mapa ÚNICO slug de URL (amigável, inglês) ↔ `entity.type` (schema interno)
// pros tipos "de topo" (browsable diretamente: home, navbar, `/library/:slug`
// — ver ADR-0030). Consumido por `RecentlyAddedRail` (link "Ver Todos"),
// `BibliotecaNavItem` (dropdown da navbar) e a rota `/library/$entitySlug`
// (`routes/library-browse.tsx`) — um tipo de topo novo (vertical futura) só
// precisa de UMA entrada aqui, nunca espalhada pelos três consumidores.
export const LIBRARY_SLUG_TO_TYPE: Record<string, string> = {
  movies: 'movie',
  series: 'tv',
  artists: 'artista',
}

const TYPE_TO_LIBRARY_SLUG: Record<string, string> = Object.fromEntries(
  Object.entries(LIBRARY_SLUG_TO_TYPE).map(([slug, type]) => [type, slug]),
)

// slugParaTipo resolve um slug de URL pro `entity.type` correspondente.
// `undefined` quando o slug não está mapeado — a rota `/library/:entitySlug`
// trata isso como estado vazio coerente, nunca 404 hard nem crash.
export function slugParaTipo(slug: string): string | undefined {
  return LIBRARY_SLUG_TO_TYPE[slug]
}

// tipoParaSlug resolve um `entity.type` pro slug de URL correspondente — usado
// pelo link "Ver Todos" dos carrosséis e pelo dropdown "Biblioteca" da navbar.
// Um tipo sem slug mapeado (sub-nó que eventualmente vazasse até aqui, cenário
// defensivo — hoje `GET /entities/types` já exclui sub-nós) devolve
// `undefined`; quem chama decide não renderizar o link nesse caso.
export function tipoParaSlug(type: string): string | undefined {
  return TYPE_TO_LIBRARY_SLUG[type]
}
