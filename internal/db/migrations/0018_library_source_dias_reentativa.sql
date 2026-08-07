-- +goose Up
-- Migration 0018 (briefing 0085): intervalo de reentativa do enriquecimento
-- retroativo OPCIONAL por library_source. Quando definido, sobrepoe o valor
-- GLOBAL (app_settings.enrichment_retroativo.retry_days, default 15 dias) so
-- para os itens desta biblioteca -- mesmo padrao de preferred_language
-- (migration 0017/briefing 0084). NULL (default) = sem override, usa o
-- global.
--
-- SEM CHECK, mesmo padrao de preferred_language: o intervalo valido (1-365
-- dias) e uma regra de produto, validada so na camada de aplicacao
-- (internal/server/library.go), nao no schema.
ALTER TABLE library_source ADD COLUMN retroactive_enrichment_days INTEGER;

-- +goose Down
ALTER TABLE library_source DROP COLUMN retroactive_enrichment_days;
