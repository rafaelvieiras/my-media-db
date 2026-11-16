# ADR-0030 — Top-level types: generic library, search and navigation driven by schema

- **Status:** Accepted
- **Date:** 2026-08-19
- **Agenda:** see [12-roadmap](../12-roadmap.md) · **Spec:** [AGENTS.md](../../AGENTS.md) §3

## Context

The home page (`RecentlyAddedSection`) and the navbar (`BibliotecaNavItem`) already
iterate over `GET /entities/types` without hardcoding the type (the carousels and the
navbar dropdown are already generic) — but the backend behind that has **two**
divergent hardcoded sub-node exclusions: `ListarTiposComContagem`
(`internal/db/queries/entity.sql`, used by `/entities/types`) excludes only
`season`/`episode`; `buscarCardsFTSSQL` (`internal/server/search.go`, used by
`/search`) makes the same exclusion, separately. Neither covers `album`/`faixa`
(sub-nodes of the artist tree, ADR-0027) — result: if there is a cataloged
track/album, each one gets its own carousel on the home page, its own entry in the
navbar dropdown and shows up loose in search, which makes no sense (a track only
exists hanging off an album, an album only exists hanging off an artist). The
frontend, in turn, has a real `movie|tv` hardcode in `MediaTypeFilter` (shared by
`PosterGrid` and `SearchPanel`) that locks the type filter to two fixed values. The
home page mixes, on the same screen, "recent" carousels with a full paginated
listing (`PosterGrid`) tied to that same binary filter.

That contradicts §3 of AGENTS.md ("universal and extensible without touching the
core... new schema... no core migration") in practice: every new schema
(music already showed it) requires touching multiple hardcoded points to behave
properly in navigation.

## Decision

1. **Single registry, in Go, of "top-level types"** (directly browsable: home,
   navbar, `/library/:entitySlug`, search by default) vs. **"sub-nodes"** (only
   reachable through drill-down from the parent — `season`, `episode`, `album`, `faixa`).
   The two points that diverge today (`ListarTiposComContagem`,
   `buscarCardsFTSSQL`) start querying that **same** source — a new schema
   only needs an entry in it (or stays out, if it is born a sub-node), never an
   edit to the two queries separately.
2. **`/search` becomes generic** over all top-level types by default
   (accepting a free `type` for explicit search/inclusion of a sub-node via
   `include`, the same pattern `season`/`episode` already use), with structured
   filters equivalent to the ones `/entities` already exposes (type, ownership,
   ordering) instead of the frontend's fixed enum.
3. **The home page shows only top-level type carousels** — the full/paginated
   listing (`PosterGrid`) leaves the home page.
4. **New route `/library/:entitySlug`** for the full library filtered
   by one top-level type at a time (friendly slug mapped to `entity.type`:
   `movies`→`movie`, `series`→`tv`, `artists`→`artista`). Each home
   carousel gets a "See All" link to that route.

## Consequences

- Finally closes "a new schema automatically enters search and navigation"
  — the next vertical (music video, for example) requires no touching of `/search`
  nor of the home page, only declaring its type as top-level or sub-node in the
  single registry.
- Reduces two hardcoded (and divergent) sources of truth to one.
- Changes the public URL contract of the filtered listing: from `/?type=X`
  (search param of the `/` route) to `/library/:entitySlug` — the implementation
  decision decides whether the old route becomes a redirect or is removed.
- `MediaTypeFilter` (frontend) stops being a fixed enum of two values and
  starts deriving the options from the same `GET /entities/types` that the navbar and
  the home page already query.
- Related decisions: [AGENTS.md](../../AGENTS.md) §3 (universal without touching
  the core), [ADR-0029](0029-local-first-music-import-per-owned-track.md)
  (the original reason that exposed the gap: album/track leaking to the top
  level), the already generic frontend carousels/dropdown.
