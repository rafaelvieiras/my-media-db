-- +goose Up
-- Migration 0006 (briefing 0022): CACHE do detalhe externo rico, ANTES de importar.
-- O dono quer ver o item externo por inteiro (como o TMDB) sem catalogar; a primeira
-- consulta puxa o detalhe completo da fonte e o GUARDA aqui, e as proximas servem do
-- cache. Decisoes travadas refletidas neste schema:
--   * Cache DESACOPLADO de `entity` (ver != catalogar): a chave e (source, source_id),
--     NAO um entity_id. Assim ver o externo nao cria linha orfa em media_external nem
--     polui o catalogo; o import (0015) e quem semeia media_external a partir daqui.
--   * Cache RE-COMPONIVEL (ADR-0011): `payload_json` guarda o ENVELOPE AUMENTADO do
--     provider ({detail, tags_by_lang} do 0020, agora com os campos ricos), que pode
--     ser apagado/re-puxado sem perda de dado do dono.
--   * Politica de REFRESH: serve do cache dentro de um TTL (regra na app); re-puxa
--     quando o TTL expira OU quando o conjunto de idiomas PERMITIDOS muda. `langs_json`
--     guarda os idiomas contidos no payload para a app detectar esse segundo gatilho
--     (um idioma recem-permitido nao esta no payload guardado -> re-puxar).
--   * `type` (movie|tv) e guardado porque a fonte exige o tipo para re-puxar o detalhe.
-- So fundacao + acesso a dados; a logica de cache-first/refresh vive na app
-- (internal/externaldetail) e o mapeamento rico no provider.

-- ── external_detail ─────────────────────────────────────────────────────────────
-- Uma linha por item externo efetivamente visto: a chave logica (source, source_id)
-- e a PK (uma consulta = um upsert por essa chave). `payload_json` e o envelope cru
-- aumentado; `langs_json` sao os idiomas contidos nele (para o gatilho de refresh);
-- `fetched_at` data a entrada (para o TTL). Sem FK para entity: o cache existe
-- independente de haver catalogo.
CREATE TABLE external_detail (
    source       TEXT NOT NULL,
    source_id    TEXT NOT NULL,
    type         TEXT NOT NULL,
    payload_json TEXT,
    langs_json   TEXT,
    fetched_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (source, source_id)
);

-- +goose Down
-- Reverte 0006: nenhuma outra tabela referencia external_detail (cache isolado),
-- entao o DROP e direto e o schema volta a versao 5.
DROP TABLE IF EXISTS external_detail;
