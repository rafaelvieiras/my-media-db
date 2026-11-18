package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
)

// doHealth performs GET /api/v1/health against the server handler and returns the
// decoded body.
func doHealth(t *testing.T, s *Server) healthResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	s.healthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, quero application/json", ct)
	}
	var got healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar corpo: %v", err)
	}
	return got
}

func TestHealthComBanco(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.sqlite")
	database, err := db.Open(db.Config{Path: path})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	s := New("v-teste", database)
	got := doHealth(t, s)

	if got.Status != "ok" {
		t.Errorf("status = %q, quero \"ok\"", got.Status)
	}
	if got.Version != "v-teste" {
		t.Errorf("version = %q, quero \"v-teste\"", got.Version)
	}
	if !got.DB.OK {
		t.Error("db.ok = false, quero true")
	}
	// The schema is compared with the DATABASE's own migrated version instead of a literal:
	// a new migration must not require editing this assertion (it did, three times).
	want, err := database.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if got.DB.Schema != want {
		t.Errorf("db.schema = %d, quero %d (schema migrado)", got.DB.Schema, want)
	}
}

// TestHealthSemBanco guarantees graceful degradation: without a database injected, health
// still responds 200 with the old fields and db.ok=false.
func TestHealthSemBanco(t *testing.T) {
	s := New("v-teste", nil)
	got := doHealth(t, s)

	if got.Status != "ok" {
		t.Errorf("status = %q, quero \"ok\"", got.Status)
	}
	if got.Version != "v-teste" {
		t.Errorf("version = %q, quero \"v-teste\"", got.Version)
	}
	if got.DB.OK {
		t.Error("db.ok = true sem banco, quero false")
	}
}
