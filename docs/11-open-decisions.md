# 11 — Open decisions, risks and related work

## Decisions already made (record)

Foundational decisions become [ADRs](adr/); the list below is the traceable summary.

- **Database: SQLite.** Postgres was considered out of fear of request volume, but for
  self-hosted use **that is not a bottleneck** — decision revisited in favor of SQLite (one
  file, zero-config, minimal memory). Search via FTS5 + generated columns instead
  of GIN (see [04](04-data-model.md)). Concurrency: WAL + single writer
  ([ADR-0010](adr/0010-sqlite-wal-concurrency-single-writer.md)).
- **Go backend, Vite/React frontend, one container** (see [03](03-architecture.md)).
- **Implementation stack** ([ADR-0014](adr/0014-implementation-stack.md)):
  **chi** router, **`modernc.org/sqlite`** driver (CGO-free), data via **sqlc**,
  mediainfo via **ffprobe**, type contract via **generated OpenAPI**.
- **`/api/tmdb` without a local item:** **passthrough + cache, never 404**; the detail can
  become a `draft` in a review queue or stay in a hidden cache (config). See
  [ADR-0015](adr/0015-tmdb-api-passthrough.md) and [05](05-api.md).
- **License: MIT** (adoption by apps is a goal). See [LICENSE](../LICENSE).
- **Universal scope by design, movies/series MVP** (see [README](../README.md)).
- **Plugins: prepared in the model, machinery outside the MVP** (see below).
- **Name: MyMediaDB**, reusing the repo `rafaelvieiras/my-media-db`.

## Open decisions (by phase)

These are not foundational indecisions — they are details **deferred** to the phase in which
they will be implemented. The foundation was already designed to accommodate them.

- **Verticals beyond movies/series** *(genuinely open)*: which ones first
  (music videos? music? games? books? comics?) and with which providers.
  Prioritize by the **real gap** in the owner's server. The model already accommodates them via
  `extension_data` without touching the core ([ADR-0005](adr/0005-uniform-composition-through-extension-data.md)).
- **Plugin system** *(post-MVP)*: the model already supports it (`extension_data`
  namespaced + schema), but the **machinery** (manifest, registry, lifecycle
  install/enable/disable/purge, execution sandbox, dynamic frontend widgets)
  is left for after the base stabilizes. Firm principles: **a plugin never changes the
  core schema** and **untrusted code runs isolated**. A first concrete piece
  of that machinery already exists outside the plugins MVP itself:
  the **integration config** registry (`internal/integration.Registry`,
  [ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md))
  is the same mold a real plugin would use to declare credentials — without
  loading third-party code dynamically.
- **Authentication/permissions** *(Phase 4)*: token scheme and scopes
  (see [10](10-users-and-permissions.md)).
- **Exact scope of the TMDB routes to mirror** *(Phase 4)*: movie, tv, search,
  images… (see [05](05-api.md)).
- **Asset layout on disk** *(Phase 3)* and cache/derived policy
  (see [08](08-artwork-and-assets.md)).
- **Scan: incremental (mtime/hash) vs full; filesystem watching vs on demand**
  *(Phase 2)*; and the **confidence threshold** that triggers the review alert (tuning,
  not model — see [06](06-library-and-scan.md)).

## Risks and notes

- ⚠️ **Secret exposed in v1.** The old `my-media-db` repo (public on GitHub)
  had an **IMVDB API key hardcoded** in the code. **Revoke/rotate that
  key.** Project rule: **secrets never in code** — via env/config.
- **Real Plex integration** is limited (Plex is closed); the practical consumption
  target is Jellyfin/Kodi via `/api/tmdb`. Plex stays as "best effort".
- **Scope ambition.** v2 died from trying the universe (plugins + N verticals)
  before delivering anything. Mitigation: **focused MVP**, prepared model, incremental
  verticals.

## Related work / prior art

- **TinyMediaManager** — feature reference (scrapers, mediainfo, NFO,
  renamer). Difference: TMM is *stateless* (NFO spread around); MyMediaDB is an
  authoritative database with an API.
- **v1/v2 of the project itself** — removed; principles extracted, code not
  copied. Reference pieces (commented NFO, mediainfo shape, providers)
  preserved outside the repo.
