-- +goose Up
-- Migration 0021 (briefing 0099): eventos de histórico órfãos + backfill.
-- Hoje um evento de webhook (Jellyfin) cujos IDs externos não casam com
-- nenhuma entity local é descartado silenciosamente -- perde informação que
-- o dono não recupera depois (o Jellyfin não reenvia webhooks passados).
-- Isso contraria o princípio 2 do AGENTS.md ("o banco é maior que a
-- biblioteca... cataloga o que se tem E o que ainda não se tem"). Esta
-- migration torna `entity_id` opcional e guarda os IDs/título/ano BRUTOS do
-- evento, permitindo (a) listar os órfãos e (b) vincular retroativamente
-- quando a entity certa ganhar aquele ID externo (backfill).
--
-- SQLite não tem ALTER COLUMN para remover NOT NULL: rebuild da tabela
-- (RENAME -> CREATE nova -> INSERT SELECT -> DROP da antiga), mesmo padrão
-- já usado neste projeto para mudanças de schema que o ALTER TABLE do
-- SQLite não cobre.
-- +goose StatementBegin
ALTER TABLE historico_evento RENAME TO historico_evento_old;

CREATE TABLE historico_evento (
    id                TEXT PRIMARY KEY,
    entity_id         TEXT REFERENCES entity (id) ON DELETE CASCADE, -- nullable agora: evento sem match vira orfao
    kind              TEXT NOT NULL,
    status            TEXT NOT NULL,
    progress_percent  REAL,
    position_seconds  REAL,
    occurred_at       TEXT NOT NULL,
    source            TEXT NOT NULL DEFAULT 'manual',
    payload_json      TEXT,
    raw_tmdb_id       TEXT, -- IDs brutos do evento (usados so quando entity_id e NULL, para
    raw_imdb_id       TEXT, -- localizar candidatos a backfill). Ficam preenchidos mesmo apos
    raw_tvdb_id       TEXT, -- o backfill vincular o evento -- e proveniencia, nao e limpo.
    raw_title         TEXT, -- titulo/ano brutos, so para exibicao da lista de orfaos quando
    raw_year          TEXT, -- o payload trouxer.
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO historico_evento (
    id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, raw_tmdb_id, raw_imdb_id,
    raw_tvdb_id, raw_title, raw_year, created_at
)
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, NULL, NULL, NULL, NULL, NULL,
    created_at
FROM historico_evento_old;

DROP TABLE historico_evento_old;

-- Recria o indice original da migration 0020 (o rebuild acima nao preserva
-- indices da tabela renomeada).
CREATE INDEX idx_historico_evento_entity_kind
    ON historico_evento (entity_id, kind, occurred_at DESC);

-- Indice parcial: so cobre as linhas orfas (entity_id IS NULL), que sao as
-- unicas consultadas por esse padrao (candidatos a backfill por ID bruto).
CREATE INDEX idx_historico_evento_orfaos
    ON historico_evento (raw_tmdb_id, raw_imdb_id, raw_tvdb_id)
    WHERE entity_id IS NULL;
-- +goose StatementEnd

-- +goose Down
-- Reverte para o schema anterior (entity_id NOT NULL, sem colunas raw_*).
-- Eventos que ficaram orfaos (entity_id NULL) violariam a constraint NOT
-- NULL do schema antigo -- nao ha valor de entity_id valido para inventar,
-- entao essas linhas sao descartadas no rollback (perda de dado esperada e
-- aceita em um downgrade: o dono optou por reverter a feature que permitia
-- representa-las).
-- +goose StatementBegin
DELETE FROM historico_evento WHERE entity_id IS NULL;

ALTER TABLE historico_evento RENAME TO historico_evento_new;

CREATE TABLE historico_evento (
    id                TEXT PRIMARY KEY,
    entity_id         TEXT NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    kind              TEXT NOT NULL,
    status            TEXT NOT NULL,
    progress_percent  REAL,
    position_seconds  REAL,
    occurred_at       TEXT NOT NULL,
    source            TEXT NOT NULL DEFAULT 'manual',
    payload_json      TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO historico_evento (
    id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at
)
SELECT id, entity_id, kind, status, progress_percent, position_seconds,
    occurred_at, source, payload_json, created_at
FROM historico_evento_new;

DROP TABLE historico_evento_new;

CREATE INDEX idx_historico_evento_entity_kind
    ON historico_evento (entity_id, kind, occurred_at DESC);
-- +goose StatementEnd
