-- Acesso a integration_config (briefing 0095): registro generico de config de
-- integracao (credenciais/parametros de provider), com precedencia BANCO >
-- env var calculada pelo Resolver (internal/integration). Comentarios em
-- ASCII e colunas explicitas -- mesma nota do sqlc v1.31 corrompendo acento
-- em .sql de query (ver entity.sql).

-- name: ObterIntegrationConfig :one
-- Le a config de uma integracao pelo slug (sql.ErrNoRows quando o slug nunca
-- foi gravado -- o Resolver cai para a env var nesse caso).
SELECT slug, values_json, updated_at
FROM integration_config
WHERE slug = ?;

-- name: UpsertIntegrationConfig :one
-- Grava (ou substitui) o values_json inteiro de um slug. Conflito na PK
-- (slug) atualiza values_json e recarimba updated_at -- nunca cria uma
-- segunda linha para o mesmo slug. O PUT parcial (merge campo a campo,
-- null remove a chave) e responsabilidade do handler HTTP, que le o valor
-- atual, mescla em memoria e chama este upsert com o objeto ja completo.
INSERT INTO integration_config (
    slug, values_json
) VALUES (
    ?, ?
)
ON CONFLICT (slug) DO UPDATE SET
    values_json = excluded.values_json,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING slug, values_json, updated_at;
