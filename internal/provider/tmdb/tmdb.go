// Package tmdb implementa o provider.Provider para o The Movie Database (TMDB),
// o primeiro provider do projeto (briefing 0014). Cobre filmes e séries no MVP,
// via /search/multi filtrado por media_type. A credencial é lida no boot (main)
// e injetada aqui — NUNCA hardcoded, nunca logada.
package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// baseURLPadrao é a base estável da API do TMDB (config hardcoded — decisão
	// do briefing: evita uma chamada a /configuration no MVP).
	baseURLPadrao = "https://api.themoviedb.org/3"
	// imagemBaseURL é a base das imagens do TMDB, tamanho w342 para pôster de card.
	imagemBaseURL = "https://image.tmdb.org/t/p/w342"
	// timeoutPadrao limita cada requisição ao TMDB; estouro vira erro (o handler
	// degrada para só resultados locais).
	timeoutPadrao = 8 * time.Second
)

// Credencial é a credencial resolvida do TMDB. Preferência decidida no briefing:
// Token (Read Access Token v4 → header Authorization: Bearer) sobre APIKey
// (v3 → query api_key). Quem lê o env é o main; aqui já chega resolvida.
type Credencial struct {
	// Token é o Read Access Token v4 (preferido).
	Token string
	// APIKey é a api_key v3 (alternativa para quem só a tem).
	APIKey string
}

// Configurada diz se há ao menos uma credencial preenchida. Sem nenhuma, o main
// nem cria o provider (busca externa desligada — não é erro).
func (c Credencial) Configurada() bool {
	return c.Token != "" || c.APIKey != ""
}

// Client é o provider TMDB. httpClient e baseURL são injetáveis para teste
// (apontar a um httptest.Server sem rede real).
type Client struct {
	cred       Credencial
	httpClient *http.Client
	baseURL    string
}

// New cria o client TMDB. httpClient nil usa um default com timeout; baseURL
// vazio usa a base pública do TMDB.
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
func (c *Client) Nome() string { return "tmdb" }

// respostaMulti espelha o payload de /search/multi (só os campos usados).
type respostaMulti struct {
	Results []itemMulti `json:"results"`
}

// itemMulti é um resultado cru de /search/multi. media_type discrimina o tipo
// (movie|tv|person|…); só movie e tv nos interessam.
type itemMulti struct {
	ID           int64  `json:"id"`
	MediaType    string `json:"media_type"`
	Title        string `json:"title"` // filmes
	Name         string `json:"name"`  // séries
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	ReleaseDate  string `json:"release_date"`   // filmes
	FirstAirDate string `json:"first_air_date"` // séries
}

// Buscar consulta /search/multi e mapeia os resultados para ResultadoExterno,
// descartando media_type fora de {movie, tv}. Se `tipo` for movie/tv, filtra
// ainda por ele. Erro/timeout é propagado (o handler degrada).
func (c *Client) Buscar(ctx context.Context, query, tipo string) ([]provider.ResultadoExterno, error) {
	if query == "" {
		return nil, nil
	}

	req, err := c.montarRequisicao(ctx, query)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb: status inesperado %d", resp.StatusCode)
	}

	var payload respostaMulti
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("tmdb: resposta inválida: %w", err)
	}

	out := make([]provider.ResultadoExterno, 0, len(payload.Results))
	for _, it := range payload.Results {
		res, ok := mapearItem(it)
		if !ok {
			continue // descarta person e outros media_type
		}
		if tipo != "" && res.Type != tipo {
			continue // filtro opcional por tipo
		}
		out = append(out, res)
	}
	return out, nil
}

// montarRequisicao constrói o GET /search/multi com a autenticação escolhida:
// Token v4 → header Authorization: Bearer; senão APIKey v3 → query api_key.
func (c *Client) montarRequisicao(ctx context.Context, query string) (*http.Request, error) {
	u, err := url.Parse(c.baseURL + "/search/multi")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("query", query)
	if c.cred.Token == "" && c.cred.APIKey != "" {
		q.Set("api_key", c.cred.APIKey)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.cred.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cred.Token)
	}
	return req, nil
}

// mapearItem converte um item cru no ResultadoExterno canônico. Devolve ok=false
// para media_type que não seja movie/tv. title/data variam por tipo (title/
// release_date para filme, name/first_air_date para série).
func mapearItem(it itemMulti) (provider.ResultadoExterno, bool) {
	var titulo, data string
	switch it.MediaType {
	case "movie":
		titulo, data = it.Title, it.ReleaseDate
	case "tv":
		titulo, data = it.Name, it.FirstAirDate
	default:
		return provider.ResultadoExterno{}, false
	}

	res := provider.ResultadoExterno{
		Source:    "tmdb",
		SourceID:  fmt.Sprintf("%d", it.ID),
		Type:      it.MediaType,
		Title:     titulo,
		Year:      anoDe(data),
		Overview:  ptrNaoVazio(it.Overview),
		PosterURL: posterURL(it.PosterPath),
		Saved:     false,
	}
	return res, true
}

// anoDe extrai os 4 primeiros caracteres (ano) de uma data ISO (YYYY-MM-DD).
// Devolve nil quando a data é vazia ou curta demais.
func anoDe(data string) *string {
	if len(data) < 4 {
		return nil
	}
	ano := data[:4]
	return &ano
}

// posterURL monta a URL direta da arte no TMDB, ou nil quando não há poster_path.
func posterURL(posterPath string) *string {
	if posterPath == "" {
		return nil
	}
	u := imagemBaseURL + posterPath
	return &u
}

// ptrNaoVazio devolve *string para uma string, ou nil quando ela é vazia.
func ptrNaoVazio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
