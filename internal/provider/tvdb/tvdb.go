// Package tvdb implements provider.Provider for TheTVDB (API v4), the SECOND provider
// of the project (ADR-0021) and the strong source for series -- the only one that models
// content like Looney Tunes (old shorts) and that exposes the ORDERINGS (season types).
// Besides the Provider (search + series detail), the Client satisfies the optional
// capability provider.SerieProvider (the seasons+episodes tree, in tvdb_serie.go). The
// credential (apikey) is read at boot (main) and injected here -- NEVER hardcoded, NEVER
// logged; the JWT token (~1 month) is cached and re-obtained under a 401.
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
	// baseURLPadrao is the stable base of TheTVDB v4 API.
	baseURLPadrao = "https://api4.thetvdb.com/v4"
	// timeoutPadrao bounds each request; an overrun becomes an error (the handler degrades).
	timeoutPadrao = 12 * time.Second
)

// Credencial is the resolved TheTVDB credential: the project-supported apikey is enough
// on its own (the pin is only required for user-supported keys, out of scope). Whoever
// reads the env (MYMEDIADB_TTDB_API_KEY) is main; here it already arrives resolved.
type Credencial struct {
	// APIKey is the project key used in the login (POST /login {"apikey":...}).
	APIKey string
}

// Configurada reports whether a credential is filled in. Without it, main does not even
// create the provider (external search of the source off -- not an error).
func (c Credencial) Configurada() bool { return c.APIKey != "" }

// Client is the TheTVDB provider. httpClient and baseURL are injectable for testing (point
// at an httptest.Server without the real network). The JWT token is cached under tokenMu.
type Client struct {
	cred       Credencial
	httpClient *http.Client
	baseURL    string

	// tokenMu protects the cached JWT token (~1 month of validity). It is re-obtained under
	// a 401 (getBytes clears it and logs in again once), not on every request.
	tokenMu sync.Mutex
	token   string
}

// New creates the TheTVDB client. A nil httpClient uses a default with timeout; an empty
// baseURL uses the public v4 base.
func New(cred Credencial, httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeoutPadrao}
	}
	if baseURL == "" {
		baseURL = baseURLPadrao
	}
	return &Client{cred: cred, httpClient: httpClient, baseURL: baseURL}
}

// Nome identifies the source.
func (c *Client) Nome() string { return "tvdb" }

// obterToken returns the cached JWT token or performs the login (POST /login
// {"apikey":...}) and caches it. The apikey and the token are NEVER logged. A missing
// credential error is signaled here (main should not have created the client without it).
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

// login performs POST /login and returns the token; it never echoes the apikey/token.
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

// getBytes performs an authenticated GET to `path` (relative to the base) with the
// `params` and returns the body. It treats 404 as provider.ErrNaoEncontrado (nonexistent
// item → 404 in the handler) and logs in again ONCE under a 401 (expired token). The
// single reading I/O point.
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

// tentarGet assembles and executes an authenticated GET, returning the body and the status
// (without interpreting it). Separate from getBytes to allow the retry under a 401 without
// duplicating I/O.
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

// limparToken discards the cached token (forcing a re-login on the next call).
func (c *Client) limparToken() {
	c.tokenMu.Lock()
	c.token = ""
	c.tokenMu.Unlock()
}

// respostaBusca mirrors /search (only the fields used). The type discriminates
// series/movie/person; only series interest us here (the search already filters
// type=series).
type respostaBusca struct {
	Data []itemBusca `json:"data"`
}

// itemBusca is a raw result of /search. tvdb_id is the numeric id (as a string).
type itemBusca struct {
	TvdbID   string `json:"tvdb_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Year     string `json:"year"`
	Overview string `json:"overview"`
	ImageURL string `json:"image_url"`
}

// Buscar queries /search?query=&type=series and maps the results to ResultadoExterno
// with Type="tv" (our canonical series type). TheTVDB is only a series source in the MVP,
// so we discard any type != "series". `tipo` restricts the search: if it is "movie" (the
// source does not cover movies here), it returns empty without an error.
func (c *Client) Buscar(ctx context.Context, query, tipo, lang string) ([]provider.ResultadoExterno, error) {
	if query == "" {
		return nil, nil
	}
	if tipo == "movie" {
		return nil, nil // TheTVDB is a series source in the MVP
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

// anoPtr returns the first 4 digits of a year (the /search already delivers "year"), or nil.
func anoPtr(year string) *string {
	if len(year) < 4 {
		return nil
	}
	a := year[:4]
	return &a
}

// ptrNaoVazio returns *string for a string, or nil when it is empty.
func ptrNaoVazio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
