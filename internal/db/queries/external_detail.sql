-- Access to the CACHE of the rich external detail (external_detail): one row
-- per (source, source_id). The first lookup does an UPSERT; the next ones READ from
-- here, with the app deciding whether to serve from the cache or fetch again
-- (TTL + trigger by languages). Comments in ASCII and explicit columns -- see the
-- note in entity.sql.

-- name: UpsertExternalDetail :exec
-- Writes (or updates) the cache of ONE external item. A conflict on the PK (source,
-- source_id) updates the payload/langs/type and restamps fetched_at, without
-- duplicating the row. It is best-effort in the app: a failure here does not block
INSERT INTO external_detail (
    source, source_id, type, payload_json, langs_json
) VALUES (
    ?, ?, ?, ?, ?
)
ON CONFLICT (source, source_id) DO UPDATE SET
    type = excluded.type,
    payload_json = excluded.payload_json,
    langs_json = excluded.langs_json,
    fetched_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: ObterExternalDetail :one
-- Reads the cache of ONE external item by the logical key (source, source_id).
-- fetched_at and langs_json feed the refresh decision (TTL + language set) in the app.
SELECT source, source_id, type, payload_json, langs_json, fetched_at
FROM external_detail
WHERE source = ? AND source_id = ?;
