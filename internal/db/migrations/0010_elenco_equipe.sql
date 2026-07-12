-- +goose Up
-- Migration 0010 (briefing 0029, MD6 Fase A): elenco, equipe e produtoras. Fecha a
-- ultima lacuna da tela rica -- quem FEZ a obra -- com um submodelo proprio (nao cabe
-- num campo simples nem numa lista de tags). Sao tres tabelas:
--
--   1. `person`  -- a PESSOA (ator ou membro de equipe), desacoplada da obra. Uma
--      pessoa aparece em MUITAS obras; e compartilhada por varios creditos. Dedup por
--      (source, source_id): o mesmo ator so tem UMA linha aqui.
--   2. `company` -- a PRODUTORA/estudio, tambem compartilhada e deduplicada por
--      (source, source_id).
--   3. `credit`  -- a ARESTA obra<->pessoa/empresa, onde moram os atributos do vinculo
--      (papel, personagem, departamento, ordem de bilhagem). E uma relacao N:N COM
--      DADOS NA ARESTA -- modelar assim (em vez de enfiar num extension_data opaco)
--      permite navegar por pessoa no futuro, deduplicar e indexar.
--
-- CACHE RE-COMPONIVEL (ADR-0011/0012): person/company/credit derivam do payload cru do
-- media_external -- podem ser REGENERADOS sem perda. O dado do DONO nunca vive aqui
-- (vive no media_override); por isso o re-enriquecimento pode apagar e repopular estas
-- tabelas sem tocar o override.
--
-- FKs: os tipos casam EXATAMENTE com as PKs referenciadas (senao a FK do SQLite nao
-- liga) -- `entity.id`, `person.id` e `company.id` sao todos TEXT (0002). O credito
-- some com a obra (ON DELETE CASCADE no entity_id: purga da entity limpa os creditos),
-- mas a pessoa/empresa NAO some quando um credito e removido (sao compartilhadas) --
-- por isso person_id/company_id NAO tem CASCADE.

CREATE TABLE person (
    id           TEXT PRIMARY KEY,
    source       TEXT NOT NULL,
    source_id    TEXT NOT NULL,
    name         TEXT NOT NULL,
    profile_path TEXT,
    payload_json TEXT,
    UNIQUE (source, source_id)
);

CREATE TABLE company (
    id           TEXT PRIMARY KEY,
    source       TEXT NOT NULL,
    source_id    TEXT NOT NULL,
    name         TEXT NOT NULL,
    logo_path    TEXT,
    payload_json TEXT,
    UNIQUE (source, source_id)
);

-- credit e a aresta. `ordering` (nao "order", palavra reservada) carrega a ordem de
-- bilhagem/estabilidade do elenco. `character` e o personagem (elenco); `department`/
-- `job` sao o departamento/cargo (equipe). `episode_count` e opcional (so serie: quantos
-- episodios aquele credito abrange). `kind` discrimina o papel: 'cast' | 'crew' |
-- 'company'. `source`/`source_id` ancoram a origem do credito (a app mantem a
-- invariante de identidade; o schema nao a expressa como constraint portavel).
CREATE TABLE credit (
    id            TEXT    PRIMARY KEY,
    entity_id     TEXT    NOT NULL REFERENCES entity(id) ON DELETE CASCADE,
    person_id     TEXT    REFERENCES person(id),
    company_id    TEXT    REFERENCES company(id),
    kind          TEXT    NOT NULL,
    department    TEXT,
    job           TEXT,
    character     TEXT,
    ordering      INTEGER NOT NULL DEFAULT 0,
    episode_count INTEGER,
    source        TEXT    NOT NULL,
    source_id     TEXT
);

-- Indice por entity_id: o lookup do detalhe (todos os creditos de uma obra).
CREATE INDEX idx_credit_entity ON credit(entity_id);
-- Indice por person_id: a filmografia futura (todas as obras de uma pessoa).
CREATE INDEX idx_credit_person ON credit(person_id);

-- +goose Down
-- Reverte 0010 na ordem inversa (indices antes das tabelas; credit antes de
-- person/company, que ele referencia).
DROP INDEX IF EXISTS idx_credit_person;
DROP INDEX IF EXISTS idx_credit_entity;
DROP TABLE IF EXISTS credit;
DROP TABLE IF EXISTS company;
DROP TABLE IF EXISTS person;
