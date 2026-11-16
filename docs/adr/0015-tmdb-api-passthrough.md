# ADR-0015 — `/api/tmdb`: passthrough with cache, never 404

- **Status:** Accepted
- **Date:** 2026-07-08
- **Agenda:** [doc 11, item 6](../11-open-decisions.md) · **Spec:** [05](../05-api.md)

## Context

The compatible layer `/api/tmdb` serves clients such as Jellyfin/Kodi. It was still
undefined what to answer when the queried item **does not exist in the local catalog**:
`404`, or fetch from the real TMDB. A `404` for unavailability would break the client.

## Decision

**Passthrough + cache, never a 404 for unavailability.**

1. **Local-first:** if a local item exists, serve from the resolver (with override).
2. **Miss → passthrough + cache**, with the destination depending on the call type and
   the config:
   - **Search (`search`)** → cache only; does **not** create an entry.
   - **Detail (specific id)** → configurable mode: **`promote_draft`** creates a `draft`
     entry in the **review queue** (auto-created item, an orphan candidate per
     [ADR-0012](0012-non-destructive-lifecycle.md)/[ADR-0008](0008-identity-and-matching-kodi-jellyfin.md)),
     or **`hidden_cache`** keeps it only in a hidden cache, revalidated by TTL.
3. **External API failure → serve (stale) cache**, never an error. The `404` is
   reserved for the item that TMDB itself says **does not exist**, never for
   unavailability.

Config `tmdb_passthrough`: `enabled` (default **on**), `on_detail`
(`promote_draft` | `hidden_cache`), revalidation TTL.

## Consequences

- Existing clients get an "infinite" catalog without breaking when the source goes
  down.
- It is the **explicit exception** to the "always serve from the resolver" principle
  (doc 05): the passthrough serves raw external data in TMDB form.
- `promote_draft` connects compatibility to curation (draft → resolver → override); the
  owner controls whether they want the database populated or only cached.
