// Package tmdb implementa o provider.Provider para o The Movie Database (TMDB),
// o primeiro provider do projeto (briefing 0014). Cobre filmes e séries no MVP,
// via /search/multi filtrado por media_type. A credencial é lida no boot (main)
// e injetada aqui — NUNCA hardcoded, nunca logada.
package tmdb

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

// montarRequisicao constrói o GET /search/multi com o termo de busca. A
// autenticação é aplicada por novoGet (comum a todas as chamadas).
func (c *Client) montarRequisicao(ctx context.Context, query string) (*http.Request, error) {
	return c.novoGet(ctx, "/search/multi", url.Values{"query": {query}})
}

// novoGet monta um GET para `path` (relativo à base) com os `params` de query e a
// autenticação escolhida no boot: Token v4 → header Authorization: Bearer; senão
// APIKey v3 → query api_key. Ponto ÚNICO de auth — busca e detalhe passam por aqui
// (a credencial nunca é logada).
func (c *Client) novoGet(ctx context.Context, path string, params url.Values) (*http.Request, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for k, vs := range params {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
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

// BuscarDetalhe puxa o detalhe cru de um item pelo id na fonte (briefing 0015):
// GET {base}/{movie|tv}/{id}. Devolve o corpo CRU (o cache media_external guarda o
// payload como veio — ADR-0011). Erro/timeout é propagado (o handler decide).
func (c *Client) BuscarDetalhe(ctx context.Context, sourceID, tipo string) (json.RawMessage, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("tmdb: sourceID vazio")
	}
	if tipo != "movie" && tipo != "tv" {
		return nil, fmt.Errorf("tmdb: tipo inválido %q (use movie|tv)", tipo)
	}

	req, err := c.novoGet(ctx, "/"+tipo+"/"+url.PathEscape(sourceID), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb: status inesperado %d ao buscar detalhe", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tmdb: ler detalhe: %w", err)
	}
	return json.RawMessage(body), nil
}

// detalheTMDB espelha o payload de /movie/{id} e /tv/{id} (só os campos usados).
// title/release_date são de filme; name/first_air_date de série — mapeamos ambos.
type detalheTMDB struct {
	Title         string       `json:"title"`
	Name          string       `json:"name"`
	OriginalTitle string       `json:"original_title"`
	OriginalName  string       `json:"original_name"`
	Overview      string       `json:"overview"`
	ReleaseDate   string       `json:"release_date"`
	FirstAirDate  string       `json:"first_air_date"`
	PosterPath    string       `json:"poster_path"`
	Genres        []generoTMDB `json:"genres"`
}

// generoTMDB é um item de `genres` do detalhe (só o nome nos interessa como tag).
type generoTMDB struct {
	Name string `json:"name"`
}

// MapearDetalhe traduz o payload cru do TMDB para o mapa CANÔNICO (nossos nomes).
// Só inclui as chaves que a fonte de fato trouxe — assim o resolver distingue
// "campo ausente" de "campo vazio" (a primeira fonte COM valor forma a base). SEM
// elenco/equipe (MD6 adiado).
func (c *Client) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	var d detalheTMDB
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("tmdb: detalhe inválido: %w", err)
	}

	out := map[string]any{}
	if titulo := primeiroNaoVazio(d.Title, d.Name); titulo != "" {
		out["title"] = titulo
	}
	if orig := primeiroNaoVazio(d.OriginalTitle, d.OriginalName); orig != "" {
		out["original_title"] = orig
	}
	if d.Overview != "" {
		out["description"] = d.Overview
	}
	if data := primeiroNaoVazio(d.ReleaseDate, d.FirstAirDate); data != "" {
		out["release_date"] = data
	}
	if tags := nomesDosGeneros(d.Genres); len(tags) > 0 {
		out["tags"] = tags
	}
	if d.PosterPath != "" {
		out["poster_url"] = imagemBaseURL + d.PosterPath
	}
	return out, nil
}

// primeiroNaoVazio devolve o primeiro argumento não-vazio (o TMDB usa campos
// distintos para filme e série).
func primeiroNaoVazio(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// nomesDosGeneros extrai os nomes de `genres` como lista de tags canônica.
func nomesDosGeneros(gs []generoTMDB) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
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
