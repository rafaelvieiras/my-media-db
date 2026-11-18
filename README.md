<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img src="assets/logo.svg" alt="MyMediaDB" width="300">
  </picture>
</p>

<p align="center">
  <strong>Your own media database. Local, self-hosted, and yours to rule.</strong><br>
  A catalog that applications read, not another app that locks your data in.
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
  <img alt="Status: pre-1.0" src="https://img.shields.io/badge/status-pre--1.0-yellow.svg">
  <img alt="Go" src="https://img.shields.io/badge/core-Go-00ADD8.svg">
  <img alt="React" src="https://img.shields.io/badge/ui-React%20%2B%20Vite-61DAFB.svg">
  <img alt="SQLite" src="https://img.shields.io/badge/store-SQLite-003B57.svg">
</p>

---

## What is MyMediaDB?

MyMediaDB is a personal media catalog that runs on your own server. It scans your
library folders, reads the technical metadata of each file, and pulls extra data
from external sources only when you ask for it. Every title, poster and rating
stays editable by you. Everything is served by an API that any client can read
(Jellyfin, Kodi, Plex, your own scripts) without quotas or approval.

It works as a **library manager** in the spirit of tinyMediaManager. It also acts
as **your own private TMDB**: the source of truth is your database, and your word
beats any external source.

## Why it exists

Existing tools solve part of the problem and then hit the same wall: someone else
owns your data.

| Pain                                                        | How tools handle it today                         | What MyMediaDB does                                                          |
| ----------------------------------------------------------- | ------------------------------------------------- | ---------------------------------------------------------------------------- |
| Metadata lives in scattered `.nfo` files, never queryable   | tinyMediaManager is stateless and only writes NFO | One database with an API. NFO becomes an optional export, not the truth      |
| Your artwork or edit needs a moderator's approval           | TMDB, Fanart.tv                                   | Nobody approves anything. Your art and text go in                            |
| The Brazilian title (VHS, TV) differs from the official one | You are stuck with the TMDB name                  | Override layer: you rename it, and your name wins                            |
| A short film outside the official taxonomy disappears       | TMDB philosophy rejects it                        | Your taxonomy: you decide what is an episode, a collection, or anything else |
| You want media that no database covers well                 | No decent source exists                           | Universal, extensible model. New verticals join without touching the core    |
| The public API has quotas and rules that are not yours      | TMDB API                                          | Your API, no limits, your users and permissions                              |
| You want to plan media you do not own yet                   | The manager only sees files                       | The catalog is bigger than the library (the `status` field)                  |

## Features

- **Library scan** — watch folders for movies, series, and music. Reads
  duration, resolution, codec, channels, and tags with `ffprobe`.
- **Matching** — resolves each file against TMDB, TheTVDB and MusicBrainz, with a
  confidence score. Automatic match runs after the scan.
- **Review queue** — uncertain matches go to one screen, grouped by album, series
  or folder, so you decide once per group instead of once per file.
- **Enrichment on demand** — descriptions, cast, ratings, posters, backdrops,
  and stills. Each provider is optional; without keys the app still runs.
- **Override layer** — edit any field and your value wins over the source. The
  original value stays readable.
- **NFO export** — writes Kodi/Jellyfin NFO next to your files, only when the
  content changed.
- **Music vertical** — albums and tracks with MusicBrainz, AcoustID and Cover Art
  Archive, including compilations and deluxe editions.
- **Native API** — `/api/v1` for everything the app does, plus a TMDB-compatible
  namespace for clients that expect it.
- **Single container** — Go binary with the React UI embedded. SQLite, no
  separate database server, runs on a Raspberry Pi.

## Quick start

You need Docker with Compose.

```bash
# 1. Get the compose file and the optional credentials template.
curl -O https://raw.githubusercontent.com/rafaelvieiras/my-media-db/master/compose.yaml
curl -o .env https://raw.githubusercontent.com/rafaelvieiras/my-media-db/master/.env.example

# 2. Point the media volume in compose.yaml to your library folder.

# 3. Create the data folder. The container fixes its ownership on start: it runs as
#    PUID:PGID (99:100 by default, the Unraid convention) and adopts ./data.
mkdir -p data

# 4. Start it.
docker compose up -d
```

Open `http://localhost:8080`. Add a library, pick the folder inside the container,
and press **Scan**.

The image is public and multi-arch (`linux/amd64`, `linux/arm64`,
`linux/arm/v7`):

```bash
docker pull ghcr.io/rafaelvieiras/my-media-db:latest
```

## Configuration

Every key is optional. Without keys the app starts and the external providers stay
off. You can also set each provider from the UI, and the UI value wins over the
environment.

| Variable                        | Default                               | Meaning                             |
| ------------------------------- | ------------------------------------- | ----------------------------------- |
| `PORT`                          | `8080`                                | HTTP port inside the container      |
| `PUID` / `PGID`                 | `99` / `100`                          | User the app runs as, and the owner `/data` is set to |
| `MYMEDIADB_PRE_MIGRATION_BACKUPS` | `3`                                 | How many pre-migration database copies to keep |
| `MYMEDIADB_SKIP_PRE_MIGRATION_BACKUP` | _(off)_                        | `1` migrates without the safety copy |
| `MYMEDIADB_DB_PATH`             | `/data/mymediadb.sqlite` in the image | Database file                       |
| `MYMEDIADB_ASSETS_DIR`          | `/data/assets` in the image           | Downloaded artwork                  |
| `MYMEDIADB_TMDB_TOKEN`          | _(empty)_                             | TMDB API read token (v4, preferred) |
| `MYMEDIADB_TMDB_API_KEY`        | _(empty)_                             | TMDB API key (v3, alternative)      |
| `MYMEDIADB_FANARTTV_API_KEY`    | _(empty)_                             | fanart.tv, artwork only             |
| `MYMEDIADB_TTDB_API_KEY`        | _(empty)_                             | TheTVDB, series metadata            |
| `MYMEDIADB_ACOUSTID_CLIENT_KEY` | _(empty)_                             | AcoustID, audio fingerprint         |

Both paths live under the `/data` volume, so `docker run -v mydata:/data` (or the
`./data` bind mount in `compose.yaml`) is all you need to keep the database and
the artwork. Running the binary without Docker keeps its development default,
`tmp/mymediadb.sqlite`.

Set `PUID`/`PGID` to the owner of your media and data folders (`id -u` / `id -g`).
Starting the container with an explicit `--user` also works: the entrypoint then leaves
the ownership alone and runs as that user.

## Updating

The image follows a moving `latest` tag, so a NAS that upgrades containers on a schedule
does it unattended. Two guarantees make that safe:

- **The database is copied before any schema migration.** The copy lands in
  `/data/backups/pre-migration-schema-<old>-<timestamp>.sqlite`, made with `VACUUM INTO`
  (a consistent snapshot while the app runs). The three newest are kept; rolling back is
  copying one file back over `/data/mymediadb.sqlite`.
- **A database newer than the binary refuses to boot.** If you roll the image back after
  an automatic upgrade, the old binary stops with a message naming both versions instead
  of writing to a schema it does not know. Nothing is modified in that case.

Migrating without the copy is possible (`MYMEDIADB_SKIP_PRE_MIGRATION_BACKUP=1`) and it is
logged as a warning, because the upgrade then has no way back.

MusicBrainz and Cover Art Archive need no key. See
[`docs/07`](docs/07-enrichment-and-providers.md) for provider precedence and cache
rules.

## How it works

1. **Scan** — walks the library folders and records each file. A file becomes a
   draft entity with its raw tags.
2. **Match** — tries to resolve the draft against the sources. Confident matches
   become cataloged. The rest wait in the review queue.
3. **Enrich** — for a cataloged entity, fetches the remaining fields and the
   artwork, and stores them in the database and the assets folder.
4. **Export** — writes NFO and artwork next to the media file, one way only. It
   never renames or moves your files.

Steps 2 to 4 also run at the end of every scan, so a new file is processed without
a second click.

## Status

MyMediaDB is **pre-1.0** and under active development. The database schema is
versioned and migrations run on start.

> [!WARNING]
> **There is no authentication yet.** Anyone who reaches the port can read and
> change the catalog. Run it on your local network or behind your own reverse
> proxy with access control. Tokens and scopes arrive in a later release (see
> [`docs/10`](docs/10-users-and-permissions.md)).

## Documentation

| Doc                                                                  | Content                                                       |
| -------------------------------------------------------------------- | ------------------------------------------------------------- |
| [01 — Vision and use cases](docs/01-vision-and-use-cases.md)         | Normative scenarios the implementation must satisfy           |
| [02 — Glossary](docs/02-glossary.md)                                 | Canonical vocabulary                                          |
| [03 — Architecture](docs/03-architecture.md)                         | Go + Vite/React, single container, layers                     |
| [04 — Data model](docs/04-data-model.md)                             | Base entity, override layer, extension data, `status`, assets |
| [05 — API](docs/05-api.md)                                           | `/api/tmdb` (compatible) and `/api/v1` (native)               |
| [06 — Library and scan](docs/06-library-and-scan.md)                 | Datasources, walk, mediainfo, matching, dedup                 |
| [07 — Enrichment and providers](docs/07-enrichment-and-providers.md) | On-demand sources, cache, image proxy                         |
| [08 — Artwork and assets](docs/08-artwork-and-assets.md)             | Storage, art types, custom upload                             |
| [09 — NFO and export](docs/09-nfo-and-export.md)                     | Kodi/Jellyfin NFO as an optional output                       |
| [10 — Users and permissions](docs/10-users-and-permissions.md)       | Multiuser, tokens, scopes                                     |
| [11 — Open decisions](docs/11-open-decisions.md)                     | What is not decided yet, risks                                |
| [12 — Roadmap](docs/12-roadmap.md)                                   | Phases and what each one delivers                             |
| [ADRs](docs/adr/)                                                    | Closed architecture decisions, one file each                  |

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the
environment, the commands, and the conventions. If you use a coding agent, see
[AGENTS.md](AGENTS.md).

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). To report
a security problem, read [SECURITY.md](SECURITY.md).

## License

Licensed under the [MIT License](LICENSE).
