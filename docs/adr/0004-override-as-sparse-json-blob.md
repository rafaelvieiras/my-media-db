# ADR-0004 — Override as a sparse JSON blob

- **Status:** Accepted
- **Date:** 2026-07-06
- **Agenda:** [R4](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

Override storage could be EAV `(entity_id, field, value)` or a sparse JSON blob.
The original sketch of doc 04 used EAV, which makes it hard to validate by
schema and to represent list merges.

## Decision

**Sparse JSON blob**: `media_override(entity_id, override_json, updated_at)` — one
row per entity, JSON mirroring the entity format, only with the changed fields.
Validatable against the same schema (core + plugins); it accommodates the
`{add, remove}` of lists and override inside `extension_data`. The resolver builds
the composed base and applies the patch. **Replaces the EAV sketch of doc 04.**

## Consequences

- One read per entity brings the whole override; it matches the schema of
  [ADR-0001](0001-resolver-partition-by-origin.md).
- Supports [ADR-0003](0003-list-override-by-operations.md),
  [ADR-0005](0005-uniform-composition-through-extension-data.md) and
  [ADR-0006](0006-key-presence-signals-override.md).
