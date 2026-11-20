package server

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// This file is the HTTP surface of the QUEUE VIEW (ADR-0040, issue #62).
//
// Until now the queue only spoke per reference: GET /jobs/{id}/executions and
// GET /library/{id}/scan. The owner could not see the QUEUE — whether a manual
// run was waiting behind a scan, in which position, or for how long — and every
// finished item came back without a duration. Two read-only endpoints close
// that: the whole picture in one request, and one item with its trail.

// queueDefaults are the pagination defaults of the queue view.
const (
	queueRecentPadrao  = 20
	queueEventosPadrao = 200
)

// queueItemDTO is ONE item of the queue view: the row plus the derived durations
// and a human label for the reference (job name, library path) — the screen must
// not have to resolve ids to say something readable.
type queueItemDTO struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	RefID    string  `json:"ref_id"`
	RefLabel *string `json:"ref_label"`
	Trigger  string  `json:"trigger"`
	Status   string  `json:"status"`
	Message  *string `json:"message"`
	// Position is 1 for the next item to run, 2 for the one after it, and so on;
	// absent for the running item and for finished ones.
	Position *int `json:"position"`

	StepLabel   *string `json:"step_label"`
	StepIndex   int     `json:"step_index"`
	StepTotal   int     `json:"step_total"`
	CurrentItem *string `json:"current_item"`
	Done        int     `json:"done"`
	Total       int     `json:"total"`
	OK          int     `json:"ok"`
	Failed      int     `json:"failed"`

	EnqueuedAt string  `json:"enqueued_at"`
	StartedAt  *string `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	// WaitMS is enqueued_at -> started_at; DurationMS started_at -> finished_at
	// (finished runs only); ElapsedMS started_at -> now (running runs only).
	WaitMS     *int64 `json:"wait_ms"`
	DurationMS *int64 `json:"duration_ms"`
	ElapsedMS  *int64 `json:"elapsed_ms"`
}

// queueViewDTO is the body of GET /api/v1/queue.
type queueViewDTO struct {
	Running *queueItemDTO  `json:"running"`
	Queued  []queueItemDTO `json:"queued"`
	Recent  []queueItemDTO `json:"recent"`
}

// queueEventoDTO is one row of the trail of one item.
type queueEventoDTO struct {
	Seq    int64   `json:"seq"`
	At     string  `json:"at"`
	Status string  `json:"status"`
	Label  string  `json:"label"`
	Detail *string `json:"detail"`
}

// queueDetailDTO is the body of GET /api/v1/queue/{id}.
type queueDetailDTO struct {
	Item    queueItemDTO     `json:"item"`
	Eventos []queueEventoDTO `json:"events"`
}

// queueUnavailable writes 503 when the execution queue was never configured
// (server without WithExecQueue) and returns true in that case.
func (s *Server) queueUnavailable(w http.ResponseWriter) bool {
	if s.fila == nil {
		writeError(w, http.StatusServiceUnavailable, "queue_unavailable",
			"fila de execução indisponível (sem fila configurada)")
		return true
	}
	return false
}

// queueView handles GET /api/v1/queue?recent=N: the running item, the waiting
// ones in FIFO order and the most recently finished ones.
func (s *Server) queueView(w http.ResponseWriter, r *http.Request) {
	if s.queueUnavailable(w) {
		return
	}
	recent := queueRecentPadrao
	if v := r.URL.Query().Get("recent"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "recent inválido")
			return
		}
		recent = n
	}

	fila, err := s.fila.Fila(r.Context(), recent)
	if err != nil {
		respondErroInterno(w)
		return
	}

	rotulos := s.rotulosDasReferencias(r, fila)
	corpo := queueViewDTO{
		Queued: make([]queueItemDTO, 0, len(fila.NaFila)),
		Recent: make([]queueItemDTO, 0, len(fila.Recentes)),
	}
	if fila.Rodando != nil {
		dto := filaParaDTO(*fila.Rodando)
		dto.RefLabel = rotulos[fila.Rodando.Kind+":"+fila.Rodando.RefID]
		corpo.Running = &dto
	}
	for _, item := range fila.NaFila {
		dto := filaParaDTO(item)
		dto.RefLabel = rotulos[item.Kind+":"+item.RefID]
		corpo.Queued = append(corpo.Queued, dto)
	}
	for _, item := range fila.Recentes {
		dto := filaParaDTO(item)
		dto.RefLabel = rotulos[item.Kind+":"+item.RefID]
		corpo.Recent = append(corpo.Recent, dto)
	}
	writeJSON(w, http.StatusOK, corpo)
}

// queueItemDetail handles GET /api/v1/queue/{id}?events=N: one item plus its
// trail (what each processed thing did). 404 for an unknown id.
func (s *Server) queueItemDetail(w http.ResponseWriter, r *http.Request) {
	if s.queueUnavailable(w) {
		return
	}
	eventos := queueEventosPadrao
	if v := r.URL.Query().Get("events"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "events inválido")
			return
		}
		eventos = n
	}

	detalhe, err := s.fila.DetalheDaFila(r.Context(), chi.URLParam(r, "id"), eventos)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if detalhe == nil {
		writeError(w, http.StatusNotFound, "not_found", "item da fila não encontrado")
		return
	}

	corpo := queueDetailDTO{
		Item:    filaParaDTO(detalhe.Item),
		Eventos: make([]queueEventoDTO, 0, len(detalhe.Eventos)),
	}
	corpo.Item.RefLabel = s.rotulosDasReferencias(r, &execqueue.Fila{
		NaFila: []execqueue.ItemDaFila{detalhe.Item},
	})[detalhe.Item.Kind+":"+detalhe.Item.RefID]
	for _, ev := range detalhe.Eventos {
		corpo.Eventos = append(corpo.Eventos, queueEventoDTO{
			Seq:    ev.Seq,
			At:     ev.At,
			Status: ev.Status,
			Label:  ev.Label,
			Detail: stringPtrOuNulo(ev.Detail),
		})
	}
	writeJSON(w, http.StatusOK, corpo)
}

// rotulosDasReferencias resolves the human label of every reference in the queue
// snapshot: the job name for kind=job, the source path for kind=scan. It is a
// per-request cache (each reference is looked up once, however many items point
// at it) — the queue is short by construction (concurrency 1 + history cap).
func (s *Server) rotulosDasReferencias(r *http.Request, fila *execqueue.Fila) map[string]*string {
	rotulos := make(map[string]*string, len(fila.NaFila)+len(fila.Recentes)+1)
	itens := make([]execqueue.ItemDaFila, 0, len(fila.NaFila)+len(fila.Recentes)+1)
	if fila.Rodando != nil {
		itens = append(itens, *fila.Rodando)
	}
	itens = append(itens, fila.NaFila...)
	itens = append(itens, fila.Recentes...)

	ctx := r.Context()
	for _, item := range itens {
		chave := item.Kind + ":" + item.RefID
		if _, pronto := rotulos[chave]; pronto {
			continue
		}
		rotulos[chave] = s.rotuloDaReferencia(ctx, item.Kind, item.RefID)
	}
	return rotulos
}

// rotuloDaReferencia resolves ONE reference label; nil when it does not resolve
// (a job or source removed between the queue row and this read — the item is
// historical, the label simply does not exist any more).
func (s *Server) rotuloDaReferencia(ctx context.Context, kind, refID string) *string {
	switch kind {
	case execqueue.KindJob:
		if s.jobs == nil {
			return nil
		}
		def, err := s.jobs.Obter(ctx, refID)
		if err != nil {
			return nil
		}
		nome := def.Nome
		return &nome
	case execqueue.KindScan:
		if s.library == nil {
			return nil
		}
		src, err := s.library.Obter(ctx, refID)
		if err != nil {
			return nil
		}
		caminho := src.Path
		return &caminho
	default:
		return nil
	}
}

// filaParaDTO converts one queue item into the API shape, carrying the derived
// durations straight through (they are computed in internal/execqueue, one
// definition for every endpoint — ADR-0040).
func filaParaDTO(item execqueue.ItemDaFila) queueItemDTO {
	dto := queueItemDTO{
		ID:          item.ID,
		Kind:        item.Kind,
		RefID:       item.RefID,
		Trigger:     item.Trigger,
		Status:      item.Status,
		Message:     stringPtrOuNulo(item.Message),
		StepLabel:   stringPtrOuNulo(item.Etapa),
		StepIndex:   item.EtapaIndice,
		StepTotal:   item.EtapaTotal,
		CurrentItem: stringPtrOuNulo(item.ItemAtual),
		Done:        item.ProgressAtual,
		Total:       item.ProgressTotal,
		OK:          item.ProgressOK,
		Failed:      item.ProgressFalhados,
		EnqueuedAt:  item.EnqueuedAt,
		StartedAt:   stringPtrOuNulo(item.StartedAt),
		FinishedAt:  stringPtrOuNulo(item.FinishedAt),
		WaitMS:      item.EsperaMS,
		DurationMS:  item.DuracaoMS,
		ElapsedMS:   item.DecorridoMS,
	}
	if item.Posicao > 0 {
		posicao := item.Posicao
		dto.Position = &posicao
	}
	return dto
}

// valorOuVazio dereferences an optional string, giving "" when absent — the
// shape execqueue.DuracaoMS expects (absent stamp = no duration).
func valorOuVazio(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// stringPtrOuNulo turns an empty string into JSON null (the API convention for
// "absent" across the whole project).
func stringPtrOuNulo(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
