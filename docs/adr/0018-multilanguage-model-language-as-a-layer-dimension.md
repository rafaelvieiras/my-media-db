# ADR-0018 — Multilanguage model: the language dimension inside the layers

- **Status:** Accepted
- **Date:** 2026-07-09
- **Agenda:** [L1](../00-decision-agenda.md) · **Spec:** [04](../04-data-model.md)

## Context

The model assumed **one value per editorial field**. But the collection is
multilanguage: the owner defines a **primary language**, a **fallback** and the
set of **allowed languages**; every value obtained during scraping or edited by
hand carries the **flag of the language** it is in, and the owner can add values
in other languages (found only English → store English; later add Portuguese).
The language dimension was missing from the model of the three layers
(`file ⟵ external ⟵ override`).

## Decision

**Translatable editorial fields are now resolved PER LANGUAGE, and the language
dimension is the KEY inside each layer — not a new table.**

- **Translatable** (per language): `title`, `description`, `tags` (and the like,
  such as `tagline`, when they arrive). **Neutral** (not translated):
  `original_title`, `original_language`, `release_date`, `external_ids`,
  technical facts (mediainfo) and, for now, `extension_data`.
- **External** (`media_external`): `payload_json` stores the source's
  translations (TMDB `/translations`) already tagged by language code; the
  provider delivers the canonical form as `language → value` (ADR-0020 for tags).
- **Override** (`media_override`): `override_json` is indexed by **field AND
  language** — e.g. `{"title": {"pt-BR": "…", "en": null}}`. The owner's authority
  (R6) applies **per language**.
- **File/core** (`entity.title`/`description`): it is the **neutral fallback**
  (title derived from the file/import), of undetermined language — the last
  resort in the chain (ADR-0019).
- **New column `entity.original_language`** (code of the work's original language,
  coming from the source) — the anchor of the fallback.
- **Config in SETTINGS in the database** (not in env): a generic table
  `app_settings(key, value_json, updated_at)` with the key `languages` =
  `{primary, fallback, allowed[]}`, managed by an endpoint + **admin UI**. Codes in
  the BCP-47/TMDB style (`pt-BR`, `en`, `es`). The **allowed** ones govern what may
  be written/scraped; primary and fallback form the base of reading.

## Consequences

- The language flag is **intrinsic** (it is the key of the layer) — no table
  explosion and no language column per value.
- **Per-language resolution** and **provenance** live in [ADR-0019](0019-per-language-resolution-fallback-and-provenance.md);
  **tag translation** in [ADR-0020](0020-translation-of-genres-and-keywords-tags.md).
- Cost: resolver and provider redesigned; schema migration (settings +
  `original_language`). **Multilanguage FTS** stays as a follow-up (the MVP indexes
  the neutral/primary value).
- Related: [R1](0001-resolver-partition-by-origin.md),
  [R4](0004-override-as-sparse-json-blob.md),
  [R6](0006-key-presence-signals-override.md),
  [MD4](0011-external-ids-authoritative-media-external-cache.md).
