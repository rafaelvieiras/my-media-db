// Package execqueue is the UNIFIED execution queue for scans and jobs
// (ADR-0028): it replaces the two parallel systems that kept reimplementing
// the same pattern ("loose goroutine + in-memory map + mutex") independently
// and redundantly. A single persisted table (`queue_item`) and a single
// global SEQUENTIAL worker (concurrency 1, a deliberate decision of ADR-0028
// — it matches "local-first and lightweight", AGENTS.md item 4, and the fact
// that SQLite already has a single writer): at most one `running` item at a
// time, processed FIFO by `enqueued_at`, never idle while there is a
// `queued` item.
//
// Enqueuing (Enqueue) does not execute — it only writes the `queued` row and
// returns. What knows how to RUN a `kind` of item is a Runner, registered
// via RegistrarRunner BEFORE Run starts (the foundation of this layer does
// not implement any real Runner — that is the responsibility of a layer
// further up, which wires `internal/scanner` and `internal/job` in here).
package execqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Known kinds of queue_item.kind — the only values accepted by the CHECK in
// migration 0024. 'scan' references a library_source.id; 'job' references a
// job_definition.id.
const (
	KindScan = "scan"
	KindJob  = "job"
)

// Known triggers of queue_item.trigger — the only values accepted by the
// CHECK in migration 0024. TriggerWatch is exclusive to kind=scan (the
// filesystem watcher never triggers a job).
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
	TriggerWatch    = "watch"
)

// Possible statuses of queue_item.status — a linear lifecycle queued ->
// running -> success|error. It never goes back to queued (re-enqueuing is
// always a NEW item).
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusError   = "error"
)

// pollInterval is the interval the worker sleeps when the queue is empty
// (ProximoQueueItemPendente returned sql.ErrNoRows) before trying again.
// Short enough not to leave the queue idle for long when new work arrives,
// without being aggressive polling. It is a `var` (not a `const`) so a test
// can shorten it without waiting for the production value — same pattern as
// internal/job.pollInterval; no code outside tests should reassign it.
var pollInterval = 500 * time.Millisecond

// mensagemProcessoReiniciado is the message written to queue_item.message
// when the worker's boot finds an orphan 'running' item (the process died in
// the middle of a previous execution — see recuperarOrfaos).
const mensagemProcessoReiniciado = "processo reiniciado durante a execução"

// Orchestration errors; sentinels for a later layer to map onto the standard
// HTTP error envelope (same spirit as scanner.ErrScanEmAndamento /
// job.ErrJobEmExecucao).
var (
	// ErrKindInvalido: kind outside {scan, job}.
	ErrKindInvalido = errors.New("execqueue: kind inválido")
	// ErrTriggerInvalido: trigger outside {manual, schedule, watch}.
	ErrTriggerInvalido = errors.New("execqueue: trigger inválido")
	// ErrRefIDVazio: empty refID (Enqueue needs to know WHICH reference the
	// item belongs to).
	ErrRefIDVazio = errors.New("execqueue: refID vazio")
	// ErrJaEnfileirado: there is already a queued/running item for the SAME
	// reference (kind, ref_id) — dedup by reference, not by the whole queue
	// (ADR-0028). -> 409 in a later layer.
	ErrJaEnfileirado = errors.New("execqueue: já existe um item enfileirado (ou em execução) para esta referência")
)

// Runner is the contract that ANY kind of item implements to be executed by
// the worker — the one that registers a Runner for "scan" and "job" is a
// LATER layer (this foundation only defines the interface and the registry).
// Executar must be blocking (the worker is sequential by design: it only
// calls the next item after this one returns) and must honor ctx.Done() so it
// can be canceled on process shutdown.
//
// registro is where the Runner reports what it is doing (ADR-0040): the
// snapshot (counters, step, current item) and the per-item trail. It is never
// nil — when there is no queue behind the Runner, the worker passes a no-op
// implementation, so a Runner can always call it unconditionally.
type Runner interface {
	Executar(ctx context.Context, refID string, trigger string, registro Registro) (mensagemFinal string, err error)
}

// Manager keeps the registered Runners and orchestrates the sequential
// worker. Same database access pattern as
// internal/job.Manager/internal/scanner.Manager: reads go through the read
// pool (db.Read), writes always through the write pool — the single writer of
// ADR-0010.
type Manager struct {
	db     *db.DB
	novoID func() string
	agora  func() time.Time

	// enqueueMu serializes the guard-then-insert of Enqueue WITHIN the process:
	// the dedup check (ObterQueueItemAtivo) and the INSERT do not form a
	// database transaction, so without this mutex two concurrent calls to
	// Enqueue for the SAME reference could interleave between their checks and
	// inserts and create two active items. The single writer of the process
	// (ADR-0010) plus this mutex make the guard effectively atomic.
	enqueueMu sync.Mutex

	runnersMu sync.Mutex
	runners   map[string]Runner
}

// NewManager creates a Manager ready to use; no Runner registered yet
// (RegistrarRunner before calling Run).
func NewManager(database *db.DB) *Manager {
	return &Manager{
		db:      database,
		novoID:  func() string { return uuid.NewString() },
		agora:   func() time.Time { return time.Now().UTC() },
		runners: make(map[string]Runner),
	}
}

// RegistrarRunner associates a Runner with a kind ("scan" or "job",
// typically the KindScan/KindJob constants). Calling it again with the same
// kind replaces the previous Runner (predictable behavior, same spirit as
// job.Registry.Registrar). It must be called BEFORE Run — the worker resolves
// an item's Runner at the moment it processes it, so registering after boot
// only applies to items not yet processed.
func (m *Manager) RegistrarRunner(kind string, r Runner) {
	m.runnersMu.Lock()
	defer m.runnersMu.Unlock()
	if m.runners == nil {
		m.runners = make(map[string]Runner)
	}
	m.runners[kind] = r
}

func (m *Manager) runner(kind string) (Runner, bool) {
	m.runnersMu.Lock()
	defer m.runnersMu.Unlock()
	r, ok := m.runners[kind]
	return r, ok
}

// ── Enqueuing ────────────────────────────────────────────────────────────────

// Enqueue writes a new item in status 'queued' and returns the id. It does
// not execute anything — the queue is consumed by the worker (Run), running
// in the background. ErrKindInvalido/ErrTriggerInvalido/ErrRefIDVazio on
// invalid input; ErrJaEnfileirado if there is already a queued/running item
// for the SAME reference (kind, ref_id) — dedup by reference, not by the whole
// queue.
//
// Whoever calls Enqueue still NEEDS to have registered (via RegistrarRunner) a
// Runner for that `kind` before the worker gets to process this item —
// otherwise the item is automatically finalized as 'error' (see
// processarProximo), and the worker does not hang.
func (m *Manager) Enqueue(ctx context.Context, kind, refID, trigger string) (string, error) {
	if err := validarKind(kind); err != nil {
		return "", err
	}
	if err := validarTrigger(trigger); err != nil {
		return "", err
	}
	if refID == "" {
		return "", ErrRefIDVazio
	}

	m.enqueueMu.Lock()
	defer m.enqueueMu.Unlock()

	_, err := m.leitura().ObterQueueItemAtivo(ctx, sqlcgen.ObterQueueItemAtivoParams{Kind: kind, RefID: refID})
	if err == nil {
		return "", ErrJaEnfileirado
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("execqueue: checar dedup: %w", err)
	}

	id := m.novoID()
	if _, err := m.escrita().CriarQueueItem(ctx, sqlcgen.CriarQueueItemParams{
		ID: id, Kind: kind, RefID: refID, Trigger: trigger,
	}); err != nil {
		return "", fmt.Errorf("execqueue: criar item: %w", err)
	}
	return id, nil
}

func validarKind(kind string) error {
	switch kind {
	case KindScan, KindJob:
		return nil
	default:
		return ErrKindInvalido
	}
}

func validarTrigger(trigger string) error {
	switch trigger {
	case TriggerManual, TriggerSchedule, TriggerWatch:
		return nil
	default:
		return ErrTriggerInvalido
	}
}

// ── Lookup ───────────────────────────────────────────────────────────────────

// QueueItemStatus is the queryable snapshot of the most recent state of ONE
// reference (kind, ref_id) — the ACTIVE item (queued/running) when there is
// one, otherwise the last finished one (success/error). Meant to feed both
// GET /library/{id}/scan and GET /jobs/{id}/executions without breaking the
// current JSON contract of those endpoints (a later layer). The timestamp
// fields come out as the text already stored in the database (the same ISO
// format the rest of the server already forwards straight to JSON, with no
// re-parse) — empty when NULL (e.g. an empty StartedAt while the item is
// still 'queued').
type QueueItemStatus struct {
	// Existe is false when there has never been a queue item for this
	// reference.
	Existe bool

	ID      string
	Kind    string
	RefID   string
	Trigger string
	// Status: queued | running | success | error.
	Status          string
	ProgressCurrent int
	ProgressTotal   int
	Message         string
	EnqueuedAt      string
	StartedAt       string
	FinishedAt      string

	// Observability of the run (ADR-0040) -- same meaning as in ItemDaFila.
	StepLabel      string
	StepIndex      int
	StepTotal      int
	CurrentItem    string
	ProgressOK     int
	ProgressFailed int
	WaitMS         *int64
	ElapsedMS      *int64
	DurationMS     *int64
}

// Status returns the most recent state of the reference (kind, ref_id): the
// ACTIVE item (queued/running) when there is one, otherwise the LAST finished
// item — QueueItemStatus{Existe: false} when there has never been any item.
// An invalid kind returns ErrKindInvalido.
func (m *Manager) Status(ctx context.Context, kind, refID string) (*QueueItemStatus, error) {
	if err := validarKind(kind); err != nil {
		return nil, err
	}

	item, err := m.leitura().ObterQueueItemAtivoPorKindRef(ctx, sqlcgen.ObterQueueItemAtivoPorKindRefParams{
		Kind: kind, RefID: refID,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("execqueue: obter item ativo: %w", err)
		}
		item, err = m.leitura().ObterUltimoQueueItemPorKindRef(ctx, sqlcgen.ObterUltimoQueueItemPorKindRefParams{
			Kind: kind, RefID: refID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &QueueItemStatus{Existe: false}, nil
			}
			return nil, fmt.Errorf("execqueue: obter ultimo item: %w", err)
		}
	}
	return queueItemToStatus(item, m.agora()), nil
}

func queueItemToStatus(item sqlcgen.QueueItem, agora time.Time) *QueueItemStatus {
	iniciado := nullString(item.StartedAt)
	finalizado := nullString(item.FinishedAt)
	st := &QueueItemStatus{
		Existe:          true,
		ID:              item.ID,
		Kind:            item.Kind,
		RefID:           item.RefID,
		Trigger:         item.Trigger,
		Status:          item.Status,
		ProgressCurrent: int(item.ProgressCurrent),
		ProgressTotal:   int(item.ProgressTotal),
		Message:         nullString(item.Message),
		EnqueuedAt:      item.EnqueuedAt,
		StartedAt:       iniciado,
		FinishedAt:      finalizado,
		StepLabel:       nullString(item.StepLabel),
		StepIndex:       int(item.StepIndex),
		StepTotal:       int(item.StepTotal),
		CurrentItem:     nullString(item.CurrentItem),
		ProgressOK:      int(item.ProgressOk),
		ProgressFailed:  int(item.ProgressFailed),
	}
	// Same derivation the queue view uses (fila.go): one definition of duration
	// per endpoint family, never two.
	st.WaitMS = DuracaoMS(item.EnqueuedAt, iniciado)
	st.DurationMS = DuracaoMS(iniciado, finalizado)
	if item.Status == StatusRunning {
		st.ElapsedMS = DecorridoMS(iniciado, agora)
	}
	return st
}

func nullString(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// ── Worker ───────────────────────────────────────────────────────────────────

// Run is the loop of the sequential worker: while ctx is not canceled, it
// fetches the oldest 'queued' item and processes it (blocking — concurrency 1
// by design, ADR-0028); when the queue is empty it sleeps pollInterval before
// trying again. Before entering the loop it recovers any orphan 'running'
// item (the process died in the middle of a previous execution).
//
// Calling Run is the responsibility of the process boot (cmd/mymediadb/main.go
// in a later layer, via `go execQueue.Run(ctx)`) — it blocks until ctx is
// canceled, so it must run in its own goroutine.
func (m *Manager) Run(ctx context.Context) {
	m.recuperarOrfaos(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		processou, err := m.processarProximo(ctx)
		if err != nil {
			log.Printf("execqueue: erro ao processar a fila: %v", err)
		}
		if processou {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// recuperarOrfaos closes, at the worker's BOOT, any item already 'running' in
// the database: a sign that the process died in the middle of a previous
// execution (the normal case would be never having 'running' before the
// worker starts processing, since only this worker moves queued->running).
// Best-effort — an error on one item does not prevent the others from being
// recovered.
func (m *Manager) recuperarOrfaos(ctx context.Context) {
	itens, err := m.leitura().ListarQueueItemsRunning(ctx)
	if err != nil {
		log.Printf("execqueue: erro ao listar itens orfaos no boot: %v", err)
		return
	}
	for _, item := range itens {
		m.finalizar(ctx, item.ID, StatusError, mensagemProcessoReiniciado)
	}
}

// processarProximo fetches the next 'queued' item and processes it to the end
// (blocking). It returns processou=true if there was an item (processed
// successfully, failed with a Runner error, or finalized immediately for lack
// of a Runner) — the caller (Run) only sleeps when processou=false (empty
// queue).
func (m *Manager) processarProximo(ctx context.Context) (processou bool, err error) {
	pendente, err := m.leitura().ProximoQueueItemPendente(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("buscar proximo item: %w", err)
	}

	item, err := m.escrita().IniciarQueueItem(ctx, pendente.ID)
	if err != nil {
		return false, fmt.Errorf("iniciar item %q: %w", pendente.ID, err)
	}

	runner, ok := m.runner(item.Kind)
	if !ok {
		m.finalizar(ctx, item.ID, StatusError,
			fmt.Sprintf("nenhum runner registrado para o kind %q", item.Kind))
		return true, nil
	}

	mensagem, execErr := m.executarComRecover(ctx, runner, item, newRegistro(ctx, m.escrita, item.ID))
	if execErr != nil {
		m.finalizar(ctx, item.ID, StatusError, execErr.Error())
	} else {
		m.finalizar(ctx, item.ID, StatusSuccess, mensagem)
	}
	return true, nil
}

// executarComRecover calls Runner.Executar recovering from a panic — it never
// leaves an item 'running' stuck forever because a Runner panicked (same
// pattern as scanner.Manager.run / job.Manager.executar).
func (m *Manager) executarComRecover(ctx context.Context, runner Runner, item sqlcgen.QueueItem, registro Registro) (mensagem string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("pânico durante a execução: %v", rec)
		}
	}()
	return runner.Executar(ctx, item.RefID, item.Trigger, registro)
}

// finalizar moves an item to success/error — best-effort (an error only goes
// to the log; there is nothing sensible left to do here — the item stays stuck
// as 'running' only on a database failure, which would already have broken the
// whole execution before this point).
func (m *Manager) finalizar(ctx context.Context, id, status, message string) {
	if _, err := m.escrita().FinalizarQueueItem(ctx, sqlcgen.FinalizarQueueItemParams{
		Status:  status,
		Message: sql.NullString{String: message, Valid: message != ""},
		ID:      id,
	}); err != nil {
		log.Printf("execqueue: erro ao finalizar item %q: %v", id, err)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (m *Manager) leitura() *sqlcgen.Queries { return sqlcgen.New(m.db.Read) }
func (m *Manager) escrita() *sqlcgen.Queries { return sqlcgen.New(m.db.Write) }
