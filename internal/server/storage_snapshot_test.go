package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// setupStorageSnapshotServer starts a Server with a migrated database, the saude_biblioteca
// job registered (WithLibraryHealthJob) AND a real assetsDir (WithAssetsDir) -- the combination
// the differentiated storage tests need (setupLibraryHealthServer, in
// library_health_job_test.go, does not configure assetsDir; setupSystemHealthServer, in
// health_system_test.go, does not register the job).
func setupStorageSnapshotServer(t *testing.T) (*Server, *db.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "storage-snapshot.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	assetsDir := filepath.Join(t.TempDir(), "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll assetsDir: %v", err)
	}
	s := New("v-teste", database, WithLibraryHealthJob(), WithAssetsDir(assetsDir))
	configurarFilaDeTeste(t, s)
	return s, database, assetsDir
}

// escreverArquivoDeTamanho creates `path` with exactly `n` bytes.
func escreverArquivoDeTamanho(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rodarSaudeBibliotecaSincrono fires POST .../jobs/saude_biblioteca/run and waits for it to
// finish (the same pattern as TestWithLibraryHealthJobWiring, in library_health_job_test.go) --
// after that, the job already classified the non-video files of every registered source and
// s.libraryHealth.UltimoResultadoStorage is populated.
func rodarSaudeBibliotecaSincrono(t *testing.T, s *Server) {
	t.Helper()
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	dispararRun(t, h, "saude_biblioteca")
	aguardarJobEstado(t, s, "saude_biblioteca", false)
}

// TestStorageSnapshotArquivoEstranhoViraUnmanaged covers the central acceptance criterion: a
// strange file (sample.txt) in a tracked movie folder shows up counted in unmanaged_bytes, and
// NOT in managed_media_bytes, after the health job runs.
func TestStorageSnapshotArquivoEstranhoViraUnmanaged(t *testing.T) {
	s, d, _ := setupStorageSnapshotServer(t)

	root := t.TempDir()
	pasta := filepath.Join(root, "Filme Teste (2020)")
	escreverArquivoDeTamanho(t, filepath.Join(pasta, "Filme Teste (2020).mkv"), 999)
	escreverArquivoDeTamanho(t, filepath.Join(pasta, "sample.txt"), 333)

	if _, err := library.New(d).Criar(context.Background(), root, "movie", "", nil, nil); err != nil {
		t.Fatalf("criar library_source: %v", err)
	}

	rodarSaudeBibliotecaSincrono(t, s)

	rec := doSystemHealth(t, s)
	if rec.Code != 200 {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	if out.Storage.UnmanagedBytes != 333 {
		t.Errorf("storage.unmanaged_bytes = %d, quero 333 (sample.txt)", out.Storage.UnmanagedBytes)
	}
	if out.Storage.ManagedMediaBytes != 999 {
		t.Errorf("storage.managed_media_bytes = %d, quero 999 (só o vídeo -- sample.txt NÃO deveria contar aqui)", out.Storage.ManagedMediaBytes)
	}
}

// TestStorageSnapshotPosterCanonicoViraManaged covers the second central criterion: a
// poster.jpg written under the canonical convention is counted in managed_media_bytes, not in
// unmanaged_bytes.
func TestStorageSnapshotPosterCanonicoViraManaged(t *testing.T) {
	s, d, _ := setupStorageSnapshotServer(t)

	root := t.TempDir()
	pasta := filepath.Join(root, "Filme Com Poster (2021)")
	escreverArquivoDeTamanho(t, filepath.Join(pasta, "Filme Com Poster (2021).mkv"), 1000)
	escreverArquivoDeTamanho(t, filepath.Join(pasta, "poster.jpg"), 777)

	if _, err := library.New(d).Criar(context.Background(), root, "movie", "", nil, nil); err != nil {
		t.Fatalf("criar library_source: %v", err)
	}

	rodarSaudeBibliotecaSincrono(t, s)

	rec := doSystemHealth(t, s)
	if rec.Code != 200 {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	if out.Storage.ManagedMediaBytes != 1777 {
		t.Errorf("storage.managed_media_bytes = %d, quero 1777 (1000 vídeo + 777 poster.jpg)", out.Storage.ManagedMediaBytes)
	}
	if out.Storage.UnmanagedBytes != 0 {
		t.Errorf("storage.unmanaged_bytes = %d, quero 0 (poster.jpg não deveria contar aqui)", out.Storage.UnmanagedBytes)
	}
}

// TestStorageSnapshotAppCacheBytesReflcteAssetsDir covers the third criterion: app_cache_bytes
// reflects the REAL size of assetsDir, with a file of known size -- without depending on the
// health job (assetsDir is computed directly by internal/telemetry.TamanhoDiretorio on every
// call, see health_system.go).
func TestStorageSnapshotAppCacheBytesReflcteAssetsDir(t *testing.T) {
	s, _, assetsDir := setupStorageSnapshotServer(t)

	escreverArquivoDeTamanho(t, filepath.Join(assetsDir, "poster-cache-1.jpg"), 555)

	rec := doSystemHealth(t, s)
	if rec.Code != 200 {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	if out.Storage.AppCacheBytes != 555 {
		t.Errorf("storage.app_cache_bytes = %d, quero 555", out.Storage.AppCacheBytes)
	}
}

// TestStorageSnapshotSemJobRegistradoDegradaParaZero proves that, without
// WithLibraryHealthJob (s.libraryHealth == nil), managed_media_bytes reflects only the indexed
// video (through UsoDisco) and unmanaged_bytes stays 0 -- a graceful degradation, the same
// defensive spirit as the rest of the endpoint.
func TestStorageSnapshotSemJobRegistradoDegradaParaZero(t *testing.T) {
	s, d := setupSystemHealthServer(t) // does NOT pass WithLibraryHealthJob

	dir := t.TempDir()
	semearLibrarySource(t, d, "src-1", dir)
	semearEntity(t, d, "e1", "movie", "Filme 1")
	semearFileComFonte(t, d, "f1", "e1", filepath.Join(dir, "f1.mkv"), "present", 12345, "src-1")

	rec := doSystemHealth(t, s)
	if rec.Code != 200 {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	if out.Storage.ManagedMediaBytes != 12345 {
		t.Errorf("storage.managed_media_bytes = %d, quero 12345 (só vídeo indexado, sem job registrado)", out.Storage.ManagedMediaBytes)
	}
	if out.Storage.UnmanagedBytes != 0 {
		t.Errorf("storage.unmanaged_bytes = %d, quero 0 (sem job registrado)", out.Storage.UnmanagedBytes)
	}
	if out.Storage.AppCacheBytes != 0 {
		t.Errorf("storage.app_cache_bytes = %d, quero 0 (sem assetsDir configurado)", out.Storage.AppCacheBytes)
	}
}
