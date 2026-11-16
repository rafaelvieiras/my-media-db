# ADR-0006 — Key presence signals override

- **Status:** Accepted
- **Date:** 2026-07-06
- **Agenda:** [R6](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

In a sparse patch it is necessary to distinguish "I did not override this field"
from "I overrode it to empty" (erasing the external value). Without that, there
is no way for the owner to silence a value that the source insists on bringing.

## Decision

The **presence of the key** in the patch is the authority signal: key present
(even with `null`/empty) → override active, it wins over the base, **including to
erase**. Key absent → inherits from the base. The UI distinguishes
**"clear override"** (removes the key) from **"set empty"** (key with `null`).
**Per-field provenance is not exposed in the API** for now.

## Consequences

- Deletion semantics with no extra column; the UI consults the override separately.
- Exposing provenance stays as an evolution (Phase 4).
- Depends on the sparse blob ([ADR-0004](0004-override-as-sparse-json-blob.md)).
