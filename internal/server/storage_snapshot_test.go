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

// setupStorageSnapshotServer sobe um Server com banco migrado, o job
// saude_biblioteca registrado (WithLibraryHealthJob) E um assetsDir real
// (WithAssetsDir) -- a combinação que os testes de storage diferenciado do
// briefing 0063 precisam (setupLibraryHealthServer, em
// library_health_job_test.go, não configura assetsDir; setupSystemHealthServer,
// em health_system_test.go, não registra o job).
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
	return s, database, assetsDir
}

// escreverArquivoDeTamanho cria `path` com exatamente `n` bytes.
func escreverArquivoDeTamanho(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rodarSaudeBibliotecaSincrono dispara POST .../jobs/saude_biblioteca/run e
// espera terminar (mesmo padrão de TestWithLibraryHealthJobWiring, em
// library_health_job_test.go) -- depois disso, o job já classificou os
// arquivos não-vídeo de toda fonte cadastrada (briefing 0063) e
// s.libraryHealth.UltimoResultadoStorage está populado.
func rodarSaudeBibliotecaSincrono(t *testing.T, s *Server) {
	t.Helper()
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	dispararRun(t, h, "saude_biblioteca")
	aguardarJobEstado(t, s, "saude_biblioteca", false)
}

// TestStorageSnapshotArquivoEstranhoViraUnmanaged cobre o critério de aceite
// central do briefing 0063: um arquivo estranho (sample.txt) numa pasta de
// filme rastreada aparece computado em unmanaged_bytes, e NÃO em
// managed_media_bytes, depois da execução do job de saúde.
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

// TestStorageSnapshotPosterCanonicoViraManaged cobre o segundo critério
// central: um poster.jpg escrito na convenção canônica do briefing 0062 é
// computado em managed_media_bytes, não em unmanaged_bytes.
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

// TestStorageSnapshotAppCacheBytesReflcteAssetsDir cobre o terceiro
// critério: app_cache_bytes reflete o tamanho REAL de assetsDir, com arquivo
// de tamanho conhecido -- sem depender do job de saúde (assetsDir é
// calculado direto por internal/telemetry.TamanhoDiretorio a cada chamada,
// ver health_system.go).
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

// TestStorageSnapshotSemJobRegistradoDegradaParaZero prova que, sem
// WithLibraryHealthJob (s.libraryHealth == nil), managed_media_bytes reflete
// só o vídeo indexado (via UsoDisco) e unmanaged_bytes fica 0 -- degradação
// graciosa, mesmo espírito defensivo do resto do endpoint (briefing 0055).
func TestStorageSnapshotSemJobRegistradoDegradaParaZero(t *testing.T) {
	s, d := setupSystemHealthServer(t) // NÃO passa WithLibraryHealthJob

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
