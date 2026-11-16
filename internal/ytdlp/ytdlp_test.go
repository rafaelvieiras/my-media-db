package ytdlp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// scriptFakeConteudo is a minimal POSIX script that behaves like the real yt-dlp
// just enough for the tests: it answers "--version" successfully (the validation
// floor of baixarBinarioLocked) and, for any other argument, exits successfully
// without doing anything (used as the "resolved binary" in the Baixar tests). It
// NEVER hits the real internet -- every server used in these tests is a local
// httptest.Server.
const scriptFakeConteudo = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo \"2024.01.01\"\n  exit 0\nfi\nexit 0\n"

// escreverScriptFake writes scriptFakeConteudo into `path`, already executable.
func escreverScriptFake(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(scriptFakeConteudo), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// TestResolverCustomPathEnvVenceSemBaixar covers the acceptance criterion: with
// MYMEDIADB_YTDLP_PATH set (env, here already resolved and passed to Novo),
// Resolver/Status use that path without trying to download (the release API is
// never queried).
func TestResolverCustomPathEnvVenceSemBaixar(t *testing.T) {
	dir := t.TempDir()
	customPath := escreverScriptFake(t, filepath.Join(t.TempDir(), "meu-yt-dlp"))

	var chamadas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&chamadas, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	g := Novo(dir, customPath, ComReleaseAPIURL(srv.URL))

	bin, err := g.Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver: %v", err)
	}
	if bin != customPath {
		t.Errorf("Resolver() = %q, quero %q (path customizado)", bin, customPath)
	}
	if atomic.LoadInt32(&chamadas) != 0 {
		t.Errorf("bateu na API de release mesmo com path customizado, chamadas=%d", chamadas)
	}

	disponivel, origem, detalhe := g.Status(context.Background())
	if !disponivel || origem != "custom" || detalhe != "" {
		t.Errorf("Status() = (%v, %q, %q), quero (true, custom, \"\")", disponivel, origem, detalhe)
	}
}

// TestSetCustomPathBancoVenceSobreEnv covers the acceptance criterion: with
// custom_binary_path written via PUT /settings/ytdlp (database, here simulated by
// SetCustomPath), that path wins over the env var -- and clearing it
// (SetCustomPath("")) hands control back to the env var, all at RUNTIME, without
// rebuilding the Gerenciador (equivalent to "without restarting the process").
func TestSetCustomPathBancoVenceSobreEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := escreverScriptFake(t, filepath.Join(t.TempDir(), "env-yt-dlp"))
	dbPath := escreverScriptFake(t, filepath.Join(t.TempDir(), "db-yt-dlp"))

	g := Novo(dir, envPath)

	bin, err := g.Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver (antes do banco): %v", err)
	}
	if bin != envPath {
		t.Fatalf("Resolver() = %q, quero %q (env, banco ainda nao configurado)", bin, envPath)
	}

	g.SetCustomPath(dbPath)
	bin2, err := g.Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver (com banco): %v", err)
	}
	if bin2 != dbPath {
		t.Errorf("Resolver() = %q, quero %q (banco vence sobre env)", bin2, dbPath)
	}

	g.SetCustomPath("")
	bin3, err := g.Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver (banco limpo): %v", err)
	}
	if bin3 != envPath {
		t.Errorf("Resolver() = %q, quero %q (volta para env apos limpar o banco)", bin3, envPath)
	}
}

// TestStatusCustomPathInacessivel covers the case of a custom path pointing to a
// file that does not exist: Status reports unavailable with a reason, instead of
// pretending everything is fine.
func TestStatusCustomPathInacessivel(t *testing.T) {
	dir := t.TempDir()
	g := Novo(dir, filepath.Join(t.TempDir(), "nao-existe"))

	disponivel, origem, detalhe := g.Status(context.Background())
	if disponivel {
		t.Error("disponivel = true, quero false (path customizado nao existe)")
	}
	if origem != "custom" {
		t.Errorf("origem = %q, quero custom", origem)
	}
	if detalhe == "" {
		t.Error("detalhe vazio, quero o motivo (path customizado inacessivel)")
	}
}

// TestResolverBaixaNaPrimeiraChamadaEReusa covers the acceptance criterion:
// without custom_binary_path and without a binary in the cache, the first call to
// Resolver downloads yt-dlp (through a local FAKE HTTP server, never the real
// internet), validates it with --version and reuses it in the following calls --
// both in the SAME Gerenciador (in-memory cache) and in a NEW Gerenciador
// pointing to the same directory (simulating a process restart, on-disk cache),
// without re-downloading.
func TestResolverBaixaNaPrimeiraChamadaEReusa(t *testing.T) {
	dir := t.TempDir()
	var chamadasRelease, chamadasBinario int32
	var srv *httptest.Server

	mux := http.NewServeMux()
	mux.HandleFunc("/release", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&chamadasRelease, 1)
		_, _ = fmt.Fprintf(w, `{"assets":[{"name":"yt-dlp","browser_download_url":%q}]}`, srv.URL+"/binario")
	})
	mux.HandleFunc("/binario", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&chamadasBinario, 1)
		_, _ = w.Write([]byte(scriptFakeConteudo))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	g := Novo(dir, "", ComReleaseAPIURL(srv.URL+"/release"))

	wantBin := filepath.Join(dir, "yt-dlp")
	bin, err := g.Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver (1a chamada): %v", err)
	}
	if bin != wantBin {
		t.Errorf("Resolver() = %q, quero %q", bin, wantBin)
	}
	if got := atomic.LoadInt32(&chamadasRelease); got != 1 {
		t.Errorf("chamadasRelease = %d, quero 1", got)
	}
	if got := atomic.LoadInt32(&chamadasBinario); got != 1 {
		t.Errorf("chamadasBinario = %d, quero 1", got)
	}

	// 2nd call on the SAME Gerenciador: reuses the in-memory cache.
	bin2, err := g.Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver (2a chamada): %v", err)
	}
	if bin2 != wantBin {
		t.Errorf("Resolver() 2a chamada = %q, quero %q", bin2, wantBin)
	}
	if got := atomic.LoadInt32(&chamadasRelease); got != 1 {
		t.Errorf("re-baixou no cache quente: chamadasRelease = %d, quero 1", got)
	}

	// A NEW Gerenciador in the same directory (simulates a process restart):
	// reuses the binary already on disk, without hitting the server again.
	g2 := Novo(dir, "", ComReleaseAPIURL(srv.URL+"/release"))
	bin3, err := g2.Resolver(context.Background())
	if err != nil {
		t.Fatalf("Resolver (gerenciador novo): %v", err)
	}
	if bin3 != wantBin {
		t.Errorf("Resolver() gerenciador novo = %q, quero %q", bin3, wantBin)
	}
	if got := atomic.LoadInt32(&chamadasRelease); got != 1 {
		t.Errorf("re-baixou apos reinicio simulado: chamadasRelease = %d, quero 1 (deveria reusar o cache em disco)", got)
	}
	if got := atomic.LoadInt32(&chamadasBinario); got != 1 {
		t.Errorf("re-baixou apos reinicio simulado: chamadasBinario = %d, quero 1", got)
	}

	disponivel, origem, detalhe := g2.Status(context.Background())
	if !disponivel || origem != "baixado" || detalhe != "" {
		t.Errorf("Status() = (%v, %q, %q), quero (true, baixado, \"\")", disponivel, origem, detalhe)
	}
}

// TestStatusFalhaSemRede covers the failure criterion: a download failure (here
// simulated with a local HTTP server already CLOSED -- connection refused, without
// hitting the real internet) makes Status report disponivel=false with a detail
// explaining the reason.
func TestStatusFalhaSemRede(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // closed BEFORE being used: any call refuses the connection.

	g := Novo(dir, "", ComReleaseAPIURL(srv.URL), ComHTTPClient(&http.Client{Timeout: 2 * time.Second}))

	disponivel, origem, detalhe := g.Status(context.Background())
	if disponivel {
		t.Error("disponivel = true, quero false (sem rede)")
	}
	if origem != "nenhum" {
		t.Errorf("origem = %q, quero nenhum", origem)
	}
	if detalhe == "" {
		t.Error("detalhe vazio, quero o motivo da falha de download")
	}
}

// TestBaixarInvocaBinarioResolvido covers the contract of Baixar (generic, reused
// by the trailer and theme flows): it resolves the binary and invokes
// `<bin> -o <destino> <urlOrigem>`.
func TestBaixarInvocaBinarioResolvido(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "chamada.log")
	scriptPath := filepath.Join(t.TempDir(), "meu-yt-dlp")
	conteudo := fmt.Sprintf("#!/bin/sh\necho \"$@\" > %q\nexit 0\n", logPath)
	if err := os.WriteFile(scriptPath, []byte(conteudo), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	g := Novo(dir, scriptPath)
	if err := g.Baixar(context.Background(), "https://youtu.be/xyz", "/tmp/saida.mp4"); err != nil {
		t.Fatalf("Baixar: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ler log de chamada: %v", err)
	}
	want := "-o /tmp/saida.mp4 https://youtu.be/xyz\n"
	if string(got) != want {
		t.Errorf("args recebidos = %q, quero %q", got, want)
	}
}

// TestBaixarErroQuandoBinarioFalha covers the case of the yt-dlp process exiting
// with an error (e.g.: video unavailable/removed): Baixar propagates the error.
func TestBaixarErroQuandoBinarioFalha(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(t.TempDir(), "meu-yt-dlp")
	conteudo := "#!/bin/sh\necho 'erro fatal' >&2\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(conteudo), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	g := Novo(dir, scriptPath)
	if err := g.Baixar(context.Background(), "https://youtu.be/xyz", "/tmp/saida.mp4"); err == nil {
		t.Fatal("Baixar() = nil, esperava erro (script saiu com falha)")
	}
}

// TestNomeAssetPlataformaSempreZipapp covers the choice of the "yt-dlp" asset (the
// platform-independent Python zipapp, not the standalone binaries per OS linked
// against glibc) -- a fix after a production finding: the Linux standalone fails at
// runtime in an Alpine/musl image (see the comment of nomeAssetPlataforma). No skip
// per platform: the value does not depend on GOOS/GOARCH.
func TestNomeAssetPlataformaSempreZipapp(t *testing.T) {
	nome, ok := nomeAssetPlataforma()
	if !ok || nome != "yt-dlp" {
		t.Errorf("nomeAssetPlataforma() = (%q, %v), quero (yt-dlp, true)", nome, ok)
	}
}
