-- name: ValorMeta :one
-- Le um valor da tabela de metadados pela chave. Serve de query-semente para
-- provar o wiring do sqlc de ponta a ponta; queries de dominio chegam no 0007.
-- (Comentario em ASCII: o sqlc corrompe o SQL gerado com acentos no .sql de
-- query -- ver a nota em entity.sql.)
SELECT valor FROM mymediadb_meta
WHERE chave = ?;
