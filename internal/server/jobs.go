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

// Este arquivo implementa a superfície HTTP do SISTEMA DE JOBS (briefing
// 0052): GET /jobs (lista + estado), PATCH /jobs/{id} (config), POST
// /jobs/{id}/run (execução manual) e GET /jobs/{id}/executions (histórico
// paginado). Toda a orquestração (registry, execução, agendador) vive em
// internal/job; aqui só mapeia HTTP <-> Manager, no mesmo espírito de scan.go
// (server chama scanner.Manager e traduz os erros sentinela ao envelope
// padrão).

// jobLimitPadrao/jobLimitMaximo são os defaults de paginação de
// GET .../executions (briefing 0052, §4: "?limit= default 20, máx. 100").
const (
	jobLimitPadrao = 20
	jobLimitMaximo = 100
)

// jobsUnavailable escreve 503 quando o Manager de jobs não existe (sem banco
// injetado) e devolve true nesse caso — guarda comum dos 4 handlers.
func (s *Server) jobsUnavailable(w http.ResponseWriter) bool {
	if s.jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "jobs_unavailable",
			"sistema de jobs indisponível (sem banco configurado)")
		return true
	}
	return false
}

// jobDTO é o shape de UM item de GET /jobs: espelha job_definition +
// `running` (estado em memória do Manager, efêmero por design).
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

// jobListDTO é o corpo de GET /jobs.
type jobListDTO struct {
	Items []jobDTO `json:"items"`
}

// jobExecutionDTO é o shape de UMA execução (histórico).
type jobExecutionDTO struct {
	ID         string  `json:"id"`
	JobID      string  `json:"job_id"`
	Trigger    string  `json:"trigger"`
	Status     string  `json:"status"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Message    *string `json:"message"`
}

// jobExecutionListDTO é o corpo de GET /jobs/{id}/executions.
type jobExecutionListDTO struct {
	Items []jobExecutionDTO `json:"items"`
}

// runJobResponse é o corpo do 202 de POST /jobs/{id}/run.
type runJobResponse struct {
	ExecutionID string `json:"execution_id"`
}

// listJobs trata GET /api/v1/jobs: todos os job_definition + `running` do
// Manager. Nunca vazio após a migration 0011 (sempre tem o seed
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

// patchJob trata PATCH /api/v1/jobs/{id}: edita enabled/schedule_cron
// (parcial). 200 com o recurso atualizado; 404 id desconhecido; 422
// schedule_cron que não parseia como cron válido (estado anterior
// preservado).
func (s *Server) patchJob(w http.ResponseWriter, r *http.Request) {
	if s.jobsUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")

	// Decodifica num mapa CRU (não numa struct de ponteiros): é a única forma
	// de distinguir "campo ausente" de "campo presente com valor null" — um
	// *json.RawMessage numa struct NÃO preserva essa distinção (o encoding/json
	// zera o ponteiro pra nil em JSON null ANTES de despachar pro
	// json.Unmarshaler de RawMessage, então "ausente" e "null" ficam
	// indistinguíveis). Mesmo padrão de patchOverride (override.go).
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

// parseJobConfigPatch interpreta o mapa CRU do corpo de PATCH /jobs/{id} num
// job.JobConfigPatch, escrevendo 422 (ok=false) num valor de forma inválida
// para `enabled`/`schedule_cron`.
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

// runJob trata POST /api/v1/jobs/{id}/run: dispara execução manual,
// independente de enabled. 202 {execution_id}; 404 id desconhecido; 409 já em
// execução; 503 sem Handler registrado.
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

// listJobExecutions trata GET /api/v1/jobs/{id}/executions: histórico
// paginado, mais recente primeiro. ?limit= (default 20, máx. 100); ?before=
// (cursor por started_at). 404 id desconhecido.
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

// ── Mapeamento de erro ────────────────────────────────────────────────────

// respondJobError mapeia os erros sentinela do internal/job ao envelope
// padrão (briefing 0052, §4/§5).
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

// ── Parse dos parâmetros de query ────────────────────────────────────────────

// parseJobLimit lê ?limit= com default jobLimitPadrao e teto jobLimitMaximo.
// Valor não-numérico ou negativo responde 400 (ok=false).
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

// ── Conversão para DTO ────────────────────────────────────────────────────

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

func execToDTO(e sqlcgen.JobExecution) jobExecutionDTO {
	return jobExecutionDTO{
		ID:         e.ID,
		JobID:      e.JobID,
		Trigger:    e.Trigger,
		Status:     e.Status,
		StartedAt:  e.StartedAt,
		FinishedAt: nsPtr(e.FinishedAt),
		Message:    nsPtr(e.Message),
	}
}

// isJSONNull diz se o JSON cru é o literal `null`.
func isJSONNull(raw json.RawMessage) bool {
	var v any
	return json.Unmarshal(raw, &v) == nil && v == nil
}
