# 09 — NFO and export

> **Status:** scoped stub. Expanded as the Phase 4 work lands.

## Principles

- **Export is optional output, not the truth.** The source of truth is the
  database. NFO (and other formats) are **projections** of the resolver result,
  produced when the owner wants them — never the primary storage mechanism
  (that is precisely what sets it apart from TinyMediaManager).
- **Non-destructive.** Generating NFO/artwork in the folders is an explicit and
  optional action.
- **It reflects the override.** The exported NFO carries the owner's title/data.
- **It preserves what is not ours.** When rewriting an existing `.nfo`, any
  top-level element that MyMediaDB does not generate/manage (Kodi tags,
  `<fileinfo>`, manual edits, etc.) is re-embedded exactly as it was — only
  the fields MyMediaDB actually models (title, synopsis, cast…) always come
  from the database.
- **`<thumb>`/`<rating>` in the format Kodi/Jellyfin can actually read.** A
  movie/series poster/background is always materialized on disk BEFORE the `.nfo`
  is written (it never points at MyMediaDB's internal API) — Kodi/Jellyfin
  discover the artwork on their own through the file naming convention.
  `<actor><thumb>` uses the provider's direct URL (never Kodi's `.actors`
  convention, which Jellyfin does not support). `<rating>` is written in Kodi's
  current format, with the `<ratings>` wrapper.

## Scope

- NFO in the **Kodi/Jellyfin/Emby/Plex** standard, per type (`movie`, `tvshow`,
  `season`, `episode`, and future verticals).
- Secondary target: the **`/api/tmdb`** API ([05](05-api.md)) already covers
  consumption without writing files — the NFO is for those who prefer the
  classic flow.

## Reusable reference (v1)

v1 had an NFO model (music video) with **every Kodi tag documented inline**
(requiredness, multiplicity, notes). That rigor in documenting the tag-by-tag
mapping is the standard to follow for movie/tvshow/episode. Rewrite in Go; use
the reference preserved in the scratchpad as the basis for the mapping.

## One file, one NFO, one block per episode

A `.nfo` belongs to the **video file**, not to the entity (Kodi names it after the video and
takes the episode numbering from the file name). A video that holds several episodes
(`S01E01E02`, `S02E01-E02-E03`) is one file with one `.nfo`, whose document carries **one
`<episodedetails>` block per episode, in file order** — the format Kodi's own library export
writes (and the reason `<season>`/`<episode>` come from the entity tree, never from the file
name). See [ADR-0044](adr/0044-one-nfo-per-file.md).

The local fields the owner or Kodi wrote (rating, watched, play count, resume, date added and
any unrecognized tag) are preserved **per block**, matched by `<season>`/`<episode>`; a legacy
single-block file is matched to the first episode of the file.

## Open decisions (see [11](11-open-decisions.md))

- Template engine vs direct struct→XML serialization.
- Writing artwork alongside the NFO or only the NFO.
