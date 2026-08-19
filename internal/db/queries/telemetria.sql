-- Leituras agregadas para GET /api/v1/health/system (briefing 0055): tamanho
-- do banco via PRAGMA (fora deste arquivo, roda direto pelo Go -- ver
-- internal/telemetry/telemetry.go) + contagens de uso + soma de disco por
-- fonte. Nenhuma tabela nova. Comentarios em ASCII (sem acento) -- mesmo
-- cuidado de entity.sql/job.sql contra o bug de corrupcao do sqlc v1.31 com
-- .sql de query acentuado.

-- name: ContarEntitiesPorTipo :many
SELECT type, COUNT(*) AS total
FROM entity
GROUP BY type;

-- name: ContarMediaFilesPorState :many
SELECT state, COUNT(*) AS total
FROM media_file
GROUP BY state;

-- name: ContarLibrarySources :one
SELECT COUNT(*) AS total FROM library_source;

-- name: ContarJobExecutions :one
-- Desde o briefing 0108/ADR-0028 (CAMADA 3): job_execution foi derrubada
-- (migration 0025); o historico de execucao de job vive em queue_item
-- (kind='job') -- ver internal/db/queries/queue_item.sql.
SELECT COUNT(*) AS total FROM queue_item WHERE kind = 'job';

-- name: SomarTamanhoMediaFilePorFonte :one
SELECT CAST(COALESCE(SUM(size), 0) AS INTEGER) AS total_bytes
FROM media_file
WHERE library_source_id = ? AND state = 'present';
