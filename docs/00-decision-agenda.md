# 00 — Decision agenda (living document)

> **What it is:** the backlog of **foundation** decisions that still need to be
> made or documented. It is not a specification — it is the work list. Each item
> here, once decided, becomes text in the spec docs (`04`, `03`, …) or an ADR.
>
> **How we work:** one item at a time. For each item, the flow is:
> 1. Problem context + a plain-language summary.
> 2. Objective questions (with suggested options) until the decision closes.
> 3. The decision is recorded in the **Decision** column below and promoted to the
>    destination document.
>
> **Possible statuses:** `open` · `in discussion` · `decided` · `documented`.

---

## Current focus: the resolver specification

The resolver composes the final value of each entity from three layers
(`override ⟵ externo ⟵ arquivo`). It is the heart of the system and today it is
underspecified. These are the decisions that close its spec.

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **R1** | Do the layers compete for the same field or is the field space partitioned by origin? | **documented** | Partition by origin. The origin is **field metadata**, not a hard-code: the core declares it in the core schema; extensions/plugins declare it in the JSON Schema of their own namespace. The resolver is a **generic** engine (it does not know fields by name). The annotation declares only the **base sources** (`arquivo`/`externo`/`nenhuma`); **override is universal** (never declared). System fields (`id`, `created_at`, `content_hash`) stay outside the resolver. | `04` (+ new resolver section) |
| **R2** | With two external sources (TMDB+TVDB) on the same field, which one wins? | **documented** | **Global configurable source priority list** (e.g. `[tmdb, tvdb, …]`); per field, the first source in the list that has a value wins — before the override. MVP default: `[tmdb]`. Alternatives (per-field priority, primary source per entity) recorded as post-MVP evolution. | `04` / ADR |
| **R3** | Override on list fields (tags, genres, cast): does it replace the whole list or merge? | **documented** | **Merge by operations** (not replacement). The override stores a set of **additions** and a set of **removals by value**; the external base stays alive underneath (re-scrape evolves the rest). Order = external, with removals applied and additions at the end; dedup when composing. It applies **only to lists of simple values** (`tags`, `genres`). Cast/crew (relations) → see MD6. | `04` |
| **R4** | Override storage: EAV `(entity_id, field, value)` vs sparse JSON blob. **(changes the doc 04 table)** | **documented** | **Sparse JSON blob**: `media_override(entity_id, override_json, updated_at)` — one row per entity, JSON mirroring the entity format, only with the fields that changed. Validatable against the same schema (core+plugins, R1); it accommodates the `{add,remove}` of R3 and override inside `extension_data` (R5). Resolver = composed base + apply patch. **Replaces the EAV draft in doc 04.** | `04` / ADR |
| **R5** | Does `extension_data` also go through the resolver (uniform composition)? | **documented** | **Yes, uniform composition.** Core and extension go through the same resolver; `override ⟵ externo ⟵ arquivo` holds field by field, including inside the `extension_data` namespaces. Consistent with R1 (generic engine) and R4 (the blob mirrors the whole entity). | `04` |
| **R6** | How does the owner "delete" a value — distinguishing "I made no override" from "override to empty"? | **documented** | **Key presence = active override**, even with `null`/empty (it erases the external value). Key absent = inherits from the base. The UI must separate "clear override" (removes the key) from "set empty" (key with `null`). **Per-field provenance is not exposed in the API for now** — the editing UI queries the override separately; exposing provenance stays as a possible evolution (Phase 4). | `04` |
| **R7** | Are technical facts (mediainfo) overridable by the owner? | **documented** | **Yes**, through the same universal mechanism (R1) — no special rule. The detected technical data stays in `media_file` (non-destructive); the override goes on top during composition. Rare use (correcting a wrong ffprobe detection), but allowed. | `04` |

---

## Data model (outside the resolver, but foundational)

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **MD1** | Identity and matching: how does a file become/associate with an `Entity` and an external id? A movie with multiple files (editions/qualities) — normative 1:N relation. | **documented** | Adopts the **Kodi/Jellyfin convention** (de-facto standard): **flat 1:N** (1 work = 1 `Entity`; qualities/editions/parts = `media_file`), **edition = label on the file**, **external id embedded in the name = deterministic key**, *stacking* of parts. A match carries a **confidence score**: high matches on its own, low **still matches + review alert** (*"mapeado com 60% de certeza. Remover?"*), zero results become an unidentified `draft`; the owner's correction is authoritative (override). Verticals outside the standard (music video→IMVDB, comics→ComicInfo) bring their own identity through `extension_data`, without touching the core. | `06` (+ `04`) |
| **MD2** | `status` (owner intent) vs ownership (does a `media_file` exist?) are conflated in doc 04. Separate them into orthogonal axes. | **documented** | **Two orthogonal axes.** **Ownership = derived fact** (does a `media_file` exist?), not editable, never lies. **`status` = owner intent**, one state at a time: `cataloged` (default), `wanted`, `unreleased`, `draft`. The `owned` value was removed (it was ownership in disguise). They combine freely (e.g. `wanted`+ownership=yes = I want an upgrade while already having a copy). Transitions between states → MD5. | `04` |
| **MD3** | Concurrency in SQLite: the API always up while the scan writes heavily (single-writer). Strategy (WAL + single writer). | **documented** | Three-layer recipe. **(1) WAL + pragmas**: `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON` — many readers + 1 writer without mutual blocking. **(2) Two pools**: a pool of N readers + **1 write connection** (single writer). **(3) Short writes, owner first**: the scan commits in short batches; interactive writes (override/import) take priority over the scan. Evolution: a writer queue/goroutine if explicit prioritization is needed. | `03` |
| **MD4** | External identity duplicated in `entity.external_ids` and `media_external.(source, source_id)`. Which one is authoritative? | **documented** | **`entity.external_ids` is the authoritative identity map** (all known ids, looked up or not; it is where the MD1 match writes and where the lookup reads). **`media_external` is a payload cache**, with `source_id` subordinate (invariant: it matches `external_ids[source]`). **Conflict → identity wins**: the diverging row is a stale cache → re-fetch/discard; the owner fixes the identity, never edits `media_external` by hand. Identity ≠ editorial value (`media_external` remains the resolver's base). Same logic for `person`/`company`. | `04` |
| **MD5** | Lifecycle / deletion: a file disappears from disk — does the entity become `wanted`? orphaned? Non-destructive behavior defined. | **documented** | **A file event touches only ownership; never the `status` nor the owner's work.** File disappears (datasource reachable) → `media_file` becomes **`missing`** (keeps `last_seen`), entity preserved, ownership→no, status **intact**. **Whole datasource offline** (unmounted HD) → nothing becomes `missing`, the datasource is flagged offline. File shows up for a `wanted` item → ownership=yes, it stays `wanted`. **Orphan** = auto-created entity, file gone, **zero investment from the owner** → cleanup queue, **never deleted on its own**. **Purge** is the only destructive operation, always explicit. | `04` (+ `06`) |
| **MD6** | Override of **relations** (cast/crew): merge by `person_id`, editing role/order. Separated from R3 because it is far more complex (`person`/`credit` tables). | **documented** | **R3 generalized to structured credits.** The override stores **operations** (add/remove/edit of role/`ord`); the external base (the payload's `credit`) stays alive and evolves on re-scrape. **Key = `person_id` + role/department** (distinguishes the same person in 2 roles), with the provider's `credit_id` as tiebreaker. A person added by the owner becomes a **local** `person` (empty `external_ids`, fine per MD4). The resolver composes credit by credit (R5); the ops live in `media_override` (R4). | `04` |

---

## Languages (multilingual) — foundational, raised in Phase 3

The collection is multilingual: the owner sets primary + fallback + allowed, every
value carries the language flag, and the owner adds translations. It reworks the
three-layer model before the per-language override lands.

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **L1** | How does the language enter the three-layer model? Where does the config live (primary/fallback/allowed)? | **documented** | **Language dimension = KEY inside each layer** (external tags by language in the `/translations` payload; override becomes `field→language→value`; the core is the neutral fallback), with no new table. Translatable fields (`title`/`description`/`tags`) vs neutral ones (`original_title`/`original_language`/`release_date`/`external_ids`/technical). New column `entity.original_language`. **Config in settings in the database** (`app_settings`, key `languages`) with an admin UI — not in env. | [ADR-0018](adr/0018-multilanguage-model-language-as-a-layer-dimension.md) |
| **L2** | Which language does the API serve and what does it expose about that? | **documented** | **Resolves to ONE language** per request (`?lang=`, default = primary), **flat** shape preserved; **fallback chain** `requested → fallback → original → file`; **owner authority per language** (R6 per language; `null` in the request stops the fallback); **language provenance** exposed (`resolved_languages`). | [ADR-0019](adr/0019-per-language-resolution-fallback-and-provenance.md) |
| **L3** | How to translate tags (TMDB does not translate keywords)? | **documented** | **Tags become a per-language list**, merge of operations (R3) **inside each language**; **genres** come from TMDB's localized list per language (cacheable); **keywords** stored in the language obtained, translated **manually** by the owner (no automatic translation). | [ADR-0020](adr/0020-translation-of-genres-and-keywords-tags.md) |

---

## Series (TV) — hierarchy, orderings and providers — foundational, raised later on

Series are the leap beyond movies (a single item): the series→season→episode
hierarchy and the multiple season orderings (aired/DVD/absolute). Founded on
**empirical research** of both providers (TMDB + TheTVDB) and of the media server
conventions.

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **TV1** | How to represent the multiple season orderings without duplicating episodes? | **documented** | **Episode = fine grain, single canonical entity** (anchored on `episode.id`); **ordering (season type) = numbering projection, never changes granularity** (confirmed: the union of all orderings = superset of `official`, zero ids of their own in the alternatives; no provider has a "combined episode"). Multi-order numbering in `extension_data core.tv@1`. | [ADR-0021](adr/0021-series-model-hierarchy-orderings-and-file-link.md) |
| **TV2** | Is a season an entity (with its own artwork/override) for every ordering or only the default one? | **documented** | **Lean: season-entity only for the series' _default_ ordering** (configurable). Alternative orderings = derived numbering, grouped at read time, no entity. (Uniform — ~103 entities for Looney Tunes — was discarded.) | [ADR-0021](adr/0021-series-model-hierarchy-orderings-and-file-link.md) |
| **TV3** | How to link a file that packs several episodes (streaming rip) to the catalog? | **documented** | **N:M file↔episode link, anchored on the id** (`media_file_episode(…, ordinal)` junction). The "combined" aspect is a property of the **file/release**, not of the catalog: 1 file→N episodes; 1 episode→N files (multi-part, inherited from movies). Anchoring on the id (not the number) immunizes against re-scrape. | [ADR-0021](adr/0021-series-model-hierarchy-orderings-and-file-link.md) |
| **TV4** | Two external sources (TMDB+TheTVDB) for series — how do they coexist? | **documented** | **Executes R2**: `name→Provider` registry + global list `[tmdb, tvdb]` (replaces the singular one and the hardcoded `["tmdb"]`). TheTVDB provides orderings + exclusive artwork; TheTVDB-only series (e.g. Looney Tunes, without a TMDB `remoteId`) use TheTVDB as primary identity. Optional `SerieProvider` interface (analogous to `ColecaoProvider`). | [ADR-0021](adr/0021-series-model-hierarchy-orderings-and-file-link.md) · [ADR-0002](adr/0002-priority-between-external-sources.md) |
| **TV5** | How to slice the epic? | **documented** | **Enrichment first** (Phase 1): schema + TheTVDB provider + registry + fetch/import/display the providers' tree. Phase 2 = scanner + TV naming + N:M linking from disk (check the Kodi/Jellyfin convention). Phase 3 = ordering selector, season artwork, i18n. | [ADR-0021](adr/0021-series-model-hierarchy-orderings-and-file-link.md) |

---

## Baseline improvements (documentation structure)

| ID | Item | Status | Decision | Destination |
|---|---|---|---|---|
| **ME1** | Adopt ADRs (one file per decision) instead of the flattened list in doc 11. | **documented** | **Yes, lightweight format.** Created `docs/adr/` with a README (index + practice), `template.md` (Context·Decision·Consequences) and **13 ADRs** migrated from the closed decisions (R1–R7 → 0001–0007; MD1–MD6 → 0008–0013). An ADR is **immutable**: a new decision that supersedes another creates a new ADR marked `Superseded by`. | `docs/adr/` |
| **ME2** | ER diagram (mermaid) of the data model. | **documented** | `erDiagram` (mermaid) in doc 04, after the table draft: entity + the 3 resolver layers (`media_file`/`media_external`/`media_override`) + `credit`/`person`/`asset`/`entity_relation`/`library_source`. `company` without a hard edge (join left to the schema work). | `04` |
| **ME3** | Create `LICENSE` and `CONTRIBUTING.md` (leaning MIT/Apache-2.0). | **documented** | **MIT.** `LICENSE` (MIT, © 2026 Rafael Vieiras and contributors) + `CONTRIBUTING.md` at the root (principles, decision→ADR→spec flow, commits in pt-BR, secrets never in code). | root |
| **ME4** | Glossary: resolver precedence terms and "ownership" vs "status". | **documented** | Doc 02 updated: `status` fixed (intent, without `owned`), **Ownership** added (derived fact), + the terms Resolver precedence, Universal override, Merge by operations, Provenance, `missing`, Orphan, Purge, `external_ids`/`media_external`. | `02` |

---

## Consumption history (tracking/scrobbling) — foundational, raised from market research

The owner asked for a tracking/milestones system agnostic to media type and to the
originating client, inspired by the scrobbling ecosystem (Trakt.tv, Last.fm,
ListenBrainz, Open Scrobbler) and by per-vertical tracking (scrob.app,
sofa.watch, Librarium, GamingGaiden, RetroAchievements and competitors).
Market research on 5 fronts (movie/series, music, books, games, current schema)
underpins the decisions below.

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **H1** | Is history a state column(s) (like migration 0019) or events? | **documented** | **Generic, append-only event table** (`historico_evento`, child of `entity`, free `kind`/`status`/`source`). Every relevant competitor models history as a sequence of events (progress updates, multiple reads/plays), never 1 mutable value. "Current state" becomes a projection derived from the last event, not a second source of truth. Migration 0019 is not migrated/unified now (known and accepted tension). | [ADR-0022](adr/0022-consumption-history-generic-events-and-tracker-provider.md) |
| **H2** | Event input: player webhook, our own API, provider capability, or all three? | **documented** | **Webhook (its own adapter, not a `provider.Provider`) + manual API first**; provider capability (`TrackerProvider`) is reserved for OUTBOUND/synchronization with an external service (Trakt), designed but with no concrete implementation in this delivery. First webhook integration: Jellyfin only (simpler/documented payload; Plex requires Plex Pass). | [ADR-0022](adr/0022-consumption-history-generic-events-and-tracker-provider.md) |
| **H3** | Field vocabulary (progress, "watched" threshold, external identity) — invent our own format or converge with the market? | **documented** | **Converge**: identity through `ids.{tmdb,imdb,tvdb}` (already `entity.external_ids_json`), `progress_percent` 0–100, an **80%** threshold to close `completed` — the same vocabulary Trakt and Simkl already converged on, minimizing friction for a future `TrackerProvider`. | [ADR-0022](adr/0022-consumption-history-generic-events-and-tracker-provider.md) |
| **H4** | How to slice the epic — everything at once (schema+webhook+UI+`TrackerProvider`, all verticals) or incremental? | **documented** | **Incremental, movie/series only in the first delivery** (the schema is born vertical-agnostic anyway). Order: schema+minimal API → Jellyfin webhook → state/history UI. Concrete `TrackerProvider`, the remaining verticals (music/book/game) and the "milestones"/achievements implementation (model already designed in H5) come later — none of them demands a new migration when its turn arrives. | [ADR-0022](adr/0022-consumption-history-generic-events-and-tracker-provider.md) |
| **H5** | "Milestone"/achievement (RetroAchievements/Steam-style) — a single event, or a browsable catalog (title/description/icon even when locked) + unlock event? | **documented** | **Catalog + event**, a revision of the initial decision (which treated an achievement as just an event). The achievement definition = its own `entity` (`type='conquista'`), linked to the parent item through the already existing generic `entity_relation`; icon = ordinary `asset`. Unlock = `historico_evento` pointing at the achievement. The source-specific unlock condition (e.g. the RetroAchievements memory address) stays opaque in `extension_data`, never interpreted by the core. The new `MarcoProvider` capability (catalog import) follows the `SimilarProvider` mold. Game identity by ROM hash reuses the already existing `media_file.content_hash`; fan-translation (different hash) = one more `media_file` of the same entity (MD1); an unrecognized hash degrades without blocking, correction is manual (MD1); an unlock condition that diverges per hash uses the already existing universal override (R6) — no new mechanism in any of the cases. | [ADR-0022](adr/0022-consumption-history-generic-events-and-tracker-provider.md) |

---

## Configurable integrations (UI) — foundational, raised from an owner request

The owner asked for a `/dashboard/integrations` screen (a subpage per integration,
dynamic form) to configure provider credentials (TMDB first) that today only exist
as an environment variable read once at boot — and that would also serve as the
extension point for future plugins to register their own integrations.

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **I1** | Where does the integration config live and what is its precedence against the environment variable currently used for credentials? | **documented** | **The database takes precedence; the env var becomes bootstrap/fallback** — the same override-wins format as the resolver (R1–R7) and the same precedent as L1 (languages in the database+UI, not env). Updates the letter of the secrets rule in AGENTS.md/CONTRIBUTING.md (new text below). | [ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md) |
| **I2** | How does an integration (core today, plugin tomorrow) declare its config fields without the framework knowing each source by name? | **documented** | `integration.Descriptor` interface (`Slug/Nome/Descricao/Campos/Configurada`) + `integration.Registry`, the same mold as `provider.Registry` (optional capability, registration order preserved). Each provider that owns a credential contributes its own adapter. | [ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md) |
| **I3** | How does the API expose this for the front end to render a dynamic form without leaking a raw secret? | **documented** | `GET /integrations`, `GET/PUT /integrations/{slug}`, the same descriptor vocabulary as the schema-driven form (`name/label/widget/...`). A `secret` field never returns the raw value (only `defined`+`origin`). PUT is partial and sparse (R6: `null` removes the override for that field). | [ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md) |
| **I4** | Hot-reload of the credential on save, or restart? How to slice the epic? | **documented** | **No hot-reload in the MVP** (saving writes to the database; it takes effect only on the next process restart — already a gain over editing `.env`+redeploying). Hot-reload and "test connection" are explicitly left as follow-up. Two slices: **foundation** (schema+registry+API+TMDB migration) → **UI** (`/dashboard/integrations`). | [ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md) |

---

## Offline survival of the catalog — foundational, raised from the 2026-08-12 migration

In the production migration to Batocera (2026-08-12), the new environment came up
with no provider credential configured at all. Symptom: items cataloged months
earlier, with `media_external`/`external_detail` intact (copied in the migration),
showed up WITHOUT synopsis/cast/crew/tags/similars — not because of data loss, but
because the reading code (`internal/server/entities.go:montarExterno`/
`camadasDaFonte`) uses the SAME gate (`provider.Registry`) both for "can fetch
live" (needs a key, correct) and for "can interpret the payload already stored"
(pure function, should not need any key). Full investigation and decision in
[ADR-0024](adr/0024-external-cache-survives-without-a-live-provider.md).

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **PR1** | Should reading already cached `media_external`/`external_detail` require a provider with a live credential, or only the new SEARCH? | **documented** | **Separate the two capabilities**: parsing an already stored payload (`MapearDetalhe`/`Similares`/richfields) stays available for any `source` known to the binary, credential present or not; only `BuscarDetalhe` (new search) remains gated by credential. `externaldetail.PayloadFresco` now serves the stale payload when the re-fetch fails, instead of discarding the data. Implemented, deployed and validated in prod on 2026-08-12. | [ADR-0024](adr/0024-external-cache-survives-without-a-live-provider.md) |

---

## Trailer and theme music — foundational, raised from research on parity with tinyMediaManager

Research on 2026-08-12 confirmed: TMDB (`/movie|tv/{id}/videos`) and TVDB v4
(`trailers`) only deliver a **YouTube video link/id**, never the file — the same
holds for the live theme music source (**ThemerrDB**, from the LizardByte project,
static JSON per TMDB/IMDb id with `youtube_theme_url`). Downloading the binary to
disk (Kodi/Jellyfin convention: `<name>-trailer.<ext>` and `theme.mp3`) therefore
requires a YouTube downloader — **yt-dlp**, the same one tinyMediaManager already
bundles. It is the only new piece that deviates from the pattern of the project's
other providers (TMDB/TVDB/fanart.tv are authorized use through an API key; yt-dlp
technically goes against the YouTube Terms of Service, despite being de-facto
practice in Kodi/Jellyfin/Plex/TMM). Full detail in
[ADR-0025](adr/0025-trailer-and-theme-through-ytdlp-and-themerrdb.md).

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **TR1** | Accept the yt-dlp dependency (ToS risk) and where does the binary come from? | **documented** | **Accept it.** The binary is obtained **at runtime, by the MyMediaDB Go process itself** (not the Dockerfile, not the entrypoint): downloaded on demand (first boot / first need) from the official yt-dlp release for the container's OS/architecture, written inside the running container, on the SAME data volume the app already uses today (no new volume). It does not enter the image at build time (the image stays lean, no rebuild to update it). The admin can **override it with their own binary path** (same database>env precedence as ADR-0023). Absence/download failure **degrades silently** (the trailer/theme feature becomes unavailable; the embedded trailer player through the link keeps working). Implemented and validated locally on 2026-08-12. | [ADR-0025](adr/0025-trailer-and-theme-through-ytdlp-and-themerrdb.md) |
| **TR2** | Which source for theme music, since TMDB/TVDB/fanart.tv have no field for it? | **documented** | **ThemerrDB** (LizardByte) as an optional provider capability (`TemaMusicalProvider`), with no API key, indexed by TMDB id (movie/series) or IMDb (movie only). Coverage is curated by the community (best-effort, may not find it) — the same "no match does not block" stance as the rest of the project. Implemented and validated locally on 2026-08-12. | [ADR-0025](adr/0025-trailer-and-theme-through-ytdlp-and-themerrdb.md) |
| **TR3** | The first slices only gave a manual button per entity. How to find/download trailer and theme **in bulk**, integrated with scan/sync and the jobs system? | **documented** | **Opt-in periodic job** (mold of `midias_faltantes`, `enabled=0` by default, reusing the existing jobs UI) does the bulk detection (always through `os.Stat`, no new column) and materializes what is missing. **Automatic trigger on scan** behind a new toggle (mold of `autoMaterializeArtworkSettings`) fires a best-effort attempt in a goroutine (never blocks the scan) after import/auto-match; the periodic job remains the base, the trigger only speeds it up (the same watch-vs-job reasoning as `AGENTS.md`). Refines item 5 of ADR-0025 ("never automatic on every scan") in light of the already existing precedent (auto-materialize). | [ADR-0026](adr/0026-trailer-and-theme-job-and-automatic-scan-trigger.md) |

---

## Music model — foundational, raised from an owner request to implement the schema

The research crossed the series model ([ADR-0021](adr/0021-series-model-hierarchy-orderings-and-file-link.md),
the direct precedent of fine-grain hierarchy + entity only for artwork/override)
with the owner's personal music organization scripts
(`~/ai-workspace/scripts/music-organizer`), which already solve in production the
real thorny points: track artist diverging from album artist (feat.), "Various
Artists" as a compilation grouper, and album identity depending on name+artist+discs+label+barcode
(varying any of them fragments the album in real players — an empirical finding
documented in the organizer's own code). Full detail in
[ADR-0027](adr/0027-music-model-artist-album-track.md).

| ID | Question | Status | Decision | Destination |
|---|---|---|---|---|
| **MU1** | Which primary metadata provider? | **documented** | **MusicBrainz + Cover Art Archive** — no API key, a role equivalent to TheTVDB; a single provider already covers the whole vertical (provider independence). Spotify/Deezer (already used in the owner's personal scripts) stay as future cross-enrichment, not a base dependency. | [ADR-0027](adr/0027-music-model-artist-album-track.md) |
| **MU2** | File↔track link: simple 1:1 or N:M like episodes? | **documented** | **1:1 for the MVP** (direct `media_file.entity_id`, the same movie pattern) — covers the dominant case (rip = 1 file = 1 track); evolves to N:M (`media_file_faixa`) if medley/DJ-mix becomes a real need, without breaking the model. | [ADR-0027](adr/0027-music-model-artist-album-track.md) |
| **MU3** | Is the artist its own entity or does it reuse `person`/`credit` (cast/crew, MD6)? | **documented** | **Its own entity** (`type='artista'`) — a band is not a person and needs artwork/bio catalogable as a first-class item; track↔artist attribution (main/feat.) through the generic `entity_relation`, not a new join table. | [ADR-0027](adr/0027-music-model-artist-album-track.md) |
| **MU4** | How to model "Various Artists" (compilation/soundtrack)? | **documented** | **An ordinary artist**, with the fixed canonical MBID that MusicBrainz itself already uses — no special case in the code/schema. | [ADR-0027](adr/0027-music-model-artist-album-track.md) |

---

## History of closed decisions

<!-- As each item closes, move the decision summary here with a date. -->

- **I1–I4 — Configurable integrations through the UI: database takes precedence over env**
  (2026-08-10). Replaces the hardcoded credential (TMDB/TVDB/fanart.tv only as an
  environment variable read at boot) with a `/dashboard/
  integrations` screen with a subpage per integration. Decision: **the database (through the UI) wins,
  the env var becomes bootstrap/fallback** — the same override-wins format as the
  resolver (R1–R7), with precedent already opened by L1 (languages in the database+UI).
  The new `internal/integration` package (`Descriptor`+`Registry`, the same mold as
  `provider.Registry`) is the first concrete extension point for the future
  plugin system (docs/11-open-decisions.md) — without implementing the full plugin
  machinery now. A secret never comes back raw through the API (only `defined`+`origin`);
  **no hot-reload in the MVP** (a restart applies the change); "test connection" stays as
  follow-up. Updates the letter of the secrets rule in AGENTS.md/
  CONTRIBUTING.md. Recorded in [ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md),
  split into **foundation** and **UI** slices.

- **H1–H5 — Consumption history: generic events + milestone catalog +
  `TrackerProvider`** (2026-08-10). Market research on 5 fronts
  (movie/series: Trakt, scrob.app, sofa.watch, Simkl, Watcharr; music:
  Last.fm, ListenBrainz, Maloja, Koito; books: Librarium, BookWyrm,
  Hardcover, StoryGraph; games: RetroAchievements, Playnite/SuccessStory,
  GamingGaiden) found a convergent pattern: identity through an already
  known external ID, history as a sequence of events (never a single state), and N
  heterogeneous clients aggregated by a canonical schema through a plugin/
  capability. Decision: a generic `historico_evento` table (append-only,
  free `kind`), input by player webhook (Jellyfin first) + manual API, output/sync as a
  new optional capability `TrackerProvider`
  (designed, no concrete provider yet). **H5 revision** (same session,
  from a direct owner question about RetroAchievements/Steam): a "milestone"/
  achievement is NOT an event alone — it is a catalog (`entity`+`entity_relation`+
  `asset`, browsable even when locked) + an unlock event; the source-specific
  unlock condition stays opaque in `extension_data`; the ROM hash reuses the
  already existing `media_file.content_hash`; fan-translation and
  condition correction by hash reuse already existing mechanisms (MD1 +
  R6), with nothing new. The epic is sliced into 3 deliveries, movie/series
  only in the first one.

- **TV1–TV5 — Series foundation (hierarchy, orderings, providers)** (2026-07-11).
  Closes the series model before any code, founded on **empirical research**
  of both APIs (Simpsons, Looney Tunes 72514, One Piece) and of the media
  server conventions (Jellyfin/Kodi/Plex/Sonarr). Decisive finding: **the two providers
  converge** — the **episode is the canonical fine-grain entity** and switching
  ordering (season type / episode group) is **pure renumbering, it never changes
  the granularity** (the union of all orderings = superset of `official`, zero ids
  of their own in the alternatives; there is no "combined episode" in any provider).
  **TV1:** multi-order numbering in `extension_data core.tv@1` on top of the fine episode.
  **TV2:** season-entity only for the _default_ ordering (lean); alternatives are
  derived numbering. **TV3:** the "combined" aspect (streaming packs shorts) belongs to the
  **file**, not the catalog → **N:M file↔episode link anchored on the id**
  (`media_file_episode`), which anchoring on the number would break on re-scrape (the
  Sonarr bug). **TV4:** two sources execute **R2** (registry + `[tmdb, tvdb]` list),
  with TheTVDB-exclusive series using its primary identity. **TV5:**
  **enrichment first**, scanner and file linking in the next phase.
  Recorded in [ADR-0021](adr/0021-series-model-hierarchy-orderings-and-file-link.md).
- **L1–L3 — Multilingual foundation** (2026-07-09). Foundational requirement raised in
  Phase 3 and missing from the initial design: the collection is multilingual. **L1 (ADR-0018):** the
  language dimension is the **key inside each layer** (external payload tagged
  by language through TMDB `/translations`; override `field→language→value`; the core as
  neutral fallback), with no new table; translatable fields (`title`/`description`/`tags`)
  vs neutral ones; new column `entity.original_language`; **config in settings in the
  database** (`app_settings`) with an admin UI. **L2 (ADR-0019):** reading
  **resolves to one language** (`?lang=`, default primary) while keeping the flat shape,
  with a **fallback chain** (`requested → fallback → original → file`), owner authority
  **per language** (R6 per language; `null` in the request does not fall back) and
  **language provenance** exposed. **L3 (ADR-0020):** **tags per language** with
  merge of operations inside each language — genres from TMDB's localized list,
  keywords in the language obtained with manual translation. Implementation in three
  slices (foundation → provider → UI), reworking the earlier work and preceding
  the per-language override.
- **Doc 11 — Implementation stack and `/api/tmdb` passthrough** (2026-07-08).
  Closes the mature decisions of doc 11. **Stack (ADR-0014):** the leanings are
  ratified — **chi** router, **`modernc.org/sqlite`** driver (CGO-free), data
  through **sqlc**, mediainfo through **ffprobe**, type contract through **generated OpenAPI**;
  all consistent with "single binary, CGO-free, trivial cross-compile". **`/api/tmdb`
  (ADR-0015):** the owner refined the default — **passthrough + cache, never a 404 for
  unavailability**; a search *miss* only caches, a detail *miss* can
  become a `draft` in a review queue (`promote_draft`) or stay in a hidden cache with
  TTL (`hidden_cache`), at the owner's choice; a source failure serves stale cache. Promoted to doc
  05. **MIT license** confirmed (already in ME3). The remaining items of doc 11 (plugins,
  verticals, auth, asset layout, incremental scan) were reclassified from
  "open" to **deferred by phase** — they are not foundation indecisions.
- **ME1–ME4 — Documentation baseline cleanup** (2026-07-08). Closes the foundation
  backlog. **ME1:** **lightweight-format ADRs** adopted — `docs/adr/` with a README
  (index + practice), `template.md` and **13 ADRs** migrated from the already closed decisions
  (R1–R7 = ADR-0001…0007; MD1–MD6 = ADR-0008…0013); an ADR is immutable and a decision that
  supersedes another becomes a new ADR. **ME2:** **ER diagram** (mermaid) in doc 04, with the
  three resolver layers hanging from `entity`. **ME3:** **MIT** license —
  `LICENSE` + `CONTRIBUTING.md` at the root (decision→ADR→spec flow, pt-BR, secrets through
  env). **ME4:** **glossary** (doc 02) updated — `status` fixed (intent,
  without `owned`), **Ownership** as a derived fact and the resolver precedence terms.
  With this, the whole foundation (R1–R7, MD1–MD6, ME1–ME4) is closed and documented.
- **MD3 — SQLite concurrency: WAL + single writer + short writes** (2026-07-08).
  Resolves "API always up × scan writing heavily" against SQLite's single-writer
  limit. Three-layer recipe: **(1)** open in **WAL** with `busy_timeout=5000`,
  `synchronous=NORMAL` and `foreign_keys=ON` (many readers + one writer without mutual
  blocking, no `database is locked`); **(2)** **two pools** — reads with N connections and
  **one** write connection (single writer); **(3)** scan in **short batches** with
  **interactive writes prioritized** over the scan's. Evolves to a writer queue/goroutine
  if prioritization needs to be explicit. Promoted to doc 03 (Concurrency
  and persistence).
- **MD6 — Relation override (cast/crew) through merge of operations** (2026-07-08).
  Closes what R3 deferred: cast/crew follow the same philosophy (merge of operations,
  live external base underneath), but over **structured credits**. The override stores
  **add/remove/edit** (role, character, billing `ord`); the **credit key** is
  `person_id` + role/department (the same person in two roles = two credits),
  with the provider's `credit_id` as tiebreaker. A person added by the owner becomes a
  **local** `person` without an external id (consistent with MD4). The resolver composes credit by
  credit (R5) and the operations live in `media_override` (R4). Promoted to doc 04
  (Relation override).
- **MD5 — Non-destructive lifecycle; a file event only touches ownership**
  (2026-07-08). Closes the question "file disappears → does it become wanted?": **no** — filesystem
  events touch only the derived ownership axis (MD2), never the `status`
  (intent) nor the owner's work. A vanished file with a reachable datasource becomes
  `media_file` **`missing`** (soft, with `last_seen`), reversible if it reappears;
  **the whole datasource inaccessible** marks nothing (it avoids a mass false disappearance,
  it flags the datasource offline). An entity **auto-created with no investment from the
  owner** whose file vanished becomes an **orphan** in a cleanup queue, never deleted
  automatically. **Purge** is the only destructive operation and it is always explicit
  (it removes the entity + `media_external`/`media_override`/assets). Promoted to doc 04
  (Lifecycle and deletion) and doc 06 (missing/offline).
- **MD2 — Ownership and `status` become two orthogonal axes** (2026-07-07). A single field
  conflated "I have the file" (fact) with "what I intend" (intent). Separated:
  **ownership is derived** from the existence of `media_file` (it is not an editable field, it does not
  diverge from disk, it can become a generated column); **`status` is intent only**, one state
  at a time — `cataloged` (default), `wanted`, `unreleased`, `draft`. The `owned` value
  is gone (it was ownership in disguise). The axes combine freely (`wanted`+ownership=yes =
  I have a copy but want an upgrade), sustaining "database larger than the library". The
  state transitions (file appears/disappears) are left to MD5. Promoted to doc 04.
- **MD4 — `external_ids` is the authoritative identity; `media_external` is cache**
  (2026-07-07). The external id lives in two places, so the authority is elected:
  `entity.external_ids` is the **identity map** (all known ids, looked up
  or not — it is where the MD1 deterministic match writes and where the lookup searches).
  `media_external` is the **payload cache** per fetched source; its `source_id` is
  subordinate, with the invariant `media_external.source_id == external_ids[source]`.
  In a conflict, **identity wins**: the diverging cache row is stale
  (re-fetch/discard); the owner fixes the identity and never edits the cache by hand.
  Identity and editorial value are distinct axes — `media_external` remains the
  resolver's base layer. The same holds for `person`/`company`. Promoted to doc 04
  (section "External identity").
- **MD1 — Identity and matching through the Kodi/Jellyfin convention** (2026-07-07). Instead
  of inventing our own rule, the ecosystem's de-facto standard is adopted (Kodi/Jellyfin;
  Emby/Plex/\*arr in the same family). Consequences: **flat 1:N grain** (1 work = 1
  `Entity`, with N `media_file` for qualities/editions/parts); **edition becomes a label**
  on the `media_file` (it does not duplicate the work); **parts** are stacked (*stacking*); the
  **match key** is the **external id embedded in the name** (`[tmdbid-…]`/`{tmdb-…}`) →
  deterministic match, and without an id it falls back to title+year. Every match has a **confidence
  score**: high matches without an alert; low matches with the best guess **+ a review
  alert** that the owner confirms/corrects; zero results → unidentified `draft`.
  Music uses the same principle through MusicBrainz+tags. Verticals not covered
  (music video→IMVDB, comics→ComicInfo) bring their own identity and provider through
  `extension_data`, without touching the core — validating R1/R5. Promoted to doc 06
  (section "Identity and matching") and reflected in the `media_file` of doc 04.
- **R1 — Field space partitioned by origin** (2026-07-06). The resolver is a
  generic engine that composes layers driven by per-field metadata, not by a
  fixed table in the code. Origin declared in the schema (core) or in the namespace's
  JSON Schema (extension/plugin); only base sources are declared; override is universal.
  Consequence: adding a vertical/plugin touches neither the core nor the resolver.
- **R2 — Priority between external sources through a global configurable list**
  (2026-07-06). Source preference order (default `[tmdb]`); per field, the
  first source in the list that has a value forms the external base, on top of which the
  override enters. Configurable by the owner. Evolutions (per field / primary source
  per entity) stay post-MVP without breaking the model.
- **R3 — List override through merge of operations** (2026-07-06). For lists of
  simple values (`tags`, `genres`), the override stores additions and removals by
  value; the external base stays alive and evolves on re-scrape. Simple values only;
  relation override (cast/crew) was separated into item **MD6**.
- **R4 — Override as a sparse JSON blob** (2026-07-06). `media_override` becomes
  one row per entity with `override_json` mirroring the entity format,
  only the changed fields. Chosen because it matches R1 (schema validation), R3 (list
  merge) and R5 (override in extension_data). It replaces the EAV draft of doc 04.
- **R5 — Uniform composition, including extension_data** (2026-07-06). The resolver
  does not distinguish core from extension: the same precedence holds field by field inside
  the namespaces. It follows from R1 and R4.
- **R6 — Key presence signals an override** (2026-07-06). In the sparse blob, a key
  present (even `null`) = an override that wins; a key absent = inherits from the base. The UI
  distinguishes "clear override" from "set empty". Per-field provenance is not
  exposed in the API for now.
- **R7 — Technical facts are overridable** (2026-07-06). The universal override also
  reaches mediainfo (correcting a wrong detection); the original detected value stays preserved
  in `media_file`. No exception to the authority principle.
- **QG1 — Quality gate: unified Go + TS ratchet** (2026-07-08). A single
  ratchet with one `baseline.json` covering the Go backend **and** the TS frontend
  (Option A among unified / two gates / extending the boatman). Runner in **Node
  starting from the boatman** (it generates the JS half; the script is extended to collect Go
  metrics: golangci-lint, `go test -cover`, govulncheck, gofmt, gocyclo). Local hooks with
  **lefthook** (polyglot) instead of husky. It runs on the local `pre-push` and on the PR (blocks
  regression + comments). Promoting the Go collectors to the upstream boatman stays as
  follow-up. Documented in [ADR-0016](adr/0016-unified-quality-gate-ratchet.md).
