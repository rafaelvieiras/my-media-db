-- +goose Up
-- Migration 0020 (briefing 0092, ADR-0022): fundação do histórico de consumo
-- -- uma tabela de eventos genérica, agnóstica de vertical (filme, série e,
-- no futuro, música/livro/jogo), append-only, filha de `entity`. `kind`/
-- `status`/`source` são TEXT livre (sem CHECK fechado -- princípio 3 do
-- AGENTS.md): fechar o vocabulário aqui reintroduziria acoplamento por-
-- vertical. Convive sem sincronização com os campos locais de reprodução da
-- migration 0019 (decisão do ADR-0022, não unificados).
-- +goose StatementBegin
CREATE TABLE historico_evento (
    id                TEXT PRIMARY KEY,
    entity_id         TEXT NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    kind              TEXT NOT NULL,
    status            TEXT NOT NULL,
    progress_percent  REAL,
    position_seconds  REAL,
    occurred_at       TEXT NOT NULL,
    source            TEXT NOT NULL DEFAULT 'manual',
    payload_json      TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_historico_evento_entity_kind
    ON historico_evento (entity_id, kind, occurred_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE historico_evento;
-- +goose StatementEnd
