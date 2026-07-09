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
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// setupScanServer sobe um Server com banco migrado e devolve o Server (para
// injeção do scanner nos testes) e o handler raiz.
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
	return s, h
}

// criarSourceHTTP cadastra um datasource via a API e devolve seu id.
func criarSourceHTTP(t *testing.T, h http.Handler, dir string) string {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": dir, "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("criar source: status %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeSource(t, rec).ID
}

// proberBloqueante trava no Probe até o canal liberar ser fechado, mantendo o
// scan "running" para exercitar o 409.
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

// TestPostScan202EStatus dispara um scan real (arquivo dummy) e acompanha o
// status até concluir.
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

	// Aguarda o scan concluir consultando o status.
	esperarStatus(t, h, id, func(st scanStatusResponse) bool { return !st.Running })

	final := lerStatus(t, h, id)
	if final.Running {
		t.Error("scan ainda running após espera")
	}
	if final.StartedAt == nil {
		t.Error("started_at nulo após um scan")
	}
}

// TestPostScanEmAndamento409 prova o 409 quando já há scan rodando, usando um
// prober que trava o scan.
func TestPostScanEmAndamento409(t *testing.T) {
	s, h := setupScanServer(t)
	dir := t.TempDir()
	escreverDummy(t, filepath.Join(dir, "Filme (2000)", "Filme (2000).mkv"))
	id := criarSourceHTTP(t, h, dir)

	// Substitui o scanner por um que trava no Probe (mantém running).
	prober := proberBloqueante{liberar: make(chan struct{})}
	s.scanner = scanner.NewManagerComProber(s.db, prober)

	rec1 := do(t, h, http.MethodPost, "/api/v1/library/"+id+"/scan", nil)
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("1º scan status = %d, quero 202", rec1.Code)
	}
	// Espera o job aparecer como running.
	esperarStatus(t, h, id, func(st scanStatusResponse) bool { return st.Running })

	rec2 := do(t, h, http.MethodPost, "/api/v1/library/"+id+"/scan", nil)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("2º scan status = %d, quero 409", rec2.Code)
	}
	checkErrorShape(t, rec2, "scan_in_progress")

	// Libera o scan e espera terminar (para não deixar goroutine tocando o db).
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

// esperarStatus consulta o status até a condição valer ou estourar o timeout.
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
