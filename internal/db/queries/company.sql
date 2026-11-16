-- Access to the company table (migration 0010): the production company/studio
-- deduplicated by (source, source_id). Re-composable cache. Comments in ASCII and
-- explicit columns -- see the note in entity.sql.

-- name: UpsertCompany :one
-- Writes (or updates) the production company deduplicated by (source, source_id).
-- A conflict on the UNIQUE constraint reuses the row and updates
-- name/logo_path/payload_json, returning the id; `id` only counts on the first insert.
INSERT INTO company (
    id, source, source_id, name, logo_path, payload_json
) VALUES (
    ?, ?, ?, ?, ?, ?
)
ON CONFLICT (source, source_id) DO UPDATE SET
    name = excluded.name,
    logo_path = excluded.logo_path,
    payload_json = excluded.payload_json
RETURNING id;

-- name: ObterCompanyPorFonte :one
-- Reads the production company by (source, source_id) -- the logical dedupe key.
SELECT id, source, source_id, name, logo_path, payload_json
FROM company
WHERE source = ? AND source_id = ?;
