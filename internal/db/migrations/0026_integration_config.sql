-- +goose Up
-- Migration 0026 (briefing 0095, ADR-0023): registro GENERICO de config de
-- integracao (credenciais/parametros de provider), fundacao para a UI de
-- Integracoes (briefing 0096). Uma linha por slug (ex.: "tmdb"); values_json
-- e um objeto RASO {campo: valor_string} -- so os campos que o dono setou
-- pela API/UI entram (blob esparso: presenca da chave = override ativo,
-- ausencia = herda da env var de bootstrap/fallback daquele campo). O
-- Resolver (internal/integration) calcula o valor EFETIVO por campo: banco
-- (esta tabela) -> env var -> ausente -- BANCO tem precedencia sobre a env
-- var (ADR-0023), o oposto do comportamento hoje (so env var, lida uma vez
-- no boot).
CREATE TABLE integration_config (
    slug        TEXT PRIMARY KEY,
    values_json TEXT NOT NULL DEFAULT '{}',
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
-- Sem dado a preservar no Down (mesmo espirito das demais migrations deste
-- projeto): so restaura o SCHEMA.
DROP TABLE integration_config;
