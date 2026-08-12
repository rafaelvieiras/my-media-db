// Package themerrdb implementa um provider APENAS de MÚSICA-TEMA (briefing
// 0103, ADR-0025): a ThemerrDB (LizardByte, BSD-3-Clause) é um JSON estático
// publicado diariamente, indexado por id TMDB/IMDb (filme) ou só TMDB
// (série), que alimenta os plugins oficiais Themerr-plex/-jellyfin/-kodi. O
// Client implementa o core provider.Provider apenas com
// Buscar/BuscarDetalhe/MapearDetalhe NO-OP (para entrar no Registry e ser
// descoberto por type-assert, mesmo padrão de internal/provider/fanarttv), e
// a capacidade real é provider.TemaMusicalProvider (BuscarTemaMusical). SEM
// credencial nenhuma — o JSON é público, sempre pode ser registrado.
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
	// baseURLPadrao é a base estável do JSON estático da ThemerrDB.
	baseURLPadrao = "https://app.lizardbyte.dev/ThemerrDB"
	// timeoutPadrao limita cada requisição; estouro vira erro (o enrich degrada).
	timeoutPadrao = 8 * time.Second
	// nomeFonte é o Nome() da fonte (também o source do candidato em media_external).
	nomeFonte = "themerrdb"
)

// Client é o provider ThemerrDB. httpClient e baseURL são injetáveis para
// teste (apontar a um httptest.Server sem rede real).
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// New cria o client. httpClient nil usa um default com timeout; baseURL vazio
// usa a base pública da ThemerrDB. Sem credencial: pode ser sempre registrado
// (cmd/mymediadb/main.go, provedoresReais).
func New(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeoutPadrao}
	}
	if baseURL == "" {
		baseURL = baseURLPadrao
	}
	return &Client{httpClient: httpClient, baseURL: baseURL}
}

// Nome identifica a fonte.
func (c *Client) Nome() string { return nomeFonte }

// Buscar é NO-OP: a ThemerrDB não é fonte de busca (nunca polui os
// resultados). Devolve vazio para satisfazer o core provider.Provider.
func (c *Client) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

// BuscarDetalhe é NO-OP: a ThemerrDB não modela o detalhe de um item (só
// tema musical). Devolve um payload vazio para satisfazer o core.
func (c *Client) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// MapearDetalhe é NO-OP: devolve um mapa vazio — o candidato de tema musical
// em media_external NÃO deve poluir a composição de camadas do resolver (só
// o endpoint de candidato o lê, direto do banco, internal/server).
func (c *Client) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// BuscarTemaMusical procura o tema do item na ThemerrDB (capacidade
// provider.TemaMusicalProvider). Filme: tenta o id TMDB; sem match (404) OU
// sem id TMDB, tenta o IMDb. Série: só o id TMDB (ThemerrDB não indexa série
// por IMDb — briefing 0103 §3 "fora"). Sem id utilizável para o tipo, ou sem
// match em NENHUMA tentativa, devolve (TemaMusical{}, false, nil) — graça.
// Erro real (rede/status inesperado) propaga.
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
			continue // item existe na fonte mas sem URL util (defensivo)
		}
		return provider.TemaMusical{URL: resp.YoutubeThemeURL}, true, nil
	}
	return provider.TemaMusical{}, false, nil
}

// rotasCandidatas monta os paths a tentar, em ordem, pelo tipo e pelos ids
// disponiveis. Filme: /movies/tmdb/{id}.json (se houver tmdb), depois
// /movies/imdb/{id}.json (se houver imdb). Série: só /tv_shows/tmdb/{id}.json.
// Vazio quando nenhum id utilizável para o tipo.
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

// respostaThemerrdb é o subconjunto do JSON da ThemerrDB que interessa: só o
// campo do tema (o payload real carrega também tmdb_id/imdb_id/uuid, que
// ignoramos).
type respostaThemerrdb struct {
	YoutubeThemeURL string `json:"youtube_theme_url"`
}

// getJSON faz o GET a `path` e devolve o corpo. ok=false num 404 (item sem
// entrada na ThemerrDB — não é erro). Status != 200/404 e I/O propagam como
// erro.
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
