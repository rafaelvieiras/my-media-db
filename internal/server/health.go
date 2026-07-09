package server

import (
	"encoding/json"
	"net/http"
)

// healthResponse é o corpo JSON retornado por GET /api/v1/health.
type healthResponse struct {
	Status  string   `json:"status"`
	Version string   `json:"version"`
	DB      dbHealth `json:"db"`
}

// dbHealth reporta o estado da fundação de persistência: se o ping respondeu e
// qual a versão de schema aplicada (goose). Campo adicionado no briefing 0006,
// sem alterar os campos existentes (status/version) do contrato do 0001.
type dbHealth struct {
	OK     bool  `json:"ok"`
	Schema int64 `json:"schema"`
}

// healthHandler responde ao endpoint de saúde com 200 e um JSON contendo o
// status, a versão do binário (injetada em build via -ldflags) e o estado do
// banco (ping + versão de schema).
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:  "ok",
		Version: s.version,
		DB:      s.checkDB(r),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// checkDB faz o ping ao banco e lê a versão de schema. Nunca entra em pânico:
// se não há banco injetado ou o ping falha, devolve ok=false.
func (s *Server) checkDB(r *http.Request) dbHealth {
	if s.db == nil {
		return dbHealth{OK: false, Schema: 0}
	}
	ctx := r.Context()
	if err := s.db.Ping(ctx); err != nil {
		return dbHealth{OK: false, Schema: 0}
	}
	schema, err := s.db.SchemaVersion(ctx)
	if err != nil {
		return dbHealth{OK: true, Schema: 0}
	}
	return dbHealth{OK: true, Schema: schema}
}
