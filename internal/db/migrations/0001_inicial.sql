-- +goose Up
-- Migration-semente (briefing 0006): prova o mecanismo de migrations do goose
-- embutido sem introduzir schema de domínio — as tabelas de domínio (entity,
-- media_file, ...) são o briefing 0007. Cria apenas uma tabela de metadados
-- trivial e reversível, que registra a versão da fundação de persistência.
CREATE TABLE mymediadb_meta (
    chave TEXT PRIMARY KEY,
    valor TEXT NOT NULL
);

INSERT INTO mymediadb_meta (chave, valor) VALUES ('fundacao', '0006');

-- +goose Down
DROP TABLE mymediadb_meta;
