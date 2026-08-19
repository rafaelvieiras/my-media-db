-- +goose Up
-- Migration 0024 (briefing 0108, ADR-0028): fila de execucao UNIFICADA para
-- scan e jobs -- substitui os dois sistemas paralelos que reimplementavam o
-- mesmo padrao ("goroutine solta + mapa em memoria + mutex") de forma
-- independente e redundante.
--
-- `queue_item` e a tabela generica: `kind` distingue 'scan' (datasource,
-- ref_id = library_source.id) de 'job' (ref_id = job_definition.id) -- mesmo
-- espirito de entity_relation (uma tabela, kind livre, nunca junção por-tipo,
-- AGENTS.md item 3). O worker global (internal/execqueue) e sequencial
-- (concorrência 1, decisão da ADR-0028): no maximo um item 'running' por vez,
-- processado em FIFO por enqueued_at.
--
-- job_definition NAO muda nesta migration (fica exatamente como esta -- e uma
-- camada futura que liga o worker a ela).
--
-- NOTA DE FASEAMENTO (diverge do texto literal da ADR-0028/briefing 0108, que
-- descreve o DROP TABLE job_execution NESTA MESMA migration): esta e a
-- CAMADA 1 (so fundacao -- tabela/queries/pacote execqueue) de uma
-- implementacao em camadas; internal/job/manager.go AINDA le/escreve
-- job_execution diretamente (CriarJobExecucao/FinalizarJobExecucao/etc, ver
-- internal/db/queries/job.sql) e so passa a enfileirar via execqueue numa
-- camada SEGUINTE. Derrubar job_execution agora quebraria internal/job (e
-- internal/telemetry, que conta linhas via ContarJobExecutions) sem que
-- exista ainda quem os reescreva para usar queue_item -- por isso
-- job_execution e seu indice PERMANECEM aqui; a camada que reescrever
-- internal/job/manager.go para chamar execqueue.Enqueue deve entao dropar
-- job_execution (e idx_job_execution_job) numa migration propria de
-- encerramento, completando a decisao da ADR-0028. Os dados de job_execution
-- SAO copiados para queue_item (kind='job') aqui mesmo assim, para o
-- historico ja aparecer unificado em queue_item desde ja.
CREATE TABLE queue_item (
    id               TEXT PRIMARY KEY,
    -- kind: 'scan' (ref_id = library_source.id) ou 'job' (ref_id =
    -- job_definition.id) -- o mesmo shape de fila serve os dois mundos.
    kind             TEXT NOT NULL CHECK (kind IN ('scan', 'job')),
    ref_id           TEXT NOT NULL,
    -- trigger: quem disparou -- dono clicando (manual), agendador embutido
    -- (schedule) ou o watcher de filesystem (watch, só para scan).
    trigger          TEXT NOT NULL CHECK (trigger IN ('manual', 'schedule', 'watch')),
    -- status: ciclo de vida linear queued -> running -> success|error. Nunca
    -- volta para queued (reenfileirar é um item NOVO).
    status           TEXT NOT NULL CHECK (status IN ('queued', 'running', 'success', 'error')) DEFAULT 'queued',
    -- progress_current/progress_total: granularidade por ARQUIVO processado
    -- (scan) ou o que o Handler reportar (job); 0/0 = sem progresso conhecido
    -- ainda. Mantido em memoria pelo Manager durante a execução e persistido
    -- a cada report (ver internal/execqueue/manager.go) -- não é hot-path de
    -- I/O critico o bastante para justificar throttle nesta fundação.
    progress_current INTEGER NOT NULL DEFAULT 0,
    progress_total   INTEGER NOT NULL DEFAULT 0,
    -- message: resultado curto (sucesso) ou erro (falha) ao finalizar --
    -- mesmo papel de job_execution.message.
    message          TEXT,
    -- enqueued_at: quando o item ENTROU na fila (nao quando comecou a rodar
    -- -- essa e a diferenca semantica frente ao antigo started_at de
    -- job_execution, que so existia apos o disparo sincrono). E o campo que
    -- ordena o FIFO do worker (idx_queue_item_status_enqueued).
    enqueued_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at       TEXT,
    finished_at      TEXT
);

-- idx_queue_item_kind_ref: usado pelo guard de dedup (Enqueue) para achar
-- rapido se ja existe um item queued/running para a MESMA referencia
-- (kind, ref_id) antes de inserir um novo.
CREATE INDEX idx_queue_item_kind_ref ON queue_item(kind, ref_id);

-- idx_queue_item_status_enqueued: usado pelo worker para achar o proximo item
-- 'queued' mais antigo (ORDER BY enqueued_at, id LIMIT 1).
CREATE INDEX idx_queue_item_status_enqueued ON queue_item(status, enqueued_at);

-- Copia o historico existente de job_execution -> queue_item (kind='job'),
-- so LEITURA (job_execution nao e alterada nem apagada -- ver nota de
-- faseamento acima). started_at original vira enqueued_at (nao havia
-- enfileiramento antes desta migration -- todo item antigo ja "nasceu"
-- running, entao enqueued_at = started_at e a leitura mais honesta do dado
-- que ja existe).
INSERT INTO queue_item (
    id, kind, ref_id, trigger, status,
    progress_current, progress_total, message,
    enqueued_at, started_at, finished_at
)
SELECT
    id, 'job', job_id, trigger, status,
    0, 0, message,
    started_at, started_at, finished_at
FROM job_execution;

-- +goose Down
-- job_execution nao foi tocada no Up (ver nota de faseamento) -- reverter e
-- so remover queue_item e seus indices.
DROP INDEX IF EXISTS idx_queue_item_status_enqueued;
DROP INDEX IF EXISTS idx_queue_item_kind_ref;
DROP TABLE queue_item;
