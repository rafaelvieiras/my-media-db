# ADR-0019 — Per-language resolution: fallback chain + provenance

- **Status:** Accepted
- **Date:** 2026-07-09
- **Agenda:** [L2](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md), [05](../05-api.md)

## Context

With per-language values in the layers ([ADR-0018](0018-multilanguage-model-language-as-a-layer-dimension.md)),
reading needs a **deterministic** rule: which language serves each field and what
the API exposes about it.

## Decision

**Reading resolves to ONE language per request, with a fallback chain and the
language provenance exposed.**

- **One language per request:** `GET /entities/{id}?lang=<code>` (default = the
  **primary** one from settings). The resolved shape stays **FLAT** — every
  translatable field becomes a single value (stable signature, the UI does not
  change shape).
- **Per-field fallback chain:** `requested language → fallback → original_language →
  neutral base (file)`. The **first step with a value** wins, respecting within each
  language the precedence `override ⟵ external ⟵ file`.
- **Owner authority per language** (R6 per language): if the override has the key
  for the requested language — **even `null`** — it decides; `null` = *deliberately
  set to empty* and it **interrupts the fallback** (does not fall through to the
  next language). Key absent for that language = the chain continues.
- **Provenance exposed:** the response includes a map `field → effective language`
  (e.g. `"resolved_languages": {"title": "pt-BR", "description": "en"}`) and echoes
  the requested `lang`. It is **LANGUAGE** provenance (so the UI can say "this
  synopsis is in English") — distinct from the **layer** provenance that R6 decided
  not to expose.
- **`Resolver(Layers) Entity` signature preserved:** `Layers` carries the language
  context (requested language + chain derived from settings).

## Consequences

- Current consumers keep the flat shape; the UI only passes `lang`. The **editor**
  reads the raw per-language values (override + external) to edit each language.
- Cost: **two-dimensional** composition (layer × language). **Per-language
  search/FTS** stays a follow-up.
- Related: [ADR-0018](0018-multilanguage-model-language-as-a-layer-dimension.md),
  [R2](0002-priority-between-external-sources.md),
  [R6](0006-key-presence-signals-override.md).
