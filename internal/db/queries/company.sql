-- Acesso a tabela company (briefing 0029, migration 0010): a produtora/estudio
-- deduplicada por (source, source_id). Cache re-componivel. Comentarios em ASCII e
-- colunas explicitas -- ver a nota em entity.sql.

-- name: UpsertCompany :one
-- Grava (ou atualiza) a produtora deduplicada por (source, source_id). Conflito na
-- UNIQUE reusa a linha e atualiza name/logo_path/payload_json, devolvendo o id
-- existente. `id` so e usado na primeira insercao.
INSERT INTO company (
    id, source, source_id, name, logo_path, payload_json
) VALUES (
    ?, ?, ?, ?, ?, ?
)
ON CONFLICT (source, source_id) DO UPDATE SET
    name = excluded.name,
    logo_path = excluded.logo_path,
    payload_json = excluded.payload_json
RETURNING id;

-- name: ObterCompanyPorFonte :one
-- Le a produtora por (source, source_id) -- a chave logica de dedup.
SELECT id, source, source_id, name, logo_path, payload_json
FROM company
WHERE source = ? AND source_id = ?;
