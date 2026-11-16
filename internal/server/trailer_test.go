package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/ytdlp"
)

// Integration tests of the trailer (materialization on disk via
// yt-dlp, ADR-0025): GET/POST .../trailer(/download). It reuses the SAME
// fake provider pattern of the similar tests (similar_test.go) to
// feed `videos` without hitting the real network, and a FAKE "yt-dlp" (shell script
// executable via SetCustomPath, same trick as escreverScriptExecutavel in
// settings_test.go) to exercise s.ytdlp.Baixar without downloading anything for
// real.

// fakeProviderTrailer is a minimal provider.Provider (fixed Nome "tmdb", to
// match media_external.source and prioridadeFontes) whose MapearDetalhe is the
// IDENTITY (it returns the raw JSON already as the canonical map) — enough to
// feed richfields.VideosDe from a test payload written by
// hand, without having to simulate the real TMDB payload.
type fakeProviderTrailer struct {
	nome string
}

func (f *fakeProviderTrailer) Nome() string { return f.nome }

func (f *fakeProviderTrailer) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (f *fakeProviderTrailer) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (f *fakeProviderTrailer) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// videosComTrailerJSON is the raw payload (already in the canonical format, thanks to
// the identity MapearDetalhe above) of an item with a single YouTube trailer.
const videosComTrailerJSON = `{"videos":[{"name":"Trailer Oficial","key":"abc123","site":"YouTube","type":"Trailer","official":true,"url":"https://www.youtube.com/watch?v=abc123"}]}`

// escreverYtdlpFakeDownload writes an executable shell script that pretends to be
// yt-dlp: it receives the SAME args that ytdlp.Gerenciador.Baixar passes ("-o"
// destination url) and WRITES fixed bytes at the destination, except when the url contains
// "falha" (it simulates a network error/removed video — non-zero exit, nothing
// written). It runs straight through exec (shebang), without needing a real binary.
func escreverYtdlpFakeDownload(t *testing.T, path string) string {
	t.Helper()
	script := "#!/bin/sh\n" +
		"dest=\"$2\"\n" +
		"url=\"$3\"\n" +
		"case \"$url\" in\n" +
		"  *falha*) echo 'erro de rede simulado' 1>&2; exit 1 ;;\n" +
		"esac\n" +
		"printf 'fake-mp4-bytes' > \"$dest\"\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile script fake yt-dlp: %v", err)
	}
	return path
}

// setupTrailerServer starts a Server with the fakeProviderTrailer registered
// (source "tmdb") and, when ytdlpBin != "", a ytdlp.Gerenciador with that
// CUSTOM path (Resolver returns it directly, without validating existence — same
// production rule). ytdlpBin == "" leaves s.ytdlp nil (the "no
// Gerenciador injected" scenario).
func setupTrailerServer(t *testing.T, ytdlpBin string) (http.Handler, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "trailer.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	opts := []Option{WithSearchProvider(&fakeProviderTrailer{nome: "tmdb"})}
	var g *ytdlp.Gerenciador
	if ytdlpBin != "" {
		g = ytdlp.Novo(t.TempDir(), "")
		opts = append(opts, WithYtdlp(g))
	}
	h, err := New("v-teste", database, opts...).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if g != nil {
		// The BOOT synchronizes the custom path with app_settings (around line 415 of
		// server.go) -- since the database has no `ytdlp` row yet,
		// that synchronization CLEARS (SetCustomPath("")) any path
		// set BEFORE New(...). That is why the custom path of the fake
		// yt-dlp is only applied AFTER Handler() has already synchronized —
		// no other point of the test flow invalidates that cache afterwards.
		g.SetCustomPath(ytdlpBin)
	}
	return h, database
}

// semearMediaExternalVideos writes the media_external(source='tmdb') row with the
// given videos payload, so the entity already exists with `videos` resolved.
func semearMediaExternalVideos(t *testing.T, d *db.DB, entityID, payload string) {
	t.Helper()
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: "me-" + entityID, EntityID: entityID, Source: "tmdb",
		SourceID: nsOf("603"), PayloadJson: nsOf(payload),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal: %v", err)
	}
}

// ── POST .../trailer/download ────────────────────────────────────────────

// TestPostTrailerDownloadSemTrailer422 covers the criterion: an item without a
// YouTube trailer (empty videos) -> 422, nothing written to disk.
func TestPostTrailerDownloadSemTrailer422(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupTrailerServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Sem Trailer")
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/trailer/download", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entradas {
		if filepath.Ext(e.Name()) != filepath.Ext("Matrix.1999.mkv") {
			t.Errorf("arquivo inesperado escrito no disco: %s", e.Name())
		}
	}
}

// TestPostTrailerDownloadSemYtdlp503 covers the criterion: without yt-dlp
// available (Gerenciador not injected) -> 503, nothing written to disk.
func TestPostTrailerDownloadSemYtdlp503(t *testing.T) {
	h, d := setupTrailerServer(t, "")
	semearEntity(t, d, "e1", "movie", "Matrix")
	semearMediaExternalVideos(t, d, "e1", videosComTrailerJSON)
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/trailer/download", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "trailer.mp4")); err == nil {
		t.Error("trailer.mp4 foi escrito, quero disco intocado (yt-dlp indisponível)")
	}
}

// TestPostTrailerDownloadSucessoIdempotenteENuncaSobrescreve covers the
// criteria: success writes to the canonical path; a second call without `force`
// returns 409 and does NOT change the already written file.
func TestPostTrailerDownloadSucessoIdempotenteENuncaSobrescreve(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupTrailerServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	semearMediaExternalVideos(t, d, "e1", videosComTrailerJSON)
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/trailer/download", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out trailerDownloadResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar corpo: %v (corpo=%s)", err, rec.Body.String())
	}
	quero := filepath.Join(dir, "trailer.mp4")
	if out.LocalPath != quero {
		t.Errorf("local_path = %q, quero %q", out.LocalPath, quero)
	}
	conteudo, err := os.ReadFile(quero)
	if err != nil {
		t.Fatalf("ler arquivo gravado: %v", err)
	}
	if string(conteudo) != "fake-mp4-bytes" {
		t.Errorf("conteudo = %q, quero %q", conteudo, "fake-mp4-bytes")
	}
	if _, err := os.Stat(quero + ".tmp"); err == nil {
		t.Error(".tmp orfao encontrado apos sucesso, quero removido/renomeado")
	}

	// 2nd call without force -> 409, original file intact.
	rec2 := do(t, h, http.MethodPost, "/api/v1/entities/e1/trailer/download", nil)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("2a chamada status = %d, quero 409 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	conteudo2, err := os.ReadFile(quero)
	if err != nil {
		t.Fatalf("ler arquivo apos 409: %v", err)
	}
	if string(conteudo2) != "fake-mp4-bytes" {
		t.Errorf("conteudo apos 409 = %q, quero intacto %q", conteudo2, "fake-mp4-bytes")
	}
}

// TestPostTrailerDownloadTrailerManualNuncaSobrescrito covers the criterion:
// a trailer.mp4 placed BY HAND by the owner (never downloaded by this endpoint)
// under the SAME canonical name blocks the download without `force`, just like one
// already downloaded from here.
func TestPostTrailerDownloadTrailerManualNuncaSobrescrito(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupTrailerServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	semearMediaExternalVideos(t, d, "e1", videosComTrailerJSON)
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	manual := filepath.Join(dir, "trailer.mp4")
	if err := os.WriteFile(manual, []byte("trailer-do-dono"), 0o644); err != nil {
		t.Fatalf("WriteFile manual: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/trailer/download", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, quero 409 (corpo=%s)", rec.Code, rec.Body.String())
	}
	conteudo, err := os.ReadFile(manual)
	if err != nil {
		t.Fatalf("ler arquivo manual: %v", err)
	}
	if string(conteudo) != "trailer-do-dono" {
		t.Errorf("conteudo = %q, quero intacto %q (nunca sobrescrever sem force)", conteudo, "trailer-do-dono")
	}

	// With force=true, it now overwrites.
	rec2 := do(t, h, http.MethodPost, "/api/v1/entities/e1/trailer/download", map[string]bool{"force": true})
	if rec2.Code != http.StatusOK {
		t.Fatalf("status com force = %d, quero 200 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	conteudo2, err := os.ReadFile(manual)
	if err != nil {
		t.Fatalf("ler arquivo apos force: %v", err)
	}
	if string(conteudo2) != "fake-mp4-bytes" {
		t.Errorf("conteudo apos force = %q, quero %q (re-baixado)", conteudo2, "fake-mp4-bytes")
	}
}

// TestPostTrailerDownloadFalhaDeRede502 covers the failure scenario of the download
// itself (network, removed video): nothing written to disk under the
// canonical name, without an orphan .tmp.
func TestPostTrailerDownloadFalhaDeRede502(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupTrailerServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	// a url containing "falha" triggers the error exit of the fake script.
	semearMediaExternalVideos(t, d, "e1", `{"videos":[{"name":"T","key":"abc","site":"YouTube","type":"Trailer","official":true,"url":"https://www.youtube.com/watch?v=falha"}]}`)
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/trailer/download", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "trailer.mp4")); err == nil {
		t.Error("trailer.mp4 foi escrito apos falha, quero disco intocado")
	}
	if _, err := os.Stat(filepath.Join(dir, "trailer.mp4.tmp")); err == nil {
		t.Error("trailer.mp4.tmp orfao apos falha, quero removido")
	}
}

// ── GET .../trailer ───────────────────────────────────────────────────────

// TestGetEntityTrailerServeBinario covers the criterion: after the download, GET
// serves the binary with a coherent content-type.
func TestGetEntityTrailerServeBinario(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupTrailerServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	semearMediaExternalVideos(t, d, "e1", videosComTrailerJSON)
	libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	recPost := do(t, h, http.MethodPost, "/api/v1/entities/e1/trailer/download", nil)
	if recPost.Code != http.StatusOK {
		t.Fatalf("download status = %d, quero 200 (corpo=%s)", recPost.Code, recPost.Body.String())
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/trailer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("Content-Type = %q, quero video/mp4", ct)
	}
	if rec.Body.String() != "fake-mp4-bytes" {
		t.Errorf("corpo = %q, quero %q", rec.Body.String(), "fake-mp4-bytes")
	}
}

// TestGetEntityTrailerSemArquivo404 covers the criterion: without a materialized
// trailer -> 404.
func TestGetEntityTrailerSemArquivo404(t *testing.T) {
	h, d := setupTrailerServer(t, "")
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/trailer", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestGetEntityTrailerSemMediaFile404 covers the same guard as getEntityTheme:
// entity without a present media_file -> 404 (no destination folder).
func TestGetEntityTrailerSemMediaFile404(t *testing.T) {
	h, d := setupTrailerServer(t, "")
	semearEntity(t, d, "e1", "movie", "Sem Arquivo")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/trailer", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}
