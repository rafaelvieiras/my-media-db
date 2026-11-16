-- Aggregate reads for GET /api/v1/health/system: database
-- size via PRAGMA (outside this file, it runs straight from Go -- see
-- internal/telemetry/telemetry.go) + usage counts + on-disk sum per
-- source. No new table. Comments in ASCII (no accent) -- the same
-- care as in entity.sql/job.sql against the sqlc v1.31 corruption bug with an
-- accented query .sql.

-- name: ContarEntitiesPorTipo :many
SELECT type, COUNT(*) AS total
FROM entity
GROUP BY type;

-- name: ContarMediaFilesPorState :many
SELECT state, COUNT(*) AS total
FROM media_file
GROUP BY state;

-- name: ContarLibrarySources :one
SELECT COUNT(*) AS total FROM library_source;

-- name: ContarJobExecutions :one
-- Since ADR-0028 (LAYER 3): job_execution was dropped
-- (migration 0025); the job execution history lives in queue_item
-- (kind='job') -- see internal/db/queries/queue_item.sql.
SELECT COUNT(*) AS total FROM queue_item WHERE kind = 'job';

-- name: SomarTamanhoMediaFilePorFonte :one
SELECT CAST(COALESCE(SUM(size), 0) AS INTEGER) AS total_bytes
FROM media_file
WHERE library_source_id = ? AND state = 'present';
