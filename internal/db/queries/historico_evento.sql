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
