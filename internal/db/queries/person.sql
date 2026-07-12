-- Acesso a tabela person (briefing 0029, migration 0010): a pessoa deduplicada por
-- (source, source_id). Cache re-componivel do payload externo -- o dado do dono nunca
-- vive aqui. Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql (o
-- sqlc v1.31 corrompe SQL de query com acento).

-- name: UpsertPerson :one
-- Grava (ou atualiza) a pessoa deduplicada por (source, source_id). Conflito na UNIQUE
-- reusa a linha e atualiza name/profile_path/payload_json, devolvendo o id existente --
-- assim dois creditos da mesma pessoa apontam para UMA linha. `id` so e usado na
-- primeira insercao (fica estavel no update).
INSERT INTO person (
    id, source, source_id, name, profile_path, payload_json
) VALUES (
    ?, ?, ?, ?, ?, ?
)
ON CONFLICT (source, source_id) DO UPDATE SET
    name = excluded.name,
    profile_path = excluded.profile_path,
    payload_json = excluded.payload_json
RETURNING id;

-- name: ObterPersonPorFonte :one
-- Le a pessoa por (source, source_id) -- a chave logica de dedup.
SELECT id, source, source_id, name, profile_path, payload_json
FROM person
WHERE source = ? AND source_id = ?;

-- name: ObterPersonPorID :one
-- Le a pessoa pelo id local (person.id) -- para a pagina da pessoa (briefing 0029
-- Fase D, GET /person/{id}).
SELECT id, source, source_id, name, profile_path, payload_json
FROM person
WHERE id = ?;
