# ADR-0026 — Trailer/theme: periodic job + automatic trigger on scan (opt-in)

- **Status:** Accepted
- **Date:** 2026-08-12
- **Agenda:** [TR3](../00-decision-agenda.md) · **Spec:** [ADR-0025](0025-trailer-and-theme-through-ytdlp-and-themerrdb.md), [07](../07-enrichment-and-providers.md)

## Context

[ADR-0025](0025-trailer-and-theme-through-ytdlp-and-themerrdb.md) (item 5)
recorded that trailer/theme download would be **always an explicit pull** — "a
button or job the admin turns on deliberately, **never automatic on every
scan**". After the manual per-entity button shipped and was working
in production, the owner asked explicitly: "the possibility of fetching the
missing trailer and theme was left out, besides adding that to the
scan/sync routines and to the jobs" — that is, he wants **bulk detection** and
**automatic execution**, not just one click per item.

That seems to contradict the "never automatic on every scan" clause of ADR-0025
and principle 4 of `AGENTS.md` ("never automatic sync"). But investigating the
code showed that the project **already has a direct precedent** for exactly this
pattern, predating ADR-0025 itself:

- `autoMaterializeArtworkSettings` (`internal/server/settings.go`) is an
  `app_settings` toggle that, when turned on, already makes the scan **download
  artwork automatically** for a newly imported item (`MaterializarSeAutoDownloadLigado`,
  `internal/server/midias_faltantes.go:161`) — no click from the owner per
  item, just the decision to turn the toggle on once.
- The movie enrichment already runs **automatically on scan** (metadata,
  including the theme music candidate via `enriquecerTemaMusical`,
  `internal/enrich/enrich.go:146-150`) — again, no explicit pull per item.
- The `midias_faltantes` job already runs **scheduled** (cron), materializing
  what is missing across the whole library without manual intervention — it is
  opt-in only in the sense that `job_definition.enabled=0` by default; once
  turned on, it runs by itself.

That is: principle 4 ("never automatic sync") was always about **not
re-fetching external data without the owner asking** (not re-querying TMDB/TVDB
on its own under some refresh policy) — never about forbidding automatic
materialization of data **already resolved/cached**, as long as it sits behind an
explicit opt-in (toggle or job disabled by default). The "never automatic on
every scan" clause of ADR-0025 item 5 was, in retrospect, more conservative than
the pattern the rest of the project already practices.

## Decision

**This ADR refines item 5 of ADR-0025** (the rest of 0025 — yt-dlp downloader,
ThemerrDB, storage convention — remains intact and is not revised here):

1. **Bulk detection**: there is no (nor is there a need for a) database column
   "has local trailer/theme" — the state is always derived via `os.Stat` on the
   real folder, the same pattern `midias_faltantes` already uses for artwork.
   Bulk detection is the iteration of the new job itself (item 2), reusing the
   existing `ListarEntitiesComMediaFilePresente`.
2. **New opt-in periodic job** (`job_definition.enabled=0` by default, same
   pattern as `midias_faltantes`/`saude_biblioteca`): sweeps the library and
   downloads the missing trailer/theme, best-effort per entity (an error in one
   does not abort the others). It reuses the existing jobs UI
   (`/dashboard/jobs`) — it gets "run now"/scheduling/history for free, with no
   new UI.
3. **Automatic trigger on scan, behind a new toggle** (same mold as
   `autoMaterializeArtworkSettings`): when on, the import/auto-match of a new
   item fires a trailer/theme attempt **in a best-effort goroutine** (it does not
   block the scan response) — the same reason for caution already recorded in the
   general principle of not blocking the scan with heavy network I/O (downloading
   a video via yt-dlp is orders of magnitude slower than an image).
4. **The periodic job remains the base; the scan trigger only accelerates** —
   the same reasoning `AGENTS.md` already uses for watch vs. periodic job
   (fsnotify "only accelerates", it is never the single source of truth): if the
   scan's synchronous-in-goroutine attempt fails (yt-dlp binary still
   downloading, YouTube rate-limiting, toggle turned on after the item was
   already imported), the periodic job covers it in the next run.
5. The two mechanisms (job and scan trigger) **share the SAME toggle**
   — it makes no sense to separate "I want it automatic in the job" from "I want
   it automatic in the scan"; it is a single decision by the owner ("I want
   automatic trailer/theme or not"). The job itself always exists registered (like
   every job in the system) and can be scheduled/run manually regardless of the
   toggle; the toggle only controls the trigger *inside the scan*.

## Consequences

- Closes the owner's request without inventing a new mechanism — it reuses 100%
  of the job pattern (the `midias_faltantes` mold) and of the settings toggle
  (auto-materialize) already validated in production.
- `AGENTS.md` principle 4 remains respected by the correct reading: explicit
  opt-in (toggle off by default, job disabled by default) — never automatic
  "out of the box".
- Cost: one more settings toggle for the owner to manage; one more new job in
  the `/dashboard/jobs` list. Both follow the existing pattern, with no new UI
  surface to invent.
- Video download (yt-dlp) never runs synchronously-blocking inside the scan's
  HTTP request — only through a best-effort goroutine (trigger) or through a
  scheduled asynchronous job.
- Related decisions: [ADR-0025](0025-trailer-and-theme-through-ytdlp-and-themerrdb.md)
  (base spec, not revised here); the `midias_faltantes` job mold; the precedent
  of automatic-on-scan behind a toggle.
