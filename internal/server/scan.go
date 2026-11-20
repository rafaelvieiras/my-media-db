package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// startScanResponse is the body of the 202 of POST /api/v1/library/{id}/scan.
type startScanResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// scanStatusResponse is the body of GET /api/v1/library/{id}/scan. StartedAt and
// LastError are pointers so they come out as null/omitted when absent.
type scanStatusResponse struct {
	Running    bool    `json:"running"`
	FilesSeen  int     `json:"files_seen"`
	FilesTotal int     `json:"files_total"`
	StartedAt  *string `json:"started_at"`
	LastError  *string `json:"last_error,omitempty"`
	// Observability of the run (ADR-0040): the file being processed right now,
	// the post-scan stage, how long it waited in the queue and how long it has
	// been running. Same numbers the queue screen shows.
	CurrentItem *string `json:"current_item"`
	StepLabel   *string `json:"step_label"`
	WaitMS      *int64  `json:"wait_ms"`
	ElapsedMS   *int64  `json:"elapsed_ms"`
}

// startScan handles POST /api/v1/library/{id}/scan: it triggers the scan in the
// background. 202 with the job id; 404 if the datasource does not exist; 409 if there is
// already a scan running or if the datasource is offline.
func (s *Server) startScan(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	src, err := s.library.Obter(r.Context(), id)
	if err != nil {
		respondLibraryError(w, err) // 404 not_found when the id does not exist
		return
	}

	jobID, err := s.scanner.Start(src, scanner.TriggerManual)
	if err != nil {
		respondScanError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, startScanResponse{JobID: jobID, Status: "accepted"})
}

// getScanStatus handles GET /api/v1/library/{id}/scan: it returns the job
// progress. 404 if the datasource does not exist.
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
	if st.CurrentItem != "" {
		resp.CurrentItem = &st.CurrentItem
	}
	if st.StepLabel != "" {
		resp.StepLabel = &st.StepLabel
	}
	if st.Wait > 0 {
		ms := st.Wait.Milliseconds()
		resp.WaitMS = &ms
	}
	if st.Elapsed > 0 {
		ms := st.Elapsed.Milliseconds()
		resp.ElapsedMS = &ms
	}
	writeJSON(w, http.StatusOK, resp)
}

// respondScanError maps the scanner errors to the standard envelope.
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
