package server

import (
	"encoding/json"
	"net/http"
)

// healthResponse is the JSON body returned by GET /api/v1/health.
type healthResponse struct {
	Status  string   `json:"status"`
	Version string   `json:"version"`
	DB      dbHealth `json:"db"`
}

// dbHealth reports the state of the persistence foundation: whether the ping answered and
// which schema version is applied (goose). The field was added without changing the
// existing ones (status/version) of the health contract.
type dbHealth struct {
	OK     bool  `json:"ok"`
	Schema int64 `json:"schema"`
}

// healthHandler answers the health endpoint with 200 and a JSON containing the status,
// the binary version (injected at build time via -ldflags) and the database state (ping +
// schema version).
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

// checkDB pings the database and reads the schema version. It never panics: if no
// database is injected or the ping fails, it returns ok=false.
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
