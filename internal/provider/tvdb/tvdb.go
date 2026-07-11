// Package tvdb implementa o provider.Provider para o TheTVDB (API v4), o SEGUNDO
// provider do projeto (briefing 0031 / ADR-0021) e a fonte forte de séries — a única
// que modela conteúdo como Looney Tunes (curtas antigos) e que expõe as ORDENAÇÕES
// (season types). Além do Provider (busca + detalhe de série), o Client cumpre a
// capacidade opcional provider.SerieProvider (a árvore temporadas+episódios, em
// tvdb_serie.go). A credencial (apikey) é lida no boot (main) e injetada aqui —
// NUNCA hardcoded, NUNCA logada; o token JWT (~1 mês) é cacheado e re-obtido sob 401.
package tvdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// baseURLPadrao é a base estável da API v4 do TheTVDB.
	baseURLPadrao = "https://api4.thetvdb.com/v4"
	// timeoutPadrao limita cada requisição; estouro vira erro (o handler degrada).
	timeoutPadrao = 12 * time.Second
	// pageSize é o tamanho de página dos episódios (o TheTVDB usa 500/pág via links.next).
	pageSize = 500
)

// Credencial é a credencial resolvida do TheTVDB: a project-supported apikey basta
// sozinha (o pin só é exigido para user-supported keys, fora do escopo). Quem lê o
// env (MYMEDIADB_TTDB_API_KEY) é o main; aqui já chega resolvida.
type Credencial struct {
	// APIKey é a chave do projeto usada no login (POST /login {"apikey":...}).
	APIKey string
}

// Configurada diz se há credencial preenchida. Sem ela, o main nem cria o provider
// (busca externa da fonte desligada — não é erro).
func (c Credencial) Configurada() bool { return c.APIKey != "" }

// Client é o provider TheTVDB. httpClient e baseURL são injetáveis para teste (apontar
// a um httptest.Server sem rede real). O token JWT é cacheado sob tokenMu.
type Client struct {
	cred       Credencial
	httpClient *http.Client
	baseURL    string

	// tokenMu protege o token JWT cacheado (~1 mês de validade). É re-obtido sob 401
	// (getBytes limpa e re-loga uma vez), não a cada request.
	tokenMu sync.Mutex
	token   string
}

// New cria o client TheTVDB. httpClient nil usa um default com timeout; baseURL vazio
// usa a base pública v4.
func New(cred Credencial, httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeoutPadrao}
	}
	if baseURL == "" {
		baseURL = baseURLPadrao
	}
	return &Client{cred: cred, httpClient: httpClient, baseURL: baseURL}
}

// Nome identifica a fonte.
func (c *Client) Nome() string { return "tvdb" }

// obterToken devolve o token JWT cacheado ou faz o login (POST /login {"apikey":...})
// e o cacheia. A apikey e o token NUNCA são logados. Erro de credencial ausente é
// sinalizado aqui (o main não deveria ter criado o client sem ela).
func (c *Client) obterToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	if c.cred.APIKey == "" {
		return "", fmt.Errorf("tvdb: credencial ausente")
	}
	tok, err := c.login(ctx)
	if err != nil {
		return "", err
	}
	c.token = tok
	return tok, nil
}

// login faz POST /login e devolve o token; nunca ecoa a apikey/token.
func (c *Client) login(ctx context.Context) (string, error) {
	corpo, err := json.Marshal(map[string]string{"apikey": c.cred.APIKey})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login", bytes.NewReader(corpo))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tvdb: login status %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("tvdb: login invalido: %w", err)
	}
	if out.Data.Token == "" {
		return "", fmt.Errorf("tvdb: login sem token")
	}
	return out.Data.Token, nil
}

// getBytes faz um GET autenticado a `path` (relativo à base) com os `params` e devolve
// o corpo. Trata 404 como provider.ErrNaoEncontrado (item inexistente → 404 no handler)
// e re-loga UMA vez sob 401 (token expirado). Ponto único de I/O de leitura.
func (c *Client) getBytes(ctx context.Context, path string, params url.Values) ([]byte, error) {
	body, status, err := c.tentarGet(ctx, path, params)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		c.limparToken()
		body, status, err = c.tentarGet(ctx, path, params)
		if err != nil {
			return nil, err
		}
	}
	switch status {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("tvdb: %s: %w", path, provider.ErrNaoEncontrado)
	default:
		return nil, fmt.Errorf("tvdb: status inesperado %d em %s", status, path)
	}
}

// tentarGet monta e executa um GET autenticado, devolvendo corpo e status (sem
// interpretar). Separado de getBytes para permitir o retry sob 401 sem duplicar I/O.
func (c *Client) tentarGet(ctx context.Context, path string, params url.Values) ([]byte, int, error) {
	tok, err := c.obterToken(ctx)
	if err != nil {
		return nil, 0, err
	}
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, 0, err
	}
	if len(params) > 0 {
		u.RawQuery = params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("tvdb: ler %s: %w", path, err)
	}
	return body, resp.StatusCode, nil
}

// limparToken descarta o token cacheado (força re-login na próxima chamada).
func (c *Client) limparToken() {
	c.tokenMu.Lock()
	c.token = ""
	c.tokenMu.Unlock()
}

// respostaBusca espelha /search (só os campos usados). O type discrimina series/movie/
// person; só series nos interessa aqui (a busca já filtra type=series).
type respostaBusca struct {
	Data []itemBusca `json:"data"`
}

// itemBusca é um resultado cru de /search. tvdb_id é o id numérico (como string).
type itemBusca struct {
	TvdbID   string `json:"tvdb_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Year     string `json:"year"`
	Overview string `json:"overview"`
	ImageURL string `json:"image_url"`
}

// Buscar consulta /search?query=&type=series e mapeia os resultados para
// ResultadoExterno com Type="tv" (o nosso tipo canônico de série). O TheTVDB só é
// fonte de séries no MVP, então descartamos qualquer type != "series". `tipo` restringe
// a busca: se for "movie" (a fonte não cobre filmes aqui), devolve vazio sem erro.
func (c *Client) Buscar(ctx context.Context, query, tipo, lang string) ([]provider.ResultadoExterno, error) {
	if query == "" {
		return nil, nil
	}
	if tipo == "movie" {
		return nil, nil // TheTVDB é fonte de séries no MVP
	}
	params := url.Values{"query": {query}, "type": {"series"}}
	body, err := c.getBytes(ctx, "/search", params)
	if err != nil {
		return nil, err
	}
	var payload respostaBusca
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("tvdb: busca invalida: %w", err)
	}
	out := make([]provider.ResultadoExterno, 0, len(payload.Data))
	for _, it := range payload.Data {
		if it.Type != "series" || it.TvdbID == "" {
			continue
		}
		out = append(out, provider.ResultadoExterno{
			Source:    "tvdb",
			SourceID:  it.TvdbID,
			Type:      "tv",
			Title:     it.Name,
			Year:      anoPtr(it.Year),
			Overview:  ptrNaoVazio(it.Overview),
			PosterURL: ptrNaoVazio(it.ImageURL),
			Saved:     false,
		})
	}
	return out, nil
}

// anoPtr devolve os 4 primeiros dígitos de um ano (o /search já entrega "year"), ou nil.
func anoPtr(year string) *string {
	if len(year) < 4 {
		return nil
	}
	a := year[:4]
	return &a
}

// ptrNaoVazio devolve *string para uma string, ou nil quando ela é vazia.
func ptrNaoVazio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
