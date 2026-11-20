package execqueue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// TestMain shortens pollInterval for ALL tests in this package: with the
// production default (500ms) the tests that depend on the worker sleeping
// between items (empty queue) would be unnecessarily slow. Same pattern as
// internal/job (reassigns the package var, never in code outside tests).
func TestMain(m *testing.M) {
	pollInterval = 10 * time.Millisecond
	os.Exit(m.Run())
}

// runnerFunc adapts a bare func to the Runner contract -- a minimal fake for
// the tests in this package (same spirit as handlerFake in internal/job).
type runnerFunc func(ctx context.Context, refID, trigger string, registro Registro) (string, error)

func (f runnerFunc) Executar(ctx context.Context, refID, trigger string, registro Registro) (string, error) {
	return f(ctx, refID, trigger, registro)
}

// setupManager brings up a Manager with a migrated database (TempDir, same
// pattern as internal/job.setupManager/internal/scanner).
func setupManager(t *testing.T) *Manager {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "execqueue.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewManager(database)
}

// aguardarStatus polls Status until the Status field matches `quer` or the
// deadline expires -- necessary because Run runs in its own goroutine.
func aguardarStatus(t *testing.T, m *Manager, kind, refID, quer string) *QueueItemStatus {
	t.Helper()
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) {
		st, err := m.Status(context.Background(), kind, refID)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st.Existe && st.Status == quer {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout esperando status=%q para (%q, %q)", quer, kind, refID)
	return nil
}

// aguardarProgresso polls until ProgressCurrent/ProgressTotal match the
// expected values.
func aguardarProgresso(t *testing.T, m *Manager, kind, refID string, current, total int) {
	t.Helper()
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) {
		st, err := m.Status(context.Background(), kind, refID)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st.Existe && st.ProgressCurrent == current && st.ProgressTotal == total {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout esperando progresso=%d/%d para (%q, %q)", current, total, kind, refID)
}

func TestEnqueueDedupRejeitaSegundo(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	id1, err := m.Enqueue(ctx, KindScan, "source-1", TriggerManual)
	if err != nil {
		t.Fatalf("1o Enqueue: %v", err)
	}
	if id1 == "" {
		t.Fatal("id vazio")
	}

	if _, err := m.Enqueue(ctx, KindScan, "source-1", TriggerManual); !errors.Is(err, ErrJaEnfileirado) {
		t.Fatalf("2o Enqueue err = %v, quero ErrJaEnfileirado", err)
	}
}

func TestEnqueueReferenciasDiferentesCoexistem(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	id1, err := m.Enqueue(ctx, KindScan, "source-1", TriggerManual)
	if err != nil {
		t.Fatalf("Enqueue source-1: %v", err)
	}
	id2, err := m.Enqueue(ctx, KindScan, "source-2", TriggerManual)
	if err != nil {
		t.Fatalf("Enqueue source-2: %v", err)
	}
	if id1 == id2 {
		t.Fatal("ids deveriam ser diferentes")
	}

	st1, err := m.Status(ctx, KindScan, "source-1")
	if err != nil {
		t.Fatalf("Status source-1: %v", err)
	}
	st2, err := m.Status(ctx, KindScan, "source-2")
	if err != nil {
		t.Fatalf("Status source-2: %v", err)
	}
	if st1.Status != StatusQueued || st2.Status != StatusQueued {
		t.Fatalf("ambos deveriam estar queued, got %q e %q", st1.Status, st2.Status)
	}
}

func TestEnqueueValidacoes(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	if _, err := m.Enqueue(ctx, "invalido", "ref", TriggerManual); !errors.Is(err, ErrKindInvalido) {
		t.Fatalf("kind invalido: err = %v, quero ErrKindInvalido", err)
	}
	if _, err := m.Enqueue(ctx, KindScan, "ref", "invalido"); !errors.Is(err, ErrTriggerInvalido) {
		t.Fatalf("trigger invalido: err = %v, quero ErrTriggerInvalido", err)
	}
	if _, err := m.Enqueue(ctx, KindScan, "", TriggerManual); !errors.Is(err, ErrRefIDVazio) {
		t.Fatalf("refID vazio: err = %v, quero ErrRefIDVazio", err)
	}
}

func TestStatusSemItemNenhum(t *testing.T) {
	m := setupManager(t)
	st, err := m.Status(context.Background(), KindScan, "nunca-existiu")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Existe {
		t.Fatal("Existe deveria ser false")
	}
}

// TestRunProcessaSequencialmente proves the central criterion of ADR-0028:
// items of DIFFERENT references coexist in the queue, but the worker never
// runs two at the same time -- while one of them is blocked in Executar, the
// other stays 'queued'. It does not assume WHICH of the two the worker
// processes first: ProximoQueueItemPendente breaks the tie by `id` (uuid) when
// both items are enqueued within the same millisecond (enqueued_at
// resolution), so the order is not guaranteed at that level -- only mutual
// exclusion is.
func TestRunProcessaSequencialmente(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	entrou := make(chan string, 2)
	liberar := make(chan struct{})

	r := runnerFunc(func(_ context.Context, refID, _ string, _ Registro) (string, error) {
		entrou <- refID
		<-liberar
		return "ok:" + refID, nil
	})
	m.RegistrarRunner(KindScan, r)

	if _, err := m.Enqueue(ctx, KindScan, "source-1", TriggerManual); err != nil {
		t.Fatalf("Enqueue source-1: %v", err)
	}
	if _, err := m.Enqueue(ctx, KindScan, "source-2", TriggerManual); err != nil {
		t.Fatalf("Enqueue source-2: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(runCtx)
		close(done)
	}()

	var primeiro string
	select {
	case primeiro = <-entrou:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout esperando o primeiro item comecar a rodar")
	}
	segundo := "source-1"
	if primeiro == "source-1" {
		segundo = "source-2"
	}

	stPrimeiro, err := m.Status(ctx, KindScan, primeiro)
	if err != nil {
		t.Fatalf("Status %s: %v", primeiro, err)
	}
	if stPrimeiro.Status != StatusRunning {
		t.Fatalf("%s status = %q, quero running", primeiro, stPrimeiro.Status)
	}
	stSegundo, err := m.Status(ctx, KindScan, segundo)
	if err != nil {
		t.Fatalf("Status %s: %v", segundo, err)
	}
	if stSegundo.Status != StatusQueued {
		t.Fatalf("%s status = %q, quero queued (nao deveria rodar enquanto %s esta running)", segundo, stSegundo.Status, primeiro)
	}

	// Release the first one and wait for it to finish BEFORE checking that the
	// second one started -- proof that the worker only moves on after Executar
	// returns (mutual exclusion, not just a timing coincidence).
	liberar <- struct{}{}
	aguardarStatus(t, m, KindScan, primeiro, StatusSuccess)

	select {
	case p := <-entrou:
		if p != segundo {
			t.Fatalf("esperava %q comecar a rodar em seguida, veio %q", segundo, p)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout esperando %q comecar a rodar", segundo)
	}
	liberar <- struct{}{}
	aguardarStatus(t, m, KindScan, segundo, StatusSuccess)

	cancel()
	<-done
}

// TestProgressoIntermediarioReflete proves that Status reflects intermediate
// progress values reported through the callback, not only the final one.
func TestProgressoIntermediarioReflete(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	prosseguir := make(chan struct{})
	r := runnerFunc(func(_ context.Context, _ string, _ string, registro Registro) (string, error) {
		registro.Progresso(Progresso{Atual: 1, Total: 10, Etapa: "iniciando"})
		<-prosseguir
		registro.Progresso(Progresso{Atual: 5})
		return "concluido", nil
	})
	m.RegistrarRunner(KindJob, r)

	if _, err := m.Enqueue(ctx, KindJob, "job-x", TriggerManual); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(runCtx)
		close(done)
	}()

	aguardarProgresso(t, m, KindJob, "job-x", 1, 10)
	close(prosseguir)

	aguardarStatus(t, m, KindJob, "job-x", StatusSuccess)

	st, err := m.Status(ctx, KindJob, "job-x")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.ProgressCurrent != 5 || st.ProgressTotal != 10 {
		t.Fatalf("progresso final = %d/%d, quero 5/10", st.ProgressCurrent, st.ProgressTotal)
	}
	if st.Message != "concluido" {
		t.Fatalf("message = %q, quero 'concluido'", st.Message)
	}

	cancel()
	<-done
}

// TestRunnerAusenteFinalizaComErroSemTravar proves that a kind with no
// registered Runner ends in 'error' with a clear message, and that the worker
// KEEPS processing the following items (it does not hang).
func TestRunnerAusenteFinalizaComErroSemTravar(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	if _, err := m.Enqueue(ctx, KindJob, "job-sem-runner", TriggerManual); err != nil {
		t.Fatalf("Enqueue job-sem-runner: %v", err)
	}

	var chamouScan int32
	r := runnerFunc(func(_ context.Context, _ string, _ string, _ Registro) (string, error) {
		atomic.AddInt32(&chamouScan, 1)
		return "ok", nil
	})
	m.RegistrarRunner(KindScan, r)
	if _, err := m.Enqueue(ctx, KindScan, "source-y", TriggerManual); err != nil {
		t.Fatalf("Enqueue source-y: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(runCtx)
		close(done)
	}()

	st := aguardarStatus(t, m, KindJob, "job-sem-runner", StatusError)
	if st.Message == "" {
		t.Fatal("esperava mensagem de erro para kind sem runner")
	}

	aguardarStatus(t, m, KindScan, "source-y", StatusSuccess)
	if atomic.LoadInt32(&chamouScan) != 1 {
		t.Fatal("worker deveria ter continuado e processado source-y")
	}

	cancel()
	<-done
}

// TestRunRecuperaItemRunningOrfaoNoBoot proves that an item left 'running' by
// a previous execution (the process died mid-run) is finalized as 'error' as
// soon as the worker comes back up.
func TestRunRecuperaItemRunningOrfaoNoBoot(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	item, err := m.escrita().CriarQueueItem(ctx, sqlcgen.CriarQueueItemParams{
		ID: "orfao-1", Kind: KindJob, RefID: "job-orfao", Trigger: TriggerManual,
	})
	if err != nil {
		t.Fatalf("CriarQueueItem: %v", err)
	}
	if _, err := m.escrita().IniciarQueueItem(ctx, item.ID); err != nil {
		t.Fatalf("IniciarQueueItem (forjando running orfao): %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(runCtx)
		close(done)
	}()

	st := aguardarStatus(t, m, KindJob, "job-orfao", StatusError)
	if st.Message != mensagemProcessoReiniciado {
		t.Fatalf("message = %q, quero %q", st.Message, mensagemProcessoReiniciado)
	}

	cancel()
	<-done
}
