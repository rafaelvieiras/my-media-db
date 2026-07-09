-- Acesso ao cache da camada EXTERNA (media_external): uma linha de payload cru por
-- (entity_id, source). O re-enriquecimento faz UPSERT por essa chave (ADR-0011).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: UpsertMediaExternal :one
-- Grava (ou atualiza) o cache de UMA fonte para a entity. Conflito na UNIQUE
-- (entity_id, source) atualiza source_id/payload_json e recarimba fetched_at, sem
-- duplicar a linha. `id` so e usado na primeira insercao (fica estavel no update).
INSERT INTO media_external (
    id, entity_id, source, source_id, payload_json
) VALUES (
    ?, ?, ?, ?, ?
)
ON CONFLICT (entity_id, source) DO UPDATE SET
    source_id = excluded.source_id,
    payload_json = excluded.payload_json,
    fetched_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING id, entity_id, source, source_id, payload_json, fetched_at;

-- name: ObterMediaExternalPorFonte :one
-- Le o cache de UMA fonte especifica da entity (a chave logica do cache).
SELECT id, entity_id, source, source_id, payload_json, fetched_at
FROM media_external
WHERE entity_id = ? AND source = ?;

-- name: ListarMediaExternalPorEntity :many
-- Todas as camadas externas cacheadas de uma entity (uma por fonte buscada).
SELECT id, entity_id, source, source_id, payload_json, fetched_at
FROM media_external
WHERE entity_id = ?
ORDER BY source, id;

-- name: ApagarMediaExternal :exec
-- Remove uma linha de cache pelo seu id (ex.: invalidar uma fonte).
DELETE FROM media_external
WHERE id = ?;
