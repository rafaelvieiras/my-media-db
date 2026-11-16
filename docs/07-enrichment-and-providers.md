# 07 — Enrichment and providers

> **Status of this doc:** scoped stub. Locked decisions are listed; the deeper
> design detail lives in the [ADRs](adr/).

## Principles

- **Pull > Sync.** Nothing syncs on its own. The owner **pulls** from the external
  source when they want to ([scenario F](01-vision-and-use-cases.md)).
- **Local-first.** Searching queries the database first; only then the external
  source, whose results show up marked as **"não salvos"** until an explicit
  import.
- **Explicit import.** The owner decides what to persist. What is pulled becomes
  the `media_external` layer (base), **without touching** the override.
- **Local cache.** Pulled payloads and images are cached locally (see
  [08](08-artwork-and-assets.md)) so as not to depend on the source afterwards.

## Provider abstraction

A **provider** encapsulates an external source: search, detail fetch, mapping to
the `Entity`/`extension_data`. Providers are pluggable and declared by
**capability**, not by a fixed name.

- **Capabilities** are **optional** interfaces discovered by type assertion, on
  top of a minimal core (`Nome/Buscar/BuscarDetalhe/MapearDetalhe`):
  `SerieProvider` (season/episode tree), `SerieRicoProvider` (cross enrichment of
  the series node), `ColecaoProvider`, and future ones (`ArteProvider` for
  fanart.tv, trailer, theme). A provider that does not implement a capability is
  degraded in silence.
- **Provider independence** (principle, see [AGENTS.md](../AGENTS.md) §6): the
  media survives **any** registered provider. TMDB only → it searches and imports
  **both** movie **and** series through TMDB; TVDB only → the same through TVDB;
  both → they cross and enrich. A field one source does not model stays empty, it
  **never** blocks the vertical. Implementation state and gaps: out of scope here
  — this doc settles only the principle.
- **N-provider registry** with order = priority; multiple providers coexist and
  compose in the resolver (`override ⟵ externo ⟵ arquivo`, external layers
  ordered by priority).
- **Vision (multi-vertical):** TVDB, IMVDB (music videos), MusicBrainz (music),
  ScreenScraper (games), fanart.tv (art) and others — each one as a provider.

## Credential configuration

**API key** management — never hardcoded in the code (alert in
[11](11-open-decisions.md); the IMVDB incident in the old repo). Since
[ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md), each
provider's credential is configurable through a dedicated UI
(`/dashboard/integrations`): a value written to the database takes precedence over
the bootstrap environment variable, through the new `internal/integration` package
(`Descriptor`+`Registry`, capability-optional, the same mold as
`provider.Registry`).

## Open decisions (see [11](11-open-decisions.md))

- Rate-limit/cache strategy per provider.
- ~~Universal scraper (combining sources) vs explicit choice of the source~~ →
  **closed**: single base source per item + the remaining providers enrich by
  capability (provider independence).
