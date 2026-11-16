# ADR-0009 — Ownership and `status` as orthogonal axes

- **Status:** Accepted
- **Date:** 2026-07-07
- **Agenda:** [MD2](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

In doc 04, a single `status` conflated "I have the file" (fact) with "what I intend"
(intention) — the value `owned` was ownership disguised as intention.

## Decision

**Two orthogonal axes.** **Ownership = derived fact** (is there a present
`media_file`?), not editable, it does not lie. **`status` = the owner's intention**,
one state at a time: `cataloged` (default), `wanted`, `unreleased`, `draft`. `owned` is
removed. The axes combine freely (e.g.: `wanted` + ownership=yes = "I have a copy, I
want an upgrade").

## Consequences

- Sustains the "database larger than the library" without hacks.
- Ownership can become a generated column for fast filtering.
- The transitions when a file appears/disappears live in
  [ADR-0012](0012-non-destructive-lifecycle.md).
