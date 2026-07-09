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

-- name: ListarEntityCards :many
-- Lista de CARDS para a UI (briefing 0010), ordenada por titulo ASCENDENTE. Uma
-- unica query com agregacao por subselect correlacionado -- evita o N+1 (nada de
-- uma query de posse/contagem por item). Colunas derivadas:
--   owned            POSSE DERIVADA (ADR-0009): EXISTS media_file present.
--   file_count       total de media_file da entity (present + missing).
--   match_confidence maior confianca de match entre os arquivos (badge de revisao).
--   has_poster       EXISTS de um asset kind='poster' (a UI decide o poster_url).
-- Filtros opcionais (NULL = sem filtro) via sqlc.narg: type e owned. A ordenacao
-- por titulo em ordem DESC vive numa query irma (ListarEntityCardsDesc) porque o
-- sqlc nao liga parametros dentro de expressoes de ORDER BY -- direcao dinamica
-- num unico statement nao e suportada; o handler escolhe qual query rodar.
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') AS owned,
    (SELECT COUNT(*) FROM media_file m WHERE m.entity_id = e.id) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
FROM entity e
WHERE (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') = sqlc.narg('owned'))
ORDER BY e.title COLLATE NOCASE ASC, e.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListarEntityCardsDesc :many
-- Igual a ListarEntityCards, mas ordena por titulo DESCENDENTE (sort=-title).
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') AS owned,
    (SELECT COUNT(*) FROM media_file m WHERE m.entity_id = e.id) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
FROM entity e
WHERE (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') = sqlc.narg('owned'))
ORDER BY e.title COLLATE NOCASE DESC, e.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ContarEntityCards :one
-- Total de itens que casam os filtros (para o campo `total` da paginacao). Mesma
-- clausula WHERE da ListarEntityCards -- sem LIMIT/OFFSET/ORDER BY.
SELECT COUNT(*) AS total
FROM entity e
WHERE (sqlc.narg('type') IS NULL OR e.type = sqlc.narg('type'))
  AND (sqlc.narg('owned') IS NULL
       OR EXISTS (SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present') = sqlc.narg('owned'));

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

-- name: AtualizarExternalIds :exec
-- Mescla a identidade externa (MD4): a app le external_ids_json, junta
-- {source: source_id} e grava aqui, sem tocar o resto da entity. As colunas
-- geradas external_id_tmdb/imdb re-derivam sozinhas do novo JSON. Usado pelo
-- enrich/import (briefing 0015) -- media_external e cache subordinado a isto.
UPDATE entity
SET external_ids_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: ApagarEntity :exec
-- Purga explicita: remove a entity e, por ON DELETE CASCADE, seus media_file,
-- asset e entity_relation.
DELETE FROM entity
WHERE id = ?;
