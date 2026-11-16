# ADR-0012 — Non-destructive lifecycle

- **Status:** Accepted
- **Date:** 2026-07-08
- **Agenda:** [MD5](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md), [06](../06-library-and-scan.md)

## Context

When a file disappears from disk, should the item become `wanted`? an orphan? The risk
is that dismantling a hard drive turns into thousands of items "vanishing", or that an
intention of the owner is rewritten by a filesystem event.

## Decision

**A file event touches only ownership; never `status` nor the owner's work.** File
disappears (datasource reachable) → `media_file` becomes **`missing`** (keeps
`last_seen`), entity preserved, ownership→no, `status` **intact**; reversible if it
reappears. **Whole datasource offline** (drive unmounted) → nothing becomes `missing`,
the datasource is flagged offline. **Orphan** = auto-created entity, file gone,
**zero owner investment** → cleanup queue, **never deleted on its own**. **Purge** is
the only destructive operation, always explicit.

## Consequences

- The absent disk is never confused with deletion.
- Consistent with [ADR-0009](0009-ownership-and-status-orthogonal-axes.md): the system
  does not rewrite intention.
- Purge removes entity + `media_external`/`media_override`/assets.
