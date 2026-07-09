-- name: ValorMeta :one
-- Lê um valor da tabela de metadados pela chave. Serve de query-semente para
-- provar o wiring do sqlc de ponta a ponta; queries de domínio chegam no 0007.
SELECT valor FROM mymediadb_meta
WHERE chave = ?;
