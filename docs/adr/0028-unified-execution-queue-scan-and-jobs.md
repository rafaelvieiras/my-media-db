# ADR-0028 — Unified execution queue for scan and jobs

- **Status:** Accepted
- **Date:** 2026-08-19
- **Agenda:** — · **Spec:** [06-library-and-scan](../06-library-and-scan.md)

## Context

`internal/scanner.Manager` and `internal/job.Manager` are two parallel systems
that reimplement the same pattern — a loose goroutine per execution, state in a
map protected by a mutex — independently and redundantly:

- Scan has no history in the database (state lives only in memory, it disappears on
  restart) nor a concurrency limit between different `library_source`s:
  each successful `POST /library/{id}/scan` fires its own goroutine,
  with no queue and no cap at all (documented as a known gap in
  `internal/scanner/scan.go:9-11`).
- Jobs have persisted history (`job_execution`) and a built-in cron, but
  *different* jobs also run freely in parallel — nothing prevents
  `backup_vacuum` and `saude_biblioteca` from running at the same time, competing for the single
  SQLite write connection (`internal/db/db.go`, `Write.SetMaxOpenConns(1)`,
  ADR-0010).
- The scan progress counter (`files_seen`/`files_total`) increments per
  batch of 25 files (`loteEscrita`), not per file — in `content_type=music`,
  where each file may cost multiple network calls to MusicBrainz at
  1 req/s, that makes the counter look stuck at `0` for minutes of wall-clock time even
  with entities actually being created (found during the production validation
  of the music vertical).

Running all of that "loose" still survives today only because the volume is low; the
addition of the music vertical and the request for real progress
granularity turn the duplication and the absence of a concurrency limit into a
maintenance and resource-predictability problem (I/O, ffprobe,
provider calls) worth fixing at the root instead of patching the two
systems separately again.

## Decision

Unify scan and jobs under **a single background execution queue**,
persisted in the database, with **one global sequential worker** (concurrency 1): the
queue always keeps at most one execution in flight and, as soon as it
finishes, immediately starts the next enqueued item (it never stays idle
while there is pending work) — that is the operational reading of "there must
always be one open execution".

- New package `internal/execqueue` with a `queue_item` table (`id`, `kind`
  — `'scan'` | `'job'` —, `ref_id` — `library_source.id` or
  `job_definition.id` —, `trigger` — `manual` | `schedule` | `watch` —,
  `status` — `queued` | `running` | `success` | `error` —, `progress_current`,
  `progress_total`, `message`, `enqueued_at`, `started_at`, `finished_at`).
  Replaces `job_execution` (data migration: existing rows become
  `kind='job'`) — the old table does not survive as a second source of
  truth.
- Enqueueing does not execute: `Enqueue(kind, refID, trigger)` writes the
  `queued` row and returns. A single worker goroutine, started at boot
  (`cmd/mymediadb/main.go`), consumes the queue in FIFO order by `enqueued_at`,
  delegates the actual execution to the `scanner` (same
  `coletarArquivos`/`processarLotes`/`computarFacts` logic, only the orchestration
  changes) or to the already registered `job.Handler` (contract `Handler.Executar`
  unchanged), and updates `progress_current`/`message` through a callback during
  execution — not only at the end.
- **Dedup by reference, not by the whole queue**: two entries for the same
  `(kind, ref_id)` with one already `queued`/`running` are rejected
  (`409`, the same error contract as `ErrScanEmAndamento`/`ErrJobEmExecucao`
  today) — it avoids enqueueing the same scan/job twice, but **does not** prevent
  items of different references from coexisting in the queue (they just do not run
  at the same time).
- Scan progress now increments per **processed file**, not per
  batch of 25 — the batch of 25 remains only as the database write
  granularity (`processarLotes`); progress and writing are separate concerns
  now.
- The public API does not change in an observable way: `POST /library/{id}/scan`,
  `GET .../scan`, `GET/POST /jobs*` keep existing with the same response
  contract — underneath, the handlers start calling `execqueue.Enqueue`
  instead of `scanner.Manager.Start`/`job.Manager.Executar` directly.
  `saude_biblioteca` (which today calls `scanner.Manager.Start` internally
  for each source) starts enqueueing each source and waiting through polling of
  `execqueue` itself, just like it already does today against the `scanner.Manager`.

## Consequences

- A single place (`internal/execqueue`) concentrates concurrency, history and
  progress — it eliminates the "goroutine + map + mutex" duplication that existed
  twice.
- Scan history now survives a restart (a new gain — it does not exist today) and is
  queryable through the same surface that already lists job
  executions.
- Globally serialized execution (concurrency 1) is a deliberate
  simplicity choice — it matches the "local-first and light" principle
  (AGENTS.md item 4) and the fact that SQLite already has a single writer; the
  cost is that a long scan (e.g. a large, rate-limited music library)
  delays scheduled jobs behind it in the queue. Accepted for the MVP; evolution to
  N workers with a queue per category (scan does not block backup, for instance)
  is left for when the real volume justifies it.
- `job_execution` ceases to exist as its own table; any consumer
  (API, tests) now reads `queue_item` filtered by `kind='job'`.
- It does not migrate `library_source.state`/`last_scan` (they stay as they are) nor
  change the music/scan data model defined in ADR-0027 — it is
  purely a change in execution orchestration.
