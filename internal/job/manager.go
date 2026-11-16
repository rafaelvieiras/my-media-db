package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// pollInterval is the ticker interval of the built-in scheduler: the minimum
// cron resolution is 1 minute, so 30s is enough to never lag more than that.
// It is a `var` (not a `const`) so the scheduler test can shorten the interval
// without waiting a real 30s — no other code should reassign it outside tests.
var pollInterval = 30 * time.Second

// manterExecucoes is the retention cap of the history per job: a Go constant,
// not exposed in the API. Every time an execution finishes (a queue_item item,
// kind='job', since ADR-0028), whatever exceeds the `manterExecucoes` most
// recent rows OF THAT job is deleted (podarHistorico).
const manterExecucoes = 100

// carimboFormato is the ISO layout used for next_run_at/last_run_at — the same
// format as scan.go (time.RFC3339 in UTC), fixed-width and lexically sortable
// (the scheduler query compares next_run_at <= now as TEXT).
const carimboFormato = time.RFC3339

// Orchestration errors; sentinels for the HTTP handler to map onto the
// standard error envelope.
var (
	// ErrJobNaoEncontrado: the id does not exist in job_definition. -> 404
	ErrJobNaoEncontrado = errors.New("job não encontrado")
	// ErrCronInvalido: schedule_cron does not parse as a 5-field cron. -> 422
	ErrCronInvalido = errors.New("schedule_cron inválido")
	// ErrJobEmExecucao: there is already an execution in progress for this job. -> 409
	ErrJobEmExecucao = errors.New("já existe uma execução em andamento para este job")
	// ErrJobDesconhecido: the job exists in the database (seed) but there is no
	// Handler registered for it in the current binary (e.g. a downgrade). -> 503
	ErrJobDesconhecido = errors.New("nenhum handler registrado para este job")
)

// Manager orchestrates job execution: it keeps the Registry of Handlers
// (exported — WithJobHandlers registers straight into it) and runs the built-in
// scheduler. Since ADR-0028 (LAYER 3), Manager no longer spawns its own
// goroutine nor keeps execution state in memory (running/job_execution) — that
// lives in the unified queue (internal/execqueue). Executar only enqueues
// (ConfigurarFila); the actual work runs afterwards, when the queue's
// sequential worker calls the Manager back as an execqueue.Runner
// (executarViaRunner) — see ConfigurarFila below. Same spirit as
// internal/scanner.Manager for the "scan" kind.
type Manager struct {
	db       *db.DB
	Registry Registry

	novoID func() string
	agora  func() time.Time

	// fila is the unified execution queue: Executar enqueues into it instead of
	// spawning its own goroutine. nil only on incomplete use of the package
	// (every production path calls ConfigurarFila at boot) -- Executar returns
	// ErrFilaNaoConfigurada in that case.
	fila *execqueue.Manager
}

// ErrFilaNaoConfigurada: Executar called before ConfigurarFila -- every
// production path sets up the queue at boot (cmd/mymediadb/main.go); this
// should only show up in a test/incomplete use of the package.
var ErrFilaNaoConfigurada = errors.New("job: fila de execução não configurada (ConfigurarFila)")

// NewManager creates a Manager ready to use; the Registry starts empty —
// handlers come in via Registry.Registrar (server.WithJobHandlers) before the
// first Executar/RunScheduler.
func NewManager(database *db.DB) *Manager {
	return &Manager{
		db:     database,
		novoID: func() string { return uuid.NewString() },
		agora:  func() time.Time { return time.Now().UTC() },
	}
}

// ConfigurarFila injects the unified execution queue (ADR-0028) and registers
// the execqueue.Runner of the "job" kind -- called by the process boot
// (cmd/mymediadb/main.go). Without it, Executar returns ErrFilaNaoConfigurada.
// Same naming pattern as scanner.Manager.ConfigurarFila (convention
// consistency across the project); the registered Runner is jobRunner{m} (see
// the type, below) -- not *Manager itself -- because of the Executar method
// name clash (see the note on jobRunner).
func (m *Manager) ConfigurarFila(fila *execqueue.Manager) {
	m.fila = fila
	fila.RegistrarRunner(execqueue.KindJob, jobRunner{m})
}

// JobConfigPatch is the PARTIAL patch of PATCH .../jobs/{id}: Enabled nil =
// unchanged; ScheduleCronSet=false = unchanged; ScheduleCronSet=true with
// ScheduleCron nil = CLEARS the schedule (the job becomes manual-only); with a
// non-nil ScheduleCron = the new value (validated as a 5-field cron).
type JobConfigPatch struct {
	Enabled         *bool
	ScheduleCronSet bool
	ScheduleCron    *string
}

// ── Read/config ──────────────────────────────────────────────────────────────

// Listar returns every job_definition (never empty after migration 0011 —
// there is always the backup_vacuum seed), in a stable order.
func (m *Manager) Listar(ctx context.Context) ([]sqlcgen.JobDefinition, error) {
	return m.leitura().ListarJobDefinitions(ctx)
}

// Obter returns a job_definition by id. ErrJobNaoEncontrado when the id does
// not exist.
func (m *Manager) Obter(ctx context.Context, id string) (sqlcgen.JobDefinition, error) {
	def, err := m.leitura().ObterJobDefinition(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlcgen.JobDefinition{}, ErrJobNaoEncontrado
		}
		return sqlcgen.JobDefinition{}, err
	}
	return def, nil
}

// AtualizarConfig applies the partial patch of enabled/schedule_cron,
// recalculates next_run_at (via ProximaExecucao) and persists it — all in ONE
// final row (the sqlc query always writes the 3 columns; the merge happens
// here, in Go). ErrJobNaoEncontrado if the id does not exist; ErrCronInvalido
// if schedule_cron does not parse (the previous state is NOT changed in that
// case).
func (m *Manager) AtualizarConfig(ctx context.Context, id string, patch JobConfigPatch) (sqlcgen.JobDefinition, error) {
	atual, err := m.Obter(ctx, id)
	if err != nil {
		return sqlcgen.JobDefinition{}, err
	}

	enabled := atual.Enabled
	if patch.Enabled != nil {
		enabled = boolParaInt(*patch.Enabled)
	}

	scheduleCron := atual.ScheduleCron
	if patch.ScheduleCronSet {
		if patch.ScheduleCron == nil {
			scheduleCron = sql.NullString{}
		} else {
			scheduleCron = sql.NullString{String: *patch.ScheduleCron, Valid: true}
		}
	}
	if scheduleCron.Valid {
		if err := ValidarCron(scheduleCron.String); err != nil {
			return sqlcgen.JobDefinition{}, ErrCronInvalido
		}
	}

	nextRunAt := formatarProxima(ProximaExecucao(enabled == 1, scheduleCron.String, m.agora()))

	return m.escrita().AtualizarJobConfig(ctx, sqlcgen.AtualizarJobConfigParams{
		Enabled:      enabled,
		ScheduleCron: scheduleCron,
		NextRunAt:    nextRunAt,
		ID:           id,
	})
}

// Historico returns the page of queue_item (kind='job') for job `id`, most
// recent first by enqueued_at. `before` nil = first page; non-nil = page
// backwards (enqueued_at strictly smaller than the cursor).
// ErrJobNaoEncontrado if the id does not exist.
//
// Since ADR-0028 (LAYER 3): the history lives in queue_item, no longer in
// job_execution. The ListarQueueItemsPorKindRef query (layer 1) has no NATIVE
// cursor support (`WHERE enqueued_at < ?`) the way ListarJobExecucoes did via
// sqlc.narg('before') -- that is why the cursor filter is applied HERE, in Go,
// over the full listing (LIMIT -1, no cap): the retention cap (podarHistorico,
// manterExecucoes=100) guarantees this never iterates more than 100 rows per
// job, so it is not a performance hot path.
func (m *Manager) Historico(ctx context.Context, id string, limit int, before *string) ([]sqlcgen.QueueItem, error) {
	if _, err := m.Obter(ctx, id); err != nil {
		return nil, err
	}
	itens, err := m.leitura().ListarQueueItemsPorKindRef(ctx, sqlcgen.ListarQueueItemsPorKindRefParams{
		Kind:  execqueue.KindJob,
		RefID: id,
		Limit: -1,
	})
	if err != nil {
		return nil, err
	}
	out := make([]sqlcgen.QueueItem, 0, limit)
	for _, item := range itens {
		if before != nil && item.EnqueuedAt >= *before {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Rodando returns, for each job_id with an ACTIVE item (queued OR running) in
// the unified queue, running=true — used by GET /jobs to compose the `running`
// field of each item. Since ADR-0028 (LAYER 3), the source of truth for
// "running" is execqueue, no longer an in-memory map of the Manager itself.
//
// Decision: for EACH job_definition it queries execqueue.Manager.Status (layer
// 1, already public) instead of trying a "running only" query -- the list of
// jobs is never large (4 seeded jobs today), so N small calls are not worth the
// extra SQL of a dedicated query (ListarQueueItemsRunningPorKind). It treats
// queued+running as "running" (not ONLY actually running) -- the SAME reading
// as scanner.Manager.Status (see the comment there: "Running=true both for
// 'queued' and 'running'"), which is necessary for the polling pattern used in
// the tests/handlers (aguardarJobEstado/aguardarRodando): a freshly enqueued
// item (still behind another one in the global concurrency-1 queue) cannot look
// like it "already finished" just because the worker has not picked it up yet.
func (m *Manager) Rodando() map[string]bool {
	out := make(map[string]bool)
	if m.fila == nil {
		return out
	}
	ctx := context.Background()
	defs, err := m.leitura().ListarJobDefinitions(ctx)
	if err != nil {
		log.Printf("job: Rodando: erro ao listar job_definition: %v", err)
		return out
	}
	for _, def := range defs {
		st, err := m.fila.Status(ctx, execqueue.KindJob, def.ID)
		if err != nil {
			log.Printf("job: Rodando: erro ao consultar status de %q: %v", def.ID, err)
			continue
		}
		if st.Existe && (st.Status == execqueue.StatusQueued || st.Status == execqueue.StatusRunning) {
			out[def.ID] = true
		}
	}
	return out
}

// ── Execution ────────────────────────────────────────────────────────────────

// Executar validates SYNCHRONOUSLY (does the job exist? is a Handler
// registered?) and then ENQUEUES the execution of job `jobID` in the unified
// queue (ADR-0028) — it no longer runs anything in its own goroutine (that
// became the responsibility of the internal/execqueue worker, which calls this
// Manager back via executarViaRunner, the method that satisfies
// execqueue.Runner). trigger is "manual" (POST .../run) or "schedule" (the
// scheduler).
//
// ErrJobNaoEncontrado if the id does not exist in job_definition;
// ErrJobDesconhecido if there is no registered Handler (job seeded, binary
// without the handler); ErrJobEmExecucao if there is already a queued/running
// item for this job in the queue (execqueue.ErrJaEnfileirado mapped to the same
// public sentinel error as always — the HTTP handler, respondJobError, already
// treats that as 409, no change); ErrFilaNaoConfigurada if ConfigurarFila was
// never called.
func (m *Manager) Executar(ctx context.Context, jobID, trigger string) (string, error) {
	if _, err := m.Obter(ctx, jobID); err != nil {
		return "", err
	}
	if _, ok := m.Registry.Get(jobID); !ok {
		return "", ErrJobDesconhecido
	}
	if m.fila == nil {
		return "", ErrFilaNaoConfigurada
	}

	id, err := m.fila.Enqueue(ctx, execqueue.KindJob, jobID, trigger)
	if err != nil {
		if errors.Is(err, execqueue.ErrJaEnfileirado) {
			return "", ErrJobEmExecucao
		}
		return "", err
	}
	return id, nil
}

// executarViaRunner implements execqueue.Runner: it is called by the queue's
// sequential worker (well after the original Enqueue of Executar), with only
// the refID (job_definition.id) and the trigger at hand. job.Handler.Executar
// has no notion of progress — progress is never reported here (it stays 0/0,
// acceptable: job_execution never had progress before this layer either). A
// panic inside the Handler is recovered by execqueue ITSELF
// (executarComRecover) -- we do not duplicate recover here (same spirit as
// scanner.Manager.Executar).
//
// Before returning to execqueue, it does what is SPECIFIC TO THE job DOMAIN
// (not the responsibility of the generic execqueue): mirrors the result into
// job_definition (atualizarDefinicaoAoFim, recalculates next_run_at) and prunes
// the history (podarHistorico) — ALWAYS, on success and on error. execqueue
// already takes care of IniciarQueueItem/FinalizarQueueItem around this call;
// this method only handles what is its own.
func (m *Manager) executarViaRunner(ctx context.Context, refID, _ string, _ execqueue.ProgressoFunc) (string, error) {
	def, err := m.leitura().ObterJobDefinition(ctx, refID)
	if err != nil {
		err = fmt.Errorf("erro ao carregar a configuração do job: %w", err)
		m.atualizarDefinicaoAoFim(ctx, refID, "error", err.Error())
		m.podarHistorico(ctx, refID)
		return "", err
	}
	var cfg json.RawMessage
	if def.ConfigJson.Valid {
		cfg = json.RawMessage(def.ConfigJson.String)
	}

	handler, ok := m.Registry.Get(refID)
	if !ok {
		// Rare case: the Handler disappeared between the Enqueue and the worker
		// getting to process the item (binary downgrade along the way).
		err := fmt.Errorf("nenhum handler registrado para o job %q", refID)
		m.atualizarDefinicaoAoFim(ctx, refID, "error", err.Error())
		m.podarHistorico(ctx, refID)
		return "", err
	}

	mensagem, execErr := handler.Executar(ctx, cfg)

	status := "success"
	msg := mensagem
	if execErr != nil {
		status = "error"
		msg = execErr.Error()
	}
	m.atualizarDefinicaoAoFim(ctx, refID, status, msg)
	m.podarHistorico(ctx, refID)

	return mensagem, execErr
}

// jobRunner is a NARROW adapter (it exposes no state of its own, it only wraps
// *Manager) to satisfy execqueue.Runner: the interface demands a method
// literally named `Executar(ctx, refID, trigger string, progresso ProgressoFunc)
// (string, error)` -- *Manager* already exposes a public `Executar` with
// ANOTHER signature (the synchronous enqueueing API above,
// `Executar(ctx, jobID, trigger string) (string, error)`), and Go does not allow
// two methods with the same name on the same type (it is not overloading by
// signature). scanner.Manager did not need this adapter because its synchronous
// API is called Start, not Executar -- job.Manager was born with that name
// (before this layer). RegistrarRunner receives a jobRunner (see
// ConfigurarFila), which only forwards to executarViaRunner.
type jobRunner struct{ m *Manager }

func (r jobRunner) Executar(ctx context.Context, refID, trigger string, progresso execqueue.ProgressoFunc) (string, error) {
	return r.m.executarViaRunner(ctx, refID, trigger, progresso)
}

// atualizarDefinicaoAoFim mirrors the result into job_definition and
// recalculates next_run_at — ALWAYS (regardless of the trigger): cron.Next(now)
// points to the next FUTURE time, so a manual execution never "skips" the next
// scheduled one.
func (m *Manager) atualizarDefinicaoAoFim(ctx context.Context, jobID, status, msg string) {
	def, err := m.leitura().ObterJobDefinition(ctx, jobID)
	if err != nil {
		log.Printf("job: erro ao carregar job_definition %q para atualizar ao fim: %v", jobID, err)
		return
	}
	agora := m.agora()
	nextRunAt := formatarProxima(ProximaExecucao(def.Enabled == 1, def.ScheduleCron.String, agora))

	if _, err := m.escrita().FinalizarJobDefinicao(ctx, sqlcgen.FinalizarJobDefinicaoParams{
		LastRunAt:   sql.NullString{String: agora.Format(carimboFormato), Valid: true},
		LastStatus:  sql.NullString{String: status, Valid: true},
		LastMessage: sql.NullString{String: msg, Valid: msg != ""},
		NextRunAt:   nextRunAt,
		ID:          jobID,
	}); err != nil {
		log.Printf("job: erro ao atualizar job_definition %q ao fim da execução: %v", jobID, err)
	}
}

// podarHistorico deletes the excess of queue_item (kind='job', ref_id=jobID)
// beyond manterExecucoes for the job — best-effort, called after every
// execution finishes. Since ADR-0028 (LAYER 3) it no longer operates on
// job_execution (ListarJobExecucaoIdsExcedentes/ApagarJobExecucao, job.sql); it
// now uses the internal/execqueue equivalent
// (ListarQueueItemsExcedentesPorRef/ApagarQueueItem, queue_item.sql, layer 1) --
// the same two-simple-queries pattern (an sqlc limitation for pruning in a
// single DELETE...NOT IN query, documented in queue_item.sql).
func (m *Manager) podarHistorico(ctx context.Context, jobID string) {
	ids, err := m.leitura().ListarQueueItemsExcedentesPorRef(ctx, sqlcgen.ListarQueueItemsExcedentesPorRefParams{
		Kind: execqueue.KindJob, RefID: jobID, Offset: manterExecucoes,
	})
	if err != nil {
		log.Printf("job: poda de histórico: erro ao listar excedentes de %q: %v", jobID, err)
		return
	}
	w := m.escrita()
	for _, id := range ids {
		if err := w.ApagarQueueItem(ctx, id); err != nil {
			log.Printf("job: poda de histórico: erro ao apagar execução %q: %v", id, err)
		}
	}
}

// ── Built-in scheduler ───────────────────────────────────────────────────────

// RunScheduler runs the loop of the built-in scheduler: every pollInterval it
// triggers (trigger="schedule") the enabled jobs whose next_run_at has already
// passed — best-effort (an error on one does not prevent the others;
// ErrJobEmExecucao is expected and silent). It exits when ctx is canceled — the
// SAME ctx of main's signal.NotifyContext, so it stops cleanly along with the
// rest on SIGINT/SIGTERM.
func (m *Manager) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

// tick queries the due schedulable jobs and triggers each of them; called on
// every tick of the RunScheduler ticker (extracted so the test can call it
// directly, without waiting for the ticker).
func (m *Manager) tick(ctx context.Context) {
	now := m.agora().Format(carimboFormato)
	defs, err := m.leitura().ListarJobDefinitionsAgendaveis(ctx, sql.NullString{String: now, Valid: true})
	if err != nil {
		log.Printf("job: agendador: erro ao listar jobs agendáveis: %v", err)
		return
	}
	for _, def := range defs {
		if _, err := m.Executar(ctx, def.ID, "schedule"); err != nil && !errors.Is(err, ErrJobEmExecucao) {
			log.Printf("job: agendador: erro ao disparar %q: %v", def.ID, err)
		}
	}
}

// ── Cron: validation and next-time calculation ───────────────────────────────

// ValidarCron validates a 5-field cron expression; empty is valid (= no
// schedule, manual-only job). Used by AtualizarConfig to reject (422) a
// malformed expression BEFORE writing it.
func ValidarCron(expr string) error {
	if expr == "" {
		return nil
	}
	_, err := cron.ParseStandard(expr)
	return err
}

// ProximaExecucao calculates the next time (UTC) from `agora` when the job is
// enabled AND has a non-empty schedule_cron; nil in the other cases
// (manual-only job, disabled, or invalid cron — call ValidarCron first if the
// origin is not trustworthy). The only parser used is cron.ParseStandard(...)
// .Next(t) — the triggering loop is always our RunScheduler, never the
// scheduler built into the robfig/cron lib.
func ProximaExecucao(enabled bool, scheduleCron string, agora time.Time) *time.Time {
	if !enabled || scheduleCron == "" {
		return nil
	}
	sched, err := cron.ParseStandard(scheduleCron)
	if err != nil {
		return nil
	}
	next := sched.Next(agora)
	return &next
}

// formatarProxima converts the *time.Time from ProximaExecucao into the
// sql.NullString writable to next_run_at (NULL when nil).
func formatarProxima(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(carimboFormato), Valid: true}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (m *Manager) leitura() *sqlcgen.Queries { return sqlcgen.New(m.db.Read) }
func (m *Manager) escrita() *sqlcgen.Queries { return sqlcgen.New(m.db.Write) }

func boolParaInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
