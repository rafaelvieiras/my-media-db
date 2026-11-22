-- Access to the cache of the EXTERNAL layer (media_external): one raw payload row per
-- (entity_id, source). Re-enrichment does an UPSERT on that key (ADR-0011).
-- Comments in ASCII and explicit columns -- see the note in entity.sql.

-- name: UpsertMediaExternal :one
-- Writes (or updates) the cache of ONE source for the entity. A conflict on the UNIQUE
-- (entity_id, source) updates source_id/payload_json and restamps fetched_at, without
-- duplicating the row. `id` is only used on the first insert (it stays stable on update).
INSERT INTO media_external (
    id, entity_id, source, source_id, payload_json
) VALUES (
    ?, ?, ?, ?, ?
)
ON CONFLICT (entity_id, source) DO UPDATE SET
    source_id = excluded.source_id,
    payload_json = excluded.payload_json,
    fetched_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING id, entity_id, source, source_id, payload_json, fetched_at;

-- name: ObterMediaExternalPorFonte :one
-- Reads the cache of ONE specific source of the entity (the logical cache key).
SELECT id, entity_id, source, source_id, payload_json, fetched_at
FROM media_external
WHERE entity_id = ? AND source = ?;

-- name: ListarMediaExternalPorEntity :many
-- All external layers cached for an entity (one per fetched source).
SELECT id, entity_id, source, source_id, payload_json, fetched_at
FROM media_external
WHERE entity_id = ?
ORDER BY source, id;

-- name: ApagarMediaExternal :exec
-- Removes a cache row by its id (e.g. invalidate a source).
DELETE FROM media_external
WHERE id = ?;

-- name: ApagarMediaExternalDaEntity :execrows
-- Drops every cached external payload of the entity (base detail, artwork inventory, cross
-- ids of the previous identity). They are CACHES re-fetched right after a forced enrichment;
-- keeping them would leave the artwork inventory of the wrong show available in the picker.
DELETE FROM media_external
WHERE entity_id = sqlc.arg('entity_id');
