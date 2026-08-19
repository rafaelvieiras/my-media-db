-- +goose Up
-- Migration 0023 (briefing 0105, ADR-0027): fundação de SCHEMA para a vertical
-- música (artista → álbum → faixa). Uma única mudança cirúrgica, retrocompatível:
--
--   `entity.external_id_mbid` — coluna GERADA que espelha `external_id_tmdb`/
--   `_imdb`/`_tvdb` (migrations 0002/0007), extraindo `$.mbid` de
--   `external_ids_json`. Dá lookup/dedup indexado por id da MusicBrainz —
--   artista, álbum (release) e faixa (recording) compartilham o mesmo esquema
--   de id da fonte (ADR-0027 §6). VIRTUAL como as irmãs (derivada em leitura,
--   não ocupa linha).
--
-- Hierarquia (artista/álbum/faixa) e atribuição (principal/feat.) usam
-- `entity.type` livre e `entity_relation.kind` livre, já existentes — nenhuma
-- tabela nova (ADR-0027, confirma AGENTS.md §3).

ALTER TABLE entity ADD COLUMN external_id_mbid TEXT
    GENERATED ALWAYS AS (json_extract(external_ids_json, '$.mbid')) VIRTUAL;

CREATE INDEX idx_entity_external_id_mbid ON entity (external_id_mbid);

-- +goose Down
-- O índice sai ANTES da coluna: o SQLite recusa DROP COLUMN de uma coluna
-- referenciada por índice.
DROP INDEX IF EXISTS idx_entity_external_id_mbid;
ALTER TABLE entity DROP COLUMN external_id_mbid;
