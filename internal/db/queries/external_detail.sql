-- Acesso ao CACHE do detalhe externo rico (external_detail, briefing 0022): uma linha
-- por (source, source_id). A primeira consulta faz UPSERT; as proximas LEEM daqui,
-- com a app decidindo servir do cache ou re-puxar (TTL + gatilho por idiomas).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: UpsertExternalDetail :exec
-- Grava (ou atualiza) o cache de UM item externo. Conflito na PK (source, source_id)
-- atualiza o payload/langs/type e recarimba fetched_at, sem duplicar a linha. E
-- best-effort na app: uma falha aqui nao impede a leitura (a proxima visita re-tenta).
INSERT INTO external_detail (
    source, source_id, type, payload_json, langs_json
) VALUES (
    ?, ?, ?, ?, ?
)
ON CONFLICT (source, source_id) DO UPDATE SET
    type = excluded.type,
    payload_json = excluded.payload_json,
    langs_json = excluded.langs_json,
    fetched_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: ObterExternalDetail :one
-- Le o cache de UM item externo pela chave logica (source, source_id). fetched_at e
-- langs_json alimentam a decisao de refresh (TTL + conjunto de idiomas) na app.
SELECT source, source_id, type, payload_json, langs_json, fetched_at
FROM external_detail
WHERE source = ? AND source_id = ?;
