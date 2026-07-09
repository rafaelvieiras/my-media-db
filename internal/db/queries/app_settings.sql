-- Acesso a app_settings (briefing 0019): configs da aplicacao em pares key->JSON.
-- A primeira chave e "languages" = {primary, fallback, allowed[]}. Comentarios em
-- ASCII e colunas explicitas -- ver a nota em entity.sql (bug de offset do sqlc
-- v1.31 com acentos/'*' em .sql de query).

-- name: ObterAppSetting :one
-- Le uma config pela chave (sql.ErrNoRows quando a chave nao existe -- o helper
-- cai no default seguro).
SELECT key, value_json, updated_at
FROM app_settings
WHERE key = ?;

-- name: UpsertAppSetting :one
-- Grava (ou substitui) uma config. Conflito na PK (key) atualiza value_json e
-- recarimba updated_at -- nunca cria uma segunda linha para a mesma chave.
INSERT INTO app_settings (
    key, value_json
) VALUES (
    ?, ?
)
ON CONFLICT (key) DO UPDATE SET
    value_json = excluded.value_json,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING key, value_json, updated_at;
