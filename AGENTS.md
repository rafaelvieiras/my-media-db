# AGENTS.md — guidance for agents (human and LLM)

> Read this **before you write code**. It is the entry point: principles that do
> not bend, operating practices that avoid rework, and where to go next. It
> **points** to the authoritative sources and never duplicates them. When this
> file disagrees with a doc or an ADR, the specific source wins and this file must
> be fixed.

## What the project is, in one sentence

MyMediaDB is a local-first media library manager in the spirit of
tinyMediaManager: it catalogs, enriches, and **downloads artwork, subtitles,
trailers and themes to disk** — it is more than a database. Go backend (chi,
`modernc.org/sqlite`, sqlc, goose) and a Vite/React front end (TanStack
Router/Query, shadcn/ui). Module `github.com/rafaelvieiras/my-media-db`.

## Principles that do not bend

The first five are in the [README](README.md) and
[CONTRIBUTING](CONTRIBUTING.md). The sixth is a firm rule of the project owner.

1. **The owner is the authority.** The local database is the source of truth and
   external data is a suggestion. Every user edit is an override that always wins.
2. **The catalog is larger than the library.** It holds what you own and what you
   plan to own, through `status`. No workarounds.
3. **Universal and extensible without touching the core.** Verticals and
   integrations live in namespaced `extension_data`, with a free `entity.type` and
   a generic `entity_relation` with `kind`. **Never** add per-type columns or join
   tables. A new vertical joins **without a core migration**.
4. **Local-first and light.** One binary, one image, low memory. External sources
   are optional enrichment, pulled on demand, never an automatic sync.
5. **Non-destructive.** Nothing deletes the owner's data on its own. A purge is
   always explicit.
6. **Provider independence and capability-based enrichment.** The media survives
   any registered provider. With one key (TMDB *or* TheTVDB) the admin searches
   and imports both movies and series. A provider that cannot model a field leaves
   it empty and never blocks the vertical. Providers declare **capabilities**
   through optional interfaces, discovered by type assertion, and degrade in
   silence when absent. This closes the open decision "universal scraper versus
   explicit choice" in [docs/11](docs/11-open-decisions.md).

A pull request that breaks a principle will probably be rejected.

## How the project works

- **Issues first.** Every non-trivial change starts as an issue that states the
  problem, the expected result, and the acceptance criteria.
- **A structural decision becomes an ADR.** Schema, resolver, API, or principle
  changes need a new ADR in [docs/adr/](docs/adr/) that supersedes the old one.
  The [decision agenda](docs/00-decision-agenda.md) is the live backlog.
- **The docs describe how the application is.** Read
  [docs/03](docs/03-architecture.md) and [docs/04](docs/04-data-model.md) for the
  map.

## Architecture in three lines

- **Three-layer resolver** (`internal/resolver`): `override ← external ← file`,
  merged by **field class** and **namespace**, not by fixed name. A new field
  needs one registry entry. External layers arrive sorted by priority.
- **Providers** (`internal/provider`): a minimal core (`Name/Search/FetchDetail/
  MapDetail`) plus optional capabilities. An N-provider registry where order means
  priority. Cross enrichment writes a `media_external` row from another source
  without clobbering the base identity.
- **Artwork** (`asset`): `kind` is free text in the database, so new kinds need no
  migration. Precedence: preferred, then user, then external, then base, then 404.
  The preferred asset can also be materialized inside the real library folder
  (`asset.local_path`) with a canonical Kodi/Emby name, and an entity can have an
  exported `.nfo` (`entity.nfo_path`). NFO export is one-way: database to disk.

## Operating practices

**Validate for real. The cache lies.**

- `go build ./...`, `go vet ./...`, `go test ./... -count=1`. The `-count=1` is
  mandatory: `go test` caches results and hides failures.
- Web: `tsc -b` has zero tolerance, plus `npm run lint` and
  `npm run test:coverage`.
- Ratchet: use `make gate-fast` while you iterate, because it only runs the
  collectors for the side your diff touched. Keep the full `make gate-local` for
  one final pass before the commit. Repeating the full gate to confirm is the
  biggest waste of time in this repo.
- **Never run `make baseline`.** Only the owner advances the baseline.
- When a collector fails to run, the terminal shows only the last lines. The full
  output is in `reports/<metric>-failure.log`.

**Conventions that break CI when ignored.**

- **A documentation-only change does not run the ratchet, and that is on purpose.**
  The `changes` job of `quality-gate.yml` classifies the diff: when every changed
  file is `*.md`, `docs/`, `LICENSE` or the root `assets/`, the Go and TS sides are
  skipped and the report job says so instead of publishing a table of numbers that
  cannot have moved. `e2e.yml` and `release.yml` skip those pushes at the trigger.
  So a `docs:` commit never costs a Chromium download, never rebuilds the image, and
  never turns the release red. Two consequences for you: a doc fix no longer needs
  the gate in the PR, and a `docs:` commit cannot release a version even if you
  wanted one.
- sqlc corrupts accent marks in `.sql` query files, so queries stay in **ASCII**.
  Migrations may use any text.
- Generated files (`gen:types`, sqlc) must land already formatted.
- A new dependency in `web/package.json` appears only after
  `docker compose -f compose.dev.yaml restart web`, because `node_modules` is an
  anonymous volume.
- **Multi-language is foundational**: title, overview, and tagline per language
  through `media_external`. Never assume a single language.

**Library on disk: scan and watch.**

- The scan is incremental. It re-hashes a file only when `mtime` or size changed.
  Do not bring back an unconditional hash.
- `fsnotify` does not report remote-side changes on FUSE mounts (rclone, the real
  deployment). That is a kernel limitation. The periodic job is the base and the
  watcher only accelerates.

**Commits and writing.**

- Conventional Commits in **English**, imperative, subject of 100 characters or
  fewer. The release pipeline reads them.
- No AI co-author trailer.
- **Secrets never go in the code.** An API key lives in the database (Integrations
  screen, [ADR-0023](docs/adr/0023-integration-config-through-the-ui-wins-over-env.md))
  or in a bootstrap environment variable. The database wins when the owner set the
  value in the UI.

### Language

Publication is the line in the sand. Before it, the project was written in
Portuguese everywhere — identifiers, comments, docs, file names — because there
was no audience. That history is real and stays where it is: **do not rename
existing Portuguese for its own sake.**

What a file must follow **from now on**:

- **New code is English**: function names, parameters, local variables, types,
  constants, comments, log messages, error strings, test names, mock data.
- **New files are English**: file names, package names, directories, migration
  names.
- **New text is English**: docs, ADRs, README, UI copy, commit subject and body,
  PR title and description, issue titles and bodies.
- **Touching a file is an opportunity, not a mandate.** When you rewrite a
  function or a block anyway, leave it in English (and rename what you rewrote).
  Do not translate the rest of the file in the same commit.
- **A rename never travels alone.** A rename that reaches dozens of call sites is
  its own PR with no behavior change, or it waits.
- **Portuguese stays allowed for data, never for code**: titles, overviews,
  taglines, provider payloads and the owner's media in the database.

So a mixed tree is expected. The ratchet is simple: **new code never adds to the
Portuguese side.**

**Scale and context.**

- Break large, multi-layer work into narrow agents per layer: data and sqlc, then
  the algorithm and provider, then integration, then tests. One monolithic agent
  covering ten subsystems burns far more context than five narrow ones.
- Dev environment: `docker compose -f compose.dev.yaml up` (web on 5173, api on
  8080 with air). The dev database lives in `tmp/mymediadb.sqlite`.

## Where to go next

- **Roadmap:** [docs/12](docs/12-roadmap.md).
- **Open work:** the [issues](https://github.com/rafaelvieiras/my-media-db/issues).
- **Open decisions:** [docs/11](docs/11-open-decisions.md).
