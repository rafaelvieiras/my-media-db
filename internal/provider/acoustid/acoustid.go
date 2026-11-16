// Package acoustid implements a FINGERPRINT-ONLY provider: the public AcoustID
// API (https://acoustid.org), queried via GET /v2/lookup with a Chromaprint
// fingerprint (computed by internal/fingerprint) plus the duration of the audio,
// returns candidate MusicBrainz `recording` MBIDs with a confidence score. It is
// the LAST fallback of the music matching pipeline (internal/scanner/musica.go),
// triggered only when the embedded MBID and the textual search have already
// failed.
//
// The Client implements the core provider.Provider with only NO-OP
// Buscar/BuscarDetalhe/MapearDetalhe (to enter the Registry and be discovered by
// type-assert, the same pattern as internal/provider/themerrdb), and the real
// capability is provider.FingerprintProvider (IdentificarPorFingerprint). It
// requires a credential (the application "client key", distinct from the
// personal "user key" used only to SUBMIT fingerprints -- out of scope): without
// it configured, IdentificarPorFingerprint degrades to (zero,false,nil) instead
// of hitting the network with an empty key -- "consistently unavailable".

package acoustid

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
	// baseURLPadrao is the stable base of the AcoustID lookup endpoint.
	baseURLPadrao = "https://api.acoustid.org/v2/lookup"
	// timeoutPadrao bounds each request; an overrun becomes an error (the scanner
	// degrades to the draft step).
	timeoutPadrao = 8 * time.Second
	// intervaloRateLimit is the minimum interval between two requests to AcoustID
	// -- the API documents AT MOST 3 requests/second, unlike the <= 1 req/s of
	// MusicBrainz (internal/provider/musicbrainz/ratelimit.go).
	intervaloRateLimit = time.Second / 3
	// nomeFonte is the Nome() of the source, also the slug of the integration
	// (internal/integration.Descriptor, see descriptor.go).
	nomeFonte = "acoustid"
	// metaPadrao asks only for the recordings of each result (enough to extract
	// the MBID) -- no releases/releasegroups/tracks, which this provider does not
	// use (the recording MBID already enters the SAME scoped import path that
	// MusicaAlbumProvider.BuscarAlbumDaFaixa already resolves).
	metaPadrao = "recordings"
)

// Credencial is the resolved AcoustID credential: the application "client key"
// (the only key the lookup requires -- the personal "user key", used only to
// SUBMIT new fingerprints, is out of scope). Whoever reads the database/env is
// main (through internal/integration.Resolver, the same mechanism as TMDB); here
// it already arrives resolved.
type Credencial struct {
	// ClientKey is the client key of the application, obtained by registering the
	// app at acoustid.org.
	ClientKey string
}

// Configurada reports whether the client key is filled in. Without it, main does
// not even register the FingerprintProvider capability (acoustic fingerprint off
// -- it is not an error, it is the same degradation contract as any other
// optional capability of the project).
func (c Credencial) Configurada() bool {
	return c.ClientKey != ""
}

// Client is the AcoustID provider. httpClient and baseURL are injectable for
// testing (point at an httptest.Server without the real network).
type Client struct {
	cred       Credencial
	httpClient *http.Client
	baseURL    string
	lim        *limitador
}

// New creates the AcoustID client. A nil httpClient uses a default with timeout;
// an empty baseURL uses the public AcoustID base.
func New(cred Credencial, httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeoutPadrao}
	}
	if baseURL == "" {
		baseURL = baseURLPadrao
	}
	return &Client{
		cred:       cred,
		httpClient: httpClient,
		baseURL:    baseURL,
		lim:        novoLimitador(intervaloRateLimit),
	}
}

// Nome identifies the source.
func (c *Client) Nome() string { return nomeFonte }

// Buscar is NO-OP: AcoustID is not a textual search source (it never pollutes
// the results). Returns empty to satisfy the core provider.Provider.
func (c *Client) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

// BuscarDetalhe is NO-OP: AcoustID does not model item detail (only fingerprint).
// Returns an empty payload to satisfy the core.
func (c *Client) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// MapearDetalhe is NO-OP: it returns an empty map -- AcoustID never enters the
// layer composition of the resolver (only the FingerprintProvider capability is
// consumed, directly by the scanner).
func (c *Client) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// Interface assertion: the Client is a complete core Provider (stubs) + the real
// fingerprint capability.
var (
	_ provider.Provider            = (*Client)(nil)
	_ provider.FingerprintProvider = (*Client)(nil)
)

// IdentificarPorFingerprint queries AcoustID (capability
// provider.FingerprintProvider): GET /v2/lookup with
// client+meta+duration+fingerprint, and returns the HIGHEST-score recording
// candidate among the results that bring at least 1 recording. Without a
// configured client key: degrades to (zero,false,nil) WITHOUT hitting the network
// -- "consistently unavailable" (see the comment of Credencial.Configurada).
// With no result at all (audio unknown to AcoustID): (zero,false,nil),
// gracefully. A real error (network, unexpected status, malformed response, or a
// status="error" from the API itself) propagates -- the caller (scanner) decides
// to degrade to the draft step.
func (c *Client) IdentificarPorFingerprint(ctx context.Context, fingerprint string, duracaoSegundos float64) (provider.RecordingCandidato, bool, error) {
	if !c.cred.Configurada() {
		return provider.RecordingCandidato{}, false, nil
	}
	if fingerprint == "" {
		return provider.RecordingCandidato{}, false, fmt.Errorf("acoustid: fingerprint vazio")
	}

	params := url.Values{}
	params.Set("client", c.cred.ClientKey)
	params.Set("meta", metaPadrao)
	params.Set("duration", fmt.Sprintf("%.0f", duracaoSegundos))
	params.Set("fingerprint", fingerprint)

	body, err := c.getLookup(ctx, params)
	if err != nil {
		return provider.RecordingCandidato{}, false, err
	}

	var resp respLookup
	if err := json.Unmarshal(body, &resp); err != nil {
		return provider.RecordingCandidato{}, false, fmt.Errorf("acoustid: resposta invalida: %w", err)
	}
	if resp.Status != "" && resp.Status != "ok" {
		msg := resp.Error.Message
		if msg == "" {
			msg = resp.Status
		}
		return provider.RecordingCandidato{}, false, fmt.Errorf("acoustid: %s", msg)
	}

	cand, ok := melhorCandidato(resp.Results)
	if !ok {
		return provider.RecordingCandidato{}, false, nil
	}
	return cand, true, nil
}

// melhorCandidato picks, among the results of a lookup, the one with the HIGHEST
// score that has at least 1 associated recording (results without any recording
// are not usable -- AcoustID sometimes returns a fingerprint match that has no
// associated recording metadata yet). The first recording of the chosen result is
// the MBID returned (the same "first item = main" criterion already used in
// other providers of the repo, e.g. musicbrainz.buscarFaixas/creditoMB).
func melhorCandidato(results []resultadoLookup) (provider.RecordingCandidato, bool) {
	melhorScore := -1.0
	var melhor provider.RecordingCandidato
	achou := false
	for _, r := range results {
		if len(r.Recordings) == 0 || r.Recordings[0].ID == "" {
			continue
		}
		if r.Score > melhorScore {
			melhorScore = r.Score
			melhor = provider.RecordingCandidato{
				RecordingMBID: r.Recordings[0].ID,
				Confianca:     r.Score,
			}
			achou = true
		}
	}
	return melhor, achou
}

// getLookup performs a RATE-LIMITED GET (<=3 req/s) to the AcoustID lookup
// endpoint. Any status != 200 is a generic error (AcoustID does not document a
// transient status equivalent to the "busy" 503 of MusicBrainz that would
// justify a retry here).
func (c *Client) getLookup(ctx context.Context, params url.Values) ([]byte, error) {
	c.lim.esperar()

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("acoustid: base url invalida: %w", err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("acoustid: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("acoustid: status inesperado %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("acoustid: ler resposta: %w", err)
	}
	return body, nil
}

// respLookup is the response of GET /v2/lookup (only the fields used).
type respLookup struct {
	Status  string            `json:"status"`
	Results []resultadoLookup `json:"results"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
}

// resultadoLookup is one item of `results` -- a fingerprint match, with its score
// and the associated MusicBrainz recordings (when AcoustID has already linked the
// fingerprint to metadata).
type resultadoLookup struct {
	ID         string            `json:"id"`
	Score      float64           `json:"score"`
	Recordings []recordingLookup `json:"recordings"`
}

// recordingLookup is a MusicBrainz recording associated with a result -- only the
// MBID matters here (title/artists are left out, unused here).
type recordingLookup struct {
	ID string `json:"id"`
}
