# 03 — Architecture

## Shape

MyMediaDB ships as **one binary and one container image**. The same Go process
serves the **static front-end assets** and answers the **API** on the same origin.
No Node in production, no mandatory second process, low memory.

One tension shapes the design: the management app exists only while it is open,
but the **API must always be up**, because Jellyfin may call it at 3 a.m. So the
**service is the product** and the UI is only one client of it, and that client can
even point at a remote instance.

## Components

```
┌──────────────────────────────────────────────────────────┐
│  Go binary  (one image, one process)                      │
│                                                            │
│   HTTP router                                              │
│    ├── /              → SPA (Vite dist, //go:embed)        │
│    ├── /api/tmdb/*    → TMDB-compatible layer              │
│    └── /api/v1/*      → native API                         │
│                                                            │
│   services                                                 │
│    ├── scanner      (walk the library sources)             │
│    ├── mediainfo    (ffprobe extraction, subprocess)       │
│    ├── enrich       (external providers, on demand)        │
│    ├── artwork      (proxy cache plus custom upload)       │
│    └── resolver     (composes override ⟵ external ⟵ file)  │
│                                                            │
│   persistence                                              │
│    └── SQLite (modernc.org/sqlite, CGO-free)               │
│        plus media and artwork files on disk (path in DB)   │
└──────────────────────────────────────────────────────────┘
```

## Stack

| Layer | Choice | Why |
| --- | --- | --- |
| Backend | **Go** | Single binary, low RAM, no heavy idle runtime, easy cross-compile for the server, including ARM. |
| Front end | **Vite + React** | Modern React with a light static build. **No Next.js or SSR**, which contradicts "low memory". |
| Database | **SQLite** | Local-first, zero configuration, one file. Request volume is **not a bottleneck** for self-hosted use. Concurrency uses **WAL plus a single writer**, below. |
| Packaging | **One container** | `//go:embed` of the Vite `dist/`; Go serves everything. |

The backend library choices are closed in
[ADR-0014](adr/0014-implementation-stack.md): the **chi** router, the
**`modernc.org/sqlite`** driver (CGO-free), data access through **sqlc**, and
mediainfo through **ffprobe**.

## Concurrency and persistence

"API always up" versus "scan writes hard" hits the SQLite limit of **one writer at
a time**. Without a strategy, the writer blocks readers and the UI freezes with
`database is locked` during a scan. Three measures solve it.

**1. WAL and pragmas.** The database opens in **WAL** (write-ahead logging), which
allows **several readers and one writer at the same time** without mutual
blocking:

```sql
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;   -- wait and retry instead of failing with 'locked'
PRAGMA synchronous = NORMAL;  -- durability versus speed, safe under WAL
PRAGMA foreign_keys = ON;
```

**2. Two connection pools.** Reads use a **pool of N connections**. **Every write**
goes through **one single write connection**, which falls out of a write pool of
size one. The API reads while the scan writes, and SQLite never sees two writers.

```
read pool  (N conns)  ← API GETs and scan reads
write conn (1)        ← overrides, imports, and scan writes
```

**3. Short writes, the owner first.** The scanner commits in **short batches**,
every N items, never in one giant transaction, so it does not hold the writer or
delay the WAL checkpoint. **Interactive** writes from the owner (override, import)
take **priority** over bulk scan writes: an edit never waits for the whole walk.

> **Evolution:** if priority needs to become explicit or richer, with backpressure
> and ordering, the single writer can become a dedicated **writer
> queue/goroutine** without changing anything else. Scan data is mostly
> **re-derivable**, since mediainfo can be extracted again, so
> `synchronous=NORMAL` is a safe trade-off.

## The execution queue is observable

Every scan and every job runs through **one** persisted queue (`queue_item`, one sequential
worker — [ADR-0028](adr/0028-unified-execution-queue-scan-and-jobs.md)), and that queue is also the place
where a run says what it is doing ([ADR-0040](adr/0040-queue-observability.md)):

```
Runner (scan | job handler)
   └── Registro ──┬── Progresso  snapshot: counters, step (i/n + label), current item
                  └── Evento     trail:    one row per finished item (ok | error | skip)
```

- **The snapshot** lives in `queue_item` (`progress_current/total`, `step_index/step_total/
  step_label`, `current_item`, `progress_ok/progress_failed`). It always describes the present,
  so it is cheap to overwrite: one `UPDATE` per report, with identical payloads filtered in
  memory.
- **The trail** lives in `queue_item_event`, capped at 200 rows per run and pruned at write
  time. It is what answers "which items succeeded and which failed" after the run — a bounded
  answer, not a log.
- **A job handler opts in** through `job.HandlerComProgresso` (capability by type assertion, the
  same pattern the providers use). A handler that does not implement it keeps running with
  `0/0` and no trail, unchanged.
- **Durations are derived, never stored**: `wait_ms` (`enqueued_at → started_at`), `elapsed_ms`
  (`started_at → now`) and `duration_ms` (`started_at → finished_at`). One definition in
  `internal/execqueue`, reused by `GET /jobs`, `GET /jobs/{id}/executions`,
  `GET /library/{id}/scan`, `GET /queue` and `GET /queue/{id}` — the same number on every
  screen.

`GET /api/v1/queue` is the whole picture in one request: the running item, the FIFO with
positions and wait times, and the most recent finished runs, each with a human label for its
reference (job name, source path).

## Run modes and the API base

The front end reaches the API through a configurable base, `VITE_API_BASE`:

| `VITE_API_BASE` | Behavior |
| --- | --- |
| **empty** (default in production) | The front end uses **relative paths**. The Go binary serves the static files and answers `/api/*` on the same origin. One container, all together. |
| **set** | The front end points at **that URL** as the API base. Used in dev (Vite on 5173 with the Go API on 8080) and to point the app at a **remote instance**. |

### Development

- `vite dev` with HMR on 5173, with `VITE_API_BASE=http://localhost:8080`.
- The Go binary serving only the API on 8080.

### Production

- `vite build`, then `dist/` embedded in the binary through `//go:embed`.
- Empty `VITE_API_BASE` means same origin: Go falls back on any path outside `/api`
  and returns `index.html` for client-side routing.

## Two languages, deliberately separate

The interface language and the language the **metadata** is read in are different
settings, and conflating them is the mistake this project already made once
([ADR-0032](adr/0032-ui-i18n-dependency-free-catalogs.md)):

| | Interface language | Reading language |
| --- | --- | --- |
| What it is | Copy of the chrome: navigation, buttons, labels, empty states | The language a title, synopsis or tagline is served in |
| Where it lives | `localStorage` of the browser (`mmdb.ui-lang`) | `app_settings` in the database, `primary` + `allowed` |
| API | none | `GET /entities/{id}?lang=<code>` for an entity, `?lang=` on the lists (ADR-0019) |
| Default | English (`navigator.language` is used only when it matches a supported locale) | The `primary` configured by the owner |
| Effect | Re-renders the chrome in place | Re-fetches the reads and recomposes the screen without a reload |

The catalogs live in `web/src/lib/i18n/catalog/`, one file per domain with the
three locales side by side (`en`, `pt-BR`, `es`). English is the source of truth:
the other two are typed against it, so a missing or extra key fails `tsc` instead of
shipping a blank label. There is no i18n runtime — a lookup and `{param}`
interpolation, with `Intl.*` handling the formatting.

## The resolver

Reading is centered on the **resolver**, which builds the final view of each entity
by composing three layers in precedence order, detailed in
[04 — Data model](04-data-model.md):

```
result  =  user OVERRIDE     (always wins)
       ⟵  EXTERNAL data      (TMDB and others, local cache)
       ⟵  FILE data          (mediainfo, detected)
```

Both `/api/v1` and `/api/tmdb` serve **from the resolver result**, never from the
raw external record. That is what makes the owner's override appear everywhere,
including in Jellyfin.

## Ready for verticals and plugins

The core gives no privilege to "movie" or "series". It knows a **generic entity**
with namespaced **extension data** (see [04](04-data-model.md)). That is what allows
new verticals and third-party **plugins** later without a core schema change. The
plugin machinery itself — registry, lifecycle, sandbox — stays **outside the MVP**
(see [11](11-open-decisions.md)).

One concrete extension point already exists for **integration config**:
`internal/integration.Registry`, the same shape as the provider registry, maps one
`Descriptor` per integration. Today only TMDB, TheTVDB, and fanart.tv register
there, but a future plugin would register the same way with no change in the core.
Credentials written through the UI (`/dashboard/integrations`) win over the
bootstrap environment variable
([ADR-0023](adr/0023-integration-config-through-the-ui-wins-over-env.md)).
