-- CRUD de historico_evento (briefing 0092, ADR-0022): eventos de consumo
-- genericos (assistir/ouvir/jogar/etc.), append-only, filhos de entity.
-- Comentarios em ASCII -- ver a nota em entity.sql (sqlc v1.31 corrompe SQL
-- de query com acento).

-- name: CriarHistoricoEvento :one
INSERT INTO historico_evento (
    id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json
) VALUES (
    ?, ?, ?, ?, ?, ?,
    ?, ?, ?
)
RETURNING id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at;

-- name: ListarHistoricoEventoPorEntity :many
-- Lista os eventos de uma entity em ordem cronologica reversa. kind e
-- filtro opcional via sqlc.narg -- NULL = sem filtro (todos os kinds).
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at
FROM historico_evento
WHERE entity_id = sqlc.arg('entity_id')
  AND (sqlc.narg('kind') IS NULL OR kind = sqlc.narg('kind'))
ORDER BY occurred_at DESC, id DESC;

-- name: ListarUltimoHistoricoEventoPorKind :many
-- Base da projecao de "estado atual" (GET .../historico/estado): todos os
-- eventos da entity, mesma ordem cronologica reversa da listagem acima --
-- o agrupamento por kind (pegar so o primeiro de cada kind, ja que a
-- ordenacao e occurred_at DESC) fica para a camada Go, conforme o
-- briefing 0092 autoriza (volume baixo por entity, nao compensa SQL
-- avancado).
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at
FROM historico_evento
WHERE entity_id = ?
ORDER BY occurred_at DESC, id DESC;

-- name: BuscarHistoricoEventoPorID :one
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at
FROM historico_evento
WHERE id = ?;

-- name: ApagarHistoricoEvento :exec
DELETE FROM historico_evento
WHERE id = ?;

-- name: CriarHistoricoEventoOrfao :one
-- Grava um evento sem entity casada (briefing 0099): entity_id fica NULL
-- implicitamente (nao e parametro). Guarda os IDs/titulo/ano brutos do
-- payload para permitir localizar o evento como candidato a backfill
-- quando uma entity ganhar aquele ID externo no futuro.
INSERT INTO historico_evento (
    id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, raw_tmdb_id, raw_imdb_id,
    raw_tvdb_id, raw_title, raw_year
) VALUES (
    ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?
)
RETURNING id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, raw_tmdb_id, raw_imdb_id,
    raw_tvdb_id, raw_title, raw_year, created_at;

-- name: ListarHistoricoEventoOrfaos :many
-- Lista paginada dos eventos ainda sem entity (entity_id IS NULL), mesmo
-- padrao de paginacao de outras listas do projeto (limit/offset).
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, raw_tmdb_id, raw_imdb_id,
    raw_tvdb_id, raw_title, raw_year, created_at
FROM historico_evento
WHERE entity_id IS NULL
ORDER BY occurred_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: ContarHistoricoEventoOrfaos :one
-- Total de eventos orfaos, para o campo "total" da paginacao acima.
SELECT COUNT(*) FROM historico_evento WHERE entity_id IS NULL;

-- name: VincularHistoricoEventoOrfaoPorTmdb :execrows
-- Backfill retroativo (briefing 0099): vincula todo evento orfao cujo
-- raw_tmdb_id bate com o ID que a entity acabou de ganhar. :execrows para o
-- chamador saber quantas linhas foram vinculadas (log opcional).
UPDATE historico_evento
SET entity_id = ?
WHERE entity_id IS NULL AND raw_tmdb_id = ?;

-- name: VincularHistoricoEventoOrfaoPorImdb :execrows
UPDATE historico_evento
SET entity_id = ?
WHERE entity_id IS NULL AND raw_imdb_id = ?;

-- name: VincularHistoricoEventoOrfaoPorTvdb :execrows
UPDATE historico_evento
SET entity_id = ?
WHERE entity_id IS NULL AND raw_tvdb_id = ?;
