-- +goose Up
-- Migration 0011 (briefing 0052): fundação do SISTEMA DE JOBS -- execução
-- periódica dentro do próprio binário (goroutine + ticker), sem depender de
-- cron do host. Duas tabelas, no mesmo espírito genérico do resto do schema
-- (ADR-0005: núcleo agnóstico, cada tipo de job interpreta o seu formato):
--
--   1. `job_definition` -- config DINÂMICA de cada tipo de job conhecido: se
--      está habilitado (gate só do agendamento automático -- execução manual
--      sempre funciona), a expressão cron do agendamento, a config opaca
--      namespaced por tipo (`config_json`) e o espelho da última execução
--      (redundância de leitura, para a lista não depender de JOIN).
--   2. `job_execution` -- histórico GENÉRICO, válido para QUALQUER tipo de
--      job (nenhuma tabela por tipo -- mesmo espírito de entity_relation).
--
-- `job_definition.id` é um SLUG ESTÁVEL (a chave de Handler.Nome() em código,
-- ex. "backup_vacuum"), NÃO um uuid -- como app_settings.key: o dado preexiste
-- ao registro do Handler (semeado via migration) e o Go precisa achá-lo por
-- NOME. `job_execution.id` já é uuid (como media_file.id/entity.id), porque é
-- gerado em runtime a cada execução.

CREATE TABLE job_definition (
    id            TEXT PRIMARY KEY,
    nome          TEXT NOT NULL,
    descricao     TEXT,
    -- enabled: só gate do agendamento AUTOMÁTICO; execução manual (POST
    -- .../run) funciona independente deste flag.
    enabled       INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    -- schedule_cron: expressão cron padrão de 5 campos. NULO = job só-manual
    -- (sem agendamento).
    schedule_cron TEXT,
    -- config_json: blob JSON NAMESPACED por tipo de job (ex. {"keep": 7} do
    -- backup) -- mesmo princípio do extension_data (ADR-0005): o núcleo não
    -- conhece o formato, cada Handler interpreta o seu.
    config_json   TEXT,
    -- next_run_at: recalculado a cada mudança de enabled/schedule_cron e ao
    -- fim de cada execução; NULO quando desabilitado ou sem schedule_cron.
    next_run_at   TEXT,
    -- last_run_at/last_status/last_message: espelham a ÚLTIMA linha de
    -- job_execution (redundância de leitura para a lista não precisar de
    -- JOIN); last_status só assume 'success'/'error' quando não-nulo.
    last_run_at   TEXT,
    last_status   TEXT CHECK (last_status IS NULL OR last_status IN ('success', 'error')),
    last_message  TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- job_execution é a aresta genérica: TODO handler grava aqui, igual
-- entity_relation serve TODO par de entities (nunca uma junção por tipo).
-- ON DELETE CASCADE em job_id: apagar a definição do job (caso teórico, sem
-- endpoint nesta fundação) leva o histórico junto.
CREATE TABLE job_execution (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES job_definition(id) ON DELETE CASCADE,
    -- trigger: quem disparou esta execução -- agendador ou o dono ("executar
    -- agora").
    trigger     TEXT NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    status      TEXT NOT NULL CHECK (status IN ('running', 'success', 'error')),
    started_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at TEXT,
    -- message: resultado curto (sucesso) ou erro (falha) -- o QUE o Handler
    -- devolveu, sem stack trace.
    message     TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Histórico por job, mais recente primeiro -- também usado pela poda de
-- retenção (cap de 100 execuções/job, aplicada em Go a cada INSERT).
CREATE INDEX idx_job_execution_job ON job_execution(job_id, started_at DESC);

-- Seed: o job semente `backup_vacuum` (briefing 0052), prova de ponta a ponta
-- do sistema. Desabilitado por padrão -- escrever no disco automaticamente
-- sem consentimento explícito do dono seria surpreendente, especialmente num
-- Pi com cartão SD pequeno. Agendado para 3h da manhã, todo dia; `keep=7`
-- espelha o default do scripts/backup.sh.
INSERT INTO job_definition (id, nome, descricao, enabled, schedule_cron, config_json)
VALUES (
    'backup_vacuum',
    'Backup do banco (VACUUM INTO)',
    'Gera uma cópia consistente do banco via VACUUM INTO e rotaciona mantendo os N backups mais recentes.',
    0,
    '0 3 * * *',
    '{"keep":7}'
);

-- +goose Down
-- Reverte 0011 na ordem inversa (índice e execuções antes da definição, que
-- job_execution referencia).
DROP INDEX IF EXISTS idx_job_execution_job;
DROP TABLE IF EXISTS job_execution;
DROP TABLE IF EXISTS job_definition;
