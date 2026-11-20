package job

import (
	"context"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// This file is the PROGRESS SURFACE of a job handler (ADR-0040, issue #62).
//
// A Handler used to have no way to say anything while it ran: the queue's
// callback was discarded, so every job reported 0/0 and the owner could only
// wait. Two pieces fix that without breaking a single existing handler:
//
//   HandlerComProgresso  an OPTIONAL interface, discovered by type assertion
//                        (the same capability pattern the providers use). A
//                        handler that does not implement it keeps working
//                        exactly as before — 0/0, no trail, no error.
//   Progresso            what the handler receives: steps, the current item, the
//                        counters and the outcome of each thing it processes.

// HandlerComProgresso is implemented by a Handler that wants to narrate its run.
// job.Manager calls ExecutarComProgresso when the registered Handler implements
// it, and plain Executar otherwise.
type HandlerComProgresso interface {
	Handler
	ExecutarComProgresso(ctx context.Context, cfg json.RawMessage, p *Progresso) (mensagem string, err error)
}

// Progresso is the narration handle a handler receives. Every method is safe on
// a nil receiver and on a Progresso without a queue behind it (tests), so a
// handler reports unconditionally. Nothing here can fail: observability never
// breaks the work it observes.
type Progresso struct {
	registro execqueue.Registro
}

// NovoProgresso builds a Progresso over a queue registrar. Exported for tests
// and for callers that run a handler outside the queue.
func NovoProgresso(registro execqueue.Registro) *Progresso {
	if registro == nil {
		registro = execqueue.RegistroNulo()
	}
	return &Progresso{registro: registro}
}

// Etapa announces the step the run is entering: "step 2 of 7: retroactive
// enrichment". It also resets the item counters of the previous step, because a
// step is the unit the owner reasons about ("3 of 20 in this step").
func (p *Progresso) Etapa(indice, total int, rotulo string) {
	if p == nil {
		return
	}
	// The counters restart with the new step (ReiniciarContadores): "step 3 of 7"
	// must never inherit "40 of 120" from the step before it.
	p.registro.Progresso(execqueue.Progresso{
		Etapa:               rotulo,
		EtapaIndice:         indice,
		EtapaTotal:          total,
		ReiniciarContadores: true,
	})
}

// Contagem reports how many items of the CURRENT step are done, out of how many.
func (p *Progresso) Contagem(atual, total int) {
	if p == nil {
		return
	}
	p.registro.Progresso(execqueue.Progresso{Atual: atual, Total: total})
}

// Item names what is being processed right now ("Automatik — 1985", "source
// /media/videos"): the answer to "which one is it on".
func (p *Progresso) Item(rotulo string) {
	if p == nil {
		return
	}
	p.registro.Progresso(execqueue.Progresso{Item: rotulo})
}

// Registro exposes the underlying queue registrar so a handler can hand the SAME
// narration down to a sub-runner it invokes inline — the health job runs the
// scan of each source itself, and the file-level progress of that scan belongs
// to the job item the owner is watching (ADR-0040), not to a second item.
func (p *Progresso) Registro() execqueue.Registro {
	if p == nil || p.registro == nil {
		return execqueue.RegistroNulo()
	}
	return p.registro
}

// Ok records that one item finished well.
func (p *Progresso) Ok(rotulo string) {
	p.comEvento(execqueue.EventoOK, rotulo, "")
}

// OkDetalhe is Ok with a short detail (the provider used, the score, the id).
func (p *Progresso) OkDetalhe(rotulo, detalhe string) {
	p.comEvento(execqueue.EventoOK, rotulo, detalhe)
}

// Falhou records that one item was tried and failed.
func (p *Progresso) Falhou(rotulo string, err error) {
	detalhe := ""
	if err != nil {
		detalhe = err.Error()
	}
	p.comEvento(execqueue.EventoErro, rotulo, detalhe)
}

// Ignorado records that one item was seen and deliberately not processed.
func (p *Progresso) Ignorado(rotulo, detalhe string) {
	p.comEvento(execqueue.EventoSkip, rotulo, detalhe)
}

func (p *Progresso) comEvento(status, rotulo, detalhe string) {
	if p == nil {
		return
	}
	p.registro.Evento(execqueue.Evento{Status: status, Label: rotulo, Detail: detalhe})
}
