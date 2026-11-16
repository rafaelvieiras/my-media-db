-- Access to queue_item (ADR-0028): unified execution queue
-- for scan and job. Comments in ASCII and explicit columns -- see the note in
-- entity.sql (sqlc v1.31 offset bug with accents/'*' in a query .sql file).

-- name: CriarQueueItem :one
-- Enqueues a new item with status 'queued' (Manager.Enqueue, AFTER the
-- dedupe guard has already confirmed there is no active item for the same reference).
INSERT INTO queue_item (
    id, kind, ref_id, trigger
) VALUES (
    ?, ?, ?, ?
)
RETURNING id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at;

-- name: ObterQueueItemAtivo :one
-- Dedupe guard (Manager.Enqueue): is there any queued/running item for
-- this SAME reference (kind, ref_id)? sql.ErrNoRows = there is none, it may
-- be enqueued. Newest first (ORDER BY enqueued_at DESC) does not really
-- matter here (there can be only ONE active item per reference, since the guard
-- itself prevents a second one), but it keeps the query deterministic.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE kind = ? AND ref_id = ? AND status IN ('queued', 'running')
ORDER BY enqueued_at DESC
LIMIT 1;

-- name: ObterQueueItemPorId :one
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE id = ?;

-- name: ObterQueueItemAtivoPorKindRef :one
-- Used by Manager.Status(kind, refID): the most recent ACTIVE item
-- (queued/running) of the reference, if any -- Status falls back to
-- ObterUltimoQueueItemPorKindRef when this query returns sql.ErrNoRows (with no
-- active item, it shows the last finished result).
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
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
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE kind = ? AND ref_id = ?
ORDER BY enqueued_at DESC
LIMIT 1;

-- name: ProximoQueueItemPendente :one
-- The worker uses this to find the oldest 'queued' item (FIFO by
-- enqueued_at; id as a stable tiebreaker when enqueued_at collides).
-- sql.ErrNoRows = empty queue, the worker sleeps for a short interval.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
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
    message, enqueued_at, started_at, finished_at;

-- name: AtualizarProgressoQueueItem :exec
-- Writes progress_current/progress_total/message on every report from the
-- progress callback during Runner.Executar (ADR-0028: "updates progress_current/
-- message via callback during the execution -- not only at the end"). No throttle:
-- it writes on every call, a decision documented in
-- internal/execqueue/manager.go (simple and correct; optimize only if it becomes
-- a real I/O problem).
UPDATE queue_item
SET progress_current = ?,
    progress_total = ?,
    message = ?
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
    message, enqueued_at, started_at, finished_at;

-- name: ListarQueueItemsPorKindRef :many
-- Paginated history of ONE reference (kind, ref_id), newest first
-- by enqueued_at -- replaces the old ListarJobExecucoes (job.sql) in a
-- future layer; the columns already cover the shape that GET /jobs/{id}/executions
-- and GET /library/{id}/scan need.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
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
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE status = 'running';
