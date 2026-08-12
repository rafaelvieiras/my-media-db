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

// scriptFakeConteudo é um script POSIX mínimo que se comporta como o yt-dlp
// real o suficiente para os testes: responde "--version" com sucesso (o piso
// de validação de baixarBinarioLocked) e, para qualquer outro argumento,
// termina com sucesso sem fazer nada (usado como "binário resolvido" nos
// testes de Baixar). NUNCA bate na internet real -- todo servidor usado
// nestes testes é um httptest.Server local.
const scriptFakeConteudo = "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo \"2024.01.01\"\n  exit 0\nfi\nexit 0\n"

// escreverScriptFake grava scriptFakeConteudo em `path`, já executável.
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

// TestResolverCustomPathEnvVenceSemBaixar cobre o critério §5: com
// MYMEDIADB_YTDLP_PATH setado (env, aqui já resolvido e passado a Novo),
// Resolver/Status usam esse path sem tentar baixar (a API de release nunca é
// consultada).
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

// TestSetCustomPathBancoVenceSobreEnv cobre o critério §5: com
// custom_binary_path gravado via PUT /settings/ytdlp (banco, aqui simulado
// por SetCustomPath), esse path vence sobre a env var -- e limpar (SetCustomPath(""))
// devolve o controle para a env var, tudo em RUNTIME, sem reconstruir o
// Gerenciador (equivalente a "sem reiniciar o processo").
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

// TestStatusCustomPathInacessivel cobre o caso de um path customizado
// apontar para um arquivo inexistente: Status reporta indisponível com
// motivo, em vez de mentir que está tudo certo.
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

// TestResolverBaixaNaPrimeiraChamadaEReusa cobre o critério §5: sem
// custom_binary_path e sem binário em cache, a primeira chamada a Resolver
// baixa o yt-dlp (via servidor HTTP FAKE local, nunca a internet real), valida
// com --version e reusa em chamadas seguintes -- tanto no MESMO Gerenciador
// (cache em memória) quanto num Gerenciador NOVO apontando pro mesmo diretório
// (simulando reinício do processo, cache em disco), sem re-baixar.
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

	// 2a chamada no MESMO Gerenciador: reusa o cache em memória.
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

	// Gerenciador NOVO no mesmo diretório (simula reinício do processo):
	// reusa o binário já em disco, sem bater no servidor de novo.
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

// TestStatusFalhaSemRede cobre o critério §5: falha de download (aqui
// simulada com um servidor HTTP local já FECHADO -- conexão recusada, sem
// bater na internet real) faz Status reportar disponivel=false com detalhe
// explicando o motivo.
func TestStatusFalhaSemRede(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // fechado ANTES de usar: qualquer chamada recusa a conexão.

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

// TestBaixarInvocaBinarioResolvido cobre o contrato de Baixar (genérico,
// reaproveitado por 0102/0103): resolve o binário e invoca
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

// TestBaixarErroQuandoBinarioFalha cobre o caso do processo yt-dlp sair com
// erro (ex.: vídeo indisponível/removido): Baixar propaga o erro.
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

// TestNomeAssetPlataformaSempreZipapp cobre a escolha do asset "yt-dlp" (o
// zipapp Python plataforma-independente, não os binários standalone por SO
// ligados a glibc) -- correção pós-achado de produção: o standalone Linux
// falha em runtime numa imagem Alpine/musl (ver comentário de
// nomeAssetPlataforma). Sem skip por plataforma: o valor não depende de
// GOOS/GOARCH.
func TestNomeAssetPlataformaSempreZipapp(t *testing.T) {
	nome, ok := nomeAssetPlataforma()
	if !ok || nome != "yt-dlp" {
		t.Errorf("nomeAssetPlataforma() = (%q, %v), quero (yt-dlp, true)", nome, ok)
	}
}
