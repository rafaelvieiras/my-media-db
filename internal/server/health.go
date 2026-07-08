package server

import (
	"encoding/json"
	"net/http"
)

// healthResponse é o corpo JSON retornado por GET /api/v1/health.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// healthHandler responde ao endpoint de saúde com 200 e um JSON contendo o
// status e a versão do binário (injetada em build via -ldflags).
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Version: s.version,
	})
}
