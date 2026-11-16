-- Access to integration_config: generic registry of integration config
-- (provider credentials/parameters), with precedence DATABASE >
-- env var computed by the Resolver (internal/integration). Comments in
-- ASCII and explicit columns -- same sqlc v1.31 note about corrupting an accent
-- in a query .sql file (see entity.sql).

-- name: ObterIntegrationConfig :one
-- Reads the config of an integration by slug (sql.ErrNoRows when the slug was
-- never written -- the Resolver falls back to the env var in that case).
SELECT slug, values_json, updated_at
FROM integration_config
WHERE slug = ?;

-- name: UpsertIntegrationConfig :one
-- Writes (or replaces) the whole values_json of a slug. A conflict on the PK
-- (slug) updates values_json and restamps updated_at -- it never creates a
-- second row for the same slug. The partial PUT (merge field by field, null
-- removes the key) is the HTTP handler's responsibility: it reads the current
-- value, merges it in memory and calls this upsert with the object already
-- complete.
INSERT INTO integration_config (
    slug, values_json
) VALUES (
    ?, ?
)
ON CONFLICT (slug) DO UPDATE SET
    values_json = excluded.values_json,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING slug, values_json, updated_at;
