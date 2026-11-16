# ADR-0025 — Trailer and theme music: yt-dlp accepted (dynamic binary + custom path) + ThemerrDB

- **Status:** Accepted
- **Date:** 2026-08-12
- **Agenda:** [TR1–TR2](../00-decision-agenda.md) · **Spec:** [07](../07-enrichment-and-providers.md), [08](../08-artwork-and-assets.md)

## Context

The owner asked for parity with tinyMediaManager (TMM) on two fronts that
MyMediaDB does not cover today: downloading a movie/series **trailer** to disk,
and downloading the **theme music** (the `theme.mp3` that Kodi/Jellyfin play while
browsing the library — not the full soundtrack). Dedicated research (2026-08-12)
gathered the facts that close this decision:

- **TMDB `/movie|tv/{id}/videos`** is already consumed today (`internal/provider/tmdb`,
  extracted by `internal/richfields`) and returns `type`/`site`/`key` — but **never
  the file**: it is always a link/id of a video on YouTube (rarely Vimeo). TVDB v4
  (`trailers`) follows the same pattern. There is no source that serves the
  trailer binary directly.
- **tinyMediaManager** solves this today by integrating **yt-dlp** as a binary
  (bundled since v5.1.2, with cookie support to avoid bot blocking) —
  there is no other de facto path in the Kodi/Jellyfin/Plex/TMM ecosystem.
- **Theme music** has no field in any official provider (TMDB, TVDB,
  fanart.tv). The only live and maintained source found is **ThemerrDB**
  (`github.com/LizardByte/ThemerrDB`, BSD-3-Clause): static JSON published
  daily, indexable by `tmdb`/`imdb` (movie) or `tmdb` only (series), curated
  through a GitHub issue. The returned field (`youtube_theme_url`) is, again, **a
  YouTube link** — not an audio file. It is the source that feeds the sibling
  official plugins Themerr-plex/-jellyfin/-kodi.
- Inevitable conclusion: **trailer and theme music share the same missing
  infrastructure piece** — a YouTube downloader. It is also the only piece
  of this ADR that departs from the pattern of every provider already registered
  in the project (TMDB/TVDB/fanart.tv are **authorized** use through their own
  API key); downloading from YouTube via yt-dlp works technically, but **goes
  against YouTube's Terms of Service** — a de-facto practice tolerated for years
  across the whole Kodi/Jellyfin/Plex/TMM ecosystem, but a categorically different
  risk from the others.
- Also investigated and **discarded as out of scope**: Jellyfin's "skip intro"
  feature (Intro Skipper plugin) is not an asset downloaded from a provider —
  it is audio fingerprinting (chromaprint) running over the video files already
  cataloged, comparing episodes of the same series. It has no relation to
  external providers; should it ever be implemented, it is a local-analysis
  vertical (scanner/ffprobe), not this ADR's.

## Decision

1. **Accept yt-dlp as an optional external dependency**, used as the project's
   only YouTube video/audio downloader — reused both to materialize a trailer
   (source: TMDB/TVDB `/videos`, already extracted today) and theme music
   (source: ThemerrDB). Downloads are always an **explicit pull**
   ("download trailer"/"download theme" button or a job the admin turns on
   deliberately), never automatic on every scan — principle 4 (never automatic
   sync).
2. **Binary strategy — download at runtime, by the Go process itself, inside
   the container:**
   - **Download on demand**: on first need (first boot or first use of the
     feature), the **MyMediaDB binary itself** (not an entrypoint script, not a
     build stage) downloads the official yt-dlp binary for the container's
     OS/architecture from the project's official release and writes it to the
     **same data volume the application already uses today** (the same area
     where the SQLite database and the assets live — no new volume to
     provision), validating the integrity before use. **It does not go into the
     Docker image at build time** (`docker build`/Dockerfile does not change) —
     it keeps the MyMediaDB binary/image light (principle 4) and yt-dlp
     updatable without an image rebuild; but the download itself happens
     **inside the running container**, fully automatically, without requiring
     any action from the admin (the custom path option below is only for those
     who WANT to take control).
   - **Admin override**: a **custom binary path** field, configurable in the
     Integrations UI (`/dashboard/integrations`, same precedence database >
     environment variable as ADR-0023) — covers air-gapped deployments, admins
     who already manage their own yt-dlp version/upgrade, or environments that
     block downloads of arbitrary binaries.
   - **Resolution order**: configured custom path (database, otherwise env) →
     binary downloaded dynamically into the data path → absent. Failure at any
     step **degrades silently**: the download feature becomes unavailable (the
     endpoint answers with the already-known "no yt-dlp available"), but nothing
     else in the product breaks — the embedded player that already plays the
     YouTube link directly (an existing feature) keeps working regardless.
3. **New optional provider capability for theme music**: `TemaMusicalProvider`
   (provisional name, same style as `SimilarProvider`/`TvdbCruzadoProvider`),
   implemented by a new `internal/provider/themerrdb` (simple HTTP client, no API
   key, queries the static JSON by TMDB/IMDb id). The trailer **does not** need a
   new capability — the `richfields.Video` already extracted from the TMDB/TVDB
   payload is reused; only the materialization-on-disk step is missing.
4. **On-disk storage convention** — extends the pattern already established for
   assets (`asset.local_path`, Kodi/Emby canonical name), with no new mechanism:
   - Trailer: `kind=trailer`, file `<canonical-name>-trailer.<ext>` in the same
     folder as the media file (Kodi/Jellyfin convention).
   - Theme music: `theme.mp3` (or the original extension) at the root of the
     item's folder — **it is already exactly** the path that
     `GET /entities/{id}/theme` expects today; the automated download only starts
     writing to that same path when it is absent.
   - **The owner's file always wins** (principle 5, non-destructive): the
     automatic download never overwrites a `theme.mp3`/trailer that the owner
     already placed manually — the same "user wins" precedence as the rest of the
     resolver.
5. **ThemerrDB is best-effort** — coverage is curated by the community through
   GitHub issues, therefore incomplete. Without a match, the field stays empty; it
   never blocks the entity nor the rest of the enrichment (the same stance as
   every optional provider in the project, AGENTS.md §6).

## Consequences

- Trailer and theme music now have an on-disk materialization path following the
  Kodi/Jellyfin standard, closing the parity gap with tinyMediaManager pointed out
  by the owner.
- New categorical risk: **YouTube ToS violation** via yt-dlp — it needs an
  explicit warning in the Integrations UI (the admin turns the feature on knowing
  the risk), remains 100% opt-in, and disabling it (no binary configured/downloaded)
  leaves the rest of the product — including the embedded player that already
  exists — intact.
- New kind of operational surface: the yt-dlp binary lifecycle (dynamic download,
  version drift, custom path) — it must not compromise the local-first principle:
  a missing binary never blocks boot nor any other feature, it only turns
  automated trailer/theme off.
- Theme music coverage through ThemerrDB is partial by nature (community
  curation) — accepted as best-effort, consistent with the rest of the project.
- Out of scope for this ADR (left to the implementation work): the exact format
  of the custom path field in the Integrations UI, the exact URL/version of the
  yt-dlp release to use, the exact wording of the ToS risk warning, the exact UX
  of the download button/job, and whether trailer and theme ship in the same
  work item or in separate ones (the infrastructure piece — the downloader — is
  shared; the two verticals can evolve independently once it exists).
- Related decisions: [ADR-0023](0023-integration-config-through-the-ui-wins-over-env.md)
  (database > env precedence, reused for the yt-dlp custom path); the asset
  materialization into the real folder (`asset.local_path`, canonical name) that
  this ADR extends; `theme.mp3` being local-only — this ADR adds an optional
  automated source to what was already manual, without changing the
  `GET /entities/{id}/theme` contract; [ADR-0024](0024-external-cache-survives-without-a-live-provider.md)
  (the trailer/theme payload, once cached, follows the same offline-survival
  guarantee).
