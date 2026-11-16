// Package ytdlp manages the lifecycle of the yt-dlp binary used to download
// trailers and theme music from YouTube — a decision closed in ADR-0025 (TR1).
// This package delivers ONLY the foundation: how the binary reaches the
// container (dynamic download at runtime, without entering the Docker image) and
// how the admin can take control through a custom path. No trailer/theme
// download happens here — only the resolution/management of the binary and the
// generic `Baixar` function that the trailer and theme download flows reuse.
//
// No dependency on internal/server (same principle as internal/localmedia), to
// avoid an import cycle — the package receives the data directory and the env
// path ALREADY RESOLVED by the caller (cmd/mymediadb/main.go), it never reads
// os.Getenv on its own (keeps the package testable).
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

// nomeBinario is the file name of the dynamically downloaded binary, written to
// <dir>/nomeBinario ("<data-directory>/bin/yt-dlp").
const nomeBinario = "yt-dlp"

// releaseAPIURLPadrao is the official endpoint of the latest yt-dlp release.
// Injectable via ComReleaseAPIURL for tests only.
const releaseAPIURLPadrao = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"

// httpTimeoutPadrao covers both the query to the release API and the download of
// the binary itself (it can be slow on bad connections; generous on purpose).
const httpTimeoutPadrao = 2 * time.Minute

// Gerenciador resolves and keeps the yt-dlp binary usable by the process,
// following the precedence order of ADR-0025: custom path (database → env) →
// binary already downloaded in the local cache → download now (only on the 1st
// need) → failure. The zero value is not usable — build it with Novo. Safe for
// concurrent use (mu serializes state reads/writes and also the download,
// preventing a double parallel download).
type Gerenciador struct {
	mu sync.Mutex

	// dir is the data directory where the downloaded binary is written/read (local
	// cache across process restarts) -- resolved by the caller (main.go,
	// resolveYtdlpDir), same pattern as resolveAssetsDir/resolveBackupDir.
	dir string
	// envPath is the custom path coming from the MYMEDIADB_YTDLP_PATH env var,
	// resolved ONCE at boot (immutable after Novo) -- the caller resolves the env,
	// this package only receives the ready value.
	envPath string
	// dbPath is the custom path coming from app_settings (database), settable at
	// RUNTIME via SetCustomPath -- it takes PRECEDENCE over envPath (ADR-0023).
	dbPath string

	// baixado memoizes the path of the binary already validated in THIS execution, so
	// that Resolver does not re-stat/re-download on every call after success.
	baixado string
	// ultimoErro stores the detail of the last download/validation error, for
	// diagnosis only -- Status always recomputes the current state, it never serves
	// this "stale" field as the source of truth.
	ultimoErro string

	httpClient    *http.Client
	releaseAPIURL string
}

// Opcao configures the Gerenciador at construction -- today only used in tests,
// to point the release/HTTP client API at a local fake server (the automated
// tests never hit the real internet).
type Opcao func(*Gerenciador)

// ComReleaseAPIURL overrides the yt-dlp release API endpoint (default
// releaseAPIURLPadrao). Tests only.
func ComReleaseAPIURL(url string) Opcao {
	return func(g *Gerenciador) { g.releaseAPIURL = url }
}

// ComHTTPClient overrides the HTTP client used both to query the release API and
// to download the binary. Tests only (a short timeout avoids slow tests in a
// simulated network failure scenario).
func ComHTTPClient(c *http.Client) Opcao {
	return func(g *Gerenciador) { g.httpClient = c }
}

// Novo builds the Gerenciador. dir is the data directory (already resolved by the
// caller, see resolveYtdlpDir in cmd/mymediadb/main.go); envPath is the custom
// path from the MYMEDIADB_YTDLP_PATH env var ("" when not configured).
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

// SetCustomPath writes (or clears, with "") the custom path coming from the
// database -- called by the settings layer (internal/server) whenever the owner
// writes/clears `custom_binary_path` via PUT /settings/ytdlp, or at boot to
// synchronize a value already persisted from a previous run. It applies at
// RUNTIME, without restarting the process.
func (g *Gerenciador) SetCustomPath(path string) {
	g.mu.Lock()
	g.dbPath = path
	g.mu.Unlock()
}

// customPathLocked returns the effective custom path (database wins over env), or
// "" when neither is configured. The caller already holds mu.
func (g *Gerenciador) customPathLocked() string {
	if g.dbPath != "" {
		return g.dbPath
	}
	return g.envPath
}

// Resolver returns the path of the USABLE yt-dlp binary, in the precedence order
// of ADR-0025: custom path (database → env, without validating existence -- the
// admin takes control) → binary already downloaded (memory or disk, from a
// previous run) → download now (only the 1st time; the following calls reuse the
// result in memory). An error only when no option works (no custom path AND the
// download failed).
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

// Status returns the CURRENT availability state of yt-dlp, computed on the spot
// (it never serves an outdated "cached" state): disponivel says whether there is a
// usable binary right now; origem is "custom" (admin path, database or env),
// "baixado" (dynamic cache) or "nenhum" (unavailable); detalhe carries the reason
// when disponivel=false. Used both by the config endpoint (GET/PUT
// /settings/ytdlp) and by the download endpoints of the following features (which
// check BEFORE trying, answering 503 without yt-dlp, never 500). When there is no
// binary in the cache yet, it triggers the download now (the same resolution as
// Resolver) -- that is how the admin finds out whether the automatic download
// failed (e.g.: no network) without having to call Baixar first.
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

// Baixar runs the resolved yt-dlp (custom path or dynamically downloaded) to
// download `urlOrigem` into `destino`. A GENERIC function, unaware of whether it
// is a trailer or a theme -- the trailer and theme flows call this SAME function.
// It resolves (and downloads, if needed) the yt-dlp binary itself before trying.
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

// baixarBinarioLocked downloads the latest official yt-dlp release into `destino`,
// validates it with `--version` before accepting it as good. The caller already
// holds mu (Resolver). It writes to a temporary file and only promotes it (rename)
// after validating -- it never leaves a broken "half downloaded" binary in the
// cache, which would stay stuck there forever in Resolver (checked via os.Stat).
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

// releaseAPIResponse is the subset of the payload of
// GET /repos/yt-dlp/yt-dlp/releases/latest that matters: only the assets (name +
// direct download URL).
type releaseAPIResponse struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// resolverURLAsset queries the release API and returns the browser_download_url of
// the asset whose name matches `nomeAsset` (e.g.: "yt-dlp_linux").
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

// baixarArquivo downloads `url` into `destino` by streaming (without loading the
// whole binary into memory).
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

// validarBinario runs `<path> --version` and returns an error if the process fails
// -- the validation floor of this delivery (checksum/signature is left as future
// hardening).
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

// nomeAssetPlataforma returns the name of the asset to download from the yt-dlp
// release. ALWAYS "yt-dlp" -- the PLATFORM-INDEPENDENT Python zipapp (it runs via
// the `#!/usr/bin/env python3` shebang), not the standalone binaries per
// OS/architecture (`yt-dlp_linux` etc., PyInstaller build). Decision after a
// production finding: the Linux standalone is linked against glibc and the
// project's final image is Alpine/musl -- it fails at runtime with "no such file
// or directory" (the kernel does not find the glibc dynamic linker embedded in the
// ELF; `gcompat` does NOT solve it either, it fails further ahead because of a
// glibc symbol missing in the embedded libpython). The zipapp avoids the problem at
// its root: it only needs `python3` on the PATH (already guaranteed by the
// Dockerfile, `apk add python3`), with no dependency on which glibc/musl the image
// uses. ok is always true -- it exists only to keep the signature extensible in
// case a real exception shows up in the future.
func nomeAssetPlataforma() (string, bool) {
	return "yt-dlp", true
}

// truncar cuts `s` at `n` bytes, with an ellipsis -- it avoids gigantic error
// messages when the output of an external process comes back large.
func truncar(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
