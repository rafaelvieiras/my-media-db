# ADR-0013 — Override of relations (cast/crew) by operations

- **Status:** Accepted
- **Date:** 2026-07-08
- **Agenda:** [MD6](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

[ADR-0003](0003-list-override-by-operations.md) covered lists of simple values, but
deferred cast/crew because they are **relations** (`person`/`credit` tables), far more
complex.

## Decision

**Generalize the by-operations merge to structured credits.** The override stores
**add/remove/edit** (role, character, billing `ord`); the external base (the payload's
`credit`) stays alive and evolves on re-scrape. The **credit key** is `person_id` +
**role/department** (the same person in two roles = two credits), with the provider's
`credit_id` as tiebreaker. A person added by the owner becomes a **local** `person`
(empty `external_ids`, fine per [ADR-0011](0011-external-ids-authoritative-media-external-cache.md)).

## Consequences

- The resolver composes **credit by credit**, like any field
  ([ADR-0005](0005-uniform-composition-through-extension-data.md)); the ops live in
  `media_override` ([ADR-0004](0004-override-as-sparse-json-blob.md)).
- Closes the resolver spec for relations.
