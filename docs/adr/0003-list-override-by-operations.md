# ADR-0003 — List override through merge of operations

- **Status:** Accepted
- **Date:** 2026-07-06
- **Agenda:** [R3](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

Override on list fields (`tags`, `genres`) could replace the whole list or
merge. Replacing as a block would discard the evolution of the external base on
every re-scrape.

## Decision

**Merge through operations**, not replacement. The override keeps a set of
**additions** and a set of **removals by value**; the external base stays alive
underneath. The displayed order follows the external one, with removals applied
and additions at the end; dedup on composition. It applies **only to lists of
simple values** (`tags`, `genres`).

## Consequences

- A re-scrape that brings a new genre includes it without undoing the owner's edits.
- Cast/crew (relations) are **not** covered here — generalized in
  [ADR-0013](0013-relation-override-by-operations.md).
- The operations live in the override blob
  ([ADR-0004](0004-override-as-sparse-json-blob.md)).
