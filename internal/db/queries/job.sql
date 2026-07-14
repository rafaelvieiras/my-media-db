-- Acesso a job_definition/job_execution (briefing 0052): fundacao do sistema
-- de jobs (registro dinamico + agendador embutido). Comentarios em ASCII e
-- colunas explicitas -- ver a nota em entity.sql (bug de offset do sqlc v1.31
-- com acentos/'*' em .sql de query).

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

-- name: CriarJobExecucao :one
-- Cria a linha de execucao em status 'running', no INICIO do disparo
-- (Manager.Executar, antes de chamar o Handler).
INSERT INTO job_execution (
    id, job_id, trigger, status
) VALUES (
    ?, ?, ?, 'running'
)
RETURNING id, job_id, trigger, status, started_at, finished_at, message, created_at;

-- name: FinalizarJobExecucao :one
-- Fecha a linha de execucao ao fim (sucesso ou erro), com a mensagem curta
-- devolvida pelo Handler (ou o erro, sem stack trace).
UPDATE job_execution
SET status = ?,
    finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    message = ?
WHERE id = ?
RETURNING id, job_id, trigger, status, started_at, finished_at, message, created_at;

-- name: ListarJobExecucoes :many
-- Historico paginado de UM job, mais recente primeiro. sqlc.narg('before') e
-- opcional (NULL = sem cursor, primeira pagina); quando presente, pagina para
-- tras (started_at estritamente menor que o cursor).
SELECT id, job_id, trigger, status, started_at, finished_at, message, created_at
FROM job_execution
WHERE job_id = sqlc.arg('job_id')
  AND (sqlc.narg('before') IS NULL OR started_at < sqlc.narg('before'))
ORDER BY started_at DESC
LIMIT sqlc.arg('limit');

-- name: ListarJobExecucaoIdsExcedentes :many
-- Retencao do historico (briefing 0052): ids do excedente mais antigo alem das
-- N mais recentes DAQUELE job_id ("manter", constante Go, nao exposta na API).
-- O parser SQL do sqlc (engine sqlite) acusa "column reference job_id
-- ambiguous" para um DELETE ... NOT IN (SELECT ... mesma tabela) sem alias --
-- mesma familia da limitacao documentada em duplicata.sql (self-reference na
-- tabela alvo). Por isso a poda e DUAS queries simples: esta lista os ids
-- excedentes (OFFSET "manter", mesmo criterio de ORDER BY do historico), e
-- ApagarJobExecucao (abaixo) apaga cada um em Go -- mesmo espirito de
-- duplicata.go (iterar ids em vez de SQL em massa).
SELECT id FROM job_execution
WHERE job_id = ?
ORDER BY started_at DESC
LIMIT -1 OFFSET ?;

-- name: ApagarJobExecucao :exec
DELETE FROM job_execution WHERE id = ?;
