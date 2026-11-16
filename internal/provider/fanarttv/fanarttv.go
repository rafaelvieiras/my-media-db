// Package fanarttv implements a provider DEDICATED TO ART: fanart.tv is an
// exclusive source of high-quality art (logos, clearart, disc, character art)
// that TMDB/TVDB do not cover well. It is NOT a search source: the Client
// implements the core provider.Provider with only NO-OP
// Buscar/BuscarDetalhe/MapearDetalhe (to enter the Registry and be discovered by
// type-assert), and the real capability is provider.ArteProvider (BuscarArtes).
// The credential (personal key) is read at boot (main) and injected here --
// NEVER hardcoded, never logged.
package fanarttv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// baseURLPadrao is the stable base of the fanart.tv v3 API.
	baseURLPadrao = "https://webservice.fanart.tv/v3"
	// timeoutPadrao bounds each request; an overrun becomes an error (enrich degrades).
	timeoutPadrao = 8 * time.Second
	// nomeFonte is the Nome() of the source (also the source of the media_external inventory).
	nomeFonte = "fanarttv"
)

// kindsFilme maps each MOVIE art type of fanart.tv to our kind.
// moviedisc carries disc_type/disc in the item itself.
var kindsFilme = map[string]string{
	"movieposter":     "poster",
	"moviebackground": "fanart",
	"hdmovielogo":     "logo",
	"movielogo":       "logo",
	"hdmovieclearart": "clearart",
	"movieart":        "clearart",
	"moviedisc":       "disc",
	"moviebanner":     "banner",
	"moviethumb":      "thumb",
}

// kindsTV maps each SERIES art type of fanart.tv to our kind. The season arts
// (season*) carry the `season` field in the item -- enrich routes them to the
// season.
var kindsTV = map[string]string{
	"tvposter":       "poster",
	"showbackground": "fanart",
	"hdtvlogo":       "logo",
	"clearlogo":      "logo",
	"hdclearart":     "clearart",
	"clearart":       "clearart",
	"tvbanner":       "banner",
	"tvthumb":        "thumb",
	"characterart":   "characterart",
	"seasonposter":   "poster",
	"seasonthumb":    "thumb",
	"seasonbanner":   "banner",
}

// kindsArtista maps each MUSIC ARTIST art type of fanart.tv to our kind
// (endpoint GET /music/{mbid} -- the same flattened type->array shape as
// movie/series, so it reuses mapearArtes without a new struct). fanart.tv also
// nests the album cover/cdart inside the SAME response under the "albums" key
// (by release-group), but that key is not in the table below, so mapearArtes
// ignores it -- the album cover is already covered by the Cover Art Archive
// (MusicaBrainz), without duplicating it here.
var kindsArtista = map[string]string{
	"artistthumb":      "thumb",
	"artistbackground": "fanart",
	"hdmusiclogo":      "logo",
	"musiclogo":        "logo",
	"musicbanner":      "banner",
}

// Credencial is the fanart.tv credential: the PERSONAL key (api_key), which
// authenticates on its own (validated, HTTP 200) and bypasses the project key
// limit. Whoever reads the env is main; here it already arrives resolved.
type Credencial struct {
	// APIKey is the personal key (api_key query).
	APIKey string
}

// Configurada reports whether a key is filled in. Without it, main does not even
// create the provider (fanart.tv art off -- it is not an error; zero regression).
func (c Credencial) Configurada() bool { return c.APIKey != "" }

// Client is the fanart.tv provider. httpClient and baseURL are injectable for
// testing (point at an httptest.Server without the real network).
type Client struct {
	cred       Credencial
	httpClient *http.Client
	baseURL    string
}

// New creates the client. A nil httpClient uses a default with timeout; an empty
// baseURL uses the public fanart.tv base.
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
func (c *Client) Nome() string { return nomeFonte }

// Buscar is NO-OP: fanart.tv is not a search source (it never pollutes the
// results). Returns empty to satisfy the core provider.Provider.
func (c *Client) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

// BuscarDetalhe is NO-OP: fanart.tv does not model the detail of an item (only
// art). Returns an empty payload to satisfy the core.
func (c *Client) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// MapearDetalhe is NO-OP: it returns an empty map -- the fanart.tv inventory
// layer in media_external must NOT pollute the layer composition of the resolver
// (only /artwork reads it).
func (c *Client) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// BuscarArtes lists the arts of the item at fanart.tv (capability
// provider.ArteProvider). Movie: /movies/{id} (TMDB id preferred, otherwise
// IMDB). Series: /tv/{id} (TheTVDB id). Music artist: /music/{id} (MBID id) --
// artist photo/background/logo; the album cover is the responsibility of the
// Cover Art Archive (MusicBrainz), not of this source. Without a usable id or
// without a match (404) it returns (nil, nil) -- gracefully. A real error
// propagates. `langs` is not used in the call (fanart.tv returns ALL languages at
// once); the lang of each art is preserved in the inventory.
func (c *Client) BuscarArtes(ctx context.Context, ids provider.ExternalIDs, tipo string, langs []string) ([]provider.Arte, error) {
	_ = langs
	path, tabela, ok := c.rotaETabela(ids, tipo)
	if !ok {
		return nil, nil // no usable id for this source
	}
	body, ok, err := c.getArtes(ctx, path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil // 404: item without art on fanart.tv (gracefully)
	}
	return mapearArtes(body, tabela)
}

// rotaETabela chooses the path and the kinds table by type and by the available
// ids. A movie prefers the TMDB id (otherwise the IMDB one); a series uses the
// TheTVDB id. ok=false when the id is missing.
func (c *Client) rotaETabela(ids provider.ExternalIDs, tipo string) (string, map[string]string, bool) {
	switch tipo {
	case "movie":
		id := ids.Tmdb
		if id == "" {
			id = ids.Imdb
		}
		if id == "" {
			return "", nil, false
		}
		return "/movies/" + url.PathEscape(id), kindsFilme, true
	case "tv":
		if ids.Tvdb == "" {
			return "", nil, false
		}
		return "/tv/" + url.PathEscape(ids.Tvdb), kindsTV, true
	case "artista":
		if ids.Mbid == "" {
			return "", nil, false
		}
		return "/music/" + url.PathEscape(ids.Mbid), kindsArtista, true
	default:
		return "", nil, false
	}
}

// getArtes performs the authenticated GET (api_key) to `path` and returns the
// body. ok=false on a 404 (item without art -- not an error). Status != 200/404
// and I/O errors propagate as an error.
func (c *Client) getArtes(ctx context.Context, path string) ([]byte, bool, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, false, err
	}
	q := u.Query()
	q.Set("api_key", c.cred.APIKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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
		return nil, false, fmt.Errorf("fanarttv: status inesperado %d em %s", resp.StatusCode, path)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("fanarttv: ler %s: %w", path, err)
	}
	return body, true, nil
}

// imagemFanart is a raw fanart.tv image. The numbers come as STRING in the JSON
// (likes/disc/season) -- converted with atoiSeguro. `season` can be
// "all"/"0"/"1"...
type imagemFanart struct {
	URL      string `json:"url"`
	Lang     string `json:"lang"`
	Likes    string `json:"likes"`
	Disc     string `json:"disc"`
	DiscType string `json:"disc_type"`
	Season   string `json:"season"`
}

// mapearArtes decodes the response (an object with one array per type) and maps
// each KNOWN type of the table to []provider.Arte. Types outside the table
// (name/tmdb_id/...) and images without a URL are ignored. A type with a
// malformed array is skipped (it degrades that type, not the whole search).
func mapearArtes(body []byte, tabela map[string]string) ([]provider.Arte, error) {
	var bruto map[string]json.RawMessage
	if err := json.Unmarshal(body, &bruto); err != nil {
		return nil, fmt.Errorf("fanarttv: resposta invalida: %w", err)
	}
	var out []provider.Arte
	for chave, kind := range tabela {
		raw, ok := bruto[chave]
		if !ok || len(raw) == 0 {
			continue
		}
		var imgs []imagemFanart
		if err := json.Unmarshal(raw, &imgs); err != nil {
			continue // array malformada deste tipo: degrada o tipo
		}
		for _, img := range imgs {
			if img.URL == "" {
				continue
			}
			out = append(out, arteDe(kind, img))
		}
	}
	return out, nil
}

// arteDe converts a raw image into our canonical Arte, normalizing the kind and
// converting the number-strings (likes/disc). DiscType/Season come through
// intact (empty for the types that do not have them).
func arteDe(kind string, img imagemFanart) provider.Arte {
	return provider.Arte{
		Kind:     kind,
		URL:      img.URL,
		Lang:     img.Lang,
		Likes:    atoiSeguro(img.Likes),
		DiscType: img.DiscType,
		Disc:     atoiSeguro(img.Disc),
		Season:   img.Season,
	}
}

// atoiSeguro converts a numeric string to int, returning 0 when it is
// empty/invalid (fanart.tv serializes likes/disc as a string).
func atoiSeguro(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
