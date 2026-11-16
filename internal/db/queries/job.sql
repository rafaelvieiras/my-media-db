-- Access to job_definition: foundation of the job system
-- (dynamic registry + embedded scheduler). Comments in ASCII and explicit
-- columns -- see the note in entity.sql (sqlc v1.31 offset bug with
-- accents/'*' in a query .sql file).
--
-- job_execution (execution history) no longer lives here since
-- ADR-0028 (LAYER 3): internal/job/manager.go now enqueues through
-- internal/execqueue, whose queue_item (kind='job') is the single source of
-- history -- see internal/db/queries/queue_item.sql. The job_execution table
-- itself was dropped by migration 0025.

-- name: ListarJobDefinitions :many
-- Lists all known jobs (there is always at least the backup_vacuum seed),
-- stable order by created_at.
SELECT id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at
FROM job_definition
ORDER BY created_at, id;

-- name: ObterJobDefinition :one
SELECT id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at
FROM job_definition
WHERE id = ?;

-- name: ListarJobDefinitionsAgendaveis :many
-- Scheduler query (Manager.RunScheduler): enabled jobs, with a
-- schedule configured, whose next run time has already passed. Go filters out the ones
-- already "running" in memory before firing (that information is not in the
-- database).
SELECT id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at
FROM job_definition
WHERE enabled = 1
  AND schedule_cron IS NOT NULL
  AND next_run_at IS NOT NULL
  AND next_run_at <= ?;

-- name: AtualizarJobConfig :one
-- PATCH .../jobs/{id}: writes enabled/schedule_cron/next_run_at already MERGED
-- by Go (the patch is partial in the API, but this query always writes the 3
-- columns -- the caller computes the final value before calling, same pattern as
-- AtualizarLibrarySource).
UPDATE job_definition
SET enabled = ?,
    schedule_cron = ?,
    next_run_at = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at;

-- name: FinalizarJobDefinicao :one
-- At the end of an execution (Manager.Executar): mirrors the result into
-- job_definition (read redundancy, avoids a JOIN on the listing) and recomputes
-- next_run_at (nil when disabled or without schedule_cron -- computed in
-- Go via cron.ParseStandard(...).Next(now)).
UPDATE job_definition
SET last_run_at = ?,
    last_status = ?,
    last_message = ?,
    next_run_at = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at;
