# ADR-0011 — `external_ids` authoritative, `media_external` as cache

- **Status:** Accepted
- **Date:** 2026-07-07
- **Agenda:** [MD4](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

The same external id appears in `entity.external_ids` and in
`media_external.(source, source_id)`. Without electing the authority, the two copies
can diverge.

## Decision

**`entity.external_ids` is the authoritative identity map** — every known id (fetched
or not); it is where matching writes and lookup reads. **`media_external` is a payload
cache** per fetched source, with `source_id` **subordinate** (invariant:
`media_external.source_id == external_ids[source]`). **Conflict → identity wins**: the
divergent cache row is stale (re-fetch/discard). The owner fixes **the identity** and
**never edits `media_external` by hand**. Same logic for `person`/`company`.

## Consequences

- Identity (which external record) and editorial value (`media_external` as the base
  for the resolver) remain distinct axes.
- Lookup by external id through a generated column + index.
- A person added by the owner is born **local** (empty `external_ids`) — used in
  [ADR-0013](0013-relation-override-by-operations.md).
