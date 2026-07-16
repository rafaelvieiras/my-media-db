-- CRUD de library_source (raizes de varredura).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: CriarLibrarySource :one
-- state e last_scan ficam de fora do INSERT: uma fonte nova nasce 'online'
-- (DEFAULT) e sem last_scan; o scan preenche depois via AtualizarLibrarySource.
-- content_type entra explicito (0034): 'movie' | 'series' -- o scanner ramifica
-- por este valor; o servico normaliza vazio para 'movie' (compat). watch_enabled
-- fica de fora do INSERT tambem: nasce 0 (DEFAULT, briefing 0064) -- o dono liga
-- depois via PATCH.
INSERT INTO library_source (
    id, path, type, content_type
) VALUES (
    ?, ?, ?, ?
)
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled;

-- name: ObterLibrarySource :one
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled
FROM library_source
WHERE id = ?;

-- name: ObterLibrarySourcePorPath :one
-- Usada pelo handler POST para detectar path duplicado (ja normalizado) antes
-- do INSERT e responder 409 com mensagem clara em vez de estourar o UNIQUE.
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled
FROM library_source
WHERE path = ?;

-- name: ListarLibrarySources :many
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled
FROM library_source
ORDER BY created_at, id;

-- name: ListarLibrarySourcesComWatchHabilitado :many
-- Usada no BOOT do servidor (briefing 0064) para registrar o watcher fsnotify
-- de cada fonte com watch_enabled=1 -- evita listar/filtrar em Go sobre TODAS
-- as fontes so para achar as poucas com watch ligado.
SELECT id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled
FROM library_source
WHERE watch_enabled = 1
ORDER BY created_at, id;

-- name: AtualizarLibrarySource :one
-- Atualiza o estado da varredura: carimbo do ultimo scan e online/offline.
UPDATE library_source
SET type = ?,
    last_scan = ?,
    state = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled;

-- name: AtualizarLibrarySourceWatch :one
-- PATCH /api/v1/library/{id} (briefing 0064): liga/desliga watch_enabled. O
-- handler chama a rotina de registro/desregistro do watcher fsnotify LOGO
-- APOS este UPDATE (aplicado a quente, sem restart -- ver internal/watch).
UPDATE library_source
SET watch_enabled = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, path, type, last_scan, state, created_at, updated_at, content_type, watch_enabled;

-- name: ApagarLibrarySource :exec
DELETE FROM library_source
WHERE id = ?;
