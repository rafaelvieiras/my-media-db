-- +goose Up
-- Migration 0008 (briefing 0034, Fase 2 de series): marca o TIPO de conteudo de
-- cada raiz de varredura. Ate aqui o scanner era 100% filme; para o caminho de
-- series o job precisa saber, ANTES de varrer, se aquela fonte e de filmes ou de
-- series (decisao do dono: tipo explicito, como Kodi/Jellyfin/Plex -- nao
-- heuristica). A coluna carrega esse fato.
--
-- Valores esperados: 'movie' | 'series'. SEM CHECK, coerente com o resto do
-- schema (o `type` da propria library_source tambem nao tem CHECK). Default
-- 'movie' mantem retrocompat: toda fonte ja cadastrada continua sendo filme e o
-- caminho de filme fica intacto.
ALTER TABLE library_source
    ADD COLUMN content_type TEXT NOT NULL DEFAULT 'movie';

-- +goose Down
-- Reverte 0008. Nenhum indice referencia content_type, entao o DROP COLUMN direto
-- funciona (o SQLite do projeto ja usa DROP COLUMN nas migrations 0003/0005/0007).
ALTER TABLE library_source DROP COLUMN content_type;
