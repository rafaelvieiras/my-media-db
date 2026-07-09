-- CRUD minimo da entity base.
--
-- IMPORTANTE (sqlc + ASCII): os comentarios deste arquivo .sql sao ASCII (sem
-- acentos) DE PROPOSITO. O sqlc v1.31 fatia o texto de cada query por offset de
-- BYTE, mas as posicoes dos tokens vem em offset de RUNE; qualquer caractere
-- multi-byte (acento pt-BR) num arquivo de query desloca o corte e CORROMPE o
-- SQL gerado (some o '?' final, troca 'AS x' por 'A', etc.). As migrations NAO
-- sofrem disso (sao parseadas por estrutura, nao guardadas como texto), entao la
-- os comentarios seguem em pt-BR acentuado. Aqui, ASCII. Regra geral: colunas
-- sempre EXPLICITAS, nunca '*'/'RETURNING *' (a expansao de '*' tem o mesmo bug
-- de offset). As colunas geradas (external_id_tmdb/imdb) derivam de
-- external_ids_json e NAO entram no INSERT/UPDATE, mas aparecem no
-- RETURNING/SELECT para espelhar o modelo.

-- name: CriarEntity :one
-- status NAO entra no INSERT: usa o DEFAULT 'cataloged' (item auto-criado nasce
-- catalogado; a curadoria muda depois via AtualizarEntity). ADR-0009.
INSERT INTO entity (
    id, type, title, original_title, description, release_date,
    tags_json, external_ids_json, extension_data_json
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb;

-- name: ObterEntity :one
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb
FROM entity
WHERE id = ?;

-- name: ObterEntityPorTmdb :one
-- Lookup por id externo tmdb via coluna gerada + indice (doc 04, busca sem GIN).
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb
FROM entity
WHERE external_id_tmdb = ?;

-- name: ObterEntityPorImdb :one
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb
FROM entity
WHERE external_id_imdb = ?;

-- name: ObterEntityPorTituloAno :one
-- Dedup do scanner sem id externo (briefing 0009): reusa a MESMA entity para o
-- mesmo (type, title, release_date). Usa `IS` (nao `=`) para casar release_date
-- NULO quando o nome nao traz ano. Mantem o re-scan idempotente para obras sem
-- id embutido no nome (nao duplica a entity).
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb
FROM entity
WHERE type = ? AND title = ? AND release_date IS ?
LIMIT 1;

-- name: ListarEntities :many
SELECT id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb
FROM entity
ORDER BY created_at DESC, id
LIMIT ? OFFSET ?;

-- name: AtualizarEntity :one
UPDATE entity
SET type = ?,
    title = ?,
    original_title = ?,
    description = ?,
    release_date = ?,
    status = ?,
    tags_json = ?,
    external_ids_json = ?,
    extension_data_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, type, title, original_title, description, release_date,
    status, tags_json, external_ids_json, extension_data_json,
    created_at, updated_at, external_id_tmdb, external_id_imdb;

-- name: ApagarEntity :exec
-- Purga explicita: remove a entity e, por ON DELETE CASCADE, seus media_file,
-- asset e entity_relation.
DELETE FROM entity
WHERE id = ?;
