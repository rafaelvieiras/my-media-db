package execqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file is the READ MODEL of the queue (ADR-0040, issue #62): the whole
// queue as one screen sees it — what is running, what is waiting (in order, and
// for how long), what finished recently — plus the derived DURATIONS the owner
// asked for ("every job must show how long it ran").
//
// Durations are DERIVED from the timestamps the queue already stores, never a
// new column: started_at/enqueued_at/finished_at are written by the worker, so a
// stored duration could only ever disagree with them. The three numbers are:
//
//	wait_ms      enqueued_at -> started_at   how long the item waited in the FIFO
//	elapsed_ms   started_at  -> now          only while it runs
//	duration_ms  started_at  -> finished_at  only after it finishes
//
// Each one is a POINTER: absent means "not applicable yet", which is different
// from zero (an item that started in the same millisecond it was enqueued).

// ItemDaFila is one item of the queue view, with the derived durations and, for
// waiting items, the position in the FIFO (1 = the next one to run).
type ItemDaFila struct {
	ID      string
	Kind    string
	RefID   string
	Trigger string
	Status  string
	Message string
	Posicao int

	ProgressAtual    int
	ProgressTotal    int
	ProgressOK       int
	ProgressFalhados int

	Etapa       string
	EtapaIndice int
	EtapaTotal  int
	ItemAtual   string

	EnqueuedAt string
	StartedAt  string
	FinishedAt string

	EsperaMS    *int64
	DecorridoMS *int64
	DuracaoMS   *int64
}

// EventoDaFila is one row of the trail of a finished item.
type EventoDaFila struct {
	Seq    int64
	At     string
	Status string
	Label  string
	Detail string
}

// Fila is the whole picture: at most one running item (the worker is sequential,
// ADR-0028), the FIFO of waiting items, and the most recently finished ones.
type Fila struct {
	Rodando  *ItemDaFila
	NaFila   []ItemDaFila
	Recentes []ItemDaFila
}

// DetalheDaFila is ONE item plus its trail.
type DetalheDaFila struct {
	Item    ItemDaFila
	Eventos []EventoDaFila
}

// Fila returns the queue view. `recentes` limits how many finished items come
// back (0 or negative means the default, recentesPadrao).
func (m *Manager) Fila(ctx context.Context, recentes int) (*Fila, error) {
	if recentes <= 0 {
		recentes = recentesPadrao
	}

	ativos, err := m.leitura().ListarQueueItemsAtivos(ctx)
	if err != nil {
		return nil, fmt.Errorf("execqueue: listar itens ativos: %w", err)
	}
	recentesItens, err := m.leitura().ListarQueueItemsRecentes(ctx, int64(recentes))
	if err != nil {
		return nil, fmt.Errorf("execqueue: listar itens recentes: %w", err)
	}

	fila := &Fila{NaFila: make([]ItemDaFila, 0, len(ativos)), Recentes: make([]ItemDaFila, 0, len(recentesItens))}
	posicao := 0
	for _, item := range ativos {
		if item.Status == StatusQueued {
			posicao++
		}
		daFila := itemParaFila(item, m.agora())
		if item.Status == StatusRunning {
			rodando := daFila
			fila.Rodando = &rodando
			continue
		}
		daFila.Posicao = posicao
		fila.NaFila = append(fila.NaFila, daFila)
	}
	for _, item := range recentesItens {
		fila.Recentes = append(fila.Recentes, itemParaFila(item, m.agora()))
	}
	return fila, nil
}

// recentesPadrao is how many finished items the queue view shows when the
// caller does not say otherwise.
const recentesPadrao = 20

// DetalheDaFila returns ONE item and its trail (oldest event first). It returns
// (nil, nil) when the id does not exist — the caller maps that to 404.
func (m *Manager) DetalheDaFila(ctx context.Context, id string, eventos int) (*DetalheDaFila, error) {
	if eventos <= 0 {
		eventos = maxEventosPorItem
	}
	item, err := m.leitura().ObterQueueItemPorId(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("execqueue: obter item %q: %w", id, err)
	}
	linhas, err := m.leitura().ListarQueueItemEventos(ctx, sqlcgen.ListarQueueItemEventosParams{
		QueueItemID: id,
		Limit:       int64(eventos),
	})
	if err != nil {
		return nil, fmt.Errorf("execqueue: listar eventos de %q: %w", id, err)
	}
	detalhe := &DetalheDaFila{Item: itemParaFila(item, m.agora()), Eventos: make([]EventoDaFila, 0, len(linhas))}
	for _, l := range linhas {
		detalhe.Eventos = append(detalhe.Eventos, EventoDaFila{
			Seq:    l.Seq,
			At:     l.At,
			Status: l.Status,
			Label:  l.Label,
			Detail: nullString(l.Detail),
		})
	}
	return detalhe, nil
}

// itemParaFila converts a queue row into the read model, deriving the three
// durations from the timestamps.
func itemParaFila(item sqlcgen.QueueItem, agora time.Time) ItemDaFila {
	enqueued, _ := parseCarimbo(item.EnqueuedAt)
	iniciado, temInicio := parseCarimbo(nullString(item.StartedAt))
	finalizado, temFim := parseCarimbo(nullString(item.FinishedAt))

	daFila := ItemDaFila{
		ID:               item.ID,
		Kind:             item.Kind,
		RefID:            item.RefID,
		Trigger:          item.Trigger,
		Status:           item.Status,
		Message:          nullString(item.Message),
		ProgressAtual:    int(item.ProgressCurrent),
		ProgressTotal:    int(item.ProgressTotal),
		ProgressOK:       int(item.ProgressOk),
		ProgressFalhados: int(item.ProgressFailed),
		Etapa:            nullString(item.StepLabel),
		EtapaIndice:      int(item.StepIndex),
		EtapaTotal:       int(item.StepTotal),
		ItemAtual:        nullString(item.CurrentItem),
		EnqueuedAt:       item.EnqueuedAt,
		StartedAt:        nullString(item.StartedAt),
		FinishedAt:       nullString(item.FinishedAt),
	}
	if temInicio && !enqueued.IsZero() {
		daFila.EsperaMS = milissegundos(iniciado.Sub(enqueued))
	}
	switch {
	case temInicio && temFim:
		daFila.DuracaoMS = milissegundos(finalizado.Sub(iniciado))
	case temInicio && item.Status == StatusRunning:
		daFila.DecorridoMS = milissegundos(agora.Sub(iniciado))
	}
	return daFila
}

// parseCarimbo parses the ISO-8601 timestamps the queue stores
// (strftime('%Y-%m-%dT%H:%M:%fZ')). ok=false for an empty/garbage value — the
// caller then simply has no duration to show.
func parseCarimbo(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// milissegundos converts a duration to the integer the API exposes, never
// negative (clock skew would otherwise show "-1 ms").
func milissegundos(d time.Duration) *int64 {
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return &ms
}

// DuracaoMS derives a duration from two stored timestamps — the same rule the
// queue view uses, exported so the job and scan endpoints answer with the SAME
// number the queue screen shows (ADR-0040: one definition of duration, not one
// per endpoint). nil when either stamp is absent or unparsable.
func DuracaoMS(inicio, fim string) *int64 {
	de, okDe := parseCarimbo(inicio)
	ate, okAte := parseCarimbo(fim)
	if !okDe || !okAte {
		return nil
	}
	return milissegundos(ate.Sub(de))
}

// DecorridoMS derives "how long it has been running" from the start stamp and
// now.
func DecorridoMS(inicio string, agora time.Time) *int64 {
	de, ok := parseCarimbo(inicio)
	if !ok {
		return nil
	}
	return milissegundos(agora.Sub(de))
}
