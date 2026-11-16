-- Access to app_settings: application configs stored as key->JSON pairs.
-- The first key is "languages" = {primary, fallback, allowed[]}. Comments in
-- ASCII and explicit columns -- see the note in entity.sql (the sqlc v1.31
-- offset bug with accents/'*' in a query .sql file).

-- name: ObterAppSetting :one
-- Reads a config by key (sql.ErrNoRows when the key does not exist -- the helper
-- falls back to the safe default).
SELECT key, value_json, updated_at
FROM app_settings
WHERE key = ?;

-- name: UpsertAppSetting :one
-- Writes (or replaces) a config. A conflict on the PK (key) updates value_json and
-- restamps updated_at -- it never creates a second row for the same key.
INSERT INTO app_settings (
    key, value_json
) VALUES (
    ?, ?
)
ON CONFLICT (key) DO UPDATE SET
    value_json = excluded.value_json,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING key, value_json, updated_at;
