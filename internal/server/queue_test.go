package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// Tests of the QUEUE VIEW endpoints (ADR-0040, issue #62). They exercise the HTTP
// mapping — labels, positions, durations, trail — over rows written directly in
// the queue, so the assertions do not race the worker.

// setupQueueServer starts a Server with a migrated database and an execution
// queue WITHOUT a worker: the tests of the queue view write the rows they want to
// see and must not race a live worker (which would pick a 'queued' row up and
// close it as 'error' — there is no runner — and would flag a 'running' row as an
// orphan at boot).
func setupQueueServer(t *testing.T) (http.Handler, *Server) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "queue.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database)
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	s.fila = execqueue.NewManager(s.db)
	// job.Manager reads the same queue (GET /jobs needs it to answer duration and
	// progress), without a worker: same reason as above.
	if s.jobs != nil {
		s.jobs.ConfigurarFila(s.fila)
	}
	return h, s
}

// semearItemDaFila inserts one queue_item straight into the database.
func semearItemDaFila(t *testing.T, s *Server, id, kind, refID, status string, progresso, total int, etapa string, indice, totalEtapa int, itemAtual string, ok, falhas int) {
	t.Helper()
	_, err := s.db.Write.ExecContext(context.Background(), `
		INSERT INTO queue_item (id, kind, ref_id, trigger, status, progress_current, progress_total,
			step_index, step_total, step_label, current_item, progress_ok, progress_failed,
			enqueued_at, started_at, finished_at)
		VALUES (?, ?, ?, 'manual', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, kind, refID, status, progresso, total, indice, totalEtapa,
		nullSeVazio(etapa), nullSeVazio(itemAtual), ok, falhas,
		"2026-11-19T10:00:00.000Z",
		carimboOuNulo(status, "2026-11-19T10:00:10.000Z"),
		carimboFinal(status),
	)
	if err != nil {
		t.Fatalf("inserir queue_item %q: %v", id, err)
	}
}

// nullSeVazio returns NULL for an empty string (the columns that mean "absent").
func nullSeVazio(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// carimboOuNulo returns the start stamp only when the item actually started.
func carimboOuNulo(status, valor string) any {
	if status == execqueue.StatusQueued {
		return nil
	}
	return valor
}

// carimboFinal returns the finish stamp only for a finished item.
func carimboFinal(status string) any {
	if status == execqueue.StatusSuccess || status == execqueue.StatusError {
		return "2026-11-19T10:02:30.000Z"
	}
	return nil
}

// TestQueueViewVazia covers the empty queue: 200 with the three fields, never a
// 404 or a null body.
func TestQueueViewVazia(t *testing.T) {
	h, _ := setupQueueServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/queue", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got queueViewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	if got.Running != nil {
		t.Errorf("running = %+v, quero null", got.Running)
	}
	if len(got.Queued) != 0 || len(got.Recent) != 0 {
		t.Errorf("queued/recent = %d/%d, quero 0/0", len(got.Queued), len(got.Recent))
	}
}

// TestQueueViewComItens covers the queue screen in one call: the running item
// with its step/current item/counters and elapsed time, the waiting ones with
// their position and wait time, and a finished one with its duration and the
// human label of the reference.
func TestQueueViewComItens(t *testing.T) {
	h, s := setupQueueServer(t)

	// The seed job exists since migration 0012 — it gives the label.
	semearItemDaFila(t, s, "q-run", execqueue.KindJob, "saude_biblioteca", execqueue.StatusRunning,
		3, 10, "enriquecimento retroativo", 4, 8, "The Matrix", 2, 1)
	semearItemDaFila(t, s, "q-wait-1", execqueue.KindScan, "src-inexistente", execqueue.StatusQueued,
		0, 0, "", 0, 0, "", 0, 0)
	semearItemDaFila(t, s, "q-done", execqueue.KindJob, "saude_biblioteca", execqueue.StatusSuccess,
		10, 10, "recalcular completude", 8, 8, "", 9, 0)

	rec := do(t, h, http.MethodGet, "/api/v1/queue", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got queueViewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	if got.Running == nil {
		t.Fatal("running = null, quero o item em execucao")
	}
	if got.Running.RefLabel == nil || *got.Running.RefLabel == "" {
		t.Errorf("ref_label = %v, quero o nome legivel do job", got.Running.RefLabel)
	}
	if got.Running.StepLabel == nil || *got.Running.StepLabel != "enriquecimento retroativo" {
		t.Errorf("step_label = %v, quero enriquecimento retroativo", got.Running.StepLabel)
	}
	if got.Running.StepIndex != 4 || got.Running.StepTotal != 8 {
		t.Errorf("step = %d/%d, quero 4/8", got.Running.StepIndex, got.Running.StepTotal)
	}
	if got.Running.CurrentItem == nil || *got.Running.CurrentItem != "The Matrix" {
		t.Errorf("current_item = %v, quero The Matrix", got.Running.CurrentItem)
	}
	if got.Running.Done != 3 || got.Running.Total != 10 {
		t.Errorf("done/total = %d/%d, quero 3/10", got.Running.Done, got.Running.Total)
	}
	if got.Running.OK != 2 || got.Running.Failed != 1 {
		t.Errorf("ok/failed = %d/%d, quero 2/1", got.Running.OK, got.Running.Failed)
	}
	if got.Running.ElapsedMS == nil {
		t.Error("elapsed_ms = null num item em execucao")
	}
	if got.Running.DurationMS != nil {
		t.Error("duration_ms preenchido num item ainda em execucao")
	}
	if got.Running.Position != nil {
		t.Errorf("position = %d, quero ausente no item em execucao", *got.Running.Position)
	}

	if len(got.Queued) != 1 {
		t.Fatalf("queued = %d, quero 1", len(got.Queued))
	}
	naFila := got.Queued[0]
	if naFila.Position == nil || *naFila.Position != 1 {
		t.Errorf("position = %v, quero 1 (primeiro da fila)", naFila.Position)
	}
	if naFila.StartedAt != nil {
		t.Errorf("started_at = %v, quero null num item que nunca rodou", naFila.StartedAt)
	}

	if len(got.Recent) != 1 {
		t.Fatalf("recent = %d, quero 1", len(got.Recent))
	}
	recente := got.Recent[0]
	if recente.DurationMS == nil || *recente.DurationMS != 140000 {
		t.Errorf("duration_ms = %v, quero 140000 (10:00:10 -> 10:02:30)", recente.DurationMS)
	}
	if recente.WaitMS == nil || *recente.WaitMS != 10000 {
		t.Errorf("wait_ms = %v, quero 10000 (10:00:00 -> 10:00:10)", recente.WaitMS)
	}
	if recente.RefLabel == nil || *recente.RefLabel == "" {
		t.Errorf("ref_label do recente = %v, quero o nome legivel do job", recente.RefLabel)
	}
}

// TestQueueItemDetalhe covers one item plus its trail — the answer to "which
// items succeeded and which failed".
func TestQueueItemDetalhe(t *testing.T) {
	h, s := setupQueueServer(t)
	semearItemDaFila(t, s, "q-detalhe", execqueue.KindJob, "saude_biblioteca", execqueue.StatusSuccess,
		5, 5, "enriquecimento retroativo", 4, 8, "", 4, 1)

	for i, linha := range []struct{ status, label, detalhe string }{
		{execqueue.EventoOK, "The Matrix", "external 100"},
		{execqueue.EventoErro, "Dune", "timeout"},
		{execqueue.EventoSkip, "Alien", "sem candidato"},
	} {
		if _, err := s.db.Write.ExecContext(context.Background(), `
			INSERT INTO queue_item_event (id, queue_item_id, seq, status, label, detail)
			VALUES (?, 'q-detalhe', ?, ?, ?, ?)`,
			"ev-"+linha.label, int64(i+1), linha.status, linha.label, nullSeVazio(linha.detalhe)); err != nil {
			t.Fatalf("inserir evento: %v", err)
		}
	}

	rec := do(t, h, http.MethodGet, "/api/v1/queue/q-detalhe", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got queueDetailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	if got.Item.ID != "q-detalhe" {
		t.Errorf("item.id = %q, quero q-detalhe", got.Item.ID)
	}
	if got.Item.OK != 4 || got.Item.Failed != 1 {
		t.Errorf("ok/failed = %d/%d, quero 4/1", got.Item.OK, got.Item.Failed)
	}
	if len(got.Eventos) != 3 {
		t.Fatalf("eventos = %d, quero 3", len(got.Eventos))
	}
	if got.Eventos[0].Label != "The Matrix" || got.Eventos[0].Status != execqueue.EventoOK {
		t.Errorf("primeiro evento = %+v, quero The Matrix/ok", got.Eventos[0])
	}
	if got.Eventos[1].Detail == nil || *got.Eventos[1].Detail != "timeout" {
		t.Errorf("detalhe do erro = %v, quero timeout", got.Eventos[1].Detail)
	}
}

// TestQueueItemDetalheNaoEncontrado covers the 404 of an unknown id.
func TestQueueItemDetalheNaoEncontrado(t *testing.T) {
	h, _ := setupQueueServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/queue/nao-existe", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestQueueIndisponivelSemFila covers the 503 when the queue was never
// configured (a server built without WithExecQueue).
func TestQueueIndisponivelSemFila(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/queue", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestQueueQueryInvalida covers ?recent=abc -> 400.
func TestQueueQueryInvalida(t *testing.T) {
	h, _ := setupQueueServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/queue?recent=abc", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestJobDTOExpoeDuracaoEProgresso covers GET /jobs carrying the live progress of
// the job (the same numbers as the queue screen) and the duration of its last
// finished run.
func TestJobDTOExpoeDuracaoEProgresso(t *testing.T) {
	h, s := setupQueueServer(t)
	semearItemDaFila(t, s, "q-job", execqueue.KindJob, "saude_biblioteca", execqueue.StatusSuccess,
		7, 7, "recalcular completude", 8, 8, "", 7, 0)

	rec := do(t, h, http.MethodGet, "/api/v1/jobs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got jobListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	var saude *jobDTO
	for i := range got.Items {
		if got.Items[i].ID == "saude_biblioteca" {
			saude = &got.Items[i]
		}
	}
	if saude == nil {
		t.Fatal("job saude_biblioteca ausente da listagem")
	}
	if saude.Running {
		t.Error("running = true, quero false (o item ja terminou)")
	}
	if saude.LastDurationMS == nil || *saude.LastDurationMS != 140000 {
		t.Errorf("last_duration_ms = %v, quero 140000", saude.LastDurationMS)
	}
	if saude.Progress == nil {
		t.Fatal("progress = null, quero o estado da ultima execucao")
	}
	if saude.Progress.StepLabel == nil || *saude.Progress.StepLabel != "recalcular completude" {
		t.Errorf("step_label = %v, quero recalcular completude", saude.Progress.StepLabel)
	}
	if saude.Progress.OK != 7 {
		t.Errorf("ok = %d, quero 7", saude.Progress.OK)
	}
}

// TestHistoricoExpoeDuracao cobre GET /jobs/{id}/executions: every run carries
// its own duration now.
func TestHistoricoExpoeDuracao(t *testing.T) {
	h, s := setupQueueServer(t)
	semearItemDaFila(t, s, "q-hist", execqueue.KindJob, "saude_biblioteca", execqueue.StatusSuccess,
		1, 1, "", 0, 0, "", 1, 0)

	rec := do(t, h, http.MethodGet, "/api/v1/jobs/saude_biblioteca/executions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got jobExecutionListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, quero 1", len(got.Items))
	}
	if got.Items[0].DurationMS == nil || *got.Items[0].DurationMS != 140000 {
		t.Errorf("duration_ms = %v, quero 140000", got.Items[0].DurationMS)
	}
	if got.Items[0].WaitMS == nil || *got.Items[0].WaitMS != 10000 {
		t.Errorf("wait_ms = %v, quero 10000", got.Items[0].WaitMS)
	}
	if _, err := time.Parse(time.RFC3339, got.Items[0].StartedAt); err != nil {
		t.Errorf("started_at = %q nao e RFC3339: %v", got.Items[0].StartedAt, err)
	}
}
