# ADR-0032 — UI i18n: three catalogs, no library, interface language apart from reading language

- **Status:** Accepted
- **Date:** 2026-11-16
- **Agenda:** [#32](https://github.com/rafaelvieiras/my-media-db/issues/32) · **Spec:** [03-architecture](../03-architecture.md)

## Context

The interface copy is hardcoded Portuguese, while `AGENTS.md` states that UI copy
is English. Two things were confused under "language" until now:

- **reading language** — which language the metadata is served in. It is content:
  it lives in the database, has a fallback chain and provenance
  ([ADR-0019](0019-per-language-resolution-fallback-and-provenance.md)), and the
  owner picks it from `allowed` in `GET /settings/languages`;
- **interface language** — the language of navigation, buttons and labels. It is a
  browser preference and has nothing to do with the catalog.

Only the first existed. The second cannot be solved by the backend: sending a
`?lang=` does not translate the button that opens the screen.

Constraints: the front end is small and local-first (principle 4 — one binary, low
memory); the web bundle is served from the same binary, so a heavy i18n runtime
with plural rules, ICU parsing and lazy chunk loading costs more than the problem.

## Decision

1. **Three catalogs, English is the source of truth.** `en` is the default and the
   reference for typing; `pt-BR` and `es` are typed against it, so a missing or
   extra key is a `tsc` error, not a runtime fallback. No i18n library: a flat
   `Record<string, string>` per locale, `{param}` interpolation, and explicit
   `_one`/`_other` keys where a count changes the sentence. `Intl.*` does the rest.
2. **Interface language is a browser preference**, resolved in this order:
   `localStorage` → `navigator.language` when it matches a supported locale →
   `en`. It never writes to the database and never changes the reading language.
3. **One control, two sections.** The header shows a single language button that
   opens the interface choice and, below it, the reading language — so the two are
   discoverable together without being the same setting.
4. **Formatting follows the interface locale.** `Intl.NumberFormat` /
   `Intl.DateTimeFormat` take the active locale; the hardcoded `pt-BR` leaves the
   formatting helpers.
5. **No user-visible string stays in a component.** A component renders text it
   received from `t()`. Literals that are data (entity types, provider names,
   slugs, API enums) stay where they are — they are not copy.

## Consequences

- The chrome can be read in three languages, and adding a fourth means adding one
  file plus a `LOCALES` entry — no build change.
- The parity guarantee comes from the type system instead of a runtime check; a
  test still walks the catalogs, because `as const` on a nested object can drift
  when someone casts.
- Tests assert the English default; the pt-BR and es catalogs are covered by the
  parity test and by one rendering test per locale, not by every screen test.
- Cost accepted: no ICU plural rules. A sentence that needs plural agreement uses
  two keys and the caller picks — enough for a catalog of this size, and the
  reason a fourth language with complex plurals would need a real runtime.
- Related: [ADR-0018](0018-multilanguage-model-language-as-a-layer-dimension.md),
  [ADR-0019](0019-per-language-resolution-fallback-and-provenance.md). The reading
  language keeps its own decision; this ADR does not touch the resolver.
