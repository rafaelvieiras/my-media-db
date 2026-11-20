# 0040 — The queue is observable: per-item progress, a trail, a queue view and a duration

Status: accepted (19/nov/2026)

## Context

Since ADR-0028 a single `queue_item` table is the one source of truth for every scan and every
job, and a single sequential worker consumes it. The table could say four things about a run:
its status, two counters, one message and three timestamps. That was enough to know *that*
something was running, and nothing about *what*.

The gap was measured, not imagined. A manual `saude_biblioteca` run was still `running` after
**75 minutes** and the only evidence available was one `message` column: no step, no current
item, no count of what was left, no record of what had already failed. Answering "is it stuck?"
required reading the production database by hand.

Three concrete defects behind that:

1. **A job could not report anything at all.** `job.Handler.Executar(ctx, cfg)` has no progress
   surface, and `job.Manager.executarViaRunner` discarded the queue's callback
   (`_ execqueue.ProgressoFunc`). Every job ran with `0/0` forever. Only the scanner reported,
   and only as two integers plus a free-form message.
2. **Only the last message survived.** Each report overwrote the previous one, so the moment a
   second item was processed, the outcome of the first was gone. "Which items failed" was
   unanswerable by construction.
3. **No duration and no queue.** The timestamps existed, but nothing derived a duration from
   them, and the API only spoke per reference (`GET /jobs/{id}/executions`,
   `GET /library/{id}/scan`). A waiting item was invisible: the owner could not tell that a
   manual run was third in line behind two scans, nor how long it had been waiting.

## Decision

The queue owns observability, in its own vocabulary. No per-job tables, no per-type columns
(principle 3): two additions to what already exists.

**`queue_item` gains the snapshot of the current run**: `step_index`, `step_total`, `step_label`,
`current_item`, `progress_ok`, `progress_failed`. It is a *snapshot*, not history — it always
describes the present and is cheap to overwrite.

**`queue_item_event` is the trail**: one append-only row per item the runner finished
(`ok` / `error` / `skip`, a free-text label and a short detail), capped at 200 rows per item and
pruned at write time. It answers "which ones succeeded and which ones failed" after the fact;
the cap keeps a 5000-file scan from turning the trail into a second copy of the library.

**A Runner reports through `execqueue.Registro`** (`Progresso` for the snapshot, `Evento` for the
trail) instead of a bare callback. The registrar merges field by field — a report that only names
the current item does not wipe the counters — and keeps `progress_ok`/`progress_failed`
consistent with the trail itself, so a Runner cannot forget.

**A job handler narrates through an optional capability.** `job.HandlerComProgresso` is
discovered by type assertion, exactly like the provider capabilities: a Handler that does not
implement it keeps working unchanged (`0/0`, no trail, no error). `job.Progresso` is the handle
(`Etapa`, `Contagem`, `Item`, `Ok`, `Falhou`, `Ignorado`), and a step transition resets the
counters — "step 3 of 8" never inherits "40 of 120" from the step before it. The same registrar
is handed down to a sub-runner invoked inline, so the file-level progress of the scan the health
job triggers belongs to the job item the owner is watching.

**Durations are derived, never stored**: `wait_ms` (`enqueued_at → started_at`), `elapsed_ms`
(`started_at → now`, running only) and `duration_ms` (`started_at → finished_at`, finished only).
One definition, in `internal/execqueue`, used by the queue endpoints *and* by
`GET /jobs`, `GET /jobs/{id}/executions` and `GET /library/{id}/scan` — the same number on every
screen. A stored duration could only ever disagree with the timestamps that produced it.

**The queue becomes a resource**: `GET /api/v1/queue` returns the running item, the FIFO with
positions and wait times, and the most recently finished ones, each with a human `ref_label`
(job name, source path). `GET /api/v1/queue/{id}` returns one item with its trail.

## Consequences

- **Everything old still works.** `queue_item`'s previous columns keep their meaning; a Runner or
  a Handler that reports nothing produces exactly the API payload it produced before (the new
  fields are `null`), so no client breaks.
- **One more write per report.** The snapshot is one `UPDATE`, and identical consecutive payloads
  are filtered in memory. The trail adds one `INSERT` plus the pruning `DELETE` per finished item
  — bounded by the cap, not by the size of the library.
- **`message` becomes final-only.** Live narration moved to `step_label`/`current_item`;
  `message` is written by `FinalizarQueueItem` alone. The scan stages that used to write a
  free-form message now write `Etapa` instead.
- **The trail is not a log.** It is a bounded, structured answer to "what did this run do", not a
  replacement for the process log: the 201st item of a run silently pushes out the first one.
- **The UI is a separate delivery.** The endpoints exist first (this ADR); the screen that shows
  them is the next change in the same issue.
- **What is deliberately out of scope**: multi-worker execution (ADR-0028 keeps concurrency at
  1), retention policy of `queue_item` itself, and historical progress charts.

Agenda: Q1 (queue observability) — see [docs/00](../00-decision-agenda.md).
