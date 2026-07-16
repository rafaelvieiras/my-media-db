-- +goose Up
-- Cache RECOMPONÍVEL (mesmo espírito do person/credit/company do 0029): o
-- resultado da avaliação de completude de catalogação (0057) por item,
-- recalculado em lote pelo job saude_biblioteca (0053/0058). Nunca guarda
-- dado do dono — pode ser apagado e regerado sem perda. 1:1 com entity;
-- ausência de linha = "ainda não avaliado" (item novo, ou draft).
CREATE TABLE entity_catalog_completeness (
    entity_id    TEXT PRIMARY KEY REFERENCES entity (id) ON DELETE CASCADE,
    complete     INTEGER NOT NULL, -- 0/1
    missing_json TEXT    NOT NULL, -- ex.: ["description","cast","artwork:fanart"]
    evaluated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE entity_catalog_completeness;
