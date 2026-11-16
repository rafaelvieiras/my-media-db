-- CRUD for entity_relation (series<->season<->episode, collection<->movie).
-- Created now; populated only by the types the scanner handles (later phase).
-- Comments in ASCII and explicit columns -- see the note in entity.sql.

-- name: CriarEntityRelation :one
-- sort_index is NULLABLE: collection passes NULL (orders by id, as before); series/
-- season pass the number (season within the series, episode within the
-- season) for the stable ordering of ListarRelacoesFilhas.
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
-- Relations where the entity is the parent (e.g. the seasons of a series). Orders by
-- sort_index (stable number of the child) and falls back to id when it is NULL. The form
-- `sort_index IS NULL, sort_index, id` pushes the NULLs to the end without relying on
-- NULLS LAST (portable across SQLite/modernc); collection (sort_index all NULL)
-- keeps the previous ordering by id.
SELECT id, parent_id, child_id, kind, sort_index
FROM entity_relation
WHERE parent_id = ?
ORDER BY sort_index IS NULL, sort_index, id;

-- name: ListarRelacoesPais :many
-- Relations where the entity is the child (e.g. the series of a season).
SELECT id, parent_id, child_id, kind, sort_index
FROM entity_relation
WHERE child_id = ?
ORDER BY id;

-- name: ApagarEntityRelation :exec
DELETE FROM entity_relation
WHERE id = ?;
