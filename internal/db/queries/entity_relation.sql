-- CRUD de entity_relation (serie<->temporada<->episodio, colecao<->filme).
-- Criada agora; populada so pelos tipos que o scanner tratar (fase posterior).
-- Comentarios em ASCII e colunas explicitas -- ver a nota em entity.sql.

-- name: CriarEntityRelation :one
-- sort_index e NULLABLE: colecao passa NULL (ordena por id, como antes); serie/
-- temporada passam o numero (temporada dentro da serie, episodio dentro da
-- temporada) para a ordenacao estavel de ListarRelacoesFilhas.
INSERT INTO entity_relation (
    id, parent_id, child_id, kind, sort_index
) VALUES (
    ?, ?, ?, ?, ?
)
RETURNING id, parent_id, child_id, kind, sort_index;

-- name: ObterEntityRelation :one
SELECT id, parent_id, child_id, kind, sort_index
FROM entity_relation
WHERE id = ?;

-- name: ListarRelacoesFilhas :many
-- Relacoes onde a entity e o pai (ex.: as temporadas de uma serie). Ordena por
-- sort_index (numero estavel do filho) e cai para id quando ele e NULL. A forma
-- `sort_index IS NULL, sort_index, id` empurra os NULL para o fim sem depender de
-- NULLS LAST (portavel entre SQLite/modernc); colecao (sort_index todo NULL)
-- mantem a ordenacao por id de antes.
SELECT id, parent_id, child_id, kind, sort_index
FROM entity_relation
WHERE parent_id = ?
ORDER BY sort_index IS NULL, sort_index, id;

-- name: ListarRelacoesPais :many
-- Relacoes onde a entity e o filho (ex.: a serie de uma temporada).
SELECT id, parent_id, child_id, kind, sort_index
FROM entity_relation
WHERE child_id = ?
ORDER BY id;

-- name: ApagarEntityRelation :exec
DELETE FROM entity_relation
WHERE id = ?;
