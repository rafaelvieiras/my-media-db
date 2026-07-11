-- CRUD de library_source (raizes de varredura).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: CriarLibrarySource :one
-- state e last_scan ficam de fora do INSERT: uma fonte nova nasce 'online'
-- (DEFAULT) e sem last_scan; o scan preenche depois via AtualizarLibrarySource.
INSERT INTO library_source (
    id, path, type
) VALUES (
    ?, ?, ?
)
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type;

-- name: ObterLibrarySource :one
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type
FROM library_source
WHERE id = ?;

-- name: ObterLibrarySourcePorPath :one
-- Usada pelo handler POST para detectar path duplicado (ja normalizado) antes
-- do INSERT e responder 409 com mensagem clara em vez de estourar o UNIQUE.
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type
FROM library_source
WHERE path = ?;

-- name: ListarLibrarySources :many
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type
FROM library_source
ORDER BY created_at, id;

-- name: AtualizarLibrarySource :one
-- Atualiza o estado da varredura: carimbo do ultimo scan e online/offline.
UPDATE library_source
SET type = ?,
    last_scan = ?,
    state = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type;

-- name: ApagarLibrarySource :exec
DELETE FROM library_source
WHERE id = ?;
