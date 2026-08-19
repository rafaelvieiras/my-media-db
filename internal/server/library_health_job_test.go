package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
)

// setupLibraryHealthServer sobe um Server com banco migrado E a option
// WithLibraryHealthJob aplicada (briefing 0053) -- o job saude_biblioteca
// tem Handler registrado, diferente de setupJobsServer (que só cobre
// backup_vacuum via WithJobHandlers).
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
	// configurarFilaDeTeste (scan_test.go) liga s.jobs na fila de teste --
	// necessário desde o briefing 0108/ADR-0028 CAMADA 3 (job.Manager não
	// dispara mais sua própria goroutine; Executar() só enfileira).
	configurarFilaDeTeste(t, s)
	return s, h
}

// dispararRun dispara POST .../run e devolve o corpo decodificado; falha o
// teste se a resposta não for 202 ou o execution_id vier vazio.
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

// obterJobDaLista busca o item `jobID` em GET /jobs; falha o teste se
// ausente da lista.
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

// TestWithLibraryHealthJobWiring prova que server.WithLibraryHealthJob()
// registra de fato o Handler do job saude_biblioteca (briefing 0053): sem
// esta option, POST .../run devolveria 503 (job seedado, sem Handler) --
// exatamente como TestRunJobSemHandler503 prova para backup_vacuum. Com a
// option, a execução dispara (202), termina com sucesso (nenhuma fonte
// cadastrada não é erro) e a mensagem reporta as contagens de revisão/órfãs.
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

// TestWithoutLibraryHealthJobDevolve503 prova o contraste: SEM a option, o
// job seedado saude_biblioteca não tem Handler registrado -> 503, mesma
// semântica de ErrJobDesconhecido documentada no briefing 0052.
func TestWithoutLibraryHealthJobDevolve503(t *testing.T) {
	_, h := setupJobsServer(t) // não passa WithLibraryHealthJob()
	rec := do(t, h, http.MethodPost, "/api/v1/jobs/saude_biblioteca/run", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "job_handler_unavailable")
}
