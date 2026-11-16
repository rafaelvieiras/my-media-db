# 06 — Library and scan

> **Status of this doc:** scoped stub. Locked decisions are listed; the deeper
> design detail lives in the [ADRs](adr/).

## Principles

- **Datasources** are server folders pointed at by the owner. A datasource has a
  `type` (e.g. movies, series) and may hold several folders.
- **Non-destructive scan.** The sweep discovers files and extracts metadata; it
  **never** renames or moves anything without an explicit command. The truth is
  the database, not the file name ([scenario E](01-vision-and-use-cases.md)).
- **Mediainfo per file.** For every video file, the technical facts are extracted
  (resolution, codecs, HDR, audio/subtitle tracks, real duration).
- **Dedup by content.** Every file has a stable `content_hash`, so that a
  rename/move does not duplicate the record.
- **Matching.** The file is associated with an `Entity` (and, optionally, with an
  external id) following the **Kodi/Jellyfin** convention — see
  [Identity and matching](#identity-and-matching).

## Identity and matching

The **Kodi/Jellyfin naming convention** is adopted — the de-facto standard of the
self-hosted ecosystem (Emby, Plex and the \*arr stack follow the same family).
**We do not reinvent it**: the standard already settles the entity granularity,
the editions and the match key. The scan reads that standard, it never imposes it
(it renames nothing).

### Granularity: 1 work = 1 `Entity`, N files (flat 1:N)

- One folder = one movie = one `Entity`. Every file whose name starts with the
  folder name is a **version of the same work** — not a new work.
- Qualities (2160p/1080p), editions (Director's Cut) and parts (cd1/cd2) are
  **`media_file`** rows linked to the **same** `Entity`, not separate entities.

```
Blade Runner 2049 (2017) [tmdbid-335984]/
├── Blade Runner 2049 (2017) [tmdbid-335984] - 2160p.mkv
├── Blade Runner 2049 (2017) [tmdbid-335984] - 1080p.mkv
└── Blade Runner 2049 (2017) [tmdbid-335984] - Directors Cut.mkv
```

- **Edition/version = a label on the `media_file`** (the `edition` field), never a
  duplicated `Entity`.
- **Parts** (`-cd1`/`-part1`/`-disc1`) are **stacked** (*stacking*) as **one
  logical** playback item.

### Match key: external id embedded in the name

- Id in the name (`[tmdbid-335984]` Jellyfin style, `{tmdb-335984}` Kodi style) →
  **deterministic match** (100% confidence), no guesswork.
- No id → **title + year → search on the provider**.
- **Music** follows the same principle of a deterministic id through
  **MusicBrainz + embedded tags** (not by file name).

### Confidence score and review queue

Every match carries a **confidence score (0–100%)**. The scan **never blocks the
library**: the best guess is always applied; what is uncertain is **flagged**,
not blocked.

| Situation | Confidence | Action |
|---|---|---|
| external id in the name | 100% | matches automatically, no alert |
| exact title + year, 1 result | high | matches automatically, no alert |
| partial title / multiple hits / no year | low | matches (best guess) **+ review alert** |
| no result | — | becomes an **unidentified** `draft` (visible, not lost) |

- The **review alert** shows the score and the target: *"esse arquivo foi mapeado
  para essa entrada com 60% de certeza. Remover esse alerta?"* The owner
  **confirms** (removes the alert), **fixes** (re-associates) or resolves it at
  the source by **adding the id to the name**.
- The owner's correction is **authoritative** — it goes in through the resolver's
  override mechanism (see [04](04-data-model.md)).

### Outside the Kodi standard: verticals of their own

The standard covers **movies, series and music**. Verticals it does not cover
bring **their own identity rule and their own provider**, isolated in
`extension_data` (see [04](04-data-model.md)) — **without touching the core, the
schema or the resolver**:

- **Music video** → **IMVDB** provider (e.g. namespace `core.musicvideo@1`).
- **Comics** → `ComicInfo.xml` / the Komga-Kavita ecosystem.

## Post-scan: the scan closes its own cycle

Since [ADR-0031](adr/0031-post-scan-per-library.md), scanning a library has a
FINAL step, executed after the last committed batch:

- **No enrichment inside the transaction.** The auto-match of the new item and
  the optional materialization run **after the `Commit`** — inside the
  transaction the auto-match cannot see the row (read pool) and it would stall
  the scan on the single write connection (ADR-0010).
- **Reconciliation of the library itself.** When it finishes, the scanned source
  reconciles the tracks and series that were left as drafts **of its own** and
  retries the auto-match of its pending movies — with a per-run cap
  (`scanner.CapReconciliacao*`), without the multi-day throttle of the periodic
  job (this is the retry of the scan the owner has just asked for), but with a
  **12h floor** per item (`scanner.PosScanHorasMinimas`) and a **4 min ceiling**
  of wall-clock time (`posScanBudget`): without them, every scan retried the same
  200 tracks and burned 45+ min of external calls without resolving anything new
  (the caps come from [ADR-0031](adr/0031-post-scan-per-library.md)). The list
  comes sorted by "never tried first", so the queue moves forward on every scan
  even with both ceilings.
- **Purge of the replaced draft.** The reconciliation that re-attaches a file to
  the definitive entity deletes the draft left over with no file, no link and no
  override — it stayed in the review queue forever as "sem arquivo" (114 husks
  measured in production). A draft with a file, with a linked child or with an
  owner override is never touched.
- **Optional materialization** (`materializar_artes_automaticamente`) of whatever
  gained an identity in this step.

The step is best-effort and cancellable through the same context as the `scan`
item of the queue; it reports its own progress as a progress **message** (the
counter is still the file counter).

## File lifecycle: `missing` and offline datasource

The scan is **non-destructive in absence too**: a disappearance never deletes a
record.

- **File not found, datasource reachable** → the `media_file` becomes
  **`missing`** (it keeps `last_seen`) and is **not** removed. If it reappears
  (same `content_hash`), it goes back to `present`.
- **Whole datasource unreachable** (unmounted drive, offline share) → **nothing**
  is marked `missing`; the datasource is flagged **offline** and its scan is
  suspended. It avoids an unmount turning into the "disappearance" of thousands
  of items.
- **Deleting for good** (`purge`) is always an **explicit action** of the owner —
  never a side effect of the scan.

Consequence in the model: a `missing` `media_file` **does not count towards
ownership**; the entity stays alive and its `status` remains intact (see
[04 — Lifecycle and deletion](04-data-model.md)).

## Reusable reference (v1)

Kept in the planning session's scratchpad — rewrite it in Go, do not port it:

- **Valid video extensions:** `.mp4 .mkv .avi .mov .wmv .flv .webm` (starting
  point; extend it).
- **Mediainfo shape** (general/video/audio) already mapped in v1 — a good
  reference contract for what to extract.

## Open decisions (see [11](11-open-decisions.md))

- **Extraction tool: `ffprobe` (leaning) vs `mediainfo-cli`.**
- Incremental scan (mtime/hash) vs full; filesystem watching vs on demand.
- Confidence threshold that fires the review alert (tuning, not model).

> **Matching strategy:** decided — Kodi/Jellyfin convention, see
> [Identity and matching](#identity-and-matching). An id embedded in an existing
> NFO counts as a deterministic id (same key family).
