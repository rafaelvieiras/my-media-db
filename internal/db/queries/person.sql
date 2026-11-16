-- Access to the person table (migration 0010): the person deduplicated by
-- (source, source_id). Re-composable cache of the external payload -- the owner's data
-- never lives here. Comments in ASCII and explicit columns -- see the note in
-- entity.sql (sqlc v1.31 corrupts query SQL with an accent).

-- name: UpsertPerson :one
-- Writes (or updates) the person deduplicated by (source, source_id). A conflict on the
-- UNIQUE constraint reuses the row and updates name/profile_path/payload_json, returning
-- the existing id -- so two credits of the same person point at ONE row. `id` is only
-- used on the first insert (it stays stable on update).
INSERT INTO person (
    id, source, source_id, name, profile_path, payload_json
) VALUES (
    ?, ?, ?, ?, ?, ?
)
ON CONFLICT (source, source_id) DO UPDATE SET
    name = excluded.name,
    profile_path = excluded.profile_path,
    payload_json = excluded.payload_json
RETURNING id;

-- name: ObterPersonPorFonte :one
-- Reads the person by (source, source_id) -- the logical dedupe key.
SELECT id, source, source_id, name, profile_path, payload_json
FROM person
WHERE source = ? AND source_id = ?;

-- name: ObterPersonPorID :one
-- Reads the person by local id (person.id) -- for the person page
-- (GET /person/{id}).
SELECT id, source, source_id, name, profile_path, payload_json
FROM person
WHERE id = ?;
