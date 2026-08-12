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

// Testes de integração do briefing 0103 (tema musical automático via
// ThemerrDB + yt-dlp, ADR-0025): GET .../theme/candidato e POST
// .../theme/download. O candidato NUNCA é buscado ao vivo por estes
// handlers -- é lido só de media_external (gravado pelo enrich,
// internal/enrich/tema_musical.go), então os testes semeiam a linha
// diretamente, sem precisar de um provider.Provider fake registrado (ao
// contrário de trailer_test.go, que precisa do fake pra alimentar `videos`).
// Reaproveita escreverYtdlpFakeDownload de trailer_test.go (mesmo pacote) pro
// "yt-dlp" fake via script shell.

// setupThemeServer sobe um Server sem NENHUM provider registrado (não é
// necessário: o candidato já cacheado é lido direto do banco) e, quando
// ytdlpBin != "", injeta um ytdlp.Gerenciador com esse path customizado --
// mesmo padrão de setupTrailerServer (trailer_test.go), incluindo a mesma
// ressalva de aplicar SetCustomPath só DEPOIS de Handler() (o boot sincroniza
// e limparia um valor setado antes).
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

// semearCandidatoTema grava o envelope de candidato de tema musical em
// media_external(source=themerrdb) -- MESMO shape que
// enrich.persistirCandidatoTema grava (provider.ChaveEnvelopeTemaMusical →
// URL). semMatch=true grava o envelope VAZIO (candidato consultado, sem
// match encontrado -- o enrich grava assim mesmo sem achar, briefing §3).
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

// TestGetThemeCandidatoEntidadeInexistente404 cobre a entity ausente.
func TestGetThemeCandidatoEntidadeInexistente404(t *testing.T) {
	h, _ := setupThemeServer(t, "")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/inexistente/theme/candidato", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestGetThemeCandidatoSemCandidatoNemArquivo cobre o caso comum (cobertura
// do ThemerrDB é parcial, briefing §6): sem linha media_external nenhuma ->
// disponivel=false, ja_tem_local=false, sem erro.
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

// TestGetThemeCandidatoEnvelopeSemMatchNaoConfundeComDisponivel cobre o
// envelope "consultado, sem match" (gravado pelo enrich mesmo sem achar,
// briefing §3): disponivel continua false.
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

// TestGetThemeCandidatoDisponivelSemVazarURL cobre o critério §6: candidato
// presente -> disponivel=true, mas a URL do YouTube NUNCA aparece no corpo da
// resposta (o download acontece server-side).
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

// TestGetThemeCandidatoJaTemLocalTrue cobre um theme.mp3 já presente (dono OU
// download anterior): ja_tem_local=true independente de haver candidato.
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

// TestPostThemeDownloadSemCandidato422 cobre o critério §5: sem candidato
// (nenhuma linha media_external) -> 422, nada escrito no disco.
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

// TestPostThemeDownloadSemYtdlp503 cobre o critério §5: sem yt-dlp disponível
// -> 503, nada escrito no disco, mesmo com candidato presente.
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

// TestPostThemeDownloadSucessoEGravaThemeMp3 cobre o critério §5: sucesso
// grava theme.mp3 na raiz da pasta -- o MESMO path que GET .../theme já
// serve (0082) -- sem deixar .tmp órfão.
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

// TestPostThemeDownloadJaExisteLocalSemForce409 cobre o critério §5: um
// theme.mp3 já presente (baixado antes OU colocado à mão pelo dono) -> 409,
// arquivo original intacto byte-a-byte. Este endpoint NÃO tem `force`
// (briefing §6): a única forma de re-baixar é o dono apagar o arquivo antes.
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

// TestPostThemeDownloadFalhaDeRede502 cobre a falha do próprio download
// (rede, vídeo removido): nada escrito com o nome canônico, sem .tmp órfão.
func TestPostThemeDownloadFalhaDeRede502(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupThemeServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	// URL contendo "falha" dispara a saida de erro do script fake (mesmo
	// truque de trailer_test.go).
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

// TestPostThemeDownloadEntidadeInexistente404 cobre a entity ausente.
func TestPostThemeDownloadEntidadeInexistente404(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, _ := setupThemeServer(t, ytBin)

	rec := do(t, h, http.MethodPost, "/api/v1/entities/inexistente/theme/download", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// ── Regressão: GET /entities/{id}/theme (briefing 0082) ─────────────────

// TestGetEntityThemeContratoInalteradoAposDownloadAutomatico é o teste de
// regressão EXPLÍCITO pedido pelo briefing 0103 §5: depois que o download
// automático via ThemerrDB grava theme.mp3, o endpoint ANTIGO (0082) --
// GET /entities/{id}/theme -- continua servindo o binário exatamente como
// serviria um theme.mp3 colocado à mão, com o MESMO content-type e sem
// nenhuma mudança de contrato (nenhum campo novo, nenhum redirecionamento).
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

	// HEAD continua funcionando (0082) -- checagem de existência sem baixar
	// o binário inteiro, contrato que o front usa hoje.
	recHead := do(t, h, http.MethodHead, "/api/v1/entities/e1/theme", nil)
	if recHead.Code != http.StatusOK {
		t.Fatalf("HEAD /theme status = %d, quero 200", recHead.Code)
	}
}
