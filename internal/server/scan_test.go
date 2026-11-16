package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// setupScanServer starts a Server with a migrated database and returns the Server (for the
// scanner injection in the tests) and the root handler.
func setupScanServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "scan.sqlite")
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
	configurarFilaDeTeste(t, s)
	return s, h
}

// configurarFilaDeTeste brings up a SINGLE test execqueue.Manager, wires the scanner of `s`
// into it (ConfigurarFila/ConfigurarLibrary) and, when `s.jobs` exists, wires the job.Manager
// into the SAME queue (ConfigurarFila) -- the same production reality since the unified
// execution queue (ADR-0028 layer 3: cmd/mymediadb/main.go has one single queue shared between
// scan and job). It runs the sequential worker in background until the test ends -- necessary
// because neither scanner.Manager nor job.Manager spawns its own goroutine anymore
// (Start()/Executar() only enqueue; the one that really runs is this worker). Call it again
// whenever `s.scanner` is REPLACED by a new *scanner.Manager (the queue/library configuration
// does not survive the instance swap) -- in that case it also reconfigures s.jobs into the NEW
// queue (harmless: the old worker is canceled in the Cleanup regardless of the new one).
func configurarFilaDeTeste(t *testing.T, s *Server) {
	t.Helper()
	fila := execqueue.NewManager(s.db)
	s.scanner.ConfigurarLibrary(s.library)
	s.scanner.ConfigurarFila(fila)
	if s.jobs != nil {
		s.jobs.ConfigurarFila(fila)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		fila.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// criarSourceHTTP registers a datasource through the API and returns its id.
func criarSourceHTTP(t *testing.T, h http.Handler, dir string) string {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": dir, "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("criar source: status %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeSource(t, rec).ID
}

// proberBloqueante blocks in Probe until the liberar channel is closed, keeping the scan
// "running" to exercise the 409.
type proberBloqueante struct{ liberar chan struct{} }

func (proberBloqueante) Disponivel() bool { return true }
func (p proberBloqueante) Probe(ctx context.Context, _ string) ([]byte, error) {
	select {
	case <-p.liberar:
		return []byte("{}"), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestPostScanNaoEncontrado(t *testing.T) {
	_, h := setupScanServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/library/inexistente/scan", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

func TestGetScanStatusNaoEncontrado(t *testing.T) {
	_, h := setupScanServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/library/inexistente/scan", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}

// TestPostScan202EStatus fires a real scan (dummy file) and follows the status until it
// finishes.
func TestPostScan202EStatus(t *testing.T) {
	_, h := setupScanServer(t)
	dir := t.TempDir()
	escreverDummy(t, filepath.Join(dir, "Filme (2000)", "Filme (2000).mkv"))
	id := criarSourceHTTP(t, h, dir)

	rec := do(t, h, http.MethodPost, "/api/v1/library/"+id+"/scan", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, quero 202 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var start startScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decodificar 202: %v", err)
	}
	if start.JobID == "" {
		t.Error("job_id vazio no 202")
	}

	// Waits for the scan to finish by polling the status.
	esperarStatus(t, h, id, func(st scanStatusResponse) bool { return !st.Running })

	final := lerStatus(t, h, id)
	if final.Running {
		t.Error("scan ainda running após espera")
	}
	if final.StartedAt == nil {
		t.Error("started_at nulo após um scan")
	}
}

// TestPostScanEmAndamento409 proves the 409 when a scan is already running, using a prober
// that blocks the scan.
func TestPostScanEmAndamento409(t *testing.T) {
	s, h := setupScanServer(t)
	dir := t.TempDir()
	escreverDummy(t, filepath.Join(dir, "Filme (2000)", "Filme (2000).mkv"))
	id := criarSourceHTTP(t, h, dir)

	// Replaces the scanner with one that blocks in Probe (it stays running) -- the new instance
	// does not inherit the queue/library configuration, so it reconfigures before use (see
	// configurarFilaDeTeste).
	prober := proberBloqueante{liberar: make(chan struct{})}
	s.scanner = scanner.NewManagerComProber(s.db, prober)
	configurarFilaDeTeste(t, s)

	rec1 := do(t, h, http.MethodPost, "/api/v1/library/"+id+"/scan", nil)
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("1º scan status = %d, quero 202", rec1.Code)
	}
	// Waits for the job to show up as running.
	esperarStatus(t, h, id, func(st scanStatusResponse) bool { return st.Running })

	rec2 := do(t, h, http.MethodPost, "/api/v1/library/"+id+"/scan", nil)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("2º scan status = %d, quero 409", rec2.Code)
	}
	checkErrorShape(t, rec2, "scan_in_progress")

	// Releases the scan and waits for it to finish (so no goroutine is left touching the db).
	close(prober.liberar)
	esperarStatus(t, h, id, func(st scanStatusResponse) bool { return !st.Running })
}

// ── helpers ──────────────────────────────────────────────────────────────────

func escreverDummy(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("dummy-"+path), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lerStatus(t *testing.T, h http.Handler, id string) scanStatusResponse {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/library/"+id+"/scan", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var st scanStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decodificar status: %v", err)
	}
	return st
}

// esperarStatus polls the status until the condition holds or the timeout expires.
func esperarStatus(t *testing.T, h http.Handler, id string, cond func(scanStatusResponse) bool) {
	t.Helper()
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) {
		if cond(lerStatus(t, h, id)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout esperando a condição de status")
}
