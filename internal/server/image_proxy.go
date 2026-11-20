package server

// This file implements GET /api/v1/images?src=<url>: a READ-THROUGH image PROXY for the
// EXTERNAL DETAIL screen (pre-import). The poster and the background (backdrop) of an
// external item used to be served straight from the source CDN (TMDB); the proxy
// downloads the image ONCE, caches it on disk under {assetsDir}/_proxy/<sha256(src)> and
// serves from the cache on the following ones — the UI stops depending on the source CDN
// at runtime, like the cached poster/fanart of the ENTITY. Difference: here there is NO
// entity to attach an asset to, so the cache is PER URL, shared between items/visits.
//
// Security: `src` is validated against an ALLOWLIST of hosts (the central defense against
// SSRF — we never fetch an arbitrary host) and the response is only cached/served if it
// is 200 image/*. The file name is the sha256 hex of the URL: deterministic and immune to
// path-traversal (it is hex only), so it does without the roots guard of
// servirBinarioAsset.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

const (
	// subdirProxyImagem is the proxy cache subdirectory under assetsDir. The "_" prefix
	// keeps it OUTSIDE the entity namespace (dirs = UUID), with no collision.
	subdirProxyImagem = "_proxy"
	// timeoutProxyImagem limits the download of ONE image; a timeout becomes 502.
	timeoutProxyImagem = 15 * time.Second
	// tamanhoMaxProxyImagem is the byte ceiling of an image (a guard against a
	// hostile/giant source); 20 MiB comfortably covers a w780 backdrop.
	tamanhoMaxProxyImagem = 20 << 20
	// rotaProxyImagem is the route prefix that the external detail DTO emits when it
	// rewrites a proxyable image URL to the local cache.
	rotaProxyImagem = "/api/v1/images"
)

// hostsImagemPadrao is the allowlist of the proxy (anti-SSRF): trusted image CDNs only.
// The TMDB one (poster/backdrop of the external detail) and the TheTVDB one
// (artworks.thetvdb.com — the episode still served on demand). Widen it here when another
// source comes in. It is the basis of the Server.hostsImagem field (injectable in
// tests).
var hostsImagemPadrao = map[string]bool{
	"image.tmdb.org":       true,
	"artworks.thetvdb.com": true,
	// fanart.tv: the inventory artwork URLs (poster/logo/clearart/...) come from this
	// host; /artwork already serves them through the proxy.
	"assets.fanart.tv": true,
}

// clienteImagemPadrao is the proxy's http.Client (short timeout). It lives here to keep
// server.go free of a "time" dependency; New injects it into Server.httpImagem.
func clienteImagemPadrao() *http.Client { return &http.Client{Timeout: timeoutProxyImagem} }

// getImageProxy handles GET /api/v1/images?src=<url>. 503 without assetsDir; 400 missing
// or invalid src, or a host outside the allowlist; 502 if the source fails; 200 with the
// (cached) binary. See the file header for the design and the security guarantees.
func (s *Server) getImageProxy(w http.ResponseWriter, r *http.Request) {
	if s.assetsDir == "" {
		writeError(w, http.StatusServiceUnavailable, "image_proxy_unavailable",
			"proxy de imagem indisponível (diretório de assets não configurado)")
		return
	}
	alvo, ok := s.urlImagemPermitida(r.URL.Query().Get("src"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query", "src ausente ou host não permitido")
		return
	}
	caminho, err := s.caminhoCacheProxy(alvo)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if _, err := os.Stat(caminho); err != nil {
		if err := s.baixarParaCache(r.Context(), alvo, caminho); err != nil {
			respondFonteIndisponivel(w)
			return
		}
	}
	s.servirBinarioProxy(w, r, caminho)
}

// urlImagemPermitida validates `src`: it must be an absolute http(s) URL with the EXACT
// host in the allowlist (the central defense against SSRF). The host includes the port, so
// `image.tmdb.org:9000` does not match `image.tmdb.org` — deliberately. It returns the
// canonical URL.
func (s *Server) urlImagemPermitida(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if !s.hostsImagem[u.Host] {
		return "", false
	}
	return u.String(), true
}

// urlImagemServida decides how the UI receives an image URL of the external detail: with
// assetsDir AND a proxyable host, it rewrites to the LOCAL PROXY route (caches and serves
// from disk, decoupling from the source CDN); otherwise it returns the DIRECT URL
// (degradation: without assetsDir or with an unknown host, the UI still renders from the
// source). nil/empty → untouched.
func (s *Server) urlImagemServida(direta *string) *string {
	if direta == nil || *direta == "" {
		return direta
	}
	if s.assetsDir == "" {
		return direta
	}
	if _, ok := s.urlImagemPermitida(*direta); !ok {
		return direta
	}
	rota := rotaProxyImagem + "?src=" + url.QueryEscape(*direta)
	return &rota
}

// proxyarCreditos rewrites profile_url/logo_url of the CREDITS (cast/crew/production
// companies) to the local proxy route, when applicable — the SAME conversion the
// poster/backdrop already do (urlImagemServida). richfields.CastDe/
// CrewDe/CompaniesDe build the DIRECT URL (pure, with no access to the proxy); this is
// the only place with the host/base to decide, reused by the external detail and by the
// entity resolver (same shape, parity). It mutates the slices in memory (in place).
func (s *Server) proxyarCreditos(cast []richfields.Cast, crew []richfields.Crew, comps []richfields.Company) {
	for i := range cast {
		cast[i].ProfileURL = s.urlImagemServida(cast[i].ProfileURL)
	}
	for i := range crew {
		crew[i].ProfileURL = s.urlImagemServida(crew[i].ProfileURL)
	}
	for i := range comps {
		comps[i].LogoURL = s.urlImagemServida(comps[i].LogoURL)
	}
}

// caminhoCacheProxy returns the path of the cached binary of `alvo`: under
// {assetsDir}/_proxy, name = sha256 hex of the URL (deterministic, with no name collision
// nor traversal). It creates the directory the first time.
func (s *Server) caminhoCacheProxy(alvo string) (string, error) {
	dir := filepath.Join(s.assetsDir, subdirProxyImagem)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	soma := sha256.Sum256([]byte(alvo))
	return filepath.Join(dir, hex.EncodeToString(soma[:])), nil
}

// baixarParaCache downloads `alvo` (timeout + byte ceiling) and writes it ATOMICALLY into
// the cache, only if the response is 200 and image/* (it avoids caching an error page as
// an image). The atomic write (temp + rename) prevents serving a partial binary under
// concurrency.
func (s *Server) baixarParaCache(ctx context.Context, alvo, destino string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, alvo, nil)
	if err != nil {
		return err
	}
	resp, err := s.httpImagem.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("proxy: content-type %q nao e imagem", ct)
	}
	dados, err := io.ReadAll(io.LimitReader(resp.Body, tamanhoMaxProxyImagem))
	if err != nil {
		return err
	}
	return escreverAtomico(destino, dados)
}

// escreverAtomico writes `dados` into `destino` via temp + rename in the same directory
// (rename is atomic within the same filesystem): readers never see a partial write.
func escreverAtomico(destino string, dados []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(destino), filepath.Base(destino)+".tmp-*")
	if err != nil {
		return err
	}
	// Same reason as localmedia.EscreverArquivoAtomico: CreateTemp cria 0600 e o rename mantem
	// o modo, o que deixa a arte do cache ilegivel para quem nao e o uid do container.
	if err := tmp.Chmod(0o664); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	nome := tmp.Name()
	if _, err := tmp.Write(dados); err != nil {
		_ = tmp.Close()
		_ = os.Remove(nome)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(nome)
		return err
	}
	return os.Rename(nome, destino)
}

// servirBinarioProxy serves the cached binary. The path is built by us from a hex hash
// under assetsDir — no traversal —, so it does without the roots guard. Content-type by
// sniffing (the cache file has no extension). Cache-Control lets the browser reuse it
// without hitting the backend again.
func (s *Server) servirBinarioProxy(w http.ResponseWriter, r *http.Request, caminho string) {
	f, err := os.Open(caminho) //nolint:gosec // path = assetsDir/_proxy/<sha256>, no traversal
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "imagem indisponivel")
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "not_found", "imagem indisponivel")
		return
	}
	w.Header().Set("Content-Type", contentTypeImagem(caminho, f))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, filepath.Base(caminho), info.ModTime(), f)
}
