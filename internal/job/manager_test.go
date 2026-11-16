package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// handlerFake is a minimal/deterministic Handler for the Manager tests;
// thread-safe (Executar runs in its own goroutine -- the tests read the state
// through Chamadas()/CfgRecebida() to keep the race detector satisfied).
type handlerFake struct {
	nome     string
	mensagem string
	err      error

	mu          sync.Mutex
	chamadas    int
	cfgRecebida json.RawMessage
}

func (h *handlerFake) Nome() string { return h.nome }

func (h *handlerFake) Executar(_ context.Context, cfg json.RawMessage) (string, error) {
	h.mu.Lock()
	h.chamadas++
	h.cfgRecebida = cfg
	h.mu.Unlock()
	return h.mensagem, h.err
}

func (h *handlerFake) Chamadas() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.chamadas
}

// handlerBloqueante blocks in Executar until the `liberar` channel closes -- used
// to exercise the 409 (ErrJobEmExecucao) deterministically.
type handlerBloqueante struct {
	nome    string
	liberar chan struct{}
}

func (h *handlerBloqueante) Nome() string { return h.nome }

func (h *handlerBloqueante) Executar(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-h.liberar:
		return "liberado", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// setupManager brings up a Manager with a migrated database (migration 0011
// already seeds backup_vacuum disabled, with no initial schedule in next_run_at)
// and already wired to a test unified execution queue (see
// configurarFilaDeTeste) -- necessary since ADR-0028 (LAYER 3): Executar only
// enqueues, it is the queue worker that really runs the Handler.
func setupManager(t *testing.T) *Manager {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "job.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	m := NewManager(database)
	configurarFilaDeTeste(t, m)
	return m
}

// configurarFilaDeTeste brings up a test execqueue.Manager, wires `m` to it
// (ConfigurarFila) and runs the sequential worker in the background until the
// test ends -- the same pattern as internal/scanner/execqueue_test.go and
// internal/server/scan_test.go (configurarFilaDeTeste), applied here for the
// "job" kind.
func configurarFilaDeTeste(t *testing.T, m *Manager) {
	t.Helper()
	fila := execqueue.NewManager(m.db)
	m.ConfigurarFila(fila)

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

func aguardarRodando(t *testing.T, m *Manager, jobID string, quer bool) {
	t.Helper()
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) {
		if m.Rodando()[jobID] == quer {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout esperando running=%v para %q", quer, jobID)
}

func TestExecutarSucessoAtualizaEstado(t *testing.T) {
	m := setupManager(t)
	h := &handlerFake{nome: "backup_vacuum", mensagem: "ok mensagem"}
	m.Registry.Registrar(h)

	execID, err := m.Executar(context.Background(), "backup_vacuum", "manual")
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if execID == "" {
		t.Fatal("execID vazio")
	}
	aguardarRodando(t, m, "backup_vacuum", false)

	if h.Chamadas() != 1 {
		t.Fatalf("handler chamado %d vezes, quero 1", h.Chamadas())
	}

	def, err := m.Obter(context.Background(), "backup_vacuum")
	if err != nil {
		t.Fatalf("Obter: %v", err)
	}
	if !def.LastStatus.Valid || def.LastStatus.String != "success" {
		t.Fatalf("last_status = %v, quero success", def.LastStatus)
	}
	if !def.LastMessage.Valid || def.LastMessage.String != "ok mensagem" {
		t.Fatalf("last_message = %v, quero 'ok mensagem'", def.LastMessage)
	}
	if !def.LastRunAt.Valid {
		t.Fatal("last_run_at deveria estar setado")
	}

	execs, err := m.Historico(context.Background(), "backup_vacuum", 20, nil)
	if err != nil {
		t.Fatalf("Historico: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("historico len = %d, quero 1", len(execs))
	}
	if execs[0].Status != "success" || execs[0].Trigger != "manual" || execs[0].ID != execID {
		t.Fatalf("execs[0] = %+v", execs[0])
	}
	if !execs[0].FinishedAt.Valid {
		t.Fatal("finished_at deveria estar setado")
	}
}

func TestExecutarErroAtualizaEstado(t *testing.T) {
	m := setupManager(t)
	h := &handlerFake{nome: "backup_vacuum", err: errors.New("deu ruim")}
	m.Registry.Registrar(h)

	if _, err := m.Executar(context.Background(), "backup_vacuum", "manual"); err != nil {
		t.Fatalf("Executar: %v", err)
	}
	aguardarRodando(t, m, "backup_vacuum", false)

	def, err := m.Obter(context.Background(), "backup_vacuum")
	if err != nil {
		t.Fatalf("Obter: %v", err)
	}
	if !def.LastStatus.Valid || def.LastStatus.String != "error" {
		t.Fatalf("last_status = %v, quero error", def.LastStatus)
	}
	if !def.LastMessage.Valid || def.LastMessage.String != "deu ruim" {
		t.Fatalf("last_message = %v, quero 'deu ruim'", def.LastMessage)
	}
}

func TestExecutarJobNaoEncontrado(t *testing.T) {
	m := setupManager(t)
	if _, err := m.Executar(context.Background(), "inexistente", "manual"); !errors.Is(err, ErrJobNaoEncontrado) {
		t.Fatalf("err = %v, quero ErrJobNaoEncontrado", err)
	}
}

func TestExecutarJobDesconhecido(t *testing.T) {
	m := setupManager(t) // backup_vacuum existe no banco (seed), mas nenhum handler registrado
	if _, err := m.Executar(context.Background(), "backup_vacuum", "manual"); !errors.Is(err, ErrJobDesconhecido) {
		t.Fatalf("err = %v, quero ErrJobDesconhecido", err)
	}
}

func TestExecutarEmAndamento409(t *testing.T) {
	m := setupManager(t)
	h := &handlerBloqueante{nome: "backup_vacuum", liberar: make(chan struct{})}
	m.Registry.Registrar(h)

	if _, err := m.Executar(context.Background(), "backup_vacuum", "manual"); err != nil {
		t.Fatalf("1a Executar: %v", err)
	}
	aguardarRodando(t, m, "backup_vacuum", true)

	if _, err := m.Executar(context.Background(), "backup_vacuum", "manual"); !errors.Is(err, ErrJobEmExecucao) {
		t.Fatalf("2a Executar erro = %v, quero ErrJobEmExecucao", err)
	}

	close(h.liberar)
	aguardarRodando(t, m, "backup_vacuum", false)
}

func TestAtualizarConfigHabilitaCalculaProxima(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	ativado := true
	def, err := m.AtualizarConfig(ctx, "backup_vacuum", JobConfigPatch{Enabled: &ativado})
	if err != nil {
		t.Fatalf("AtualizarConfig: %v", err)
	}
	if def.Enabled != 1 {
		t.Fatal("enabled deveria ser 1")
	}
	if !def.NextRunAt.Valid {
		t.Fatal("next_run_at deveria estar setado (job ja tinha schedule_cron do seed)")
	}

	desativado := false
	def2, err := m.AtualizarConfig(ctx, "backup_vacuum", JobConfigPatch{Enabled: &desativado})
	if err != nil {
		t.Fatalf("AtualizarConfig: %v", err)
	}
	if def2.NextRunAt.Valid {
		t.Fatal("next_run_at deveria ser NULL quando desabilitado")
	}
}

func TestAtualizarConfigCronInvalidoNaoAltera(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	antes, err := m.Obter(ctx, "backup_vacuum")
	if err != nil {
		t.Fatalf("Obter: %v", err)
	}

	invalido := "isto nao e uma expressao cron"
	if _, err := m.AtualizarConfig(ctx, "backup_vacuum", JobConfigPatch{
		ScheduleCronSet: true, ScheduleCron: &invalido,
	}); !errors.Is(err, ErrCronInvalido) {
		t.Fatalf("err = %v, quero ErrCronInvalido", err)
	}

	depois, err := m.Obter(ctx, "backup_vacuum")
	if err != nil {
		t.Fatalf("Obter: %v", err)
	}
	if depois.ScheduleCron != antes.ScheduleCron || depois.Enabled != antes.Enabled {
		t.Fatal("estado nao deveria ter mudado apos um patch com cron invalido (422)")
	}
}

func TestAtualizarConfigJobNaoEncontrado(t *testing.T) {
	m := setupManager(t)
	ativado := true
	if _, err := m.AtualizarConfig(context.Background(), "inexistente", JobConfigPatch{Enabled: &ativado}); !errors.Is(err, ErrJobNaoEncontrado) {
		t.Fatalf("err = %v, quero ErrJobNaoEncontrado", err)
	}
}

func TestAtualizarConfigLimpaSchedule(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	if _, err := m.AtualizarConfig(ctx, "backup_vacuum", JobConfigPatch{ScheduleCronSet: true, ScheduleCron: nil}); err != nil {
		t.Fatalf("AtualizarConfig: %v", err)
	}
	def, err := m.Obter(ctx, "backup_vacuum")
	if err != nil {
		t.Fatalf("Obter: %v", err)
	}
	if def.ScheduleCron.Valid {
		t.Fatal("schedule_cron deveria ser NULL apos limpar")
	}
	if def.NextRunAt.Valid {
		t.Fatal("next_run_at deveria ser NULL sem schedule_cron")
	}
}

// TestPodarHistoricoMantemLimite seeds executions directly into queue_item
// (kind='job') -- since ADR-0028 (LAYER 3), the job history lives there, no
// longer in job_execution (dropped by migration 0025).
func TestPodarHistoricoMantemLimite(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	total := manterExecucoes + 5
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("exec-%03d", i)
		if _, err := m.escrita().CriarQueueItem(ctx, sqlcgen.CriarQueueItemParams{
			ID: id, Kind: execqueue.KindJob, RefID: "backup_vacuum", Trigger: "manual",
		}); err != nil {
			t.Fatalf("CriarQueueItem: %v", err)
		}
	}

	m.podarHistorico(ctx, "backup_vacuum")

	execs, err := m.Historico(ctx, "backup_vacuum", 200, nil)
	if err != nil {
		t.Fatalf("Historico: %v", err)
	}
	if len(execs) != manterExecucoes {
		t.Fatalf("historico len = %d, quero %d (poda deveria manter o limite)", len(execs), manterExecucoes)
	}
}

// TestHistoricoPaginacaoComBefore proves the cursor pagination of
// Manager.Historico -- since ADR-0028 (LAYER 3), the cursor filters by
// enqueued_at (no longer started_at: ListarQueueItemsPorKindRef, layer 1, has no
// native cursor support, so the filter runs in Go over enqueued_at, the same
// field as the ORDER BY of the query -- see the comment on Historico).
func TestHistoricoPaginacaoComBefore(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("exec-%d", i)
		if _, err := m.escrita().CriarQueueItem(ctx, sqlcgen.CriarQueueItemParams{
			ID: id, Kind: execqueue.KindJob, RefID: "backup_vacuum", Trigger: "manual",
		}); err != nil {
			t.Fatalf("CriarQueueItem: %v", err)
		}
	}

	primeira, err := m.Historico(ctx, "backup_vacuum", 2, nil)
	if err != nil {
		t.Fatalf("Historico (pagina 1): %v", err)
	}
	if len(primeira) != 2 {
		t.Fatalf("pagina 1 len = %d, quero 2", len(primeira))
	}

	cursor := primeira[len(primeira)-1].EnqueuedAt
	segunda, err := m.Historico(ctx, "backup_vacuum", 2, &cursor)
	if err != nil {
		t.Fatalf("Historico (pagina 2): %v", err)
	}
	for _, e := range segunda {
		if e.EnqueuedAt >= cursor {
			t.Fatalf("execucao %q com enqueued_at >= cursor %q na pagina 2", e.ID, cursor)
		}
	}
}

// TestExecutarDoisJobsDiferentesRespeitaConcorrenciaGlobal proves the central
// decision of ADR-0028: the unified queue has GLOBAL concurrency 1 -- two
// DIFFERENT jobs can coexist in the queue (dedup is per reference, not across
// the whole queue), but they NEVER run at the same time. It is a REAL behavior
// change against the pre-ADR-0028 system (where different jobs ran freely in
// parallel) -- it is not a bug, it is the deliberate decision documented in the
// ADR.
func TestExecutarDoisJobsDiferentesRespeitaConcorrenciaGlobal(t *testing.T) {
	m := setupManager(t)
	liberar := make(chan struct{})
	h1 := &handlerBloqueante{nome: "backup_vacuum", liberar: liberar}
	h2 := &handlerFake{nome: "saude_biblioteca", mensagem: "ok"}
	m.Registry.Registrar(h1)
	m.Registry.Registrar(h2)

	if _, err := m.Executar(context.Background(), "backup_vacuum", "manual"); err != nil {
		t.Fatalf("Executar backup_vacuum: %v", err)
	}
	aguardarRodando(t, m, "backup_vacuum", true)

	// saude_biblioteca is a DIFFERENT reference -- Enqueue does not reject it
	// (dedup is by (kind, ref_id), not across the whole queue), but the item
	// stays 'queued' behind backup_vacuum until it releases.
	if _, err := m.Executar(context.Background(), "saude_biblioteca", "manual"); err != nil {
		t.Fatalf("Executar saude_biblioteca: %v", err)
	}

	// On purpose: a short interval confirms that saude_biblioteca has STILL not
	// run while backup_vacuum remains blocked -- proof that the queue is really
	// sequential (concurrency 1), not two parallel workers.
	time.Sleep(100 * time.Millisecond)
	if h2.Chamadas() != 0 {
		t.Fatal("saude_biblioteca rodou mesmo com backup_vacuum ainda bloqueando a fila (concorrência deveria ser 1)")
	}

	close(liberar)
	aguardarRodando(t, m, "backup_vacuum", false)

	prazo := time.Now().Add(5 * time.Second)
	for h2.Chamadas() == 0 && time.Now().Before(prazo) {
		time.Sleep(10 * time.Millisecond)
	}
	if h2.Chamadas() != 1 {
		t.Fatalf("saude_biblioteca chamado %d vez(es) depois de liberar backup_vacuum, quero 1", h2.Chamadas())
	}
}

func TestHistoricoJobNaoEncontrado(t *testing.T) {
	m := setupManager(t)
	if _, err := m.Historico(context.Background(), "inexistente", 20, nil); !errors.Is(err, ErrJobNaoEncontrado) {
		t.Fatalf("err = %v, quero ErrJobNaoEncontrado", err)
	}
}

// TestSchedulerDisparaJobVencido proves the acceptance criterion of the
// built-in scheduler: an enabled job whose next_run_at has already passed is
// triggered without manual intervention, and next_run_at is recalculated to the
// future.
func TestSchedulerDisparaJobVencido(t *testing.T) {
	m := setupManager(t)
	h := &handlerFake{nome: "backup_vacuum", mensagem: "agendado ok"}
	m.Registry.Registrar(h)
	ctx := context.Background()

	cronExpr := "0 3 * * *"
	passado := m.agora().Add(-time.Hour).Format(carimboFormato)
	if _, err := m.escrita().AtualizarJobConfig(ctx, sqlcgen.AtualizarJobConfigParams{
		Enabled:      1,
		ScheduleCron: sql.NullString{String: cronExpr, Valid: true},
		NextRunAt:    sql.NullString{String: passado, Valid: true},
		ID:           "backup_vacuum",
	}); err != nil {
		t.Fatalf("forjar next_run_at no passado: %v", err)
	}

	m.tick(ctx)
	aguardarRodando(t, m, "backup_vacuum", false)

	if h.Chamadas() != 1 {
		t.Fatalf("handler chamado %d vezes, quero 1", h.Chamadas())
	}

	def, err := m.Obter(ctx, "backup_vacuum")
	if err != nil {
		t.Fatalf("Obter: %v", err)
	}
	if !def.NextRunAt.Valid {
		t.Fatal("next_run_at deveria ter sido recalculado")
	}
	proximo, err := time.Parse(carimboFormato, def.NextRunAt.String)
	if err != nil {
		t.Fatalf("parse next_run_at: %v", err)
	}
	if !proximo.After(m.agora()) {
		t.Fatal("next_run_at deveria apontar para o futuro apos a execucao")
	}
}

// TestRunSchedulerViaTicker proves that the RunScheduler loop (goroutine +
// ticker) triggers on its own, shortening pollInterval so the test does not wait
// 30s.
func TestRunSchedulerViaTicker(t *testing.T) {
	m := setupManager(t)
	h := &handlerFake{nome: "backup_vacuum"}
	m.Registry.Registrar(h)
	ctx := context.Background()

	original := pollInterval
	pollInterval = 20 * time.Millisecond
	defer func() { pollInterval = original }()

	passado := m.agora().Add(-time.Hour).Format(carimboFormato)
	if _, err := m.escrita().AtualizarJobConfig(ctx, sqlcgen.AtualizarJobConfigParams{
		Enabled:      1,
		ScheduleCron: sql.NullString{String: "0 3 * * *", Valid: true},
		NextRunAt:    sql.NullString{String: passado, Valid: true},
		ID:           "backup_vacuum",
	}); err != nil {
		t.Fatalf("forjar next_run_at no passado: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.RunScheduler(runCtx)
		close(done)
	}()

	prazo := time.Now().Add(3 * time.Second)
	for h.Chamadas() == 0 && time.Now().Before(prazo) {
		time.Sleep(10 * time.Millisecond)
	}
	// Wait for the triggered execution to finish (running back to false) BEFORE
	// canceling/closing the database -- it avoids the benign (but noisy in the
	// logs) race between the finishing goroutine (context.Background(), which
	// survives the cancel) and the test Cleanup closing the *sql.DB.
	aguardarRodando(t, m, "backup_vacuum", false)
	cancel()
	<-done

	if h.Chamadas() == 0 {
		t.Fatal("RunScheduler nao disparou o job vencido")
	}
}

func TestValidarCronEProximaExecucao(t *testing.T) {
	if err := ValidarCron(""); err != nil {
		t.Fatalf("ValidarCron(\"\") = %v, quero nil (sem agendamento e valido)", err)
	}
	if err := ValidarCron("0 3 * * *"); err != nil {
		t.Fatalf("ValidarCron(cron valido) = %v", err)
	}
	if err := ValidarCron("nao e cron"); err == nil {
		t.Fatal("ValidarCron(invalido) deveria devolver erro")
	}

	agora := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if p := ProximaExecucao(false, "0 3 * * *", agora); p != nil {
		t.Fatal("ProximaExecucao com enabled=false deveria ser nil")
	}
	if p := ProximaExecucao(true, "", agora); p != nil {
		t.Fatal("ProximaExecucao sem schedule_cron deveria ser nil")
	}
	p := ProximaExecucao(true, "0 3 * * *", agora)
	if p == nil {
		t.Fatal("ProximaExecucao deveria devolver um horario")
	}
	if !p.After(agora) {
		t.Fatal("ProximaExecucao deveria apontar para o futuro")
	}
}
