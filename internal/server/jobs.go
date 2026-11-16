package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/job"
)

// This file implements the HTTP surface of the JOB SYSTEM:
// GET /jobs (list + state), PATCH /jobs/{id} (config), POST
// /jobs/{id}/run (manual run) and GET /jobs/{id}/executions (paginated
// history). All the orchestration (registry, execution, scheduler) lives in
// internal/job; here it only maps HTTP <-> Manager, in the same spirit as scan.go
// (the server calls scanner.Manager and translates the sentinel errors to the standard
// envelope).

// jobLimitPadrao/jobLimitMaximo are the pagination defaults of
// GET .../executions ("?limit= default 20, max. 100").
const (
	jobLimitPadrao = 20
	jobLimitMaximo = 100
)

// jobsUnavailable writes 503 when the jobs Manager does not exist (no database
// injected) and returns true in that case — common guard of the 4 handlers.
func (s *Server) jobsUnavailable(w http.ResponseWriter) bool {
	if s.jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "jobs_unavailable",
			"sistema de jobs indisponível (sem banco configurado)")
		return true
	}
	return false
}

// jobDTO is the shape of ONE item of GET /jobs: it mirrors job_definition +
// `running` (in-memory state of the Manager, ephemeral by design).
type jobDTO struct {
	ID           string  `json:"id"`
	Nome         string  `json:"nome"`
	Descricao    *string `json:"descricao"`
	Enabled      bool    `json:"enabled"`
	ScheduleCron *string `json:"schedule_cron"`
	NextRunAt    *string `json:"next_run_at"`
	LastRunAt    *string `json:"last_run_at"`
	LastStatus   *string `json:"last_status"`
	LastMessage  *string `json:"last_message"`
	Running      bool    `json:"running"`
}

// jobListDTO is the body of GET /jobs.
type jobListDTO struct {
	Items []jobDTO `json:"items"`
}

// jobExecutionDTO is the shape of ONE execution (history).
type jobExecutionDTO struct {
	ID         string  `json:"id"`
	JobID      string  `json:"job_id"`
	Trigger    string  `json:"trigger"`
	Status     string  `json:"status"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Message    *string `json:"message"`
}

// jobExecutionListDTO is the body of GET /jobs/{id}/executions.
type jobExecutionListDTO struct {
	Items []jobExecutionDTO `json:"items"`
}

// runJobResponse is the body of the 202 of POST /jobs/{id}/run.
type runJobResponse struct {
	ExecutionID string `json:"execution_id"`
}

// listJobs handles GET /api/v1/jobs: every job_definition + the Manager's
// `running`. Never empty after migration 0011 (there is always the seed
// backup_vacuum).
func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	if s.jobsUnavailable(w) {
		return
	}
	defs, err := s.jobs.Listar(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	rodando := s.jobs.Rodando()
	items := make([]jobDTO, 0, len(defs))
	for _, def := range defs {
		items = append(items, jobToDTO(def, rodando[def.ID]))
	}
	writeJSON(w, http.StatusOK, jobListDTO{Items: items})
}

// patchJob handles PATCH /api/v1/jobs/{id}: it edits enabled/schedule_cron
// (partially). 200 with the updated resource; 404 unknown id; 422
// schedule_cron that does not parse as a valid cron (previous state
// preserved).
func (s *Server) patchJob(w http.ResponseWriter, r *http.Request) {
	if s.jobsUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")

	// Decodes into a RAW map (not a struct of pointers): it is the only way
	// to distinguish "absent field" from "field present with a null value" — a
	// *json.RawMessage in a struct does NOT preserve that distinction (encoding/json
	// zeroes the pointer to nil on a JSON null BEFORE dispatching to
	// the json.Unmarshaler of RawMessage, so "absent" and "null" become
	// indistinguishable). Same pattern as patchOverride (override.go).
	var raw map[string]json.RawMessage
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid_body", "corpo de patch inválido")
			return
		}
	}

	patch, ok := parseJobConfigPatch(w, raw)
	if !ok {
		return
	}

	def, err := s.jobs.AtualizarConfig(r.Context(), id, patch)
	if err != nil {
		respondJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobToDTO(def, s.jobs.Rodando()[def.ID]))
}

// parseJobConfigPatch interprets the RAW map of the PATCH /jobs/{id} body into a
// job.JobConfigPatch, writing 422 (ok=false) on an invalid-shaped value
// for `enabled`/`schedule_cron`.
func parseJobConfigPatch(w http.ResponseWriter, raw map[string]json.RawMessage) (job.JobConfigPatch, bool) {
	var patch job.JobConfigPatch
	if v, ok := raw["enabled"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body", "enabled deve ser bool")
			return job.JobConfigPatch{}, false
		}
		patch.Enabled = &b
	}
	if v, ok := raw["schedule_cron"]; ok {
		patch.ScheduleCronSet = true
		if !isJSONNull(v) {
			var sched string
			if err := json.Unmarshal(v, &sched); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "invalid_body",
					"schedule_cron deve ser string ou null")
				return job.JobConfigPatch{}, false
			}
			patch.ScheduleCron = &sched
		}
	}
	return patch, true
}

// runJob handles POST /api/v1/jobs/{id}/run: it triggers a manual execution,
// regardless of enabled. 202 {execution_id}; 404 unknown id; 409 already
// running; 503 no Handler registered.
func (s *Server) runJob(w http.ResponseWriter, r *http.Request) {
	if s.jobsUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	execID, err := s.jobs.Executar(r.Context(), id, "manual")
	if err != nil {
		respondJobError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, runJobResponse{ExecutionID: execID})
}

// listJobExecutions handles GET /api/v1/jobs/{id}/executions: paginated
// history, most recent first. ?limit= (default 20, max. 100); ?before=
// (cursor by started_at). 404 unknown id.
func (s *Server) listJobExecutions(w http.ResponseWriter, r *http.Request) {
	if s.jobsUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")

	limit, ok := parseJobLimit(w, r.URL.Query())
	if !ok {
		return
	}
	var before *string
	if v := r.URL.Query().Get("before"); v != "" {
		before = &v
	}

	execs, err := s.jobs.Historico(r.Context(), id, limit, before)
	if err != nil {
		respondJobError(w, err)
		return
	}
	items := make([]jobExecutionDTO, 0, len(execs))
	for _, e := range execs {
		items = append(items, execToDTO(e))
	}
	writeJSON(w, http.StatusOK, jobExecutionListDTO{Items: items})
}

// ── Error mapping ──────────────────────────────────────────────────────────

// respondJobError maps the sentinel errors of internal/job to the standard
// envelope.
func respondJobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, job.ErrJobNaoEncontrado):
		writeError(w, http.StatusNotFound, "not_found", "job não encontrado")
	case errors.Is(err, job.ErrCronInvalido):
		writeError(w, http.StatusUnprocessableEntity, "invalid_schedule_cron",
			"schedule_cron inválido (esperado uma expressão cron de 5 campos)")
	case errors.Is(err, job.ErrJobEmExecucao):
		writeError(w, http.StatusConflict, "job_running", err.Error())
	case errors.Is(err, job.ErrJobDesconhecido):
		writeError(w, http.StatusServiceUnavailable, "job_handler_unavailable", err.Error())
	default:
		respondErroInterno(w)
	}
}

// ── Query parameter parsing ──────────────────────────────────────────────────

// parseJobLimit reads ?limit= with the jobLimitPadrao default and the jobLimitMaximo
// ceiling. A non-numeric or negative value responds 400 (ok=false).
func parseJobLimit(w http.ResponseWriter, q url.Values) (int, bool) {
	limit := jobLimitPadrao
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "limit inválido")
			return 0, false
		}
		limit = n
	}
	if limit == 0 {
		limit = jobLimitPadrao
	}
	if limit > jobLimitMaximo {
		limit = jobLimitMaximo
	}
	return limit, true
}

// ── Conversion to DTO ──────────────────────────────────────────────────────

func jobToDTO(def sqlcgen.JobDefinition, running bool) jobDTO {
	return jobDTO{
		ID:           def.ID,
		Nome:         def.Nome,
		Descricao:    nsPtr(def.Descricao),
		Enabled:      def.Enabled == 1,
		ScheduleCron: nsPtr(def.ScheduleCron),
		NextRunAt:    nsPtr(def.NextRunAt),
		LastRunAt:    nsPtr(def.LastRunAt),
		LastStatus:   nsPtr(def.LastStatus),
		LastMessage:  nsPtr(def.LastMessage),
		Running:      running,
	}
}

// execToDTO converts a sqlcgen.QueueItem (kind='job') into the usual shape of
// ONE execution (jobExecutionDTO) -- since ADR-0028 (LAYER 3), the job history
// comes from queue_item, no longer from sqlcgen.JobExecution
// (job_execution was dropped, migration 0025); the JSON shape exposed by
// GET /jobs/{id}/executions remains IDENTICAL.
//
// StartedAt in the DTO is a NON-nullable string, but queue_item.started_at MAY be
// NULL (item still 'queued', it never got to run -- theoretically rare for a
// job, since the queue is concurrency 1, but it can happen if another item
// -- scan or job -- was running when this one was enqueued). It applies the
// SAME fallback that Layer 2 already applied for the scanner, for the SAME
// consistency reason (see the comment in internal/scanner/manager.go, function
// Status: "started_at: empty while the item is still 'queued' ... falls
// back to enqueued_at when started_at does not exist yet"): it uses
// queue_item.enqueued_at as the fallback when started_at is NULL.
func execToDTO(e sqlcgen.QueueItem) jobExecutionDTO {
	startedAt := e.EnqueuedAt
	if e.StartedAt.Valid {
		startedAt = e.StartedAt.String
	}
	return jobExecutionDTO{
		ID:         e.ID,
		JobID:      e.RefID,
		Trigger:    e.Trigger,
		Status:     e.Status,
		StartedAt:  startedAt,
		FinishedAt: nsPtr(e.FinishedAt),
		Message:    nsPtr(e.Message),
	}
}

// isJSONNull says whether the raw JSON is the literal `null`.
func isJSONNull(raw json.RawMessage) bool {
	var v any
	return json.Unmarshal(raw, &v) == nil && v == nil
}
