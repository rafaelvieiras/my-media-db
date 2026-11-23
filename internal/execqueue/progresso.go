package execqueue

import (
	"context"
	"database/sql"
	"log"
	"sync"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file is the OBSERVABILITY layer of the queue (ADR-0040, issue #62): what
// a Runner can REPORT while it runs, and how that report becomes queryable rows.
//
// Before it, a Runner could only say "current/total + one message", and the job
// domain said nothing at all (it discarded the callback). The owner's question
// — "what is it doing now, how many are left, which ones failed" — needs three
// things the queue stores separately:
//
//   progress  a single-row SNAPSHOT (queue_item.progress_*, step_*, current_item).
//             Cheap to write, always reflects the present, no history.
//   counters  progress_ok/progress_failed, incremented by the registro itself on
//             every Evento, so a runner cannot forget to keep them consistent.
//   eventos   an append-only TRAIL (queue_item_event), capped per item, which
//             survives the run and answers "which items succeeded and which
//             failed" after the screen has moved on.

// Eventos do trail (queue_item_event.status) -- the only values accepted by the
// CHECK in migration 0030.
const (
	EventoOK   = "ok"
	EventoErro = "error"
	EventoSkip = "skip"
	// maxEventosPorItem caps the trail of ONE item (retention, ADR-0040): a scan
	// touching thousands of files must not turn the trail into a second copy of
	// the library. The oldest rows beyond the cap are deleted at write time.
	maxEventosPorItem = 200
)

// Progresso is the SNAPSHOT a Runner reports. Fields left at their zero value
// keep whatever was reported before, so a caller can report only what changed
// (e.g. only Item) without wiping the counters. It is a merge, not a reset.
type Progresso struct {
	// Atual/Total is the item count of the current step (a scan: files seen of
	// files found). 0/0 means "not known yet".
	Atual int
	Total int
	// Etapa is the human label of the current step ("scanning library X",
	// "retroactive enrichment"), EtapaIndice/EtapaTotal its position ("step 2 of
	// 7"). EtapaTotal 0 means the run has no steps.
	Etapa       string
	EtapaIndice int
	EtapaTotal  int
	// Item is what is being processed RIGHT NOW (a file path relative to the
	// source, an entity title, a source path) -- the answer to "which item".
	// A runner that only has the ENTITY ID may report it: the queue view resolves
	// the readable label itself (server.currentItemLabel), so the screen never
	// shows a bare UUID where a title exists. The title/path is still what a new
	// runner should report, because it is the label every consumer shows.
	Item string
	// ReiniciarContadores forces Atual/Total to the values reported here, even
	// when both are zero. Needed at a STEP transition: a step with "0 of 4 done"
	// must not inherit the counters of the previous step (which is what the
	// merge does for every other report).
	ReiniciarContadores bool
	// ReiniciarItem forces Item to the value reported here, INCLUDING empty. Also
	// needed at a STEP transition: the item of the previous step is not "the item
	// of this one", and a step that reports no item at all must not inherit a
	// file name from a step that reported one per file (measured: the health job
	// froze an episode file name from the scan on screen through the six
	// steps that followed it).
	ReiniciarItem bool
}

// Evento is one finished item of the trail: the outcome of ONE thing the runner
// processed. Label is free text chosen by the runner; Detail is the short
// reason/extra context (error text, provider, score) and may be empty.
type Evento struct {
	Status string // EventoOK | EventoErro | EventoSkip
	Label  string
	Detail string
}

// Registro is the reporting surface handed to a Runner for ONE item. The queue
// owns the persistence and the counters; the Runner only describes what is
// happening. All methods are safe for concurrent use and never return an error:
// observability must not be able to break the work it observes (a failed write
// goes to the log).
type Registro interface {
	// Progresso merges a snapshot into the item row.
	Progresso(p Progresso)
	// Evento appends one row to the trail, increments the matching counter and
	// prunes the trail beyond maxEventosPorItem.
	Evento(e Evento)
}

// registro is the Registro implementation bound to one queue item.
type registro struct {
	// ctx is the execution context of the item (the same one the Runner got):
	// the trail and the snapshot are written with it, so a canceled run stops
	// writing too.
	ctx     context.Context
	escrita func() *sqlcgen.Queries
	itemID  string

	mu     sync.Mutex
	atual  Progresso
	ok     int
	falhou int
}

func newRegistro(ctx context.Context, escrita func() *sqlcgen.Queries, itemID string) *registro {
	return &registro{ctx: ctx, escrita: escrita, itemID: itemID}
}

// Progresso merges the snapshot and writes the whole row state in one UPDATE.
// Identical consecutive payloads are discarded in memory: a Runner that reports
// the same counters on every file of a batch does not turn that into I/O.
func (r *registro) Progresso(p Progresso) {
	r.mu.Lock()
	defer r.mu.Unlock()

	anterior := r.atual
	r.atual = mergeProgresso(r.atual, p)
	if r.atual == anterior {
		return
	}
	r.gravar()
}

// Evento appends the trail row, bumps the counter and prunes. The counter keeps
// progress_ok/progress_failed coherent with the trail without the Runner having
// to think about it (ADR-0040).
func (r *registro) Evento(e Evento) {
	if e.Label == "" {
		return // a row with no subject would be noise in the trail
	}
	if e.Status != EventoOK && e.Status != EventoErro && e.Status != EventoSkip {
		log.Printf("execqueue: evento com status invalido %q ignorado (item %q)", e.Status, r.itemID)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	q := r.escrita()
	if err := q.CriarQueueItemEvento(r.ctx, sqlcgen.CriarQueueItemEventoParams{
		ID:            uuid.NewString(),
		QueueItemID:   r.itemID,
		QueueItemID_2: r.itemID,
		Status:        e.Status,
		Label:         e.Label,
		Detail:        sql.NullString{String: e.Detail, Valid: e.Detail != ""},
	}); err != nil {
		log.Printf("execqueue: erro ao gravar evento de %q: %v", r.itemID, err)
		return
	}

	switch e.Status {
	case EventoOK:
		r.ok++
	case EventoErro:
		r.falhou++
	}

	if err := q.ApagarQueueItemEventosExcedentes(r.ctx, sqlcgen.ApagarQueueItemEventosExcedentesParams{
		QueueItemID:   r.itemID,
		QueueItemID_2: r.itemID,
		Seq:           maxEventosPorItem,
	}); err != nil {
		log.Printf("execqueue: erro ao podar eventos de %q: %v", r.itemID, err)
	}

	// The counters live in the same row as the snapshot: write it again so a
	// reader never sees "3 failures" without "6 of 20".
	r.gravar()
}

// gravar writes the current snapshot + counters. Caller holds the mutex.
func (r *registro) gravar() {
	if err := r.escrita().AtualizarProgressoQueueItem(r.ctx, sqlcgen.AtualizarProgressoQueueItemParams{
		ProgressCurrent: int64(r.atual.Atual),
		ProgressTotal:   int64(r.atual.Total),
		StepIndex:       int64(r.atual.EtapaIndice),
		StepTotal:       int64(r.atual.EtapaTotal),
		StepLabel:       sql.NullString{String: r.atual.Etapa, Valid: r.atual.Etapa != ""},
		CurrentItem:     sql.NullString{String: r.atual.Item, Valid: r.atual.Item != ""},
		ProgressOk:      int64(r.ok),
		ProgressFailed:  int64(r.falhou),
		ID:              r.itemID,
	}); err != nil {
		log.Printf("execqueue: erro ao gravar progresso de %q: %v", r.itemID, err)
	}
}

// mergeProgresso applies a snapshot over the previous state, field by field: a
// zero value means "not reported this time", never "reset to zero". The runner
// reports what changed; the queue keeps the rest.
func mergeProgresso(anterior, novo Progresso) Progresso {
	if novo.ReiniciarContadores {
		anterior.Atual = novo.Atual
		anterior.Total = novo.Total
	} else if novo.Total != 0 {
		anterior.Total = novo.Total
		anterior.Atual = novo.Atual
	} else if novo.Atual != 0 {
		anterior.Atual = novo.Atual
	}
	if novo.Etapa != "" {
		anterior.Etapa = novo.Etapa
	}
	if novo.EtapaTotal != 0 {
		anterior.EtapaTotal = novo.EtapaTotal
		anterior.EtapaIndice = novo.EtapaIndice
	} else if novo.EtapaIndice != 0 {
		anterior.EtapaIndice = novo.EtapaIndice
	}
	if novo.ReiniciarItem {
		anterior.Item = novo.Item
	} else if novo.Item != "" {
		anterior.Item = novo.Item
	}
	return anterior
}

// registroNulo is the Registro used when there is no queue behind the Runner
// (unit tests, or a Runner invoked directly): it accepts every report and
// persists nothing.
type registroNulo struct{}

func (registroNulo) Progresso(Progresso) {}
func (registroNulo) Evento(Evento)       {}

// RegistroNulo is a Registro that keeps nothing. The queue passes it to a Runner
// invoked OUTSIDE the queue (a unit test, a direct call), so the Runner can
// always report unconditionally.
func RegistroNulo() Registro { return registroNulo{} }

// RegistroGravador is a Registro that REMEMBERS what it received — the test
// double for code that only needs to assert WHAT was reported (which step, which
// item, which events), without a database.
type RegistroGravador struct {
	mu         sync.Mutex
	progressos []Progresso
	eventos    []Evento
}

// NovoRegistroGravador returns an empty recorder.
func NovoRegistroGravador() *RegistroGravador { return &RegistroGravador{} }

// Progresso records one snapshot.
func (g *RegistroGravador) Progresso(p Progresso) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.progressos = append(g.progressos, p)
}

// Evento records one trail row.
func (g *RegistroGravador) Evento(e Evento) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.eventos = append(g.eventos, e)
}

// Progressos returns every snapshot recorded, in order.
func (g *RegistroGravador) Progressos() []Progresso {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Progresso(nil), g.progressos...)
}

// Eventos returns every trail row recorded, in order.
func (g *RegistroGravador) Eventos() []Evento {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Evento(nil), g.eventos...)
}

// UltimoProgresso returns the last snapshot recorded (ok=false when there is
// none) — the common assertion "what did it say it was doing".
func (g *RegistroGravador) UltimoProgresso() (Progresso, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.progressos) == 0 {
		return Progresso{}, false
	}
	return g.progressos[len(g.progressos)-1], true
}
