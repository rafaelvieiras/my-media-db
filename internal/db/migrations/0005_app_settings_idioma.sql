-- +goose Up
-- Migration 0005 (briefing 0019): fundação MULTI-IDIOMA no banco. Materializa as
-- duas peças de schema previstas no ADR-0018:
--   * `app_settings` — tabela genérica key/value (JSON) para configs da aplicação.
--     A primeira chave é `languages` = {primary, fallback, allowed[]}; o formato
--     genérico comporta futuras configs (tema, política de scan) sem nova migração.
--   * `entity.original_language` — coluna NEUTRA (código BCP-47/TMDB do idioma
--     original da obra). É a ÂNCORA do fallback por idioma (ADR-0019): a cadeia de
--     resolução `pedido → fallback → original_language → arquivo` a consulta antes
--     de cair na base neutra/arquivo. Preenchida pela fonte (o provider multi-idioma
--     do 0020); aqui só nasce a coluna (NULL por padrão).
--
-- Só fundação: schema + acesso a dados. A composição por idioma vive no resolver
-- (mesma migração de código, briefing 0019); o preenchimento das traduções externas
-- vem no 0020. SEM validação de JSON no schema — `value_json` é só TEXT; a validação
-- da config de idioma (principal/fallback ⊆ permitidos) vive onde ela é ESCRITA (o
-- handler PUT /settings/languages).

-- ── app_settings ──────────────────────────────────────────────────────────────
-- Configs da aplicação em pares chave→JSON. `key` é o slug estável da config
-- (ex.: "languages"); `value_json` guarda o valor serializado (objeto/array/escalar);
-- `updated_at` recarimba a cada UPSERT. O acesso (Obter/Upsert) vive nas queries
-- sqlc app_settings.sql.
CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- ── entity.original_language ──────────────────────────────────────────────────
-- Âncora neutra do fallback por idioma (ADR-0018/0019). NULL enquanto a fonte não
-- informar o idioma original — a cadeia simplesmente pula essa etapa quando nula.
ALTER TABLE entity ADD COLUMN original_language TEXT;

-- +goose Down
-- Reverte 0005: derruba a tabela de settings e remove a coluna âncora. O SQLite
-- moderno (>= 3.35) suporta DROP COLUMN diretamente; `original_language` não é
-- referenciada por índice nem por coluna gerada, então a remoção é direta e o
-- schema volta à versão 4.
ALTER TABLE entity DROP COLUMN original_language;
DROP TABLE IF EXISTS app_settings;
