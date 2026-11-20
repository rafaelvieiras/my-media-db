package job

import (
	"errors"
	"testing"

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
