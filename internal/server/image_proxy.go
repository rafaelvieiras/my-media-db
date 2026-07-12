package server

// Este arquivo implementa GET /api/v1/images?src=<url>: um PROXY READ-THROUGH de
// imagem para a tela de DETALHE DO EXTERNO (pré-import, briefing 0022). O pôster e o
// fundo (backdrop) de um item externo eram servidos direto do CDN da fonte (TMDB); o
// proxy baixa a imagem UMA vez, cacheia em disco sob {assetsDir}/_proxy/<sha256(src)>
// e serve do cache nas próximas — a UI deixa de depender do CDN da fonte em runtime,
// como o pôster/fanart cacheados da ENTITY (0015). Diferença: aqui NÃO há entity para
// anexar um asset, então o cache é POR URL, compartilhado entre itens/visitas.
//
// Segurança: o `src` é validado contra uma ALLOWLIST de hosts (defesa central contra
// SSRF — jamais buscamos host arbitrário) e a resposta só é cacheada/servida se for
// 200 image/*. O nome do arquivo é o sha256 hex da URL: determinístico e imune a
// path-traversal (é só hex), então dispensa a guarda de raízes do servirBinarioAsset.

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
	// subdirProxyImagem é o subdiretório do cache do proxy sob assetsDir. O prefixo "_"
	// o mantém FORA do espaço de nomes das entities (dirs = UUID), sem colisão.
	subdirProxyImagem = "_proxy"
	// timeoutProxyImagem limita o download de UMA imagem; estouro vira 502.
	timeoutProxyImagem = 15 * time.Second
	// tamanhoMaxProxyImagem é o teto de bytes de uma imagem (guarda contra fonte
	// hostil/gigante); 20 MiB cobre com folga um backdrop w780.
	tamanhoMaxProxyImagem = 20 << 20
	// rotaProxyImagem é o prefixo da rota que o DTO do detalhe externo emite quando
	// reescreve uma URL de imagem proxyável para o cache local.
	rotaProxyImagem = "/api/v1/images"
)

// hostsImagemPadrao é a allowlist do proxy (anti-SSRF): só CDNs de imagem confiáveis.
// O do TMDB (pôster/backdrop do detalhe externo, 0015/0024) e o do TheTVDB
// (artworks.thetvdb.com — o still de episódio servido sob demanda, 0042). Ampliar aqui
// ao entrar outra fonte. É a base do campo Server.hostsImagem (injetável em teste).
var hostsImagemPadrao = map[string]bool{
	"image.tmdb.org":       true,
	"artworks.thetvdb.com": true,
	// fanart.tv (briefing 0048): as URLs de arte do inventario (poster/logo/clearart/...)
	// vem deste host; o /artwork as serve ja via proxy.
	"assets.fanart.tv": true,
}

// clienteImagemPadrao é o http.Client do proxy (timeout curto). Fica aqui para manter
// server.go sem dependência de "time"; New o injeta em Server.httpImagem.
func clienteImagemPadrao() *http.Client { return &http.Client{Timeout: timeoutProxyImagem} }

// getImageProxy trata GET /api/v1/images?src=<url>. 503 sem assetsDir; 400 src
// ausente/inválida ou host fora da allowlist; 502 se a fonte falhar; 200 com o binário
// (cacheado). Ver o cabeçalho do arquivo para o desenho e as garantias de segurança.
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

// urlImagemPermitida valida o `src`: precisa ser URL absoluta http(s) com host EXATO
// na allowlist (a defesa central contra SSRF). O host inclui a porta, então
// `image.tmdb.org:9000` não casa `image.tmdb.org` — proposital. Devolve a URL canônica.
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

// urlImagemServida decide como a UI recebe uma URL de imagem do detalhe externo: com
// assetsDir E host proxyável, reescreve para a rota do PROXY LOCAL (cacheia e serve do
// disco, decoplando do CDN da fonte); senão devolve a URL DIRETA (degradação: sem
// assetsDir ou host desconhecido, a UI ainda renderiza da fonte). nil/vazia → intacta.
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

// proxyarCreditos reescreve profile_url/logo_url dos CRÉDITOS (elenco/equipe/
// produtoras, briefing 0029) para a rota do proxy local, quando aplicável — a MESMA
// conversão que o pôster/backdrop já fazem (urlImagemServida). richfields.CastDe/
// CrewDe/CompaniesDe montam a URL DIRETA (puras, sem acesso ao proxy); este é o único
// lugar com o host/base para decidir, reusado pelo detalhe externo e pelo resolver da
// entity (mesmo shape, paridade). Muta as fatias em memória (in place).
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

// caminhoCacheProxy devolve o caminho do binário cacheado de `alvo`: sob
// {assetsDir}/_proxy, nome = sha256 hex da URL (determinístico, sem colisão de nome
// nem traversal). Cria o diretório na primeira vez.
func (s *Server) caminhoCacheProxy(alvo string) (string, error) {
	dir := filepath.Join(s.assetsDir, subdirProxyImagem)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	soma := sha256.Sum256([]byte(alvo))
	return filepath.Join(dir, hex.EncodeToString(soma[:])), nil
}

// baixarParaCache baixa `alvo` (timeout + teto de bytes) e grava ATOMICAMENTE no cache,
// só se a resposta for 200 e image/* (evita cachear página de erro como imagem). A
// escrita atômica (temp + rename) impede servir binário parcial sob concorrência.
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

// escreverAtomico grava `dados` em `destino` via temp + rename no mesmo diretório
// (rename é atômico no mesmo filesystem): leitores nunca veem escrita parcial.
func escreverAtomico(destino string, dados []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(destino), filepath.Base(destino)+".tmp-*")
	if err != nil {
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

// servirBinarioProxy serve o binário cacheado. O caminho é montado por nós de um hash
// hex sob assetsDir — sem traversal —, então dispensa a guarda de raízes. Content-type
// por farejo (o arquivo do cache não tem extensão). Cache-Control deixa o browser
// reusar sem rebater no backend.
func (s *Server) servirBinarioProxy(w http.ResponseWriter, r *http.Request, caminho string) {
	f, err := os.Open(caminho) //nolint:gosec // caminho = assetsDir/_proxy/<sha256>, sem traversal
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
