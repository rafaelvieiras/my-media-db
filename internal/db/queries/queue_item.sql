-- Acesso a queue_item (briefing 0108, ADR-0028): fila de execucao unificada
-- para scan e job. Comentarios em ASCII e colunas explicitas -- ver a nota em
-- entity.sql (bug de offset do sqlc v1.31 com acentos/'*' em .sql de query).

-- name: CriarQueueItem :one
-- Enfileira um item novo em status 'queued' (Manager.Enqueue, DEPOIS do guard
-- de dedup ja ter confirmado que nao ha item ativo para a mesma referencia).
INSERT INTO queue_item (
    id, kind, ref_id, trigger
) VALUES (
    ?, ?, ?, ?
)
RETURNING id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at;

-- name: ObterQueueItemAtivo :one
-- Guard de dedup (Manager.Enqueue): existe algum item queued/running para
-- esta MESMA referencia (kind, ref_id)? sql.ErrNoRows = nao existe, pode
-- enfileirar. O mais recente primeiro (ORDER BY enqueued_at DESC) nao importa
-- de verdade aqui (so pode existir UM ativo por referencia, ja que o proprio
-- guard impede um segundo), mas mantem a query deterministica.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE kind = ? AND ref_id = ? AND status IN ('queued', 'running')
ORDER BY enqueued_at DESC
LIMIT 1;

-- name: ObterQueueItemPorId :one
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE id = ?;

-- name: ObterQueueItemAtivoPorKindRef :one
-- Usado por Manager.Status(kind, refID): o item ATIVO (queued/running) mais
-- recente da referencia, se houver -- Status cai para
-- ObterUltimoQueueItemPorKindRef quando esta query devolve sql.ErrNoRows (sem
-- item ativo, mostra o ultimo resultado finalizado).
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE kind = ? AND ref_id = ? AND status IN ('queued', 'running')
ORDER BY enqueued_at DESC
LIMIT 1;

-- name: ObterUltimoQueueItemPorKindRef :one
-- Complemento de ObterQueueItemAtivoPorKindRef: o item mais recente (QUALQUER
-- status) de uma referencia -- usado por Manager.Status quando nao ha item
-- ativo, para devolver o resultado da ULTIMA execucao (sql.ErrNoRows = nunca
-- houve item nenhum para esta referencia).
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE kind = ? AND ref_id = ?
ORDER BY enqueued_at DESC
LIMIT 1;

-- name: ProximoQueueItemPendente :one
-- O worker usa isso para achar o proximo item 'queued' mais antigo (FIFO por
-- enqueued_at; id como desempate estavel quando enqueued_at colide).
-- sql.ErrNoRows = fila vazia, o worker dorme um intervalo curto.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE status = 'queued'
ORDER BY enqueued_at, id
LIMIT 1;

-- name: IniciarQueueItem :one
-- Transicao queued -> running, no INICIO do processamento pelo worker.
UPDATE queue_item
SET status = 'running',
    started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at;

-- name: AtualizarProgressoQueueItem :exec
-- Grava progress_current/progress_total/message a cada report do callback de
-- progresso durante Runner.Executar (ADR-0028: "atualiza progress_current/
-- message via callback durante a execucao -- nao so no fim"). Sem throttle:
-- grava a cada chamada, decisao documentada em
-- internal/execqueue/manager.go (simples e correta; otimizar so se virar
-- problema real de I/O).
UPDATE queue_item
SET progress_current = ?,
    progress_total = ?,
    message = ?
WHERE id = ?;

-- name: FinalizarQueueItem :one
-- Transicao running -> success|error, ao FIM do processamento (ou quando nao
-- ha Runner registrado para o kind).
UPDATE queue_item
SET status = ?,
    finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    message = ?
WHERE id = ?
RETURNING id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at;

-- name: ListarQueueItemsPorKindRef :many
-- Historico paginado de UMA referencia (kind, ref_id), mais recente primeiro
-- por enqueued_at -- substitui a antiga ListarJobExecucoes (job.sql) numa
-- camada futura; as colunas ja cobrem o shape que GET /jobs/{id}/executions e
-- GET /library/{id}/scan precisam.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE kind = ? AND ref_id = ?
ORDER BY enqueued_at DESC
LIMIT ?;

-- name: ListarQueueItemsExcedentesPorRef :many
-- Retencao do historico (mesmo padrao de ListarJobExecucaoIdsExcedentes em
-- job.sql): ids do excedente mais antigo alem das N mais recentes DAQUELA
-- (kind, ref_id) -- poda aplicada em Go, um DELETE por id.
SELECT id FROM queue_item
WHERE kind = ? AND ref_id = ?
ORDER BY enqueued_at DESC
LIMIT -1 OFFSET ?;

-- name: ApagarQueueItem :exec
DELETE FROM queue_item WHERE id = ?;

-- name: ListarQueueItemsRunning :many
-- Usado no BOOT do worker (Manager.Run, antes do loop comecar): qualquer item
-- 'running' encontrado aqui e orfao de uma execucao anterior que nao
-- terminou (o processo caiu no meio) -- o Manager finaliza cada um como
-- 'error' antes de comecar a processar a fila normalmente.
SELECT id, kind, ref_id, trigger, status, progress_current, progress_total,
    message, enqueued_at, started_at, finished_at
FROM queue_item
WHERE status = 'running';
