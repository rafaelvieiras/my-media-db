# ADR-0001 — Resolver: fields partitioned by origin

- **Status:** Accepted
- **Date:** 2026-07-06
- **Agenda:** [R1](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

The resolver composes three layers (`override ⟵ external ⟵ file`). One point was
still open: whether the layers compete indiscriminately for every field or
whether the field space is partitioned by origin — and where that rule lives.

## Decision

Partition by origin, with origin being **metadata of the field**, not a
hard-code. The core declares the origin in the core schema; extensions/plugins
declare it in the JSON Schema of their own namespace. The resolver is a **generic
engine** that does not know fields by name. The annotation declares only the
**base sources** (`file`/`external`/`none`); the **override is universal** (never
declared). System fields (`id`, `created_at`, `content_hash`) stay outside the
resolver.

## Consequences

- Adding a vertical/plugin touches neither the core nor the resolver.
- The resolver stays small and stable; the complexity moves to the schemas.
- Basis for [ADR-0004](0004-override-as-sparse-json-blob.md) (validatable blob) and
  [ADR-0005](0005-uniform-composition-through-extension-data.md) (uniform
  composition).
