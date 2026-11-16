package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/job"
)

// jobHandlerFake is a minimal/deterministic job.Handler for the HTTP tests.
type jobHandlerFake struct {
	nome     string
	mensagem string
	err      error
}

func (h *jobHandlerFake) Nome() string { return h.nome }

func (h *jobHandlerFake) Executar(context.Context, json.RawMessage) (string, error) {
	return h.mensagem, h.err
}

// jobHandlerBloqueante blocks in Executar until the `liberar` channel closes — used to
// exercise the 409 (job_running) deterministically.
type jobHandlerBloqueante struct {
	nome    string
	liberar chan struct{}
}

func (h *jobHandlerBloqueante) Nome() string { return h.nome }

func (h *jobHandlerBloqueante) Executar(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-h.liberar:
		return "liberado", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// setupJobsServer starts a Server with a migrated database (the backup_vacuum seed already
// exists) and registers the given handlers through WithJobHandlers. Without handlers, the
// seeded job stays "unknown" (503 when running) — used to exercise that case.
func setupJobsServer(t *testing.T, handlers ...job.Handler) (*Server, http.Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "jobs.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var opts []Option
	if len(handlers) > 0 {
		jh := make([]job.Handler, len(handlers))
		copy(jh, handlers)
		opts = append(opts, WithJobHandlers(jh...))
	}
	s := New("v-teste", database, opts...)
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	// configurarFilaDeTeste (scan_test.go) wires BOTH the scanner and s.jobs (when it
	// exists, which is always the case here) into the SAME test execqueue.Manager and brings
	// up the worker in background -- required since the unified execution queue (ADR-0028
	// layer 3: job.Manager no longer spawns its own goroutine; Executar() only enqueues).
	configurarFilaDeTeste(t, s)
	return s, h
}

func aguardarJobEstado(t *testing.T, s *Server, jobID string, rodando bool) {
	t.Helper()
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) {
		if s.jobs.Rodando()[jobID] == rodando {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout esperando running=%v para %q", rodando, jobID)
}

// assertJobSeedado checks the default state (freshly migrated, never run) of ONE seeded job
// inside the id->jobDTO map -- extracted to keep TestListJobsSeed with low cyclomatic
// complexity even though it covers the FOUR jobs seeded today (backup_vacuum +
// saude_biblioteca + midias_faltantes + trailer_tema_faltantes).
func assertJobSeedado(t *testing.T, byID map[string]jobDTO, id, cron string) {
	t.Helper()
	item, ok := byID[id]
	if !ok {
		t.Fatalf("%s ausente da lista", id)
	}
	if item.Enabled {
		t.Errorf("%s: enabled deveria ser false por padrão", id)
	}
	if item.ScheduleCron == nil || *item.ScheduleCron != cron {
		t.Fatalf("%s: schedule_cron = %v, quero %q", id, item.ScheduleCron, cron)
	}
	if item.LastStatus != nil {
		t.Fatalf("%s: last_status = %v, quero nil (sem execuções)", id, item.LastStatus)
	}
	if item.Running {
		t.Errorf("%s: running deveria ser false", id)
	}
}

// TestListJobsSeed proves the acceptance criterion: GET /jobs lists the jobs seeded through
// migrations -- backup_vacuum, saude_biblioteca, midias_faltantes and
// trailer_tema_faltantes -- regardless of which Handlers are registered in the binary (the
// job_definition row exists through the migration; the Handler only matters when RUNNING).
// All of them disabled by default, with no executions yet.
func TestListJobsSeed(t *testing.T) {
	_, h := setupJobsServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/jobs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var body jobListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 4 {
		t.Fatalf("items = %d, quero 4 (seed backup_vacuum + saude_biblioteca + midias_faltantes + trailer_tema_faltantes)", len(body.Items))
	}
	byID := make(map[string]jobDTO, len(body.Items))
	for _, it := range body.Items {
		byID[it.ID] = it
	}

	assertJobSeedado(t, byID, "backup_vacuum", "0 3 * * *")
	assertJobSeedado(t, byID, "saude_biblioteca", "0 4 * * *")
	assertJobSeedado(t, byID, "midias_faltantes", "0 5 * * *")
	assertJobSeedado(t, byID, "trailer_tema_faltantes", "30 4 * * *")
}

// TestPatchJobHabilitaCalculaNextRunAt proves the PATCH acceptance criterion: enabled=true
// computes a future next_run_at; enabled=false zeroes it.
func TestPatchJobHabilitaCalculaNextRunAt(t *testing.T) {
	_, h := setupJobsServer(t)

	rec := do(t, h, http.MethodPatch, "/api/v1/jobs/backup_vacuum", map[string]any{"enabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var item jobDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !item.Enabled {
		t.Fatal("enabled deveria ser true")
	}
	if item.NextRunAt == nil {
		t.Fatal("next_run_at deveria estar setado (próxima ocorrência futura de 0 3 * * *)")
	}

	rec2 := do(t, h, http.MethodPatch, "/api/v1/jobs/backup_vacuum", map[string]any{"enabled": false})
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	var item2 jobDTO
	if err := json.Unmarshal(rec2.Body.Bytes(), &item2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if item2.NextRunAt != nil {
		t.Fatal("next_run_at deveria ser null quando desabilitado")
	}
}

// TestPatchJobCronInvalido422 proves the acceptance criterion: an invalid schedule_cron
// answers 422 and does NOT change the previous state.
func TestPatchJobCronInvalido422(t *testing.T) {
	_, h := setupJobsServer(t)

	rec := do(t, h, http.MethodPatch, "/api/v1/jobs/backup_vacuum", map[string]any{"schedule_cron": "isto nao e cron"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_schedule_cron")

	recGet := do(t, h, http.MethodGet, "/api/v1/jobs", nil)
	var body jobListDTO
	if err := json.Unmarshal(recGet.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Items[0].ScheduleCron == nil || *body.Items[0].ScheduleCron != "0 3 * * *" {
		t.Fatalf("schedule_cron mudou apesar do 422: %v", body.Items[0].ScheduleCron)
	}
}

func TestPatchJobScheduleCronNullLimpa(t *testing.T) {
	_, h := setupJobsServer(t)

	rec := do(t, h, http.MethodPatch, "/api/v1/jobs/backup_vacuum", json.RawMessage(`{"schedule_cron":null}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var item jobDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if item.ScheduleCron != nil {
		t.Fatalf("schedule_cron = %v, quero nil apos limpar", item.ScheduleCron)
	}
}

func TestPatchJobNaoEncontrado404(t *testing.T) {
	_, h := setupJobsServer(t)
	rec := do(t, h, http.MethodPatch, "/api/v1/jobs/inexistente", map[string]any{"enabled": true})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestRunJobEHistorico proves the central acceptance criterion: POST .../run fires even
// when disabled, creates running->success with a message, updates
// last_run_at/last_status/last_message, and shows up in the history.
func TestRunJobEHistorico(t *testing.T) {
	fake := &jobHandlerFake{nome: "backup_vacuum", mensagem: "ok mensagem"}
	s, h := setupJobsServer(t, fake)

	rec := do(t, h, http.MethodPost, "/api/v1/jobs/backup_vacuum/run", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, quero 202 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var run runJobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.ExecutionID == "" {
		t.Fatal("execution_id vazio")
	}

	aguardarJobEstado(t, s, "backup_vacuum", false)

	recGet := do(t, h, http.MethodGet, "/api/v1/jobs", nil)
	var body jobListDTO
	if err := json.Unmarshal(recGet.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	item := body.Items[0]
	if item.LastStatus == nil || *item.LastStatus != "success" {
		t.Fatalf("last_status = %v, quero success", item.LastStatus)
	}
	if item.LastMessage == nil || *item.LastMessage != "ok mensagem" {
		t.Fatalf("last_message = %v, quero 'ok mensagem'", item.LastMessage)
	}

	recList := do(t, h, http.MethodGet, "/api/v1/jobs/backup_vacuum/executions", nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", recList.Code, recList.Body.String())
	}
	var execs jobExecutionListDTO
	if err := json.Unmarshal(recList.Body.Bytes(), &execs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(execs.Items) != 1 {
		t.Fatalf("executions len = %d, quero 1", len(execs.Items))
	}
	if execs.Items[0].Status != "success" || execs.Items[0].Trigger != "manual" || execs.Items[0].ID != run.ExecutionID {
		t.Fatalf("execs.Items[0] = %+v", execs.Items[0])
	}
}

// TestRunJobConflito409 proves the acceptance criterion: a second call while the first one
// runs answers 409.
func TestRunJobConflito409(t *testing.T) {
	blk := &jobHandlerBloqueante{nome: "backup_vacuum", liberar: make(chan struct{})}
	s, h := setupJobsServer(t, blk)

	rec1 := do(t, h, http.MethodPost, "/api/v1/jobs/backup_vacuum/run", nil)
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("1a run status = %d, quero 202 (corpo=%s)", rec1.Code, rec1.Body.String())
	}
	aguardarJobEstado(t, s, "backup_vacuum", true)

	rec2 := do(t, h, http.MethodPost, "/api/v1/jobs/backup_vacuum/run", nil)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("2a run status = %d, quero 409 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	checkErrorShape(t, rec2, "job_running")

	close(blk.liberar)
	aguardarJobEstado(t, s, "backup_vacuum", false)
}

func TestRunJobNaoEncontrado404(t *testing.T) {
	_, h := setupJobsServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/jobs/inexistente/run", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestRunJobSemHandler503 proves the acceptance criterion: a seeded job without a Handler
// registered in the binary answers 503.
func TestRunJobSemHandler503(t *testing.T) {
	_, h := setupJobsServer(t) // no handler registered
	rec := do(t, h, http.MethodPost, "/api/v1/jobs/backup_vacuum/run", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "job_handler_unavailable")
}

func TestListJobExecutionsNaoEncontrado404(t *testing.T) {
	_, h := setupJobsServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/jobs/inexistente/executions", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestListJobExecutionsLimit proves the pagination (?limit=) of the history.
func TestListJobExecutionsLimit(t *testing.T) {
	fake := &jobHandlerFake{nome: "backup_vacuum", mensagem: "ok"}
	s, h := setupJobsServer(t, fake)

	for i := 0; i < 3; i++ {
		rec := do(t, h, http.MethodPost, "/api/v1/jobs/backup_vacuum/run", nil)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("run #%d status = %d (corpo=%s)", i, rec.Code, rec.Body.String())
		}
		aguardarJobEstado(t, s, "backup_vacuum", false)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/jobs/backup_vacuum/executions?limit=2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var execs jobExecutionListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &execs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(execs.Items) != 2 {
		t.Fatalf("items = %d, quero 2 (limit=2 de 3 execuções)", len(execs.Items))
	}
}

func TestJobsUnavailableSemBanco(t *testing.T) {
	s := New("v-teste", nil)
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodGet, "/api/v1/jobs", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "jobs_unavailable")
}
