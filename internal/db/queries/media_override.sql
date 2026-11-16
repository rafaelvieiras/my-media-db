-- Access to the OVERRIDE layer (media_override): the owner's sparse patch, 1:1 with
-- the entity (PK = entity_id). The override editor writes here through an UPSERT.
-- Comments in ASCII and explicit columns -- see the note in entity.sql.

-- name: UpsertMediaOverride :one
-- Writes (or replaces) the entity override. A conflict on the PK (entity_id)
-- updates override_json and restamps updated_at -- 1:1, never a second row.
INSERT INTO media_override (
    entity_id, override_json
) VALUES (
    ?, ?
)
ON CONFLICT (entity_id) DO UPDATE SET
    override_json = excluded.override_json,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING entity_id, override_json, updated_at;

-- name: ObterMediaOverride :one
-- Reads the entity override (sql.ErrNoRows when there is no active override).
SELECT entity_id, override_json, updated_at
FROM media_override
WHERE entity_id = ?;

-- name: ApagarMediaOverride :exec
-- Removes the entity override (falls back to the value from the editorial/file base).
DELETE FROM media_override
WHERE entity_id = ?;
