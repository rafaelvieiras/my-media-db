-- CRUD de media_file + a query de POSSE DERIVADA (ADR-0009).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: CriarMediaFile :one
INSERT INTO media_file (
    id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at;

-- name: ObterMediaFile :one
SELECT id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at
FROM media_file
WHERE id = ?;

-- name: ListarMediaFilesPorEntity :many
SELECT id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at
FROM media_file
WHERE entity_id = ?
ORDER BY created_at, id;

-- name: AtualizarMediaFileState :one
-- Transicao de ciclo de vida: marca present/missing e carimba last_seen.
UPDATE media_file
SET state = ?,
    last_seen = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, entity_id, path, content_hash, size, mtime, edition, part_no,
    match_confidence, mediainfo_json, state, last_seen, created_at, updated_at;

-- name: ApagarMediaFile :exec
DELETE FROM media_file
WHERE id = ?;

-- name: EntityTemPosse :one
-- POSSE DERIVADA (ADR-0009): verdadeiro se, e so se, existe ao menos um
-- media_file 'present' para a entity. Posse NAO e coluna (uma coluna gerada nao
-- enxerga outra tabela) -- e computada aqui.
SELECT EXISTS (
    SELECT 1 FROM media_file
    WHERE entity_id = ? AND state = 'present'
) AS tem_posse;
