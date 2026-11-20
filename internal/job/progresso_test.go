package job

import (
	"errors"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// Tests of the progress facade a handler receives (ADR-0040): every method is a
// thin translation into the queue's vocabulary, and none of them can panic on a
// nil receiver (a handler reports unconditionally).

func TestProgressoTraduzParaORegistro(t *testing.T) {
	g := execqueue.NovoRegistroGravador()
	p := NovoProgresso(g)

	p.Etapa(2, 5, "enriquecimento retroativo")
	p.Contagem(3, 10)
	p.Item("The Matrix")
	p.Ok("The Matrix")
	p.OkDetalhe("Dune", "external 100")
	p.Falhou("Alien", errors.New("timeout"))
	p.Ignorado("Brazil", "sem candidato")

	progressos := g.Progressos()
	if len(progressos) != 3 {
		t.Fatalf("progressos = %d, quero 3 (etapa, contagem, item)", len(progressos))
	}
	etapa := progressos[0]
	if etapa.Etapa != "enriquecimento retroativo" || etapa.EtapaIndice != 2 || etapa.EtapaTotal != 5 {
		t.Errorf("etapa = %+v, quero enriquecimento retroativo 2/5", etapa)
	}
	// A step transition must RESET the counters: "step 2 of 5" cannot inherit
	// "40 of 120" from the step before it.
	if !etapa.ReiniciarContadores {
		t.Error("ReiniciarContadores = false na troca de etapa, quero true")
	}
	if progressos[1].Atual != 3 || progressos[1].Total != 10 {
		t.Errorf("contagem = %+v, quero 3/10", progressos[1])
	}
	if progressos[2].Item != "The Matrix" {
		t.Errorf("item = %q, quero The Matrix", progressos[2].Item)
	}

	eventos := g.Eventos()
	if len(eventos) != 4 {
		t.Fatalf("eventos = %d, quero 4", len(eventos))
	}
	if eventos[0].Status != execqueue.EventoOK || eventos[0].Label != "The Matrix" {
		t.Errorf("evento 0 = %+v, quero ok/The Matrix", eventos[0])
	}
	if eventos[1].Detail != "external 100" {
		t.Errorf("detalhe do ok = %q, quero external 100", eventos[1].Detail)
	}
	if eventos[2].Status != execqueue.EventoErro || eventos[2].Detail != "timeout" {
		t.Errorf("evento de falha = %+v, quero error/timeout", eventos[2])
	}
	if eventos[3].Status != execqueue.EventoSkip {
		t.Errorf("evento de ignorado = %+v, quero skip", eventos[3])
	}
}

// TestProgressoToleraNulo covers the reason a handler can report without
// guarding: without a queue behind it (tests, direct call) every method is a
// no-op, and a nil *Progresso does not panic either.
func TestProgressoToleraNulo(t *testing.T) {
	p := NovoProgresso(nil)
	p.Etapa(1, 1, "passo")
	p.Contagem(1, 1)
	p.Item("x")
	p.Ok("x")
	p.Falhou("y", errors.New("erro"))
	p.Ignorado("z", "")
	if p.Registro() == nil {
		t.Error("Registro() deveria devolver um execqueue.Registro nao nulo")
	}

	var nulo *Progresso
	nulo.Etapa(1, 1, "passo")
	nulo.Contagem(1, 1)
	nulo.Item("x")
	nulo.Ok("x")
	nulo.Falhou("y", nil)
	nulo.Ignorado("z", "")
	if nulo.Registro() == nil {
		t.Error("Registro() de um Progresso nulo deveria devolver o registro nulo, nunca nil")
	}
}

// TestProgressoComIntervaloDescartaSnapshotsIntermediarios covers the throttle the
// queue applies to a handler it runs (issue #73): the progress snapshot is one
// committed UPDATE on the queue row, ~30 ms on the production array, and the UI
// polls it every 4 s. Losing intermediate positions costs nothing; losing the LAST
// one would leave the item frozen at "3 of 19" after a successful run.
func TestProgressoComIntervaloDescartaSnapshotsIntermediarios(t *testing.T) {
	g := execqueue.NovoRegistroGravador()
	p := NovoProgressoComIntervalo(g, time.Minute)

	// A controllable clock: each call advances by the amount the test wants.
	agora := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	p.agora = func() time.Time { return agora }

	p.Contagem(0, 3) // first report always goes through
	agora = agora.Add(10 * time.Second)
	p.Contagem(1, 3) // inside the interval: dropped
	p.Item("Duna")   // same clock: dropped
	agora = agora.Add(10 * time.Second)
	p.Contagem(2, 3) // still inside: dropped
	agora = agora.Add(time.Minute)
	p.Contagem(3, 3) // interval elapsed: goes through

	progressos := g.Progressos()
	if len(progressos) != 2 {
		t.Fatalf("progressos = %d, quero 2 (o primeiro e o último)", len(progressos))
	}
	if progressos[0].Atual != 0 || progressos[0].Total != 3 {
		t.Errorf("primeiro snapshot = %+v, quero 0/3", progressos[0])
	}
	if progressos[1].Atual != 3 || progressos[1].Total != 3 {
		t.Errorf("último snapshot = %+v, quero 3/3", progressos[1])
	}
}

// TestProgressoComIntervaloNuncaDescartaOFinal: the end of the step is forced
// through even inside the interval -- otherwise a run that finishes in less than
// one interval would never show its final counter.
func TestProgressoComIntervaloNuncaDescartaOFinal(t *testing.T) {
	g := execqueue.NovoRegistroGravador()
	p := NovoProgressoComIntervalo(g, time.Hour)
	p.agora = func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }

	p.Contagem(0, 1)
	p.Contagem(1, 1) // atual == total: forced

	progressos := g.Progressos()
	if len(progressos) != 2 {
		t.Fatalf("progressos = %d, quero 2 (a contagem final nunca é engolida)", len(progressos))
	}
	if ultimo := progressos[len(progressos)-1]; ultimo.Atual != 1 || ultimo.Total != 1 {
		t.Errorf("último snapshot = %+v, quero 1/1", ultimo)
	}
}

// TestProgressoComIntervaloNaoEngoleTrilha: the throttle belongs to the snapshot
// only. Every trail row (ok/erro/skip) is the record of what happened and must
// reach the queue, however many an item produces.
func TestProgressoComIntervaloNaoEngoleTrilha(t *testing.T) {
	g := execqueue.NovoRegistroGravador()
	p := NovoProgressoComIntervalo(g, time.Hour)
	p.agora = func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }

	p.Ok("Duna")
	p.OkDetalhe("Alien", "poster")
	p.Ignorado("Brazil", "nada faltando")
	p.Falhou("Matrix", errors.New("sem destino"))

	if eventos := g.Eventos(); len(eventos) != 4 {
		t.Fatalf("eventos = %d, quero 4 (o throttle não se aplica à trilha)", len(eventos))
	}

}
