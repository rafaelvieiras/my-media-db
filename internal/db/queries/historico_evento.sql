-- CRUD for historico_evento (ADR-0022): consumption events,
-- generic (watch/listen/play/etc.), append-only, children of entity.
-- Comments in ASCII -- see the note in entity.sql (sqlc v1.31 corrupts
-- query SQL with an accent).

-- name: CriarHistoricoEvento :one
INSERT INTO historico_evento (
    id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json
) VALUES (
    ?, ?, ?, ?, ?, ?,
    ?, ?, ?
)
RETURNING id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at;

-- name: ListarHistoricoEventoPorEntity :many
-- Lists the events of an entity in reverse chronological order. kind is an
-- optional filter via sqlc.narg -- NULL = no filter (all kinds).
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at
FROM historico_evento
WHERE entity_id = sqlc.arg('entity_id')
  AND (sqlc.narg('kind') IS NULL OR kind = sqlc.narg('kind'))
ORDER BY occurred_at DESC, id DESC;

-- name: ListarUltimoHistoricoEventoPorKind :many
-- Base of the "current state" projection (GET .../historico/estado): all the
-- events of the entity, same reverse chronological order as the listing above --
-- the grouping by kind (keeping only the first of each kind, since the
-- ordering is occurred_at DESC) is left to the Go layer, as
-- ADR-0022 documents it (low volume per entity, advanced SQL is not
-- worth it).
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at
FROM historico_evento
WHERE entity_id = ?
ORDER BY occurred_at DESC, id DESC;

-- name: BuscarHistoricoEventoPorID :one
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at
FROM historico_evento
WHERE id = ?;

-- name: ApagarHistoricoEvento :exec
DELETE FROM historico_evento
WHERE id = ?;

-- name: CriarHistoricoEventoOrfao :one
-- Writes an event with no matched entity: entity_id stays NULL
-- implicitly (it is not a parameter). Stores the raw IDs/title/year from the
-- payload to allow locating the event as a backfill candidate
-- when an entity gains that external ID in the future.
INSERT INTO historico_evento (
    id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, raw_tmdb_id, raw_imdb_id,
    raw_tvdb_id, raw_title, raw_year
) VALUES (
    ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?
)
RETURNING id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, raw_tmdb_id, raw_imdb_id,
    raw_tvdb_id, raw_title, raw_year, created_at;

-- name: ListarHistoricoEventoOrfaos :many
-- Paginated list of the events still without an entity (entity_id IS NULL), same
-- pagination pattern as other lists in the project (limit/offset).
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, raw_tmdb_id, raw_imdb_id,
    raw_tvdb_id, raw_title, raw_year, created_at
FROM historico_evento
WHERE entity_id IS NULL
ORDER BY occurred_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: ContarHistoricoEventoOrfaos :one
-- Total orphan events, for the "total" field of the pagination above.
SELECT COUNT(*) FROM historico_evento WHERE entity_id IS NULL;

-- name: VincularHistoricoEventoOrfaoPorTmdb :execrows
-- Retroactive backfill: links every orphan event whose
-- raw_tmdb_id matches the ID the entity has just gained. :execrows so the
-- caller knows how many rows were linked (optional log).
UPDATE historico_evento
SET entity_id = ?
WHERE entity_id IS NULL AND raw_tmdb_id = ?;

-- name: VincularHistoricoEventoOrfaoPorImdb :execrows
UPDATE historico_evento
SET entity_id = ?
WHERE entity_id IS NULL AND raw_imdb_id = ?;

-- name: VincularHistoricoEventoOrfaoPorTvdb :execrows
UPDATE historico_evento
SET entity_id = ?
WHERE entity_id IS NULL AND raw_tvdb_id = ?;
