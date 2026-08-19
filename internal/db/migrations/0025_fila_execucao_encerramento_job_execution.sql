-- +goose Up
-- Migration 0025 (briefing 0108, ADR-0028): CAMADA 3 (final) da fila de
-- execucao unificada -- completa a decisao faseada que a migration 0024
-- deixou registrada em nota ("NOTA DE FASEAMENTO"): job_execution so podia
-- ser derrubada depois que internal/job/manager.go parasse de ler/escrever
-- nela diretamente e passasse a enfileirar via internal/execqueue (agora
-- feito). Os dados de job_execution ja foram copiados para queue_item
-- (kind='job') pela migration 0024 e continuam la -- esta migration so
-- remove a tabela/indice que virou a SEGUNDA fonte de verdade redundante
-- (AGENTS.md: nunca duas fontes de verdade para o mesmo dado).
DROP INDEX IF EXISTS idx_job_execution_job;
DROP TABLE job_execution;

-- +goose Down
-- Recria job_execution com o MESMO shape da migration 0011 (internal/db/
-- migrations/0011_jobs.sql) + seu indice -- SEM tentar restaurar os dados:
-- a linha ja foi apagada de verdade pelo DROP TABLE do Up, e nao ha copia
-- para trazer de volta aqui (o historico equivalente continua em
-- queue_item, que esta migration nao mexe). Mesma limitacao inerente a
-- qualquer DROP TABLE -- o Down deste arquivo restaura o SCHEMA, nao o dado.
CREATE TABLE job_execution (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES job_definition(id) ON DELETE CASCADE,
    trigger     TEXT NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    status      TEXT NOT NULL CHECK (status IN ('running', 'success', 'error')),
    started_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at TEXT,
    message     TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_job_execution_job ON job_execution(job_id, started_at DESC);
