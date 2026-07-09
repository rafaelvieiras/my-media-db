-- +goose Up
-- Migration 0003 (briefing 0009): liga cada media_file à library_source de
-- origem. Sem essa coluna, o ciclo de vida por datasource (ADR-0012) não teria
-- como saber QUAIS arquivos pertencem ao datasource que está sendo varrido — e
-- portanto não poderia marcar `missing` apenas os arquivos daquela fonte num
-- scan completo, nem suspender com segurança quando a fonte fica offline.
--
-- Alinha o schema ao ER das docs 04/06 ("library_source varre media_file"). A
-- coluna é NULA por padrão (arquivos legados de antes desta migration ficam sem
-- origem conhecida até o próximo scan os reencontrar) e referencia
-- library_source(id) SEM cascata: apagar uma fonte NÃO apaga arquivos (o
-- descadastro de fonte é não-destrutivo, ADR-0012); a limpeza é sempre explícita.
ALTER TABLE media_file
    ADD COLUMN library_source_id TEXT REFERENCES library_source (id);

-- Índice para o ciclo de vida: marcar `missing` os arquivos não-vistos de UMA
-- fonte varre por library_source_id; sem índice seria full scan da tabela.
CREATE INDEX idx_media_file_library_source ON media_file (library_source_id);

-- +goose Down
-- Reverte na ordem inversa: o índice antes da coluna (o SQLite recusa DROP COLUMN
-- de uma coluna referenciada por índice).
DROP INDEX IF EXISTS idx_media_file_library_source;
ALTER TABLE media_file DROP COLUMN library_source_id;
