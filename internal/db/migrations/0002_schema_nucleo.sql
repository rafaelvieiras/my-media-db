-- +goose Up
-- Migration 0002 (briefing 0007): materializa o SCHEMA MÍNIMO DA FASE 2 do
-- núcleo — promove o "esboço de tabelas" da doc 04 a schema real. Cria as
-- entidades genéricas (`entity`), seus arquivos (`media_file`), as fontes de
-- biblioteca (`library_source`), os assets (`asset`) e as relações entre
-- entidades (`entity_relation`), além da busca textual FTS5 e das colunas
-- geradas de `external_ids` para lookup por id externo.
--
-- Decisões travadas refletidas aqui:
--   * Posse é DERIVADA (ADR-0009): NÃO existe coluna `owned`. A posse é
--     computada por EXISTS de um `media_file` com state='present' (ver a query
--     `EntityTemPosse` em queries/media_file.sql). `status` é só intenção.
--   * external_ids é o mapa autoritativo (ADR-0011): expomos `$.tmdb`/`$.imdb`
--     como COLUNAS GERADAS + índice para lookup por id externo (busca sem GIN).
--   * Ciclo de vida não-destrutivo (ADR-0012): `media_file.state`
--     (present|missing) + `last_seen`, e `library_source.state` (online|offline)
--     modelam o sumiço de arquivo/disco sem mexer no `status` do dono.

-- ── entity ────────────────────────────────────────────────────────────────
-- A entidade base, genérica para qualquer mídia. Campos *_json são TEXT com
-- payload JSON validado PELA APP antes de gravar (JSON1 disponível). As duas
-- colunas geradas ao fim (external_id_tmdb/imdb) são derivadas de
-- external_ids_json e existem só para o lookup indexado por id externo — não são
-- editáveis nem participam de INSERT/UPDATE.
CREATE TABLE entity (
    id                  TEXT PRIMARY KEY,
    type                TEXT NOT NULL,
    title               TEXT NOT NULL,
    original_title      TEXT,
    description         TEXT,
    release_date        TEXT,
    status              TEXT NOT NULL DEFAULT 'cataloged'
                            CHECK (status IN ('cataloged', 'wanted', 'unreleased', 'draft')),
    tags_json           TEXT,
    external_ids_json   TEXT,
    extension_data_json TEXT,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    -- Colunas geradas para lookup por id externo (doc 04 — busca sem GIN).
    external_id_tmdb    TEXT GENERATED ALWAYS AS (json_extract(external_ids_json, '$.tmdb')) VIRTUAL,
    external_id_imdb    TEXT GENERATED ALWAYS AS (json_extract(external_ids_json, '$.imdb')) VIRTUAL
);

-- Índices das colunas geradas: fazem o lookup por tmdb/imdb usar índice
-- (SEARCH) em vez de varredura (SCAN). Exigido pelo critério de aceite.
CREATE INDEX idx_entity_external_id_tmdb ON entity (external_id_tmdb);
CREATE INDEX idx_entity_external_id_imdb ON entity (external_id_imdb);

-- ── media_file ──────────────────────────────────────────────────────────────
-- Um arquivo físico associado a uma entity (1:N — versões/qualidades/edições).
-- `state` + `last_seen` implementam o ciclo de vida não-destrutivo (ADR-0012):
-- um arquivo some vira 'missing', a entity é preservada. ON DELETE CASCADE só
-- age numa PURGA explícita da entity (remoção do pai), nunca por sumiço de disco.
CREATE TABLE media_file (
    id               TEXT PRIMARY KEY,
    entity_id        TEXT NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    path             TEXT NOT NULL UNIQUE,
    content_hash     TEXT,
    size             INTEGER,
    mtime            TEXT,
    edition          TEXT,
    part_no          INTEGER,
    match_confidence REAL,
    mediainfo_json   TEXT,
    state            TEXT NOT NULL DEFAULT 'present'
                        CHECK (state IN ('present', 'missing')),
    last_seen        TEXT,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- entity_id: acelera o join/posse por entity. content_hash: indexado (não-unique)
-- para achar duplicatas/mesmos bytes em caminhos diferentes.
CREATE INDEX idx_media_file_entity ON media_file (entity_id);
CREATE INDEX idx_media_file_content_hash ON media_file (content_hash);

-- ── library_source ──────────────────────────────────────────────────────────
-- Uma raiz de varredura (pasta/volume). `state` online|offline sinaliza disco
-- desmontado — um source offline NÃO marca seus arquivos como missing (ADR-0012).
CREATE TABLE library_source (
    id         TEXT PRIMARY KEY,
    path       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL,
    last_scan  TEXT,
    state      TEXT NOT NULL DEFAULT 'online'
                    CHECK (state IN ('online', 'offline')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- ── asset ────────────────────────────────────────────────────────────────────
-- Artes e binários referenciados por uma entity (poster/fanart/…). O binário
-- vive no disco; aqui guardamos só a referência (path_or_uri) e metadados.
CREATE TABLE asset (
    id          TEXT PRIMARY KEY,
    entity_id   TEXT NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    origin      TEXT NOT NULL,
    path_or_uri TEXT NOT NULL,
    meta_json   TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_asset_entity ON asset (entity_id);

-- ── entity_relation ─────────────────────────────────────────────────────────
-- Relações entre entidades (série↔temporada↔episódio, coleção↔filme). Criada
-- agora; populada só pelos tipos que o scanner tratar (fase posterior). A
-- UNIQUE evita relação duplicada do mesmo tipo entre o mesmo par.
CREATE TABLE entity_relation (
    id        TEXT PRIMARY KEY,
    parent_id TEXT NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    child_id  TEXT NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    kind      TEXT NOT NULL,
    UNIQUE (parent_id, child_id, kind)
);

CREATE INDEX idx_entity_relation_parent ON entity_relation (parent_id);
CREATE INDEX idx_entity_relation_child ON entity_relation (child_id);

-- ── entity_fts (FTS5) ────────────────────────────────────────────────────────
-- Busca textual sobre título/original/tags (doc 04 — busca sem GIN).
--
-- Decisão de design (FTS5 com PK textual): `entity.id` é TEXT/uuid, então o
-- padrão FTS5 "external content" (que exige um content_rowid INTEGER espelhando
-- a PK) NÃO encaixa. Usamos uma FTS5 STANDALONE (armazena o próprio conteúdo)
-- com uma coluna extra `entity_id UNINDEXED` que mapeia cada hit de volta à
-- entity. Os triggers abaixo sincronizam por `entity_id` (o DELETE apaga por
-- entity_id = old.id). Simplicidade > economia de storage no contexto
-- self-hosted. `tags` recebe o tags_json cru: o tokenizer unicode61 quebra nos
-- sinais do JSON e indexa os valores das tags.
CREATE VIRTUAL TABLE entity_fts USING fts5 (
    title,
    original_title,
    tags,
    entity_id UNINDEXED
);

-- Triggers de sincronização entity → entity_fts. Envolvidos em
-- StatementBegin/End porque contêm ';' internos que o goose, sem a anotação,
-- cortaria no meio.

-- +goose StatementBegin
CREATE TRIGGER entity_fts_ai AFTER INSERT ON entity BEGIN
    INSERT INTO entity_fts (title, original_title, tags, entity_id)
    VALUES (new.title, new.original_title, new.tags_json, new.id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER entity_fts_ad AFTER DELETE ON entity BEGIN
    DELETE FROM entity_fts WHERE entity_id = old.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER entity_fts_au AFTER UPDATE ON entity BEGIN
    DELETE FROM entity_fts WHERE entity_id = old.id;
    INSERT INTO entity_fts (title, original_title, tags, entity_id)
    VALUES (new.title, new.original_title, new.tags_json, new.id);
END;
-- +goose StatementEnd

-- +goose Down
-- Reverte 0002 na ordem inversa: triggers e FTS primeiro, depois as tabelas dos
-- filhos antes dos pais (FK). Índices somem junto das tabelas.
DROP TRIGGER IF EXISTS entity_fts_au;
DROP TRIGGER IF EXISTS entity_fts_ad;
DROP TRIGGER IF EXISTS entity_fts_ai;
DROP TABLE IF EXISTS entity_fts;
DROP TABLE IF EXISTS entity_relation;
DROP TABLE IF EXISTS asset;
DROP TABLE IF EXISTS library_source;
DROP TABLE IF EXISTS media_file;
DROP TABLE IF EXISTS entity;
