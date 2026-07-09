-- CRUD de asset (poster/fanart/... referenciados por uma entity).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: CriarAsset :one
INSERT INTO asset (
    id, entity_id, kind, origin, path_or_uri, meta_json
) VALUES (
    ?, ?, ?, ?, ?, ?
)
RETURNING id, entity_id, kind, origin, path_or_uri, meta_json, created_at;

-- name: ObterAsset :one
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at
FROM asset
WHERE id = ?;

-- name: ListarAssetsPorEntity :many
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at
FROM asset
WHERE entity_id = ?
ORDER BY created_at, id;

-- name: ObterAssetPosterPorEntity :one
-- Poster local da entity (briefing 0010): o primeiro asset kind='poster'. Usado
-- pelo endpoint que serve o binario. Sem poster -> sql.ErrNoRows -> 404 na API.
SELECT id, entity_id, kind, origin, path_or_uri, meta_json, created_at
FROM asset
WHERE entity_id = ? AND kind = 'poster'
ORDER BY created_at, id
LIMIT 1;

-- name: ApagarAsset :exec
DELETE FROM asset
WHERE id = ?;
