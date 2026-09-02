// Package acoustid implementa um provider APENAS de FINGERPRINT ACÚSTICO
// (briefing 0126): a API pública do AcoustID (https://acoustid.org),
// consultada via GET /v2/lookup com um fingerprint Chromaprint (calculado
// por internal/fingerprint) + a duração do áudio, devolve candidato(s) de
// `recording` MBID no MusicBrainz com um escore de confiança. É o ÚLTIMO
// fallback do pipeline de matching de música (internal/scanner/musica.go),
// disparado só quando MBID embutido e busca textual já falharam.
//
// O Client implementa o core provider.Provider apenas com
// Buscar/BuscarDetalhe/MapearDetalhe NO-OP (para entrar no Registry e ser
// descoberto por type-assert, mesmo padrão de internal/provider/themerrdb),
// e a capacidade real é provider.FingerprintProvider
// (IdentificarPorFingerprint). Exige credencial (a "client key" da
// aplicação, distinta da "user key" pessoal usada só para SUBMETER
// fingerprints — fora de escopo, ver briefing 0126 §3): sem ela configurada,
// IdentificarPorFingerprint degrada para (zero,false,nil) em vez de bater na
// rede com uma chave vazia — "indisponível de forma consistente" (briefing
// 0126, seção Config/credencial).
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
	// baseURLPadrao é a base estável do endpoint de lookup do AcoustID.
	baseURLPadrao = "https://api.acoustid.org/v2/lookup"
	// timeoutPadrao limita cada requisição; estouro vira erro (o scanner degrada
	// para o passo de draft).
	timeoutPadrao = 8 * time.Second
	// intervaloRateLimit é o intervalo mínimo entre duas requisições ao AcoustID
	// -- a API documenta NO MÁXIMO 3 requisições/segundo (briefing 0126 §4,
	// §6), diferente do <= 1 req/s do MusicBrainz
	// (internal/provider/musicbrainz/ratelimit.go).
	intervaloRateLimit = time.Second / 3
	// nomeFonte é o Nome() da fonte, também o slug da integração
	// (internal/integration.Descriptor, ver descriptor.go).
	nomeFonte = "acoustid"
	// metaPadrao pede só os recordings de cada resultado (o suficiente para
	// extrair o MBID) -- sem releases/releasegroups/tracks, que este briefing
	// não usa (o MBID de recording já entra no MESMO caminho de import
	// escopado que MusicaAlbumProvider.BuscarAlbumDaFaixa já resolve).
	metaPadrao = "recordings"
)

// Credencial é a credencial resolvida do AcoustID: a "client key" da
// aplicação (única chave que o lookup exige -- a "user key" pessoal, usada
// só para SUBMETER fingerprints novos, está fora de escopo deste briefing).
// Quem lê o banco/env é o main (via internal/integration.Resolver, mesmo
// mecanismo do TMDB); aqui já chega resolvida.
type Credencial struct {
	// ClientKey é a client key da aplicação, obtida registrando o app em
	// acoustid.org.
	ClientKey string
}

// Configurada diz se a client key está preenchida. Sem ela, o main nem
// registra a capacidade FingerprintProvider (fingerprint acústico
// desligado -- não é erro, é o mesmo contrato de degradação de qualquer
// outra capacidade opcional do projeto).
func (c Credencial) Configurada() bool {
	return c.ClientKey != ""
}

// Client é o provider AcoustID. httpClient e baseURL são injetáveis para
// teste (apontar a um httptest.Server sem rede real).
type Client struct {
	cred       Credencial
	httpClient *http.Client
	baseURL    string
	lim        *limitador
}

// New cria o client AcoustID. httpClient nil usa um default com timeout;
// baseURL vazio usa a base pública do AcoustID.
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

// Nome identifica a fonte.
func (c *Client) Nome() string { return nomeFonte }

// Buscar é NO-OP: o AcoustID não é fonte de busca textual (nunca polui os
// resultados). Devolve vazio para satisfazer o core provider.Provider.
func (c *Client) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

// BuscarDetalhe é NO-OP: o AcoustID não modela detalhe de item (só
// fingerprint). Devolve um payload vazio para satisfazer o core.
func (c *Client) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// MapearDetalhe é NO-OP: devolve um mapa vazio -- o AcoustID nunca entra na
// composição de camadas do resolver (só a capacidade FingerprintProvider é
// consumida, direto pelo scanner).
func (c *Client) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// Asserção de interface: o Client é um Provider core completo (stubs) + a
// capacidade real de fingerprint.
var (
	_ provider.Provider            = (*Client)(nil)
	_ provider.FingerprintProvider = (*Client)(nil)
)

// IdentificarPorFingerprint consulta o AcoustID (capacidade
// provider.FingerprintProvider, briefing 0126): GET /v2/lookup com
// client+meta+duration+fingerprint, e devolve o candidato de recording de
// MAIOR escore entre os resultados que trouxerem ao menos 1 recording.
// Sem client key configurada: degrada para (zero,false,nil) SEM bater na
// rede -- "indisponível de forma consistente" (ver comentário de
// Credencial.Configurada). Sem resultado algum (áudio desconhecido do
// AcoustID): (zero,false,nil), graça. Erro real (rede, status inesperado,
// resposta malformada, ou status="error" da própria API) propaga -- o
// chamador (scanner) decide degradar para o passo de draft.
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

// melhorCandidato escolhe, entre os resultados de um lookup, o de MAIOR
// score que tenha ao menos 1 recording associado (resultados sem nenhum
// recording não são utilizáveis -- o AcoustID às vezes devolve um match de
// fingerprint sem metadado de recording associado ainda). O primeiro
// recording do resultado escolhido é o MBID devolvido (mesmo critério de
// "primeiro item = principal" já usado em outros providers do repo, ex.
// musicbrainz.buscarFaixas/creditoMB).
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

// getLookup faz um GET RATE-LIMITADO (<=3 req/s) ao endpoint de lookup do
// AcoustID. Qualquer status != 200 é erro genérico (o AcoustID não documenta
// um status transitório equivalente ao 503 "busy" do MusicBrainz que
// justifique retry aqui).
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

// respLookup é a resposta de GET /v2/lookup (só os campos usados).
type respLookup struct {
	Status  string            `json:"status"`
	Results []resultadoLookup `json:"results"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
}

// resultadoLookup é um item de `results` -- um match de fingerprint, com
// escore e os recordings MusicBrainz associados (quando o AcoustID já
// linkou o fingerprint a metadado).
type resultadoLookup struct {
	ID         string            `json:"id"`
	Score      float64           `json:"score"`
	Recordings []recordingLookup `json:"recordings"`
}

// recordingLookup é um recording MusicBrainz associado a um resultado --
// só o MBID importa aqui (título/artistas ficam de fora, sem uso neste
// briefing).
type recordingLookup struct {
	ID string `json:"id"`
}
