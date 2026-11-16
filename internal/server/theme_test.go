package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/ytdlp"
)

// Integration tests of the automatic theme song (ThemerrDB + yt-dlp, ADR-0025):
// GET .../theme/candidato and POST .../theme/download. The candidate is NEVER fetched live by
// these handlers -- it is read only from media_external (written by enrich,
// internal/enrich/tema_musical.go), so the tests seed the row directly, without needing a
// registered fake provider.Provider (unlike trailer_test.go, which needs the fake to feed
// `videos`). It reuses escreverYtdlpFakeDownload from trailer_test.go (same package) for the
// fake "yt-dlp" through a shell script.

// setupThemeServer starts a Server with NO provider registered (it is not needed: the already
// cached candidate is read straight from the database) and, when ytdlpBin != "", injects a
// ytdlp.Gerenciador with that customized path -- the same pattern as setupTrailerServer
// (trailer_test.go), including the same caveat of applying SetCustomPath only AFTER Handler()
// (the boot synchronizes and would clear a value set before).
func setupThemeServer(t *testing.T, ytdlpBin string) (http.Handler, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "theme.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var opts []Option
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
		g.SetCustomPath(ytdlpBin)
	}
	return h, database
}

// semearCandidatoTema writes the theme song candidate envelope into
// media_external(source=themerrdb) -- the SAME shape enrich.persistirCandidatoTema writes
// (provider.ChaveEnvelopeTemaMusical → URL). semMatch=true writes the EMPTY envelope (the
// candidate was queried, no match found -- enrich writes it even without finding one).
func semearCandidatoTema(t *testing.T, d *db.DB, entityID, url string, semMatch bool) {
	t.Helper()
	corpo := map[string]any{}
	if !semMatch {
		corpo[provider.ChaveEnvelopeTemaMusical] = url
	}
	payload, err := json.Marshal(corpo)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if _, err := escrita(t, d).UpsertMediaExternal(context.Background(), sqlcgen.UpsertMediaExternalParams{
		ID: "me-tema-" + entityID, EntityID: entityID, Source: "themerrdb",
		SourceID: nsOf(""), PayloadJson: nsOf(string(payload)),
	}); err != nil {
		t.Fatalf("UpsertMediaExternal: %v", err)
	}
}

// ── GET .../theme/candidato ──────────────────────────────────────────────

// TestGetThemeCandidatoEntidadeInexistente404 covers the missing entity.
func TestGetThemeCandidatoEntidadeInexistente404(t *testing.T) {
	h, _ := setupThemeServer(t, "")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/inexistente/theme/candidato", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestGetThemeCandidatoSemCandidatoNemArquivo covers the common case (ThemerrDB coverage is
// partial): with no media_external row at all -> disponivel=false, ja_tem_local=false, no
// error.
func TestGetThemeCandidatoSemCandidatoNemArquivo(t *testing.T) {
	h, d := setupThemeServer(t, "")
	semearEntity(t, d, "e1", "movie", "Sem Tema")
	libDirComVideo(t, d, "e1", "mf1", "Filme.2020.mkv")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme/candidato", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out themeCandidatoResult
	decodeInto(t, rec, &out)
	if out.Disponivel {
		t.Error("disponivel = true, quero false (sem candidato semeado)")
	}
	if out.JaTemLocal {
		t.Error("ja_tem_local = true, quero false (sem theme.* na pasta)")
	}
}

// TestGetThemeCandidatoEnvelopeSemMatchNaoConfundeComDisponivel covers the "queried, no match"
// envelope (written by enrich even without finding one): disponivel stays false.
func TestGetThemeCandidatoEnvelopeSemMatchNaoConfundeComDisponivel(t *testing.T) {
	h, d := setupThemeServer(t, "")
	semearEntity(t, d, "e1", "movie", "Sem Match")
	libDirComVideo(t, d, "e1", "mf1", "Filme.2020.mkv")
	semearCandidatoTema(t, d, "e1", "", true)

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme/candidato", nil)
	var out themeCandidatoResult
	decodeInto(t, rec, &out)
	if out.Disponivel {
		t.Error("disponivel = true, quero false (envelope sem match)")
	}
}

// TestGetThemeCandidatoDisponivelSemVazarURL covers criterion §6: candidate present ->
// disponivel=true, but the YouTube URL NEVER appears in the response body (the download happens
// server-side).
func TestGetThemeCandidatoDisponivelSemVazarURL(t *testing.T) {
	h, d := setupThemeServer(t, "")
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")
	semearCandidatoTema(t, d, "e1", "https://www.youtube.com/watch?v=matrix-theme", false)

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme/candidato", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out themeCandidatoResult
	decodeInto(t, rec, &out)
	if !out.Disponivel {
		t.Error("disponivel = false, quero true (candidato semeado)")
	}
	if out.JaTemLocal {
		t.Error("ja_tem_local = true, quero false (nenhum theme.* na pasta ainda)")
	}
	if corpo := rec.Body.String(); strings.Contains(corpo, "youtube.com") {
		t.Errorf("corpo vazou a URL do YouTube: %s", corpo)
	}
}

// TestGetThemeCandidatoJaTemLocalTrue covers an already present theme.mp3 (owner OR a previous
// download): ja_tem_local=true regardless of there being a candidate.
func TestGetThemeCandidatoJaTemLocalTrue(t *testing.T) {
	h, d := setupThemeServer(t, "")
	semearEntity(t, d, "e1", "movie", "Matrix")
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")
	if err := os.WriteFile(filepath.Join(dir, "theme.mp3"), []byte("theme-do-dono"), 0o644); err != nil {
		t.Fatalf("WriteFile theme.mp3: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme/candidato", nil)
	var out themeCandidatoResult
	decodeInto(t, rec, &out)
	if !out.JaTemLocal {
		t.Error("ja_tem_local = false, quero true (theme.mp3 do dono presente)")
	}
}

// ── POST .../theme/download ──────────────────────────────────────────────

// TestPostThemeDownloadSemCandidato422 covers criterion §5: without a candidate (no
// media_external row) -> 422, nothing written to disk.
func TestPostThemeDownloadSemCandidato422(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupThemeServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Sem Candidato")
	dir := libDirComVideo(t, d, "e1", "mf1", "Filme.2020.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/theme/download", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "theme.mp3")); err == nil {
		t.Error("theme.mp3 foi escrito, quero disco intocado (sem candidato)")
	}
}

// TestPostThemeDownloadSemYtdlp503 covers criterion §5: without yt-dlp available -> 503, nothing
// written to disk, even with a candidate present.
func TestPostThemeDownloadSemYtdlp503(t *testing.T) {
	h, d := setupThemeServer(t, "")
	semearEntity(t, d, "e1", "movie", "Matrix")
	semearCandidatoTema(t, d, "e1", "https://www.youtube.com/watch?v=matrix-theme", false)
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/theme/download", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "theme.mp3")); err == nil {
		t.Error("theme.mp3 foi escrito, quero disco intocado (yt-dlp indisponivel)")
	}
}

// TestPostThemeDownloadSucessoEGravaThemeMp3 covers criterion §5: success writes theme.mp3 at
// the root of the folder -- the SAME path that GET .../theme already serves -- without leaving an
// orphan .tmp.
func TestPostThemeDownloadSucessoEGravaThemeMp3(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupThemeServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	semearCandidatoTema(t, d, "e1", "https://www.youtube.com/watch?v=matrix-theme", false)
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/theme/download", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var out themeDownloadResult
	decodeInto(t, rec, &out)
	quero := filepath.Join(dir, "theme.mp3")
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
}

// TestPostThemeDownloadJaExisteLocalSemForce409 covers criterion §5: an already present
// theme.mp3 (downloaded before OR placed by hand by the owner) -> 409, the original file intact
// byte by byte. This endpoint has NO `force`: the only way to re-download is the owner deleting
// the file beforehand.
func TestPostThemeDownloadJaExisteLocalSemForce409(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupThemeServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	semearCandidatoTema(t, d, "e1", "https://www.youtube.com/watch?v=matrix-theme", false)
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	manual := filepath.Join(dir, "theme.mp3")
	if err := os.WriteFile(manual, []byte("theme-do-dono"), 0o644); err != nil {
		t.Fatalf("WriteFile manual: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/theme/download", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, quero 409 (corpo=%s)", rec.Code, rec.Body.String())
	}
	conteudo, err := os.ReadFile(manual)
	if err != nil {
		t.Fatalf("ler arquivo manual: %v", err)
	}
	if string(conteudo) != "theme-do-dono" {
		t.Errorf("conteudo = %q, quero intacto %q (nunca sobrescrever)", conteudo, "theme-do-dono")
	}
}

// TestPostThemeDownloadFalhaDeRede502 covers the failure of the download itself (network, video
// removed): nothing written under the canonical name, without an orphan .tmp.
func TestPostThemeDownloadFalhaDeRede502(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupThemeServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	// A URL containing "falha" triggers the error output of the fake script (the same trick as
	// trailer_test.go).
	semearCandidatoTema(t, d, "e1", "https://www.youtube.com/watch?v=falha", false)
	dir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/theme/download", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, quero 502 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "theme.mp3")); err == nil {
		t.Error("theme.mp3 foi escrito apos falha, quero disco intocado")
	}
	if _, err := os.Stat(filepath.Join(dir, "theme.mp3.tmp")); err == nil {
		t.Error("theme.mp3.tmp orfao apos falha, quero removido")
	}
}

// TestPostThemeDownloadEntidadeInexistente404 covers the missing entity.
func TestPostThemeDownloadEntidadeInexistente404(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, _ := setupThemeServer(t, ytBin)

	rec := do(t, h, http.MethodPost, "/api/v1/entities/inexistente/theme/download", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// ── Regression: GET /entities/{id}/theme ─────────────────────────────────

// TestGetEntityThemeContratoInalteradoAposDownloadAutomatico is the EXPLICIT regression test
// requested by the automatic theme design: after the automatic download through ThemerrDB writes
// theme.mp3, the OLD endpoint -- GET /entities/{id}/theme -- keeps serving the binary exactly as
// it would serve a theme.mp3 placed by hand, with the SAME content-type and with no contract
// change (no new field, no redirect).
func TestGetEntityThemeContratoInalteradoAposDownloadAutomatico(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupThemeServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	semearCandidatoTema(t, d, "e1", "https://www.youtube.com/watch?v=matrix-theme", false)
	libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	recDownload := do(t, h, http.MethodPost, "/api/v1/entities/e1/theme/download", nil)
	if recDownload.Code != http.StatusOK {
		t.Fatalf("download status = %d, quero 200 (corpo=%s)", recDownload.Code, recDownload.Body.String())
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/theme", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /theme status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q, quero audio/mpeg (contrato do briefing 0082 inalterado)", ct)
	}
	if rec.Body.String() != "fake-mp4-bytes" {
		t.Errorf("corpo = %q, quero %q", rec.Body.String(), "fake-mp4-bytes")
	}

	// HEAD keeps working -- an existence check without downloading the whole binary, a contract
	// the frontend uses today.
	recHead := do(t, h, http.MethodHead, "/api/v1/entities/e1/theme", nil)
	if recHead.Code != http.StatusOK {
		t.Fatalf("HEAD /theme status = %d, quero 200", recHead.Code)
	}
}
