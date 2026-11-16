# ADR-0005 — Uniform composition (includes `extension_data`)

- **Status:** Accepted
- **Date:** 2026-07-06
- **Agenda:** [R5](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

It remained to define whether the `extension_data` fields (verticals) go through
the resolver like the core ones, or whether they would get their own treatment.

## Decision

**Yes, uniform composition.** Core and extension go through the same resolver:
`override ⟵ external ⟵ file` applies field by field, **including inside the
namespaces of `extension_data`**. The override patch mirrors the whole entity,
extension included.

## Consequences

- A new vertical gets override/enrichment "for free", with no code in the resolver.
- Follows from [ADR-0001](0001-resolver-partition-by-origin.md) (generic engine)
  and [ADR-0004](0004-override-as-sparse-json-blob.md) (blob mirrors the entity).
