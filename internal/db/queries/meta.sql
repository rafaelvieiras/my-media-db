-- name: ValorMeta :one
-- Reads a value from the metadata table by key. It serves as the seed query that
-- proves the sqlc wiring end to end; domain queries arrive in 0007.
-- (Comment in ASCII: sqlc corrupts the generated SQL when the query .sql has
-- accents -- see the note in entity.sql.)
SELECT valor FROM mymediadb_meta
WHERE chave = ?;
