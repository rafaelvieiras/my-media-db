// Package ytdlp gerencia o ciclo de vida do binário yt-dlp usado para baixar
// trailer (briefing 0102) e música-tema (briefing 0103) do YouTube — decisão
// fechada na ADR-0025 (TR1). Este pacote entrega SÓ a fundação: como o
// binário chega até o container (download dinâmico em runtime, sem entrar na
// imagem Docker) e como o admin pode assumir controle via path customizado.
// Nenhum download de trailer/tema acontece aqui — só a resolução/gestão do
// binário e a função genérica `Baixar` que 0102/0103 reaproveitam.
//
// Sem dependência de internal/server (mesmo princípio de internal/localmedia),
// para não criar ciclo de import — o pacote recebe o diretório de dados e o
// path de env JÁ RESOLVIDOS pelo chamador (cmd/mymediadb/main.go), nunca lê
// os.Getenv sozinho (mantém o pacote testável).
package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// nomeBinario é o nome do arquivo do binário baixado dinamicamente, gravado
// em <dir>/nomeBinario (briefing 0101: "<diretório-de-dados>/bin/yt-dlp").
const nomeBinario = "yt-dlp"

// releaseAPIURLPadrao é o endpoint oficial da release mais recente do yt-dlp
// (briefing 0101). Injetável via ComReleaseAPIURL só para teste.
const releaseAPIURLPadrao = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"

// httpTimeoutPadrao cobre tanto a consulta à API de release quanto o download
// do binário em si (pode ser lento em conexões ruins; generoso de propósito).
const httpTimeoutPadrao = 2 * time.Minute

// Gerenciador resolve e mantém o binário yt-dlp utilizável pelo processo,
// seguindo a ordem de precedência da ADR-0025: path customizado (banco →
// env) → binário já baixado no cache local → baixa agora (só na 1ª
// necessidade) → falha. Zero-value não é utilizável — construa com Novo.
// Seguro para uso concorrente (mu serializa leituras/escritas de estado e
// também o download, evitando baixar duas vezes em paralelo).
type Gerenciador struct {
	mu sync.Mutex

	// dir é o diretório de dados onde o binário baixado é gravado/lido (cache
	// local entre reinícios do processo) -- resolvido pelo chamador (main.go,
	// resolveYtdlpDir), mesmo padrão de resolveAssetsDir/resolveBackupDir.
	dir string
	// envPath é o path customizado vindo da env var MYMEDIADB_YTDLP_PATH,
	// resolvido UMA VEZ no boot (imutável depois de Novo) -- o chamador
	// resolve o env, este pacote só recebe o valor pronto.
	envPath string
	// dbPath é o path customizado vindo de app_settings (banco), setável em
	// RUNTIME via SetCustomPath -- tem PRECEDÊNCIA sobre envPath (ADR-0023).
	dbPath string

	// baixado memoiza o path do binário já validado NESTA execução, para
	// Resolver não re-stat/re-baixar a cada chamada depois do sucesso.
	baixado string
	// ultimoErro guarda o detalhe do último erro de download/validação, só
	// para diagnóstico -- Status sempre recalcula o estado atual, nunca serve
	// este campo "stale" como fonte de verdade.
	ultimoErro string

	httpClient    *http.Client
	releaseAPIURL string
}

// Opcao configura o Gerenciador na construção -- hoje só usada em teste, para
// apontar a API de release/HTTP client a um servidor fake local (nunca bate
// na internet real nos testes automatizados).
type Opcao func(*Gerenciador)

// ComReleaseAPIURL sobrescreve o endpoint da API de release do yt-dlp
// (default releaseAPIURLPadrao). Uso exclusivo de teste.
func ComReleaseAPIURL(url string) Opcao {
	return func(g *Gerenciador) { g.releaseAPIURL = url }
}

// ComHTTPClient sobrescreve o cliente HTTP usado tanto para consultar a API
// de release quanto para baixar o binário. Uso exclusivo de teste (timeout
// curto evita testes lentos em cenário de falha de rede simulada).
func ComHTTPClient(c *http.Client) Opcao {
	return func(g *Gerenciador) { g.httpClient = c }
}

// Novo constrói o Gerenciador. dir é o diretório de dados (já resolvido pelo
// chamador, ver resolveYtdlpDir em cmd/mymediadb/main.go); envPath é o path
// customizado da env var MYMEDIADB_YTDLP_PATH ("" quando não configurada).
func Novo(dir, envPath string, opcoes ...Opcao) *Gerenciador {
	g := &Gerenciador{
		dir:           dir,
		envPath:       envPath,
		httpClient:    &http.Client{Timeout: httpTimeoutPadrao},
		releaseAPIURL: releaseAPIURLPadrao,
	}
	for _, opt := range opcoes {
		opt(g)
	}
	return g
}

// SetCustomPath grava (ou limpa, com "") o path customizado vindo do banco --
// chamado pela camada de settings (internal/server) sempre que o dono grava/
// limpa `custom_binary_path` via PUT /settings/ytdlp, ou no boot para
// sincronizar um valor já persistido de uma execução anterior. Aplica em
// RUNTIME, sem reiniciar o processo.
func (g *Gerenciador) SetCustomPath(path string) {
	g.mu.Lock()
	g.dbPath = path
	g.mu.Unlock()
}

// customPathLocked devolve o path customizado efetivo (banco vence sobre
// env), ou "" quando nenhum dos dois está configurado. Chamador já segura mu.
func (g *Gerenciador) customPathLocked() string {
	if g.dbPath != "" {
		return g.dbPath
	}
	return g.envPath
}

// Resolver devolve o path do binário yt-dlp UTILIZÁVEL, na ordem de
// precedência da ADR-0025: path customizado (banco → env, sem validar
// existência -- o admin assume o controle) → binário já baixado (memória ou
// disco, de uma execução anterior) → baixa agora (só a 1ª vez; chamadas
// seguintes reusam o resultado em memória). Erro só quando nenhuma opção
// funciona (sem customizado E download falhou).
func (g *Gerenciador) Resolver(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if custom := g.customPathLocked(); custom != "" {
		return custom, nil
	}

	if g.baixado != "" {
		return g.baixado, nil
	}

	destino := filepath.Join(g.dir, nomeBinario)
	if info, err := os.Stat(destino); err == nil && !info.IsDir() {
		g.baixado = destino
		g.ultimoErro = ""
		return destino, nil
	}

	if err := g.baixarBinarioLocked(ctx, destino); err != nil {
		g.ultimoErro = err.Error()
		return "", err
	}
	g.baixado = destino
	g.ultimoErro = ""
	return destino, nil
}

// Status devolve o estado ATUAL de disponibilidade do yt-dlp, calculado na
// hora (nunca serve um estado "cacheado" desatualizado): disponivel diz se
// há um binário utilizável agora; origem é "custom" (path do admin, banco ou
// env), "baixado" (cache dinâmico) ou "nenhum" (indisponível); detalhe carrega
// o motivo quando disponivel=false. Usado tanto pelo endpoint de config
// (GET/PUT /settings/ytdlp) quanto pelos endpoints de download dos briefings
// seguintes (que checam ANTES de tentar, respondendo 503 sem yt-dlp, nunca
// 500). Quando ainda não há binário em cache, dispara o download agora (mesma
// resolução de Resolver) -- é assim que o admin descobre se o download
// automático falhou (ex.: sem rede) sem precisar chamar Baixar primeiro.
func (g *Gerenciador) Status(ctx context.Context) (disponivel bool, origem string, detalhe string) {
	g.mu.Lock()
	custom := g.customPathLocked()
	g.mu.Unlock()

	if custom != "" {
		if info, err := os.Stat(custom); err != nil || info.IsDir() {
			return false, "custom", fmt.Sprintf("path customizado inacessivel: %s", custom)
		}
		return true, "custom", ""
	}

	if _, err := g.Resolver(ctx); err != nil {
		return false, "nenhum", err.Error()
	}
	return true, "baixado", ""
}

// Baixar executa o yt-dlp resolvido (path customizado ou baixado
// dinamicamente) para baixar `urlOrigem` em `destino`. Função GENÉRICA, sem
// saber se é trailer ou tema -- 0102/0103 chamam esta MESMA função. Resolve
// (e baixa, se necessário) o binário do yt-dlp em si antes de tentar.
func (g *Gerenciador) Baixar(ctx context.Context, urlOrigem, destino string) error {
	bin, err := g.Resolver(ctx)
	if err != nil {
		return fmt.Errorf("yt-dlp indisponivel: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin, "-o", destino, urlOrigem)
	saida, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("yt-dlp: falha ao baixar %q: %w (saida: %s)", urlOrigem, err, truncar(string(saida), 500))
	}
	return nil
}

// baixarBinarioLocked baixa a release oficial do yt-dlp mais recente para
// `destino`, valida com `--version` antes de aceitar como bom. Chamador já
// segura mu (Resolver). Escreve num arquivo temporário e só promove (rename)
// depois de validar -- nunca deixa um binário quebrado "meio baixado" no
// cache, que ficaria preso lá para sempre em Resolver (checa via os.Stat).
func (g *Gerenciador) baixarBinarioLocked(ctx context.Context, destino string) error {
	nomeAsset, ok := nomeAssetPlataforma()
	if !ok {
		return fmt.Errorf("plataforma nao suportada para download automatico do yt-dlp (%s/%s) -- configure um path customizado", runtime.GOOS, runtime.GOARCH)
	}

	assetURL, err := g.resolverURLAsset(ctx, nomeAsset)
	if err != nil {
		return fmt.Errorf("falha ao consultar release do yt-dlp: %w", err)
	}

	if err := os.MkdirAll(g.dir, 0o755); err != nil {
		return fmt.Errorf("falha ao preparar diretorio %q: %w", g.dir, err)
	}

	tmp := destino + ".tmp"
	if err := g.baixarArquivo(ctx, assetURL, tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("falha ao baixar binario do yt-dlp: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("falha ao tornar o binario executavel: %w", err)
	}

	if err := validarBinario(ctx, tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("binario baixado nao passou na validacao: %w", err)
	}

	if err := os.Rename(tmp, destino); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("falha ao mover o binario baixado para %q: %w", destino, err)
	}
	return nil
}

// releaseAPIResponse é o subconjunto do payload de
// GET /repos/yt-dlp/yt-dlp/releases/latest que interessa: só os assets (nome +
// URL de download direto).
type releaseAPIResponse struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// resolverURLAsset consulta a API de release e devolve a browser_download_url
// do asset cujo nome bate com `nomeAsset` (ex.: "yt-dlp_linux").
func (g *Gerenciador) resolverURLAsset(ctx context.Context, nomeAsset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.releaseAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resposta inesperada da API de release: status %d", resp.StatusCode)
	}
	var parsed releaseAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("resposta da API de release ilegivel: %w", err)
	}
	for _, a := range parsed.Assets {
		if a.Name == nomeAsset {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("nenhum asset %q encontrado na release mais recente", nomeAsset)
}

// baixarArquivo baixa `url` para `destino` via streaming (sem carregar o
// binário inteiro em memória).
func (g *Gerenciador) baixarArquivo(ctx context.Context, url, destino string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("resposta inesperada ao baixar o binario: status %d", resp.StatusCode)
	}

	f, err := os.OpenFile(destino, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

// validarBinario roda `<path> --version` e devolve erro se o processo falhar
// -- o piso de validação desta entrega (checksum/assinatura fica como
// hardening futuro, ver §3 do briefing 0101).
func validarBinario(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, path, "--version")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w (saida: %s)", err, truncar(buf.String(), 300))
	}
	return nil
}

// nomeAssetPlataforma devolve o nome do asset a baixar da release do yt-dlp.
// SEMPRE "yt-dlp" -- o zipapp Python PLATAFORMA-INDEPENDENTE (roda via shebang
// `#!/usr/bin/env python3`), não os binários standalone por SO/arquitetura
// (`yt-dlp_linux` etc., build PyInstaller). Decisão pós-achado de produção
// (briefing 0101 §6): o standalone Linux é ligado a glibc e a imagem final do
// projeto é Alpine/musl -- falha em runtime com "no such file or directory"
// (o kernel não acha o linker dinâmico glibc embutido no ELF; `gcompat`
// TAMBÉM não resolve, falha mais adiante por símbolo glibc ausente na libpython
// embutida). O zipapp evita o problema na raiz: precisa só de `python3` no
// PATH (já garantido pelo Dockerfile, `apk add python3`), sem depender de
// qual glibc/musl a imagem usa. ok sempre true -- existe só pra manter a
// assinatura extensível caso uma exceção real apareça no futuro.
func nomeAssetPlataforma() (string, bool) {
	return "yt-dlp", true
}

// truncar corta `s` em `n` bytes, com reticências -- evita mensagens de erro
// gigantes quando a saída de um processo externo vem grande.
func truncar(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
