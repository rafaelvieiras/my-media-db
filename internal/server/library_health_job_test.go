package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
)

// setupLibraryHealthServer starts a Server with a migrated database AND the
// WithLibraryHealthJob option applied -- the saude_biblioteca job has a registered Handler,
// unlike setupJobsServer (which only covers backup_vacuum through WithJobHandlers).
func setupLibraryHealthServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "libhealth.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database, WithLibraryHealthJob())
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	// configurarFilaDeTeste (scan_test.go) wires s.jobs into the test queue -- required since
	// the unified execution queue (ADR-0028 layer 3: job.Manager no longer spawns its own
	// goroutine; Executar() only enqueues).
	configurarFilaDeTeste(t, s)
	return s, h
}

// dispararRun fires POST .../run and returns the decoded body; it fails the test if the
// response is not 202 or the execution_id comes empty.
func dispararRun(t *testing.T, h http.Handler, jobID string) runJobResponse {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/jobs/"+jobID+"/run", nil)
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
	return run
}

// obterJobDaLista looks for the `jobID` item in GET /jobs; it fails the test if it is absent
// from the list.
func obterJobDaLista(t *testing.T, h http.Handler, jobID string) jobDTO {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/jobs", nil)
	var body jobListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, it := range body.Items {
		if it.ID == jobID {
			return it
		}
	}
	t.Fatalf("%s ausente da lista", jobID)
	return jobDTO{}
}

// TestWithLibraryHealthJobWiring proves that server.WithLibraryHealthJob() really registers
// the Handler of the saude_biblioteca job: without this option, POST .../run would answer 503
// (seeded job, no Handler) -- exactly as TestRunJobSemHandler503 proves for backup_vacuum.
// With the option, the execution fires (202), finishes successfully (no source registered is
// not an error) and the message reports the review/orphan counts.
func TestWithLibraryHealthJobWiring(t *testing.T) {
	s, h := setupLibraryHealthServer(t)

	dispararRun(t, h, "saude_biblioteca")
	aguardarJobEstado(t, s, "saude_biblioteca", false)

	item := obterJobDaLista(t, h, "saude_biblioteca")
	if item.LastStatus == nil || *item.LastStatus != "success" {
		t.Fatalf("last_status = %v, quero success", item.LastStatus)
	}
	if item.LastMessage == nil || *item.LastMessage == "" {
		t.Fatal("last_message vazio")
	}
}

// TestWithoutLibraryHealthJobDevolve503 proves the contrast: WITHOUT the option, the seeded
// saude_biblioteca job has no registered Handler -> 503, the same semantics of
// ErrJobDesconhecido.
func TestWithoutLibraryHealthJobDevolve503(t *testing.T) {
	_, h := setupJobsServer(t) // does not pass WithLibraryHealthJob()
	rec := do(t, h, http.MethodPost, "/api/v1/jobs/saude_biblioteca/run", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "job_handler_unavailable")
}
