-- CRUD de entity_relation (serie<->temporada<->episodio, colecao<->filme).
-- Criada agora; populada so pelos tipos que o scanner tratar (fase posterior).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: CriarEntityRelation :one
INSERT INTO entity_relation (
    id, parent_id, child_id, kind
) VALUES (
    ?, ?, ?, ?
)
RETURNING id, parent_id, child_id, kind;

-- name: ObterEntityRelation :one
SELECT id, parent_id, child_id, kind
FROM entity_relation
WHERE id = ?;

-- name: ListarRelacoesFilhas :many
-- Relacoes onde a entity e o pai (ex.: as temporadas de uma serie).
SELECT id, parent_id, child_id, kind
FROM entity_relation
WHERE parent_id = ?
ORDER BY id;

-- name: ListarRelacoesPais :many
-- Relacoes onde a entity e o filho (ex.: a serie de uma temporada).
SELECT id, parent_id, child_id, kind
FROM entity_relation
WHERE child_id = ?
ORDER BY id;

-- name: ApagarEntityRelation :exec
DELETE FROM entity_relation
WHERE id = ?;
