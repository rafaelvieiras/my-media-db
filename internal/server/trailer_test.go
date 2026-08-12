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

// Testes de integração do briefing 0102 (trailer: materialização em disco via
// yt-dlp, ADR-0025): GET/POST .../trailer(/download). Reaproveita o MESMO
// padrão de fake provider dos testes de similar (similar_test.go) para
// alimentar `videos` sem bater em rede real, e um "yt-dlp" FAKE (script shell
// executável via SetCustomPath, mesmo truque de escreverScriptExecutavel em
// settings_test.go) para exercitar s.ytdlp.Baixar sem baixar nada de verdade.

// fakeProviderTrailer é um provider.Provider mínimo (Nome fixo "tmdb", para
// casar com media_external.source e prioridadeFontes) cujo MapearDetalhe é a
// IDENTIDADE (devolve o JSON cru já como o mapa canônico) — suficiente para
// alimentar richfields.VideosDe a partir de um payload de teste escrito à
// mão, sem precisar simular o payload real do TMDB.
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

// videosComTrailerJSON é o payload cru (já no formato canônico, graças ao
// MapearDetalhe identidade acima) de um item com um único trailer YouTube.
const videosComTrailerJSON = `{"videos":[{"name":"Trailer Oficial","key":"abc123","site":"YouTube","type":"Trailer","official":true,"url":"https://www.youtube.com/watch?v=abc123"}]}`

// escreverYtdlpFakeDownload grava um script shell executável que finge ser o
// yt-dlp: recebe os MESMOS args que ytdlp.Gerenciador.Baixar passa ("-o"
// destino url) e ESCREVE bytes fixos no destino, exceto quando a url contém
// "falha" (simula erro de rede/vídeo removido — saída não-zero, nada
// gravado). Roda direto via exec (shebang), sem precisar de binário real.
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

// setupTrailerServer sobe um Server com o fakeProviderTrailer registrado
// (fonte "tmdb") e, quando ytdlpBin != "", um ytdlp.Gerenciador com esse path
// CUSTOMIZADO (Resolver o devolve direto, sem validar existência — mesma
// regra de produção). ytdlpBin == "" deixa s.ytdlp nil (cenário "sem
// Gerenciador injetado").
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
		// O BOOT sincroniza o custom path com app_settings (linha ~415 de
		// server.go) -- como o banco não tem nenhuma linha `ytdlp` ainda,
		// essa sincronização LIMPA (SetCustomPath("")) qualquer path
		// setado ANTES de New(...). Por isso o path customizado do fake
		// yt-dlp só é aplicado APÓS o Handler() já ter sincronizado —
		// nenhum outro ponto do fluxo de teste invalida esse cache depois.
		g.SetCustomPath(ytdlpBin)
	}
	return h, database
}

// semearMediaExternalVideos grava a linha media_external(source='tmdb') com o
// payload de vídeos dado, para a entity já existir com `videos` resolvido.
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

// TestPostTrailerDownloadSemTrailer422 cobre o critério §5: item sem
// trailer YouTube (videos vazio) -> 422, nada escrito no disco.
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

// TestPostTrailerDownloadSemYtdlp503 cobre o critério §5: sem yt-dlp
// disponível (Gerenciador não injetado) -> 503, nada escrito no disco.
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

// TestPostTrailerDownloadSucessoIdempotenteENuncaSobrescreve cobre os
// critérios §5: sucesso grava no path canônico; segunda chamada sem `force`
// devolve 409 e NÃO altera o arquivo já gravado.
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

	// 2a chamada sem force -> 409, arquivo original intacto.
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

// TestPostTrailerDownloadTrailerManualNuncaSobrescrito cobre o critério §5:
// um trailer.mp4 colocado À MÃO pelo dono (nunca baixado por este endpoint)
// no MESMO nome canônico bloqueia o download sem `force`, igual a um já
// baixado por aqui.
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

	// Com force=true, agora sobrescreve.
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

// TestPostTrailerDownloadFalhaDeRede502 cobre o cenário de falha do próprio
// download (rede, vídeo removido): nada escrito no disco com o nome
// canônico, sem .tmp órfão.
func TestPostTrailerDownloadFalhaDeRede502(t *testing.T) {
	ytBin := escreverYtdlpFakeDownload(t, filepath.Join(t.TempDir(), "yt-dlp"))
	h, d := setupTrailerServer(t, ytBin)
	semearEntity(t, d, "e1", "movie", "Matrix")
	// url contendo "falha" dispara a saída de erro do script fake.
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

// TestGetEntityTrailerServeBinario cobre o critério §5: após o download, GET
// serve o binário com content-type coerente.
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

// TestGetEntityTrailerSemArquivo404 cobre o critério §5: sem trailer
// materializado -> 404.
func TestGetEntityTrailerSemArquivo404(t *testing.T) {
	h, d := setupTrailerServer(t, "")
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/trailer", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestGetEntityTrailerSemMediaFile404 cobre o mesmo guard de getEntityTheme:
// entity sem media_file present -> 404 (sem pasta de destino).
func TestGetEntityTrailerSemMediaFile404(t *testing.T) {
	h, d := setupTrailerServer(t, "")
	semearEntity(t, d, "e1", "movie", "Sem Arquivo")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e1/trailer", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}
