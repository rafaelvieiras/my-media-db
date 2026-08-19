-- Acesso a job_definition (briefing 0052): fundacao do sistema de jobs
-- (registro dinamico + agendador embutido). Comentarios em ASCII e colunas
-- explicitas -- ver a nota em entity.sql (bug de offset do sqlc v1.31 com
-- acentos/'*' em .sql de query).
--
-- job_execution (historico de execucao) NAO vive mais aqui desde o briefing
-- 0108/ADR-0028 (CAMADA 3): internal/job/manager.go passou a enfileirar via
-- internal/execqueue, cujo queue_item (kind='job') e a fonte unica de
-- historico -- ver internal/db/queries/queue_item.sql. A tabela job_execution
-- em si foi derrubada pela migration 0025.

-- name: ListarJobDefinitions :many
-- Lista todos os jobs conhecidos (sempre tem ao menos o seed backup_vacuum),
-- ordem estavel por created_at.
SELECT id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at
FROM job_definition
ORDER BY created_at, id;

-- name: ObterJobDefinition :one
SELECT id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at
FROM job_definition
WHERE id = ?;

-- name: ListarJobDefinitionsAgendaveis :many
-- Consulta do agendador (Manager.RunScheduler): jobs habilitados, com
-- agendamento configurado, cujo proximo horario ja passou. O Go filtra os que
-- ja estao "running" em memoria antes de disparar (nao ha essa informacao no
-- banco).
SELECT id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at
FROM job_definition
WHERE enabled = 1
  AND schedule_cron IS NOT NULL
  AND next_run_at IS NOT NULL
  AND next_run_at <= ?;

-- name: AtualizarJobConfig :one
-- PATCH .../jobs/{id}: grava enabled/schedule_cron/next_run_at ja MESCLADOS
-- pelo Go (o patch e parcial na API, mas esta query sempre grava as 3 colunas
-- -- o chamador computa o valor final antes de chamar, mesmo padrao de
-- AtualizarLibrarySource).
UPDATE job_definition
SET enabled = ?,
    schedule_cron = ?,
    next_run_at = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at;

-- name: FinalizarJobDefinicao :one
-- Ao fim de uma execucao (Manager.Executar): espelha o resultado em
-- job_definition (redundancia de leitura, evita JOIN na listagem) e recalcula
-- next_run_at (nil quando desabilitado ou sem schedule_cron -- computado em
-- Go via cron.ParseStandard(...).Next(agora)).
UPDATE job_definition
SET last_run_at = ?,
    last_status = ?,
    last_message = ?,
    next_run_at = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, nome, descricao, enabled, schedule_cron, config_json, next_run_at,
    last_run_at, last_status, last_message, created_at, updated_at;
