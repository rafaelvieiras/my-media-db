# ADR-0020 — Tag translation (localized genres + keywords)

- **Status:** Accepted
- **Date:** 2026-07-09
- **Agenda:** [L3](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md), [07](../07-enrichment-and-providers.md)

## Context

**Tags** (genres/keywords) must be translated too. TMDB **localizes genres**
(via `language`), but **does not translate keywords** — that is where it falls
short. A single-language tags model does not serve a multilingual collection.

## Decision

**Tags become a list-field PER LANGUAGE, composed by merging operations within each
language.**

- **Shape:** `tags` becomes `language → list`. It composes by **merging operations**
  (R3) **within each language**: the external base for that language + the
  override's `{add, remove}` for that language; it resolves through the fallback
  chain of [ADR-0019](0019-per-language-resolution-fallback-and-provenance.md).
- **Genres:** use TMDB's **localized genre list** per allowed language
  (`GET /genre/{movie|tv}/list?language=`, **cacheable**), mapping the item's genre
  ids to the names in each language.
- **Keywords:** TMDB **does not translate** them — store them under the language
  obtained (or the original one); the owner translates them manually through the
  per-language override. **No automatic translation.**

## Consequences

- The collection gets **multilingual tags**, going beyond the TMDB gap.
- Cost: fetching/caching the genre lists per language; keywords stay **partial**
  (only in the language obtained) until the owner translates them by hand.
- Related: [R3](0003-list-override-by-operations.md),
  [ADR-0018](0018-multilanguage-model-language-as-a-layer-dimension.md),
  [ADR-0019](0019-per-language-resolution-fallback-and-provenance.md).
