-- Access to queue_item (ADR-0028): unified execution queue
-- for scan and job. Comments in ASCII and explicit columns -- see the note in
-- entity.sql (sqlc v1.31 offset bug with accents/'*' in a query .sql file).
--
-- COLUMN ORDER: the new observable-progress columns added by ADR-0040
-- (step_index, step_total, step_label, current_item, progress_ok,
-- progress_failed) come LAST in each SELECT, in the exact order the ALTER
-- TABLE gave them in the table -- with the set AND order matching the table
-- sqlc reuses sqlcgen.QueueItem for every query instead of generating a
-- per-query Row type, which keeps the Go call sites unchanged.

-- name: CriarQueueItem :one
-- Enqueues a new item with status 'queued' (Manager.Enqueue, AFTER the
-- dedupe guard has already confirmed there is no active item for the same reference).
INSERT INTO queue_item (
    id, kind, ref_id, trigger
) VALUES (
    ?, ?, ?, ?
)
RETURNING id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed;

-- name: ObterQueueItemAtivo :one
-- Dedupe guard (Manager.Enqueue): is there any queued/running item for
-- this SAME reference (kind, ref_id)? sql.ErrNoRows = there is none, it may
-- be enqueued. Newest first (ORDER BY enqueued_at DESC) does not really
-- matter here (there can be only ONE active item per reference, since the guard
-- itself prevents a second one), but it keeps the query deterministic.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE kind = ? AND ref_id = ? AND status IN ('queued', 'running')
ORDER BY enqueued_at DESC
LIMIT 1;

-- name: ObterQueueItemPorId :one
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE id = ?;

-- name: ObterQueueItemAtivoPorKindRef :one
-- Used by Manager.Status(kind, refID): the most recent ACTIVE item
-- (queued/running) of the reference, if any -- Status falls back to
-- ObterUltimoQueueItemPorKindRef when this query returns sql.ErrNoRows (with no
-- active item, it shows the last finished result).
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE kind = ? AND ref_id = ? AND status IN ('queued', 'running')
ORDER BY enqueued_at DESC
LIMIT 1;

-- name: ObterUltimoQueueItemPorKindRef :one
-- Complement of ObterQueueItemAtivoPorKindRef: the most recent item (ANY
-- status) of a reference -- used by Manager.Status when there is no active
-- item, to return the result of the LAST execution (sql.ErrNoRows = there never
-- was any item for this reference).
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE kind = ? AND ref_id = ?
ORDER BY enqueued_at DESC
LIMIT 1;

-- name: ProximoQueueItemPendente :one
-- The worker uses this to find the oldest 'queued' item (FIFO by
-- enqueued_at; id as a stable tiebreaker when enqueued_at collides).
-- sql.ErrNoRows = empty queue, the worker sleeps for a short interval.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE status = 'queued'
ORDER BY enqueued_at, id
LIMIT 1;

-- name: IniciarQueueItem :one
-- queued -> running transition, at the START of processing by the worker.
UPDATE queue_item
SET status = 'running',
    started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed;

-- name: AtualizarProgressoQueueItem :exec
-- Writes the WHOLE progress snapshot (counters, step and current item) on every
-- report from the progress callback during Runner.Executar (ADR-0028: "updates
-- progress_current/message via callback during the execution -- not only at the
-- end"; extended by ADR-0040 with step/current_item/ok/failed). One row per
-- report, no history: the trail of finished items lives in queue_item_event.
-- No throttle (same decision documented in internal/execqueue/manager.go);
-- repeated identical reports are filtered in Go before reaching here.
-- `message` is DELIBERATELY not touched: since ADR-0040 it holds only the final
-- result of the run (set by FinalizarQueueItem); the live narration is
-- step_label/current_item.
UPDATE queue_item
SET progress_current = ?,
    progress_total = ?,
    step_index = ?,
    step_total = ?,
    step_label = ?,
    current_item = ?,
    progress_ok = ?,
    progress_failed = ?
WHERE id = ?;

-- name: FinalizarQueueItem :one
-- running -> success|error transition, at the END of processing (or when there
-- is no Runner registered for the kind).
UPDATE queue_item
SET status = ?,
    finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    message = ?
WHERE id = ?
RETURNING id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed;

-- name: ListarQueueItemsPorKindRef :many
-- Paginated history of ONE reference (kind, ref_id), newest first
-- by enqueued_at -- replaces the old ListarJobExecucoes (job.sql) in a
-- future layer; the columns already cover the shape that GET /jobs/{id}/executions
-- and GET /library/{id}/scan need.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE kind = ? AND ref_id = ?
ORDER BY enqueued_at DESC
LIMIT ?;

-- name: ListarQueueItemsExcedentesPorRef :many
-- History retention (same pattern as ListarJobExecucaoIdsExcedentes in
-- job.sql): ids of the oldest surplus beyond the N most recent OF THAT
-- (kind, ref_id) -- pruning applied in Go, one DELETE per id.
SELECT id FROM queue_item
WHERE kind = ? AND ref_id = ?
ORDER BY enqueued_at DESC
LIMIT -1 OFFSET ?;

-- name: ApagarQueueItem :exec
DELETE FROM queue_item WHERE id = ?;

-- name: ListarQueueItemsRunning :many
-- Used at worker BOOT (Manager.Run, before the loop starts): any item
-- 'running' found here is an orphan from a previous execution that did not
-- finish (the process died in the middle) -- the Manager finishes each one as
-- 'error' before starting to process the queue normally.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE status = 'running';

-- name: ListarQueueItemsAtivos :many
-- The QUEUE VIEW (ADR-0040, issue #62): every item still waiting or running,
-- FIFO by enqueued_at (same order the worker consumes), with `started_at` NULL
-- marking the ones that never ran. The API exposes this as "running" (at most
-- one, concurrency 1) plus "queued" (with their position).
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE status IN ('queued', 'running')
ORDER BY enqueued_at, id;

-- name: ListarQueueItemsRecentes :many
-- The recent past of the QUEUE VIEW (ADR-0040): the most recently enqueued
-- FINISHED items across every reference, newest first. Finished items are the
-- ones with a duration to show, which is the second half of what the owner
-- asked for.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at,
    step_index, step_total, step_label, current_item, progress_ok, progress_failed
FROM queue_item
WHERE status IN ('success', 'error')
ORDER BY enqueued_at DESC
LIMIT ?;

-- name: CriarQueueItemEvento :exec
-- Appends one row to the per-item trail (ADR-0040): "this item finished with
-- this outcome". seq is assigned by the database (MAX+1 per item) so the reader
-- can order the trail without depending on `at` (two events can share a
-- millisecond). The caller caps the trail right after inserting
-- (ApagarQueueItemEventosExcedentes).
INSERT INTO queue_item_event (id, queue_item_id, seq, status, label, detail)
VALUES (?, ?, (SELECT COALESCE(MAX(e.seq), 0) + 1 FROM queue_item_event e WHERE e.queue_item_id = ?), ?, ?, ?);

-- name: ListarQueueItemEventos :many
-- Reads the trail of ONE item, oldest first, limited (the API exposes the most
-- recent slice of the trail; the trail itself is already capped at write time).
SELECT id, queue_item_id, seq, at, status, label, detail
FROM queue_item_event
WHERE queue_item_id = ?
ORDER BY seq
LIMIT ?;

-- name: ApagarQueueItemEventosExcedentes :exec
-- Retention of the trail (ADR-0040): keeps only the N most recent events of an
-- item. Called right after each insert, so the table never grows with the
-- number of files a scan touched.
DELETE FROM queue_item_event AS qie
WHERE qie.queue_item_id = ?
  AND qie.seq <= (SELECT COALESCE(MAX(e.seq), 0) FROM queue_item_event e WHERE e.queue_item_id = ?) - ?;
