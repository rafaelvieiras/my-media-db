# ADR-0002 — Priority between external sources (global list)

- **Status:** Accepted
- **Date:** 2026-07-06
- **Agenda:** [R2](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

With more than one external source (e.g.: TMDB + TVDB) bringing a value for the
same field, a tie-breaking criterion was needed in the external layer, before
the override.

## Decision

Use a **global, configurable priority list** of sources (e.g.:
`[tmdb, tvdb, musicbrainz]`). Per field, the **first source in the list that has
a value** wins; the result forms the external base, over which the override comes
in. **MVP default:** `[tmdb]`.

## Consequences

- Predictable behaviour, configurable by the owner, with no per-field rule in
  the MVP.
- Per-field priority and "primary source per entity" stay as a post-MVP evolution,
  without breaking the model.
- Independent of the override, which always wins
  ([ADR-0001](0001-resolver-partition-by-origin.md)).
