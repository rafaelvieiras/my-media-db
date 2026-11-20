package scanner

// Tests of the ORCHESTRATION layer (ADR-0028): scanner.Manager now enqueues
// instead of running its own goroutine. This file covers two complementary angles:
//   - TestExecutarReportaProgressoPorArquivo: the progress LOGIC (option "a" of the
//     test notes) -- it calls Manager.Executar DIRECTLY, bypassing the queue
//     entirely, for a deterministic assertion that the callback is called once PER
//     FILE (not per batch of 25).
//   - TestOrquestracaoViaFilaStartStatusEDedup: the orchestration CONTRACT (option
//     "b") -- it spins up a real execqueue.Manager, registers the scanner as a
//     Runner (ConfigurarFila) and runs the real worker, proving that
//     Start/Status/dedup keep the SAME observable behavior as before on the public
//     surface of the scanner.

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// TestExecutarReportaProgressoPorArquivo proves the central criterion of the
// unified queue: the progress callback is called ONCE PER FILE processed (current
// growing 1, 2, 3...), not a single time at the end of the batch of 25 -- that was
// what made files_seen look stuck on slow music scans (a real finding from the
// production validation of the music vertical).
func TestExecutarReportaProgressoPorArquivo(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	const nArquivos = 4
	for i := 0; i < nArquivos; i++ {
		pasta := filepath.Join(root, fmt.Sprintf("Filme %d (2020) [tmdbid-%d]", i, i))
		escrever(t, filepath.Join(pasta, fmt.Sprintf("Filme %d (2020) [tmdbid-%d] - 1080p.mkv", i, i)))
	}

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	m.ConfigurarLibrary(libraryGetterFake{source: src})

	var mu sync.Mutex
	var chamadas [][2]int
	registro := &gravadorDeProgresso{aoReportar: func(current, total int, _ string) {
		mu.Lock()
		defer mu.Unlock()
		chamadas = append(chamadas, [2]int{current, total})
	}}

	if _, err := m.Executar(context.Background(), src.ID, TriggerManual, registro); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// The first call is the report of the total (0/nArquivos), right after
	// coletarArquivos -- see executarScan. The following nArquivos calls are one per
	// file processed.
	if len(chamadas) != nArquivos+1 {
		t.Fatalf("progresso chamado %d vez(es), quero %d (1 do total + 1 por arquivo)", len(chamadas), nArquivos+1)
	}
	if chamadas[0] != [2]int{0, nArquivos} {
		t.Errorf("1a chamada = %v, quero [0 %d] (total conhecido, nenhum arquivo visto ainda)", chamadas[0], nArquivos)
	}
	for i := 1; i <= nArquivos; i++ {
		want := [2]int{i, nArquivos}
		if chamadas[i] != want {
			t.Errorf("chamada %d = %v, quero %v", i, chamadas[i], want)
		}
	}
}

// TestOrquestracaoViaFilaStartStatusEDedup spins up a real execqueue.Manager (the
// same pattern of internal/execqueue/manager_test.go), wires the scanner into it
// via ConfigurarFila/ConfigurarLibrary and runs the real worker -- it proves that,
// from outside the scanner package, Start/Status keep behaving as before: 202 with
// an id, 409 (ErrScanEmAndamento) on dedup, and Status reflecting the final result
// (running=false, no error, correct counts) after the worker processes the item.
func TestOrquestracaoViaFilaStartStatusEDedup(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Filme (2020) [tmdbid-1]")
	escrever(t, filepath.Join(pasta, "Filme (2020) [tmdbid-1] - 1080p.mkv"))

	m := managerFake(t, d)
	lib := library.New(d)
	m.ConfigurarLibrary(lib)
	fila := execqueue.NewManager(d)
	m.ConfigurarFila(fila)

	src := criarSource(t, d, root)

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		fila.Run(runCtx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	jobID, err := m.Start(src, TriggerManual)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if jobID == "" {
		t.Fatal("Start devolveu id vazio")
	}

	// Dedup: a second Start for the SAME source, while the previous item is still
	// queued/running in the queue, returns the SAME public sentinel error as always
	// (ErrScanEmAndamento) -- the HTTP 409 contract of internal/server/scan.go
	// depends on it.
	if _, err := m.Start(src, TriggerManual); err != ErrScanEmAndamento {
		t.Fatalf("2o Start err = %v, quero ErrScanEmAndamento", err)
	}

	st := aguardarScanConcluido(t, m, src.ID)
	if !st.Existe {
		t.Fatal("Status.Existe = false depois do scan concluído")
	}
	if st.Running {
		t.Fatal("Status.Running = true depois do scan concluído")
	}
	if st.LastError != "" {
		t.Fatalf("LastError = %q, quero vazio (scan bem-sucedido)", st.LastError)
	}
	if st.FilesSeen != 1 || st.FilesTotal != 1 {
		t.Fatalf("FilesSeen/FilesTotal = %d/%d, quero 1/1", st.FilesSeen, st.FilesTotal)
	}
	if st.StartedAt.IsZero() {
		t.Error("StartedAt zero depois do scan concluído")
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entities = %d, quero 1 (scan disparado via fila casou o arquivo)", n)
	}

	// StorageNaoVideo is still reachable through the new path (queue worker),
	// without having to walk the disk again.
	if _, _, ok := m.StorageNaoVideo(src.ID); !ok {
		t.Error("StorageNaoVideo ok=false depois de um scan concluído via fila")
	}
}

// aguardarScanConcluido polls Manager.Status until Existe=true and Running=false,
// or the deadline expires -- necessary because the queue worker runs in its own
// goroutine.
func aguardarScanConcluido(t *testing.T, m *Manager, sourceID string) Status {
	t.Helper()
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) {
		st := m.Status(sourceID)
		if st.Existe && !st.Running {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout esperando o scan concluir via fila")
	return Status{}
}

// gravadorDeProgresso adapts the execqueue.Registro contract to a bare function
// for the tests in this package: the scan reports through it, and aoReportar
// receives the counters (the item is not relevant to these assertions).
type gravadorDeProgresso struct {
	aoReportar func(current, total int, item string)
}

func (g *gravadorDeProgresso) Progresso(p execqueue.Progresso) {
	if g.aoReportar != nil {
		g.aoReportar(p.Atual, p.Total, p.Item)
	}
}

func (g *gravadorDeProgresso) Evento(execqueue.Evento) {}
