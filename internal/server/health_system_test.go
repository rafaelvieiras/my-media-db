package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupSystemHealthServer starts a Server with a migrated temporary database, returning
// the *Server (not only the Handler) — the cache/TTL tests need to touch
// s.telemetryCache.agora, a non-exported field only reachable from inside the
// package.
func setupSystemHealthServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "system-health.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return New("v-teste", database), database
}

// doSystemHealth calls the handler directly (the same pattern as doHealth in
// health_test.go) and returns the raw recorder — the tests decide whether they check a
// 200 status or an error.
func doSystemHealth(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/system", nil)
	rec := httptest.NewRecorder()
	s.systemHealthHandler(rec, req)
	return rec
}

// semearLibrarySource inserts a library_source straight through sqlc (it bypasses the
// path validation of library.Service.Criar — this allows testing an unreachable path
// without exercising that flow).
func semearLibrarySource(t *testing.T, d *db.DB, id, path string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarLibrarySource(context.Background(), sqlcgen.CriarLibrarySourceParams{
		ID: id, Path: path, Type: "movie", ContentType: "movie",
	}); err != nil {
		t.Fatalf("CriarLibrarySource(%s): %v", id, err)
	}
}

// semearFileComFonte creates a media_file linked to a library_source_id (the semearFile of
// entities_test.go does not cover that field).
func semearFileComFonte(t *testing.T, d *db.DB, id, entityID, path, state string, size int64, sourceID string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: id, EntityID: entityID, Path: path, State: state,
		Size:            sql.NullInt64{Int64: size, Valid: true},
		LibrarySourceID: sql.NullString{String: sourceID, Valid: true},
	}); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

// TestSystemHealthSemBancoDevolve503 proves that, with no database injected, the new
// endpoint degrades with 503 in the standard error envelope — and that this does NOT
// affect GET /api/v1/health (independent code, health contract preserved).
func TestSystemHealthSemBancoDevolve503(t *testing.T) {
	s := New("v-teste", nil)

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", rec.Code)
	}
	checkErrorShape(t, rec, "db_unavailable")

	// The basic GET /health keeps answering 200, even without a database.
	basico := doHealth(t, s)
	if basico.Status != "ok" {
		t.Fatalf("GET /health status = %q, quero \"ok\" mesmo sem banco", basico.Status)
	}
}

// TestSystemHealthShapeCompleto proves that, with a database and at least one
// library_source, the endpoint returns 200 with database/usage/storage filled in, and that
// database.size_bytes matches a direct PRAGMA read at the same instant.
func TestSystemHealthShapeCompleto(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	dir := t.TempDir()
	semearLibrarySource(t, d, "src-1", dir)
	semearEntity(t, d, "e1", "movie", "Filme 1")
	semearFileComFonte(t, d, "f1", "e1", filepath.Join(dir, "f1.mkv"), "present", 12345, "src-1")

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar systemHealthResponse: %v", err)
	}

	var pageCount, pageSize int64
	if err := d.Read.QueryRowContext(context.Background(), "PRAGMA page_count").Scan(&pageCount); err != nil {
		t.Fatalf("PRAGMA page_count direto: %v", err)
	}
	if err := d.Read.QueryRowContext(context.Background(), "PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatalf("PRAGMA page_size direto: %v", err)
	}
	if out.Database.SizeBytes != pageCount*pageSize {
		t.Errorf("database.size_bytes = %d, quero %d (page_count*page_size)", out.Database.SizeBytes, pageCount*pageSize)
	}
	if out.Database.SchemaVersion != 27 {
		t.Errorf("database.schema_version = %d, quero 27", out.Database.SchemaVersion)
	}

	if len(out.Storage.Sources) != 1 {
		t.Fatalf("storage.sources = %d itens, quero 1", len(out.Storage.Sources))
	}
	src := out.Storage.Sources[0]
	if src.SourceID != "src-1" || src.Path != dir {
		t.Errorf("storage.sources[0] = %+v, quero source_id=src-1 path=%s", src, dir)
	}
	if src.UsedBytes != 12345 {
		t.Errorf("storage.sources[0].used_bytes = %d, quero 12345", src.UsedBytes)
	}
	if src.Offline {
		t.Error("storage.sources[0].offline = true, quero false (path existe)")
	}
	if src.Filesystem == nil || src.Filesystem.TotalBytes == 0 {
		t.Error("storage.sources[0].filesystem = nil/zero, quero preenchido (path real)")
	}
}

// TestSystemHealthEntitiesByType proves that usage.entities.by_type reflects the real
// counts per type of a mixed fixture (movie/season/episode/collection).
func TestSystemHealthEntitiesByType(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	semearEntity(t, d, "m1", "movie", "Filme 1")
	semearEntity(t, d, "m2", "movie", "Filme 2")
	semearEntity(t, d, "s1", "season", "Temporada 1")
	semearEntity(t, d, "ep1", "episode", "Episódio 1")
	semearEntity(t, d, "c1", "collection", "Coleção 1")

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	want := map[string]int64{"movie": 2, "season": 1, "episode": 1, "collection": 1}
	for tipo, total := range want {
		if out.Usage.Entities.ByType[tipo] != total {
			t.Errorf("entities.by_type[%s] = %d, quero %d", tipo, out.Usage.Entities.ByType[tipo], total)
		}
	}
	if len(out.Usage.Entities.ByType) != len(want) {
		t.Errorf("entities.by_type tem %d chaves, quero %d (so tipos que ocorrem)", len(out.Usage.Entities.ByType), len(want))
	}
	if out.Usage.Entities.Total != 5 {
		t.Errorf("entities.total = %d, quero 5", out.Usage.Entities.Total)
	}
}

// TestSystemHealthFilaBateComReviewEOrphans proves that
// usage.review_queue_size/orphans_count match EXACTLY the size that GET /review and
// GET /orphans would return for the SAME fixture (the same guarantee already proven for
// internal/curation).
func TestSystemHealthFilaBateComReviewEOrphans(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	// e-draft: unidentified -> it enters review.
	semearEntity(t, d, "e-draft", "movie", "Draft")
	definirStatus(t, d, "e-draft", "draft")

	// e-baixa: present with confidence 40 (< 70) -> it enters review.
	semearEntity(t, d, "e-baixa", "movie", "Baixa Confianca")
	semearFile(t, d, "f-baixa", "e-baixa", "/x/baixa.mkv", "present", 40, "")

	// e-alta: present with confidence 95 -> it enters neither review nor orphans.
	semearEntity(t, d, "e-alta", "movie", "Alta Confianca")
	semearFile(t, d, "f-alta", "e-alta", "/x/alta.mkv", "present", 95, "")

	// e-orfa: all files missing, no investment -> orphan.
	semearEntity(t, d, "e-orfa", "movie", "Orfa")
	semearFile(t, d, "f-orfa", "e-orfa", "/x/orfa.mkv", "missing", 0, "")

	// Calls /review and /orphans for real, to compare against the same fixture.
	recReview := httptest.NewRecorder()
	s.listReview(recReview, httptest.NewRequest(http.MethodGet, "/api/v1/review", nil))
	var review []entityCardDTO
	if err := json.Unmarshal(recReview.Body.Bytes(), &review); err != nil {
		t.Fatalf("decodificar /review: %v", err)
	}

	recOrphans := httptest.NewRecorder()
	s.listOrphans(recOrphans, httptest.NewRequest(http.MethodGet, "/api/v1/orphans", nil))
	var orphans []entityCardDTO
	if err := json.Unmarshal(recOrphans.Body.Bytes(), &orphans); err != nil {
		t.Fatalf("decodificar /orphans: %v", err)
	}

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	if out.Usage.ReviewQueueSize != len(review) {
		t.Errorf("usage.review_queue_size = %d, quero %d (== GET /review)", out.Usage.ReviewQueueSize, len(review))
	}
	if out.Usage.OrphansCount != len(orphans) {
		t.Errorf("usage.orphans_count = %d, quero %d (== GET /orphans)", out.Usage.OrphansCount, len(orphans))
	}
	// It pins the concrete numbers too, not only the cross equality.
	if len(review) != 2 {
		t.Errorf("len(review) = %d, quero 2 (draft + baixa confiança)", len(review))
	}
	if len(orphans) != 1 {
		t.Errorf("len(orphans) = %d, quero 1", len(orphans))
	}
}

// TestSystemHealthUsedBytesSemWalkDir proves that storage.sources[].used_bytes matches the
// sum of media_file.size of the source with state='present' — the "no WalkDir"
// criterion is structural (UsoDisco only runs a sqlc SUM, there is no call to
// filepath.WalkDir in the code path of this endpoint, see
// internal/telemetry/telemetry.go); here we prove the number matches even with missing
// files and files from another source mixed in.
func TestSystemHealthUsedBytesSemWalkDir(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	dirA := t.TempDir()
	dirB := t.TempDir()
	semearLibrarySource(t, d, "src-a", dirA)
	semearLibrarySource(t, d, "src-b", dirB)

	semearEntity(t, d, "ea1", "movie", "A1")
	semearEntity(t, d, "ea2", "movie", "A2")
	semearEntity(t, d, "eb1", "movie", "B1")
	semearFileComFonte(t, d, "fa1", "ea1", filepath.Join(dirA, "a1.mkv"), "present", 1000, "src-a")
	semearFileComFonte(t, d, "fa2", "ea2", filepath.Join(dirA, "a2.mkv"), "missing", 5000, "src-a")
	semearFileComFonte(t, d, "fb1", "eb1", filepath.Join(dirB, "b1.mkv"), "present", 2000, "src-b")

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	got := map[string]int64{}
	for _, src := range out.Storage.Sources {
		got[src.SourceID] = src.UsedBytes
	}
	if got["src-a"] != 1000 {
		t.Errorf("used_bytes[src-a] = %d, quero 1000 (so o present)", got["src-a"])
	}
	if got["src-b"] != 2000 {
		t.Errorf("used_bytes[src-b] = %d, quero 2000", got["src-b"])
	}
}

// TestSystemHealthFonteOfflineNaoAbortaAsDemais proves that a source with an unreachable
// Path shows up with offline:true/filesystem:null (with the error field filled in),
// WITHOUT aborting the other sources nor returning 5xx for the whole endpoint.
func TestSystemHealthFonteOfflineNaoAbortaAsDemais(t *testing.T) {
	s, d := setupSystemHealthServer(t)

	dirOK := t.TempDir()
	semearLibrarySource(t, d, "src-ok", dirOK)
	semearLibrarySource(t, d, "src-offline", "/caminho/que/definitivamente/nao/existe/xyz123")

	rec := doSystemHealth(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 mesmo com uma fonte offline (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	if len(out.Storage.Sources) != 2 {
		t.Fatalf("storage.sources = %d itens, quero 2 (as duas fontes, nenhuma abortada)", len(out.Storage.Sources))
	}
	byID := map[string]sourceStorageDTO{}
	for _, src := range out.Storage.Sources {
		byID[src.SourceID] = src
	}

	ok := byID["src-ok"]
	if ok.Offline || ok.Filesystem == nil {
		t.Errorf("src-ok = %+v, quero offline=false com filesystem preenchido", ok)
	}

	off := byID["src-offline"]
	if !off.Offline {
		t.Errorf("src-offline.offline = false, quero true")
	}
	if off.Filesystem != nil {
		t.Errorf("src-offline.filesystem = %+v, quero nil", off.Filesystem)
	}
	if off.Error == "" {
		t.Error("src-offline.error vazio, quero mensagem de erro")
	}
}

// TestSystemHealthCacheDentroDoTTLNaoRecalcula proves that two calls within the TTL return
// the SAME computed_at (the second request did not recalculate), and that a call after the
// TTL expires returns a new computed_at.
func TestSystemHealthCacheDentroDoTTLNaoRecalcula(t *testing.T) {
	s, d := setupSystemHealthServer(t)
	semearLibrarySource(t, d, "src-1", t.TempDir())

	// Controlled clock: the test advances the time manually, with no real sleep.
	agora := time.Now()
	s.telemetryCache.agora = func() time.Time { return agora }

	rec1 := doSystemHealth(t, s)
	var out1 systemHealthResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &out1); err != nil {
		t.Fatalf("decodificar 1a chamada: %v", err)
	}

	// Still within the TTL (it advances far less than ttlTelemetria).
	agora = agora.Add(1 * time.Second)
	rec2 := doSystemHealth(t, s)
	var out2 systemHealthResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &out2); err != nil {
		t.Fatalf("decodificar 2a chamada: %v", err)
	}
	if out1.ComputedAt != out2.ComputedAt {
		t.Errorf("computed_at mudou dentro do TTL: %q -> %q, queria igual (cache hit)", out1.ComputedAt, out2.ComputedAt)
	}

	// Expires the TTL: a new call must recalculate. ComputedAt comes from the REAL CLOCK
	// (telemetry.Servico.Snapshot uses time.Now(), independently of the mocked now of the
	// cache, which only controls the expiration) and is truncated to milliseconds in the
	// serialization -- a short real sleep avoids the rare coincidence of two calls landing
	// in the same millisecond.
	time.Sleep(2 * time.Millisecond)
	agora = agora.Add(ttlTelemetria + time.Second)
	rec3 := doSystemHealth(t, s)
	var out3 systemHealthResponse
	if err := json.Unmarshal(rec3.Body.Bytes(), &out3); err != nil {
		t.Fatalf("decodificar 3a chamada: %v", err)
	}
	if out3.ComputedAt == out1.ComputedAt {
		t.Error("computed_at não mudou após o TTL expirar, queria um novo cálculo")
	}
}

// TestSystemHealthNaoAlteraHealthBasico is a structural proof complementary to the 503
// above: with the database up, GET /health keeps the old shape (status/version/db) even
// after hitting /health/system — the two handlers do not share state that leaks a
// contract.
func TestSystemHealthNaoAlteraHealthBasico(t *testing.T) {
	s, d := setupSystemHealthServer(t)
	semearLibrarySource(t, d, "src-1", t.TempDir())

	_ = doSystemHealth(t, s)

	basico := doHealth(t, s)
	if basico.Status != "ok" || basico.Version != "v-teste" || !basico.DB.OK || basico.DB.Schema != 27 {
		t.Errorf("GET /health mudou de shape/valor apos /health/system: %+v", basico)
	}
}
