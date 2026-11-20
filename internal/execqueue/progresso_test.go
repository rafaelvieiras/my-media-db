package execqueue

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Tests of the observability layer (ADR-0040): the snapshot a Runner reports, the
// per-item trail, the derived durations and the queue view.

// runnerQueRelata reports what the test tells it to, then finishes.
func runnerQueRelata(relatar func(Registro)) Runner {
	return runnerFunc(func(_ context.Context, _, _ string, registro Registro) (string, error) {
		relatar(registro)
		return "concluido", nil
	})
}

// rodarSincrono enqueues one item of `kind`, runs it to completion and returns
// its id — a tiny helper so each test starts from a finished (or reportable)
// item without repeating the worker dance.
func rodarSincrono(t *testing.T, m *Manager, kind, refID string, r Runner) string {
	t.Helper()
	m.RegistrarRunner(kind, r)
	id, err := m.Enqueue(context.Background(), kind, refID, TriggerManual)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := m.processarProximo(context.Background()); err != nil {
		t.Fatalf("processarProximo: %v", err)
	}
	return id
}

// TestProgressoGravaSnapshot covers the criterion "the running item shows step,
// current item, counters and ok/failed": everything a Runner reports must come
// back through the read model.
func TestProgressoGravaSnapshot(t *testing.T) {
	m := setupManager(t)
	id := rodarSincrono(t, m, KindJob, "job-x", runnerQueRelata(func(registro Registro) {
		registro.Progresso(Progresso{Etapa: "enriquecimento", EtapaIndice: 2, EtapaTotal: 5, ReiniciarContadores: true})
		registro.Progresso(Progresso{Atual: 3, Total: 10, Item: "The Matrix"})
		registro.Evento(Evento{Status: EventoOK, Label: "The Matrix", Detail: "external 100"})
		registro.Evento(Evento{Status: EventoErro, Label: "Dune", Detail: "timeout"})
		registro.Evento(Evento{Status: EventoSkip, Label: "Alien", Detail: "sem candidato"})
	}))

	detalhe, err := m.DetalheDaFila(context.Background(), id, 0)
	if err != nil {
		t.Fatalf("DetalheDaFila: %v", err)
	}
	if detalhe == nil {
		t.Fatal("DetalheDaFila = nil, quero o item")
	}
	item := detalhe.Item
	if item.Etapa != "enriquecimento" || item.EtapaIndice != 2 || item.EtapaTotal != 5 {
		t.Errorf("etapa = %q %d/%d, quero enriquecimento 2/5", item.Etapa, item.EtapaIndice, item.EtapaTotal)
	}
	if item.ProgressAtual != 3 || item.ProgressTotal != 10 {
		t.Errorf("progresso = %d/%d, quero 3/10", item.ProgressAtual, item.ProgressTotal)
	}
	if item.ItemAtual != "The Matrix" {
		t.Errorf("item atual = %q, quero The Matrix", item.ItemAtual)
	}
	// The counters are the registro's job: 1 ok, 1 error, and the skip counts as
	// neither (it was not a failure).
	if item.ProgressOK != 1 || item.ProgressFalhados != 1 {
		t.Errorf("contadores ok/falhas = %d/%d, quero 1/1", item.ProgressOK, item.ProgressFalhados)
	}
	if len(detalhe.Eventos) != 3 {
		t.Fatalf("eventos = %d, quero 3", len(detalhe.Eventos))
	}
	if detalhe.Eventos[0].Label != "The Matrix" || detalhe.Eventos[0].Status != EventoOK {
		t.Errorf("primeiro evento = %+v, quero The Matrix/ok (ordem por seq)", detalhe.Eventos[0])
	}
	if detalhe.Eventos[1].Detail != "timeout" {
		t.Errorf("detalhe do erro = %q, quero timeout", detalhe.Eventos[1].Detail)
	}
}

// TestProgressoFundeCampos covers the merge contract: a report that only names
// the current item must not wipe the counters already reported.
func TestProgressoFundeCampos(t *testing.T) {
	m := setupManager(t)
	id := rodarSincrono(t, m, KindJob, "job-x", runnerQueRelata(func(registro Registro) {
		registro.Progresso(Progresso{Atual: 7, Total: 20})
		registro.Progresso(Progresso{Item: "so o item"})
	}))

	detalhe, err := m.DetalheDaFila(context.Background(), id, 0)
	if err != nil {
		t.Fatalf("DetalheDaFila: %v", err)
	}
	if detalhe.Item.ProgressAtual != 7 || detalhe.Item.ProgressTotal != 20 {
		t.Errorf("progresso = %d/%d, quero 7/20 (o merge preserva o que nao foi reenviado)",
			detalhe.Item.ProgressAtual, detalhe.Item.ProgressTotal)
	}
	if detalhe.Item.ItemAtual != "so o item" {
		t.Errorf("item atual = %q, quero 'so o item'", detalhe.Item.ItemAtual)
	}
}

// TestEventosRespeitamRetencao covers the retention cap: a scan touching
// thousands of files must not grow the trail without limit.
func TestEventosRespeitamRetencao(t *testing.T) {
	m := setupManager(t)
	total := maxEventosPorItem + 25
	id := rodarSincrono(t, m, KindScan, "src-x", runnerQueRelata(func(registro Registro) {
		for i := 0; i < total; i++ {
			registro.Evento(Evento{Status: EventoOK, Label: fmt.Sprintf("arquivo-%03d", i)})
		}
	}))

	var n int
	if err := m.db.Read.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM queue_item_event WHERE queue_item_id = ?", id).Scan(&n); err != nil {
		t.Fatalf("contar eventos: %v", err)
	}
	if n != maxEventosPorItem {
		t.Errorf("eventos gravados = %d, quero %d (retencao por item)", n, maxEventosPorItem)
	}

	// The rows kept are the NEWEST ones: the trail answers "what just failed".
	detalhe, err := m.DetalheDaFila(context.Background(), id, 0)
	if err != nil {
		t.Fatalf("DetalheDaFila: %v", err)
	}
	ultimo := detalhe.Eventos[len(detalhe.Eventos)-1].Label
	if quero := fmt.Sprintf("arquivo-%03d", total-1); ultimo != quero {
		t.Errorf("ultimo evento = %q, quero %q", ultimo, quero)
	}
}

// TestFilaOrdemPosicaoEDuracoes covers the queue view: the running item is
// separate from the FIFO, the waiting ones carry their position, and a finished
// item carries its duration.
func TestFilaOrdemPosicaoEDuracoes(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	// One finished item, to have a duration to show.
	m.RegistrarRunner(KindJob, runnerFunc(func(context.Context, string, string, Registro) (string, error) {
		return "ok", nil
	}))
	if _, err := m.Enqueue(ctx, KindJob, "job-feito", TriggerManual); err != nil {
		t.Fatalf("Enqueue job-feito: %v", err)
	}
	if _, err := m.processarProximo(ctx); err != nil {
		t.Fatalf("processarProximo: %v", err)
	}

	// Two waiting items, enqueued in this order.
	if _, err := m.Enqueue(ctx, KindScan, "src-1", TriggerManual); err != nil {
		t.Fatalf("Enqueue src-1: %v", err)
	}
	if _, err := m.Enqueue(ctx, KindJob, "job-2", TriggerManual); err != nil {
		t.Fatalf("Enqueue job-2: %v", err)
	}

	fila, err := m.Fila(ctx, 10)
	if err != nil {
		t.Fatalf("Fila: %v", err)
	}
	if fila.Rodando != nil {
		t.Errorf("Rodando = %+v, quero nil (o worker nao esta rodando)", fila.Rodando)
	}
	if len(fila.NaFila) != 2 {
		t.Fatalf("NaFila = %d, quero 2", len(fila.NaFila))
	}
	// The FIFO tie-break inside the same millisecond is the id (exactly what the
	// worker uses), so the test asserts the SHAPE — two items, positions 1 and 2,
	// both references present — not an arbitrary order between two items
	// enqueued in the same millisecond.
	if fila.NaFila[0].Posicao != 1 || fila.NaFila[1].Posicao != 2 {
		t.Errorf("posicoes = %d,%d, quero 1,2", fila.NaFila[0].Posicao, fila.NaFila[1].Posicao)
	}
	refs := map[string]bool{fila.NaFila[0].RefID: true, fila.NaFila[1].RefID: true}
	if !refs["src-1"] || !refs["job-2"] {
		t.Errorf("referencias na fila = %v, quero src-1 e job-2", refs)
	}
	if len(fila.Recentes) != 1 {
		t.Fatalf("Recentes = %d, quero 1", len(fila.Recentes))
	}
	recente := fila.Recentes[0]
	if recente.DuracaoMS == nil {
		t.Error("DuracaoMS = nil, quero a duracao do item finalizado")
	}
	if recente.DecorridoMS != nil {
		t.Errorf("DecorridoMS = %v, quero nil num item finalizado", *recente.DecorridoMS)
	}
}

// TestFilaRodandoTemDecorrido covers the "how long has it been running" half: an
// item in status 'running' has no duration yet, only elapsed time.
func TestFilaRodandoTemDecorrido(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	if _, err := m.Enqueue(ctx, KindJob, "job-longo", TriggerManual); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	pendente, err := m.leitura().ProximoQueueItemPendente(ctx)
	if err != nil {
		t.Fatalf("ProximoQueueItemPendente: %v", err)
	}
	if _, err := m.escrita().IniciarQueueItem(ctx, pendente.ID); err != nil {
		t.Fatalf("IniciarQueueItem: %v", err)
	}
	// "Now" two minutes after the start, so elapsed is measurable without sleep.
	m.agora = func() time.Time { return time.Now().UTC().Add(2 * time.Minute) }

	fila, err := m.Fila(ctx, 10)
	if err != nil {
		t.Fatalf("Fila: %v", err)
	}
	if fila.Rodando == nil {
		t.Fatal("Rodando = nil, quero o item em execucao")
	}
	if fila.Rodando.DecorridoMS == nil {
		t.Fatal("DecorridoMS = nil, quero o tempo decorrido")
	}
	if *fila.Rodando.DecorridoMS < int64(2*time.Minute/time.Millisecond) {
		t.Errorf("DecorridoMS = %d, quero >= 120000", *fila.Rodando.DecorridoMS)
	}
	if fila.Rodando.DuracaoMS != nil {
		t.Errorf("DuracaoMS = %v, quero nil enquanto roda", *fila.Rodando.DuracaoMS)
	}
}

// TestDetalheDaFilaInexistente covers the 404 path: an unknown id returns (nil,
// nil), never an error.
func TestDetalheDaFilaInexistente(t *testing.T) {
	m := setupManager(t)
	detalhe, err := m.DetalheDaFila(context.Background(), "nao-existe", 10)
	if err != nil {
		t.Fatalf("DetalheDaFila: %v", err)
	}
	if detalhe != nil {
		t.Errorf("DetalheDaFila = %+v, quero nil", detalhe)
	}
}

// TestItemOrfaoRecebeEventoDeErro garante que o boot continua fechando orfaos
// (ADR-0028) sem quebrar com as colunas novas.
func TestItemOrfaoRecebeEventoDeErro(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()
	id, err := m.Enqueue(ctx, KindJob, "job-orfao", TriggerManual)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := m.escrita().IniciarQueueItem(ctx, id); err != nil {
		t.Fatalf("IniciarQueueItem: %v", err)
	}
	m.recuperarOrfaos(ctx)

	item, err := m.leitura().ObterQueueItemPorId(ctx, id)
	if err != nil {
		t.Fatalf("ObterQueueItemPorId: %v", err)
	}
	if item.Status != StatusError {
		t.Errorf("status = %q, quero error", item.Status)
	}
	if !item.FinishedAt.Valid {
		t.Error("finished_at nao gravado no fechamento do orfao")
	}
	if item.CurrentItem.Valid {
		t.Errorf("current_item = %q, quero NULL num orfao", item.CurrentItem.String)
	}
	var n int
	if err := m.db.Read.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM queue_item_event WHERE queue_item_id = ?", id).Scan(&n); err != nil {
		t.Fatalf("contar eventos: %v", err)
	}
	if n != 0 {
		t.Errorf("eventos = %d, quero 0 (orfaos nao passam pelo registro)", n)
	}
}
