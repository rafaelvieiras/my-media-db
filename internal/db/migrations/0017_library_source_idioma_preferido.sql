-- +goose Up
-- Migration 0017 (briefing 0084): idioma preferido OPCIONAL por library_source.
-- Quando definido, vira o idioma-alvo tanto da busca aos provedores externos
-- (scan automatico, auto-match, enriquecimento retroativo) quanto o default de
-- EXIBICAO dos itens originados daquela biblioteca -- sem exigir que o dono
-- troque o seletor de idioma de leitura toda vez que navega entre bibliotecas
-- diferentes. NULL (default) = sem preferenca, usa o idioma global de
-- app_settings (comportamento identico ao de hoje).
--
-- SEM CHECK, mesmo padrao de content_type (migration 0008): o conjunto valido
-- de idiomas e dinamico, vive em app_settings.languages.allowed -- nao faz
-- sentido travar no schema SQLite. Validacao fica na camada de aplicacao
-- (internal/server/library.go), no momento da escrita.
ALTER TABLE library_source ADD COLUMN preferred_language TEXT;

-- +goose Down
ALTER TABLE library_source DROP COLUMN preferred_language;
