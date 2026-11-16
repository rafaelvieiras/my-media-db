# 05 — API

> **Status:** scoped stub. The closed decisions are listed here. Routes, payloads,
> and auth are detailed as the Fase 4 work lands.

## Principles

- **Two namespaces, separated on purpose:**
  - **`/api/tmdb/*`** — **TMDB-compatible** layer. The goal: point a client that
    already speaks TMDB (Jellyfin, Kodi) at the server by changing only the base
    URL. Nothing outside the TMDB scope belongs here.
  - **`/api/v1/*`** — **native** API, which owns everything TMDB does not model:
    override, library, mediainfo, custom assets, users.
- **Always serve from the resolver** (see [03](03-architecture.md) and
  [04](04-data-model.md)): responses reflect the owner's catalog **with the
  overrides applied**, never the raw external record. **One explicit exception:**
  the `/api/tmdb` passthrough below, which serves external data when the item is
  not local.
- **Versioned** for contract stability (`v1`, `v2`…).
- **Type contract through generated OpenAPI.** The API describes itself in
  **OpenAPI**, which is the source of truth for the front-end TypeScript types.
  They are generated, never hand-maintained, so the two sides cannot drift. See
  [ADR-0014](adr/0014-implementation-stack.md).

## `/api/tmdb`: passthrough with cache, never a 404

When a compatible client (Jellyfin, Kodi) calls `/api/tmdb` and the item does not
exist locally, the layer **never answers 404 because the API is unavailable**. It
queries the real TMDB, caches the payload, and serves it. Three rules:

1. **Local first.** If the item exists in the catalog, serve it from the
   **resolver**, with the override, like any other route. The passthrough only
   applies when there is no local item.
2. **Miss, then passthrough and cache.** Without a local item, fetch from the real
   TMDB and **cache the payload**. What happens next depends on the call type and
   the configuration:
   - **Search** → passthrough and cache only. It **does not** create an entry, so
     the database does not fill with every search result.
   - **Detail** (fetch of a specific id) → according to the configured mode:
     - **`promote_draft`** creates a `draft` entry and puts it in the **review
       queue**. An auto-created item with no investment from the owner is an
       **orphan** candidate if nobody curates it (see
       [MD1](00-decision-agenda.md) and [04](04-data-model.md)). From then on it
       flows through the resolver like any entity.
     - **`hidden_cache`** keeps it **in a hidden cache only**, never as an entity,
       revalidated every X (TTL). It serves compatibility without dirtying the
       catalog.
3. **External API failure serves the stale cache, never a 404.** If TMDB is
   unreachable, answer with the existing cache, even expired, instead of an error.
   A 404 is reserved for the item that TMDB itself says does not exist, never for
   unavailability.

> Config: `tmdb_passthrough` with `enabled` (default **on**), `on_detail`
> (`promote_draft` or `hidden_cache`), and the revalidation TTL. Decision in
> [ADR-0015](adr/0015-tmdb-api-passthrough.md).

## Planned native surface

- `/api/v1/library` — library sources, scan, status, missing and duplicate items.
- `/api/v1/entities/{id}` — resolved entity, with override.
- `/api/v1/entities/{id}/override` — edit fields, the owner's override.
- `/api/v1/entities/{id}/files` — files and mediainfo.
- `/api/v1/entities/{id}/assets` — artwork, upload and serve.
- `/api/v1/search` — local-first search.
- `/api/v1/enrich` — trigger on-demand enrichment.
- `/api/v1/users`, `/api/v1/tokens` — multiuser, Fase 4.

## Standard error shape of `/api/v1`

Every error response uses the same envelope:

```json
{ "error": { "code": "<stable slug>", "message": "<human readable text>" } }
```

- `code` is a **stable, non-localized slug** so the client can branch. `message`
  is the readable text. Content type is `application/json`.
- A success serializes the resource directly, with no envelope. Lists use
  `{ "items": [...] }`.
- Codes in use: `invalid_body` (400), `invalid_type`, `invalid_path`,
  `not_a_directory`, `path_unreadable` (422), `duplicate_path` (409), `not_found`
  (404), `internal_error` (500). The generic `/api` 404 for an unknown route uses
  the same shape.

## `/api/v1/library`: library sources

- **`POST /api/v1/library`** — body `{ "path": "...", "type": "movie"|"tv" }`.
  Normalizes the path (absolute and clean), checks that it exists, is a directory,
  and is readable, then creates the source with `state=online` and a null
  `last_scan`. Replies `201` with the record, `422` for an invalid or unreadable
  path, `409` for a duplicate path after normalization, and `400` for a bad body.
- **`GET /api/v1/library`** — `200 { "items": [ { id, path, type, state, last_scan } ] }`.
  `state` is **re-evaluated on read** with a cheap stat call and is not persisted.
- **`GET /api/v1/library/{id}`** — `200` with `state` re-evaluated, or `404`.
- **`DELETE /api/v1/library/{id}`** — removes only the source record, `204` or
  `404`. It does not purge entities.

## Open decisions (see [11](11-open-decisions.md))

- The exact set of TMDB routes to mirror: movie, tv, search, images.
- The auth header and token scheme, in Fase 4 (see
  [10](10-users-and-permissions.md)).

> **Closed:** the **generated OpenAPI** contract and the `/api/tmdb` policy of
> **passthrough plus cache, never a 404**. See the ADRs above.

## `review_group` on the entity card

`GET /api/v1/review` annotates each card with `review_group`
(`{kind, key, label, size}`): the **file** context that explains why the item is in
the queue. Kinds are `album` for tracks, ignoring `Disc N`; `serie` for episodes,
ignoring `Season N`; `pasta` for movies; and `sem_arquivo` for an imported item.
It is a presentation annotation, the same shape as `ownership` and `numbering`. It
**does not** change the queue predicate and never appears in other card listings.
