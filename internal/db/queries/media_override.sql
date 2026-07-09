-- Acesso a camada de OVERRIDE (media_override): patch esparso do dono, 1:1 com a
-- entity (PK = entity_id). O editor de override (0016) grava aqui via UPSERT.
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: UpsertMediaOverride :one
-- Grava (ou substitui) o override da entity. Conflito na PK (entity_id) atualiza
-- override_json e recarimba updated_at -- 1:1, nunca cria uma segunda linha.
INSERT INTO media_override (
    entity_id, override_json
) VALUES (
    ?, ?
)
ON CONFLICT (entity_id) DO UPDATE SET
    override_json = excluded.override_json,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING entity_id, override_json, updated_at;

-- name: ObterMediaOverride :one
-- Le o override da entity (sql.ErrNoRows quando nao ha override ativo).
SELECT entity_id, override_json, updated_at
FROM media_override
WHERE entity_id = ?;

-- name: ApagarMediaOverride :exec
-- Remove o override da entity (volta ao valor da base editorial/arquivo).
DELETE FROM media_override
WHERE entity_id = ?;
