package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// startScanResponse é o corpo do 202 de POST /api/v1/library/{id}/scan.
type startScanResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// scanStatusResponse é o corpo de GET /api/v1/library/{id}/scan. StartedAt e
// LastError são ponteiros para sair como null/omitidos quando ausentes.
type scanStatusResponse struct {
	Running    bool    `json:"running"`
	FilesSeen  int     `json:"files_seen"`
	FilesTotal int     `json:"files_total"`
	StartedAt  *string `json:"started_at"`
	LastError  *string `json:"last_error,omitempty"`
}

// startScan trata POST /api/v1/library/{id}/scan: dispara o scan em background.
// 202 com o id do job; 404 se o datasource não existe; 409 se já há scan em
// andamento ou se o datasource está offline.
func (s *Server) startScan(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	src, err := s.library.Obter(r.Context(), id)
	if err != nil {
		respondLibraryError(w, err) // 404 not_found quando o id não existe
		return
	}

	jobID, err := s.scanner.Start(src)
	if err != nil {
		respondScanError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, startScanResponse{JobID: jobID, Status: "accepted"})
}

// getScanStatus trata GET /api/v1/library/{id}/scan: devolve o progresso do job.
// 404 se o datasource não existe.
func (s *Server) getScanStatus(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := s.library.Obter(r.Context(), id); err != nil {
		respondLibraryError(w, err)
		return
	}

	st := s.scanner.Status(id)
	resp := scanStatusResponse{
		Running:    st.Running,
		FilesSeen:  st.FilesSeen,
		FilesTotal: st.FilesTotal,
	}
	if st.Existe {
		iso := st.StartedAt.UTC().Format(time.RFC3339)
		resp.StartedAt = &iso
	}
	if st.LastError != "" {
		resp.LastError = &st.LastError
	}
	writeJSON(w, http.StatusOK, resp)
}

// respondScanError mapeia os erros do scanner ao envelope padrão.
func respondScanError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, scanner.ErrScanEmAndamento):
		writeError(w, http.StatusConflict, "scan_in_progress", err.Error())
	case errors.Is(err, scanner.ErrSourceOffline):
		writeError(w, http.StatusConflict, "source_offline", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error",
			"erro interno ao iniciar o scan")
	}
}
