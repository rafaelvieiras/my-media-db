-- +goose Up
-- Migration 0007 (briefing 0031, Fase 1 de séries): fundação de SCHEMA para o
-- modelo de séries do ADR-0021 (série → temporada → episódio). Só a base de dados
-- desta fase; a árvore vem dos provedores (TheTVDB + registry), sem NENHUM arquivo
-- local — a junção `media_file_episode` (N:M arquivo↔episódio) é da Fase 2 e NÃO
-- nasce aqui. Duas mudanças cirúrgicas, ambas retrocompatíveis (coleção existente
-- segue funcionando):
--
--   1. `entity_relation.sort_index` — ORDENAÇÃO ESTÁVEL de filhos. Hoje
--      `ListarRelacoesFilhas` ordena por `id` (ordem de inserção, arbitrária). Uma
--      série precisa que a temporada 1 venha antes da 2 e o episódio 1 antes do 2,
--      independente da ordem de importação; o `sort_index` carrega esse número
--      (nº da temporada dentro da série, nº do episódio dentro da temporada —
--      ADR-0021 §3). NULLABLE de propósito: as relações de coleção já gravadas
--      ficam com NULL e a query as ordena por `id` (comportamento atual preservado
--      via `ORDER BY sort_index IS NULL, sort_index, id`).
--
--   2. `entity.external_id_tvdb` — coluna GERADA que espelha `external_id_tmdb`/
--      `_imdb` (migration 0002), extraindo `$.tvdb` de `external_ids_json`. Dá
--      lookup/dedup indexado por id do TheTVDB (ADR-0021 §5 / TV4): séries
--      exclusivas do TheTVDB (ex.: Looney Tunes) têm identidade primária lá, sem
--      contraparte no TMDB, então precisam do mesmo caminho de dedup que o tmdb.
--      VIRTUAL como as irmãs (derivada em leitura, não ocupa linha); o SQLite
--      aceita ADD COLUMN de coluna gerada VIRTUAL (o mesmo não valeria para STORED).

-- 1. Ordenação estável de filhos em entity_relation.
ALTER TABLE entity_relation ADD COLUMN sort_index INTEGER;

-- 2. Coluna gerada + índice para lookup por id do TheTVDB (espelha tmdb/imdb).
ALTER TABLE entity ADD COLUMN external_id_tvdb TEXT
    GENERATED ALWAYS AS (json_extract(external_ids_json, '$.tvdb')) VIRTUAL;

CREATE INDEX idx_entity_external_id_tvdb ON entity (external_id_tvdb);

-- +goose Down
-- Reverte 0007 para o schema 0006. O índice sai ANTES da coluna: o SQLite recusa
-- DROP COLUMN de uma coluna referenciada por índice. Depois removem-se a coluna
-- gerada (DROP COLUMN funciona para gerada VIRTUAL) e o `sort_index`.
DROP INDEX IF EXISTS idx_entity_external_id_tvdb;
ALTER TABLE entity DROP COLUMN external_id_tvdb;
ALTER TABLE entity_relation DROP COLUMN sort_index;
