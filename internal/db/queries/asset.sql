-- CRUD for asset (poster/fanart/... referenced by an entity).
-- Comments in ASCII and explicit columns -- see the note in entity.sql.

-- name: CriarAsset :one
INSERT INTO asset (
    id, entity_id, kind, origin, path_or_uri, meta_json
) VALUES (
    ?, ?, ?, ?, ?, ?
)
RETURNING id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal;

-- name: ObterAsset :one
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal
FROM asset
WHERE id = ?;

-- name: ListarAssetsPorEntity :many
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal
FROM asset
WHERE entity_id = ?
ORDER BY created_at, id;

-- name: ObterAssetPosterPorEntity :one
-- Local poster of the entity: the first asset kind='poster'. Used
-- by the endpoint that serves the binary. No poster -> sql.ErrNoRows -> 404 in the API.
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal
FROM asset
WHERE entity_id = ? AND kind = 'poster'
ORDER BY created_at, id
LIMIT 1;

-- name: ObterAssetPorEntityKindOrigin :one
-- Asset from ONE specific origin (e.g. kind='poster', origin='external'), for the
-- image cache dedupe: reuse/update the asset instead of duplicating
-- it. Filtering by origin guarantees the external cache NEVER matches an asset
-- origin='user' (the owner's art, untouchable). No row -> sql.ErrNoRows.
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at, local_path, is_principal
FROM asset
WHERE entity_id = ? AND kind = ? AND origin = ?
ORDER BY created_at, id
LIMIT 1;

-- name: AtualizarAssetPath :exec
-- Updates the path/binary of an existing asset (external cache dedupe:
-- downloading the poster again reuses the same row). Does not change entity_id/kind/origin.
UPDATE asset
SET path_or_uri = ?,
    meta_json = ?
WHERE id = ?;

-- name: AtualizarAssetMeta :exec
-- Updates ONLY the meta_json of an asset (flags/unflags the owner's preferred one).
-- Does not touch entity_id/kind/origin/path_or_uri. Used when (un)flagging a
-- preferred one: the merge preserving the other meta_json keys is done in Go.
UPDATE asset
SET meta_json = ?
WHERE id = ?;

-- name: ApagarAsset :exec
DELETE FROM asset
WHERE id = ?;

-- name: AtualizarAssetLocalPath :exec
-- Writes the materialized PHYSICAL path in the library's real folder:
-- a column orthogonal to path_or_uri (internal cache/external URL) -- only
-- filled when the art was also copied into the item folder.
UPDATE asset
SET local_path = ?
WHERE id = ?;

-- name: MarcarAssetPrincipal :exec
-- Sets is_principal=1 on the asset (source of truth in a column).
-- The caller ZEROES the previous principal of the same (entity,kind) in a
-- separate query (DesmarcarAssetsPrincipalPorKind) before calling this one.
UPDATE asset
SET is_principal = 1
WHERE id = ?;

-- name: DesmarcarAssetsPrincipalPorKind :exec
-- Zeroes is_principal on ALL assets of the (entity,kind) -- enforces the
-- invariant "at most one principal per kind" in code:
-- always called BEFORE MarcarAssetPrincipal, in the same write
-- sequence (single-writer pool, ADR-0010 -- no concurrency between the
-- two calls).
UPDATE asset
SET is_principal = 0
WHERE entity_id = ? AND kind = ?;
