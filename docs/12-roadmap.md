# 12 — Roadmap

Development is **UI-first** (validate the direction early) and driven by
self-contained specs: each item below becomes one or more self-contained specs
when it enters execution.

The model and the architecture are already born ready for the **universal vision**
([README](../README.md)); the phases deliver **movies/series** first.

---

## Phase 0 — Foundation documentation ✅ done

Goal: close the model, the architecture and the foundational decisions **before** coding.

- **R1–R7** (resolver), **MD1–MD6** (data model) and **ME1–ME4** (doc base),
  recorded in the [agenda](00-decision-agenda.md) and in the [ADRs](adr/).
- Implementation stack and `/api/tmdb` passthrough ratified
  ([ADR-0014](adr/0014-implementation-stack.md)/[ADR-0015](adr/0015-tmdb-api-passthrough.md)).

## Phase 1 — Single-container skeleton

Goal: a binary that boots, serves an empty SPA and answers `/api/v1/health`.

- **Vite + React** scaffold (+ shadcn/ui) and **Go** binary with `//go:embed` of `dist/`.
- Router: statics + `/api/v1/health`; route fallback to the SPA.
- **`VITE_API_BASE`** flag working (dev: Vite 5173 + API 8080; prod: same origin).
- **Multi-stage Dockerfile** (Vite build → Go build → lean final image).

## Phase 2 — Library UI (validate the direction)

Goal: point at folders, scan, see the detected media with its technical facts.

- **datasource** registration + **scan** in the background (Go: fs walk + ffprobe).
- Minimal model: `entity`, `media_file`, `library_source`.
- **Poster grid**, list of detected items, **detail screen** with mediainfo.
- Still no heavy external enrichment.

## Phase 3 — Enrichment + override

Goal: complement items from external sources and **give authority to the owner**.

- **local-first → TMDB** search on demand ("unsaved" results).
- **Explicit import** + image proxy/cache.
- **Multi-language** (L1–L3): primary/fallback/allowed config, per-language
  resolution with fallback + provenance, translation of fields and tags.
- **Override editor** (title, fields, **per language**) + **custom art upload**.
- The **resolver** composing `override ⟵ external ⟵ file` on every read.

**Breakdown:**

| Deliverable | Closes |
|---|---|
| Materialize `media_external` + `media_override` | foundation (R4/MD4) |
| Resolver `override ⟵ external ⟵ file` | R1–R7 |
| TMDB provider + `/api/v1/search` local-first search | doc 07 |
| Explicit import + image proxy/cache | doc 07/08 |
| Multi-language foundation: settings + per-language resolution | L1/L2 |
| Multi-language provider + tag translation | L3 |
| Language UI: admin + reading selector | L1/L2 |
| Override editor + UI (per language) | R3/R6 |
| Custom art (upload) + UI | doc 08 |
| Review queue + orphans + match correction | MD1/MD5 |

| Rich external detail + persistent cache (provider) | doc 07 |
| Rich detail screen for the external item (UI) | doc 07/08 |

The **last two items** were born in the **Phase 3 review**: a rich detail screen for
the external item (like TMDB) **before** importing, persisting the detail on the
1st lookup. The 1st version is **without credits** and **without local rating** — see below.

**Deferred to the end of the phase / Phase 4:** cast/crew (MD6 — `person`/`credit`/
`company` and credit override), split off because it is far more complex; and the
owner's **local rating/review** (new model), raised in the Phase 3 review.

## Phase 4 — TMDB-compatible API + multi-user

Goal: other apps consume the catalog; controlled access.

- **`/api/tmdb/*`** served from the database; test pointing a real TMDB client at it.
- **Users, tokens, permissions** ([10](10-users-and-permissions.md)).
- Optional **NFO export** ([09](09-nfo-and-export.md)).

## Phase 5+ — Verticals and plugins (vision)

Goal: fulfill the universal scope.

- New **internal verticals** (music videos, music, games…) via `extension_data`.
- **Third-party plugin machinery** (manifest, registry, lifecycle,
  isolation) — only after the base is stable ([11](11-open-decisions.md)).
- **Consumption history (tracking/scrobbling)** agnostic to vertical —
  generic events table + media server webhook + `TrackerProvider` capability
  for external sync (Trakt and the like). Founded on market
  research, decision in [ADR-0022](adr/0022-consumption-history-generic-events-and-tracker-provider.md)
  (agenda [H1–H4](00-decision-agenda.md)).
- **Integrations configurable through the UI** — `/dashboard/integrations` screen to
  configure provider credentials (TMDB first) that today are only available through
  environment variables, with a dynamic registry (`internal/integration`) ready
  for future plugins to declare their own. Decision in
  [ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md)
  (agenda [I1–I4](00-decision-agenda.md)).
- **Trailer and theme song** (parity with tinyMediaManager) — materialize
  trailer (source: TMDB/TVDB `/videos`) and theme song (source: ThemerrDB) in the real
  folder, Kodi/Jellyfin convention (`<name>-trailer.<ext>` / `theme.mp3`), with
  yt-dlp as an optional downloader (dynamic binary + path customizable by the
  admin). Decision in
  [ADR-0025](adr/0025-trailer-and-theme-through-ytdlp-and-themerrdb.md) (agenda
  [TR1–TR2](00-decision-agenda.md)).
- **Music** — the first genuinely new vertical (artist/album/track), the real
  test of the "no core migration" promise: generic `entity`/`entity_relation`
  cover the whole hierarchy and the artist↔track/album attribution (feat./Various
  Artists), the only schema touch is a generated column `external_id_mbid`.
  Base provider **MusicBrainz + Cover Art Archive** (no key). Decision in
  [ADR-0027](adr/0027-music-model-artist-album-track.md)
  (agenda [MU1–MU4](00-decision-agenda.md)) —
  the breakdown starts with the data foundation; provider, scanner/matching and UI
  come in the following items.

---

## How each phase is broken down

1. When starting a phase, break its items into self-contained specs (one per delivery,
   under `docs/`).
2. Each spec references this phase and the `docs/` that ground it.
