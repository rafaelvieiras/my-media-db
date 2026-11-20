# 08 — Artwork and assets

> **Status:** scoped stub. Expanded as the Phase 3 work lands.

## Principles

- **Binary on disk, metadata in the database.** An `asset` keeps only
  type, origin, path/URI and metadata in SQLite; the file itself lives on disk
  (never in the JSON — see [04](04-data-model.md)).
- **Origin `external` or `user`.** Artwork pulled from sources and artwork
  uploaded by the owner coexist.
- **The owner's choice wins.** The artwork marked as preferred is the one the
  API serves by default — including for Jellyfin
  ([Scenario B/G](01-vision-and-use-cases.md)).
- **Upload without approval.** The owner uploads whatever artwork they want, with
  no external moderation.
- **Non-destructive.** Re-enriching does not remove custom artwork.

## Asset types (starting point)

`poster`, `fanart/backdrop`, `banner`, `clearlogo`, `thumb`, `season poster`,
`episode thumb` — to be extended as verticals land.

## File names on disk

Two conventions, because a video gives its name to the artwork and a music folder does not
([ADR-0043](adr/0043-music-artwork-lands-in-the-folder.md)).

**Video (movie/series/season/episode)** — the artwork takes the video's base name when the
folder holds more than one video, and the bare name when the folder is exclusive:
`<video>-poster.jpg` / `poster.jpg` (see `localmedia.NomeArquivoArte`).

**Music (album/artist)** — always bare, in the entity's own folder (the album folder, or the
artist folder one level above it), under the names the library already uses:

| kind | file |
| --- | --- |
| `poster` | `folder.jpg` |
| `fanart` | `backdrop.jpg` |
| `banner` | `banner.jpg` |
| `logo` | `logo.png` |
| `disc` | `cdart.png` |

A music entity with no reachable folder is **not** materialized: the destination is resolved
before the download, and "no destination" is a no-op, never a success.

## Image proxy and cache

External images are fetched on demand and **cached locally** after the first
access, avoiding a continuous dependency on the source.

## Open decisions (see [11](11-open-decisions.md))

- Directory layout for the cache/assets on disk.
- Thumbnail/derivative generation and formats.
