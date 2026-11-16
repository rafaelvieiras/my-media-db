// SINGLE map of URL slug (friendly, English) ↔ `entity.type` (internal schema)
// for the "top-level" types (directly browsable: home, navbar, `/library/:slug`
// — see ADR-0030). Consumed by `RecentlyAddedRail` ("Ver Todos" link),
// `BibliotecaNavItem` (navbar dropdown) and the route `/library/$entitySlug`
// (`routes/library-browse.tsx`) — a new top-level type (future vertical) only
// needs ONE entry here, never spread across the three consumers.
export const LIBRARY_SLUG_TO_TYPE: Record<string, string> = {
  movies: 'movie',
  series: 'tv',
  artists: 'artista',
}

const TYPE_TO_LIBRARY_SLUG: Record<string, string> = Object.fromEntries(
  Object.entries(LIBRARY_SLUG_TO_TYPE).map(([slug, type]) => [type, slug]),
)

// slugParaTipo resolves a URL slug into the corresponding `entity.type`.
// `undefined` when the slug is not mapped — the route `/library/:entitySlug`
// treats that as a coherent empty state, never a hard 404 nor a crash.
export function slugParaTipo(slug: string): string | undefined {
  return LIBRARY_SLUG_TO_TYPE[slug]
}

// tipoParaSlug resolves an `entity.type` into the corresponding URL slug — used
// by the "Ver Todos" link of the carousels and by the "Biblioteca" dropdown of the navbar.
// A type without a mapped slug (a sub-node that eventually leaked up to here, a defensive
// scenario — today `GET /entities/types` already excludes sub-nodes) returns
// `undefined`; the caller decides not to render the link in that case.
export function tipoParaSlug(type: string): string | undefined {
  return TYPE_TO_LIBRARY_SLUG[type]
}
