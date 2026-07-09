-- +goose Up
-- Migration 0004 (briefing 0012): materializa as OUTRAS DUAS CAMADAS da Fase 3
-- previstas no modelo de dados (doc 04) — o `externo` (payload puxado das fontes,
-- base editorial) e o `override` (patch esparso do dono, autoridade final). É só
-- fundação: schema + acesso a dados, sem comportamento. A composição das três
-- camadas (arquivo → externo → override) é do resolver (briefing 0013); o
-- preenchimento real vem do enriquecimento (0014+). Um schema errado aqui
-- contamina toda a fase, por isso ele nasce isolado e testável.
--
-- Decisões travadas refletidas aqui:
--   * external_ids é o mapa autoritativo, media_external é o CACHE (ADR-0011):
--     `payload_json` guarda o payload CRU da fonte para re-compor sem re-puxar.
--     `UNIQUE(entity_id, source)` = uma linha de cache por fonte por entidade; o
--     re-enriquecimento faz UPSERT por essa chave (atualiza, não duplica).
--   * override como blob JSON esparso (ADR-0004): `media_override` é 1:1 com a
--     entity (PK = entity_id); `override_json` espelha o formato da entity só com
--     os campos mudados. SEM validação de JSON aqui — as colunas são só TEXT; a
--     validação por JSON Schema (R1/R5) vive onde o override é ESCRITO (0016).
--   * Invariante MD4 (`media_external.source_id == external_ids[source]`) é
--     responsabilidade da APP ao gravar, não do schema (SQLite não a expressa bem
--     como constraint portável).
--   * FKs ON DELETE CASCADE: só a PURGA explícita da entity remove as camadas,
--     coerente com o núcleo da Fase 2 e o ciclo de vida não-destrutivo.

-- ── media_external ────────────────────────────────────────────────────────────
-- Cache do payload de cada fonte efetivamente buscada para uma entity. `source` é
-- o slug da fonte ("tmdb"); `source_id` é o id externo naquela fonte (subordinado
-- ao entity.external_ids, mantido pela app). `payload_json` é o payload cru.
CREATE TABLE media_external (
    id           TEXT PRIMARY KEY,
    entity_id    TEXT NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    source       TEXT NOT NULL,
    source_id    TEXT,
    payload_json TEXT,
    fetched_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    -- Uma linha de cache por (entidade, fonte): o re-enriquecimento faz UPSERT.
    UNIQUE (entity_id, source)
);

-- Índice para o lookup por id externo (achar a entity pelo par fonte+id da fonte,
-- ex.: reconciliar identidade). Sem ele seria varredura da tabela.
CREATE INDEX idx_media_external_source ON media_external (source, source_id);

-- ── media_override ────────────────────────────────────────────────────────────
-- Patch esparso do dono, UMA linha por entidade (PK = entity_id, 1:1). A presença
-- de uma chave em `override_json` = override ativo daquele campo — mas isso é
-- regra de COMPOSIÇÃO do resolver (0013), não do schema; aqui é só persistência.
CREATE TABLE media_override (
    entity_id     TEXT PRIMARY KEY REFERENCES entity (id) ON DELETE CASCADE,
    override_json TEXT,
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
-- Reverte 0004 na ordem inversa/segura: cada DROP TABLE já leva junto o índice e a
-- UNIQUE da própria tabela. Nenhuma outra tabela referencia estas duas, então a
-- ordem entre elas é livre; removemos as duas e o schema volta à versão 3.
DROP TABLE IF EXISTS media_override;
DROP TABLE IF EXISTS media_external;
