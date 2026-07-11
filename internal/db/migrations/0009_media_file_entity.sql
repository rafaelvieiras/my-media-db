-- +goose Up
-- Migration 0009 (briefing 0034, Fase 2 de series): a juncao N:M GENERICA entre
-- arquivos e entidades. Nasceu para ligar arquivo<->episodio de TV, mas e
-- deliberadamente agnostica ao tipo: um `kind` discrimina o papel do vinculo
-- (`tv_episode` para o scanner de series; um plugin de musica gravaria `track`,
-- etc.). Assim futuros plugins reusam esta tabela SEM migration do core -- espelha
-- a mesma ideia de `entity_relation` (relacao generica entre entidades).
--
-- O vinculo e SEMPRE pelo `entity_id` (a licao do bug do Sonarr, ADR-0021 §3/§4):
-- trocar a ordenacao da serie re-projeta numeros SEM tocar vinculo nenhum.
--
-- Por que N:M: um mesmo arquivo pode conter varias entidades (rip que empacota um
-- range `S01E01-E03`, ou combinado de streaming) e uma mesma entidade pode ter
-- mais de um arquivo. `ordinal` ordena as entidades DENTRO de um mesmo arquivo
-- (0,1,2... na ordem do range) -- nao e numero de episodio, e so a sequencia local.
--
-- Tipos das FKs casam EXATAMENTE com as PKs referenciadas (senao a FK do SQLite
-- nao liga): `media_file.id` e TEXT (0002) e `entity.id` e TEXT (0002) -- ambos
-- TEXT aqui, NAO INTEGER. ON DELETE CASCADE: apagar o arquivo ou a entidade limpa
-- os vinculos orfaos. A PK inclui `kind`: o mesmo arquivo pode, em tese, ligar a
-- mesma entidade com papeis (kinds) diferentes.
CREATE TABLE media_file_entity (
    media_file_id TEXT    NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
    entity_id     TEXT    NOT NULL REFERENCES entity(id)     ON DELETE CASCADE,
    kind          TEXT    NOT NULL,
    ordinal       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (media_file_id, entity_id, kind)
);

-- Indice para a leitura por entidade+kind (arquivos de um episodio, posse
-- agregada): a PK ja cobre a busca por media_file_id (prefixo), mas nao a busca
-- por entity_id, que fica sem indice sem isto. Inclui `kind` para casar o filtro
-- tipico (entity_id + kind).
CREATE INDEX idx_mfe_entity ON media_file_entity(entity_id, kind);

-- +goose Down
-- Reverte 0009 na ordem inversa: o indice antes da tabela.
DROP INDEX IF EXISTS idx_mfe_entity;
DROP TABLE IF EXISTS media_file_entity;
