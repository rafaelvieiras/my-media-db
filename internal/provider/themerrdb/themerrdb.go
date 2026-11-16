// Package themerrdb implements a MUSICAL-THEME-ONLY provider (ADR-0025): the
// ThemerrDB (LizardByte, BSD-3-Clause) is a static JSON published daily, indexed
// by TMDB/IMDb id (movie) or TMDB only (series), which feeds the official
// Themerr-plex/-jellyfin/-kodi plugins. The Client implements the core
// provider.Provider with only NO-OP Buscar/BuscarDetalhe/MapearDetalhe (to enter
// the Registry and be discovered by type-assert, the same pattern as
// internal/provider/fanarttv), and the real capability is
// provider.TemaMusicalProvider (BuscarTemaMusical). WITHOUT any credential -- the
// JSON is public, so it can always be registered.
package themerrdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// baseURLPadrao is the stable base of the ThemerrDB static JSON.
	baseURLPadrao = "https://app.lizardbyte.dev/ThemerrDB"
	// timeoutPadrao bounds each request; an overrun becomes an error (enrich degrades).
	timeoutPadrao = 8 * time.Second
	// nomeFonte is the Nome() of the source (also the source of the media_external candidate).
	nomeFonte = "themerrdb"
)

// Client is the ThemerrDB provider. httpClient and baseURL are injectable for
// testing (point at an httptest.Server without the real network).
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// New creates the client. A nil httpClient uses a default with timeout; an empty
// baseURL uses the public ThemerrDB base. Without a credential: it can always be
// registered (cmd/mymediadb/main.go, provedoresReais).
func New(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeoutPadrao}
	}
	if baseURL == "" {
		baseURL = baseURLPadrao
	}
	return &Client{httpClient: httpClient, baseURL: baseURL}
}

// Nome identifies the source.
func (c *Client) Nome() string { return nomeFonte }

// Buscar is NO-OP: ThemerrDB is not a search source (it never pollutes the
// results). Returns empty to satisfy the core provider.Provider.
func (c *Client) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

// BuscarDetalhe is NO-OP: ThemerrDB does not model the detail of an item (only
// the musical theme). Returns an empty payload to satisfy the core.
func (c *Client) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// MapearDetalhe is NO-OP: it returns an empty map -- the musical theme candidate
// in media_external must NOT pollute the layer composition of the resolver (only
// the candidate endpoint reads it, straight from the database, internal/server).
func (c *Client) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// BuscarTemaMusical looks up the theme of the item in ThemerrDB (capability
// provider.TemaMusicalProvider). Movie: it tries the TMDB id; without a match
// (404) OR without a TMDB id, it tries the IMDb one. Series: only the TMDB id
// (ThemerrDB does not index series by IMDb -- out of scope). Without a usable id
// for the type, or without a match in ANY attempt, it returns (TemaMusical{},
// false, nil) -- gracefully. A real error (network/unexpected status)
// propagates.
func (c *Client) BuscarTemaMusical(ctx context.Context, ids provider.ExternalIDs, tipo string) (provider.TemaMusical, bool, error) {
	for _, path := range c.rotasCandidatas(ids, tipo) {
		corpo, ok, err := c.getJSON(ctx, path)
		if err != nil {
			return provider.TemaMusical{}, false, err
		}
		if !ok {
			continue // 404 nesta rota: tenta a proxima (ou acaba a lista)
		}
		var resp respostaThemerrdb
		if err := json.Unmarshal(corpo, &resp); err != nil {
			return provider.TemaMusical{}, false, fmt.Errorf("themerrdb: resposta invalida em %s: %w", path, err)
		}
		if resp.YoutubeThemeURL == "" {
			continue // item exists in the source but without a usable URL (defensive)
		}
		return provider.TemaMusical{URL: resp.YoutubeThemeURL}, true, nil
	}
	return provider.TemaMusical{}, false, nil
}

// rotasCandidatas builds the paths to try, in order, by type and by the available
// ids. Movie: /movies/tmdb/{id}.json (if there is a tmdb id), then
// /movies/imdb/{id}.json (if there is an imdb id). Series: only
// /tv_shows/tmdb/{id}.json. Empty when no id is usable for the type.
func (c *Client) rotasCandidatas(ids provider.ExternalIDs, tipo string) []string {
	switch tipo {
	case "movie":
		var rotas []string
		if ids.Tmdb != "" {
			rotas = append(rotas, "/movies/tmdb/"+url.PathEscape(ids.Tmdb)+".json")
		}
		if ids.Imdb != "" {
			rotas = append(rotas, "/movies/imdb/"+url.PathEscape(ids.Imdb)+".json")
		}
		return rotas
	case "tv":
		if ids.Tmdb == "" {
			return nil
		}
		return []string{"/tv_shows/tmdb/" + url.PathEscape(ids.Tmdb) + ".json"}
	default:
		return nil
	}
}

// respostaThemerrdb is the subset of the ThemerrDB JSON that matters: only the
// theme field (the real payload also carries tmdb_id/imdb_id/uuid, which we
// ignore).
type respostaThemerrdb struct {
	YoutubeThemeURL string `json:"youtube_theme_url"`
}

// getJSON performs the GET to `path` and returns the body. ok=false on a 404
// (item without an entry in ThemerrDB -- not an error). Status != 200/404 and I/O
// errors propagate as an error.
func (c *Client) getJSON(ctx context.Context, path string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("themerrdb: status inesperado %d em %s", resp.StatusCode, path)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("themerrdb: ler %s: %w", path, err)
	}
	return body, true, nil
}
