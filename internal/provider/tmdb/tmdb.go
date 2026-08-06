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
	"sync"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// baseURLPadrao é a base estável da API do TMDB (config hardcoded — decisão
	// do briefing: evita uma chamada a /configuration no MVP).
	baseURLPadrao = "https://api.themoviedb.org/3"
	// imagemBaseURL é a base das imagens do TMDB, tamanho w342 para pôster de card.
	imagemBaseURL = "https://image.tmdb.org/t/p/w342"
	// backdropBaseURL é a base do fundo (backdrop) do detalhe rico (0022), num
	// tamanho maior (w780) por ser arte de destaque, não miniatura de card.
	backdropBaseURL = "https://image.tmdb.org/t/p/w780"
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

	// genreMu protege genreCache. As listas de gênero localizadas (0020) mudam
	// muito raramente, então são cacheadas em memória por (tipo|idioma) e reusadas
	// entre itens — não multiplicam chamadas por item importado.
	genreMu    sync.Mutex
	genreCache map[string]map[int64]string
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
	return &Client{
		cred:       cred,
		httpClient: httpClient,
		baseURL:    baseURL,
		genreCache: map[string]map[int64]string{},
	}
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
func (c *Client) Buscar(ctx context.Context, query, tipo, lang string) ([]provider.ResultadoExterno, error) {
	if query == "" {
		return nil, nil
	}

	req, err := c.montarRequisicao(ctx, query, lang)
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

// montarRequisicao constrói o GET /search/multi com o termo de busca e, quando
// informado, o idioma de leitura (`language=`, ex. pt-BR) — assim os títulos/
// sinopses da busca vêm no idioma ativo. A autenticação é aplicada por novoGet
// (comum a todas as chamadas).
func (c *Client) montarRequisicao(ctx context.Context, query, lang string) (*http.Request, error) {
	params := url.Values{"query": {query}}
	if lang != "" {
		params.Set("language", lang)
	}
	return c.novoGet(ctx, "/search/multi", params)
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

// BuscarDetalhe puxa o detalhe do item na fonte já MULTI-IDIOMA (briefing 0020):
// GET {base}/{movie|tv}/{id}?append_to_response=translations (idioma base = o
// primeiro de `langs`) numa só ida, e busca/reusa as listas de gênero localizadas
// (uma por idioma permitido, cacheadas) para traduzir os gêneros do item. Devolve
// o PAYLOAD AUMENTADO `{detail, tags_by_lang}` (ainda um cache re-componível —
// ADR-0011): `detail` é a resposta crua com as traduções; `tags_by_lang` é o
// mapa idioma→[nomes de gênero] derivado. Sem `langs`, degrada ao mono-idioma
// (envelope só com o idioma original do item). Erro/timeout é propagado.
func (c *Client) BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("tmdb: sourceID vazio")
	}
	if tipo != "movie" && tipo != "tv" {
		return nil, fmt.Errorf("tmdb: tipo inválido %q (use movie|tv)", tipo)
	}

	// append_to_response numa só ida (0022): translations (traduções de título/
	// sinopse/slogan), videos, keywords, release_dates (classificação de filme),
	// content_ratings (classificação de série), external_ids (imdb…) e similar. O
	// TMDB ignora os appends que não se aplicam ao tipo (ex.: content_ratings em
	// filme). belongs_to_collection já vem no detalhe de filme, sem append.
	//
	// CREDITOS (MD6, 0029): `credits` traz o elenco/equipe do FILME; `aggregate_credits`
	// o da SERIE (agregado por episodio). Pedimos os dois — a fonte ignora o que não se
	// aplica ao tipo; o mapeamento prefere o agregado quando presente (série).
	params := url.Values{"append_to_response": {"translations,videos,keywords,release_dates,content_ratings,external_ids,similar,credits,aggregate_credits"}}
	if len(langs) > 0 && langs[0] != "" {
		params.Set("language", langs[0])
	}
	detail, err := c.getBytes(ctx, "/"+tipo+"/"+url.PathEscape(sourceID), params)
	if err != nil {
		return nil, err
	}

	var d detalheTMDB
	if err := json.Unmarshal(detail, &d); err != nil {
		return nil, fmt.Errorf("tmdb: detalhe inválido: %w", err)
	}

	env := envelopeTMDB{
		Detail:     json.RawMessage(detail),
		TagsByLang: c.tagsPorIdioma(ctx, tipo, d, langs),
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("tmdb: montar payload aumentado: %w", err)
	}
	return json.RawMessage(body), nil
}

// BuscarTitulosAlternativos implementa provider.TituloAlternativoProvider
// (briefing 0084): GET {base}/{movie|tv}/{id}?append_to_response=translations
// -- uma chamada NOVA e mais leve que BuscarDetalhe, SEM os appends pesados
// (credits/keywords/similar/etc), usada só para repontuar os TOP-3 candidatos
// do auto-match contra o leque de títulos que o TMDB conhece. Extrai `title`
// (filme) ou `name` (série) de cada item de `translations.translations[].data`
// -- pode haver strings vazias/duplicatas, o chamador não exige dedupe
// perfeito. Erro/timeout propaga (o chamador degrada, best-effort).
func (c *Client) BuscarTitulosAlternativos(ctx context.Context, tipo, sourceID string) ([]string, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("tmdb: sourceID vazio")
	}
	if tipo != "movie" && tipo != "tv" {
		return nil, fmt.Errorf("tmdb: tipo inválido %q (use movie|tv)", tipo)
	}

	params := url.Values{"append_to_response": {"translations"}}
	body, err := c.getBytes(ctx, "/"+tipo+"/"+url.PathEscape(sourceID), params)
	if err != nil {
		return nil, err
	}

	var d detalheTMDB
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("tmdb: titulos alternativos invalidos: %w", err)
	}

	out := make([]string, 0, len(d.Translations.Translations))
	for _, tr := range d.Translations.Translations {
		if titulo := primeiroNaoVazio(tr.Data.Title, tr.Data.Name); titulo != "" {
			out = append(out, titulo)
		}
	}
	return out, nil
}

// getBytes faz um GET autenticado a `path` com os `params` e devolve o corpo, com
// checagem de status. Ponto único de I/O de leitura (detalhe e listas de gênero).
func (c *Client) getBytes(ctx context.Context, path string, params url.Values) ([]byte, error) {
	req, err := c.novoGet(ctx, path, params)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		// Item inexistente na fonte: sentinela para o handler responder 404 (não 502).
		return nil, fmt.Errorf("tmdb: %s: %w", path, provider.ErrNaoEncontrado)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb: status inesperado %d em %s", resp.StatusCode, path)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tmdb: ler %s: %w", path, err)
	}
	return body, nil
}

// tagsPorIdioma mapeia os ids de gênero do item para os nomes em CADA idioma
// permitido, via as listas de gênero localizadas (cacheadas). Um idioma cuja lista
// falhe é PULADO (degrada aquele idioma, não a busca). Sem `langs`, degrada usando
// os nomes de gênero embutidos no próprio detalhe, sob o idioma original do item.
func (c *Client) tagsPorIdioma(ctx context.Context, tipo string, d detalheTMDB, langs []string) map[string][]string {
	if len(langs) == 0 {
		nomes := nomesDosGeneros(d.Genres)
		if len(nomes) == 0 || d.OriginalLanguage == "" {
			return nil
		}
		return map[string][]string{d.OriginalLanguage: nomes}
	}
	out := map[string][]string{}
	for _, lang := range langs {
		if lang == "" {
			continue
		}
		catalogo, err := c.listaDeGeneros(ctx, tipo, lang)
		if err != nil {
			continue // degrada este idioma; os outros seguem
		}
		if nomes := traduzirGeneros(d.Genres, catalogo); len(nomes) > 0 {
			out[lang] = nomes
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// traduzirGeneros mapeia os ids de gênero do item para os nomes no catálogo de um
// idioma (id ausente no catálogo é descartado). Ordem estável = ordem do detalhe.
func traduzirGeneros(generos []generoTMDB, catalogo map[int64]string) []string {
	nomes := make([]string, 0, len(generos))
	for _, g := range generos {
		if nome, ok := catalogo[g.ID]; ok && nome != "" {
			nomes = append(nomes, nome)
		}
	}
	return nomes
}

// listaDeGeneros devolve o mapa id→nome dos gêneros de `tipo` no `lang`, do cache
// em memória quando presente (invalida raro) ou buscando GET /genre/{movie|tv}/list
// e cacheando. A rede roda FORA do lock (permite concorrência); um empate de corrida
// só re-busca, sem corromper o cache.
func (c *Client) listaDeGeneros(ctx context.Context, tipo, lang string) (map[int64]string, error) {
	chave := tipo + "|" + lang
	c.genreMu.Lock()
	cached, ok := c.genreCache[chave]
	c.genreMu.Unlock()
	if ok {
		return cached, nil
	}

	body, err := c.getBytes(ctx, "/genre/"+tipo+"/list", url.Values{"language": {lang}})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Genres []generoTMDB `json:"genres"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("tmdb: lista de generos invalida: %w", err)
	}
	m := make(map[int64]string, len(resp.Genres))
	for _, g := range resp.Genres {
		m[g.ID] = g.Name
	}

	c.genreMu.Lock()
	c.genreCache[chave] = m
	c.genreMu.Unlock()
	return m, nil
}

// envelopeTMDB é o PAYLOAD AUMENTADO gravado em media_external (0020): o detalhe
// cru (com as traduções appended) mais as tags já traduzidas por idioma (derivadas
// no enrich, que faz rede — o read-time é puro). Detectar `detail` != vazio
// distingue o envelope novo do payload LEGADO (detalhe cru do 0015).
type envelopeTMDB struct {
	Detail     json.RawMessage     `json:"detail"`
	TagsByLang map[string][]string `json:"tags_by_lang"`
}

// detalheTMDB espelha o payload de /movie/{id} e /tv/{id} (só os campos usados).
// title/release_date são de filme; name/first_air_date de série — mapeamos ambos.
// Translations vem do append_to_response=translations; original_language é a âncora
// do fallback por idioma (0019).
type detalheTMDB struct {
	ID               int64         `json:"id"`
	Title            string        `json:"title"`
	Name             string        `json:"name"`
	OriginalTitle    string        `json:"original_title"`
	OriginalName     string        `json:"original_name"`
	OriginalLanguage string        `json:"original_language"`
	Overview         string        `json:"overview"`
	ReleaseDate      string        `json:"release_date"`
	FirstAirDate     string        `json:"first_air_date"`
	PosterPath       string        `json:"poster_path"`
	Genres           []generoTMDB  `json:"genres"`
	Translations     traducoesTMDB `json:"translations"`

	// Campos do DETALHE RICO (0022), preenchidos pelo append_to_response ampliado.
	Tagline             string             `json:"tagline"`
	BackdropPath        string             `json:"backdrop_path"`
	VoteAverage         float64            `json:"vote_average"`
	Runtime             int                `json:"runtime"`          // filme (minutos)
	EpisodeRunTime      []int              `json:"episode_run_time"` // série (minutos/ep)
	BelongsToCollection *colecaoTMDB       `json:"belongs_to_collection"`
	Videos              videosTMDB         `json:"videos"`
	Keywords            keywordsTMDB       `json:"keywords"`
	ReleaseDates        listaPaisTMDB      `json:"release_dates"`   // classificação de filme
	ContentRatings      contentRatingsTMDB `json:"content_ratings"` // classificação de série
	ExternalIds         externalIdsTMDB    `json:"external_ids"`
	Similar             similarTMDB        `json:"similar"`

	// Blocos de CREDITOS (MD6, briefing 0029): `credits` (filme, cast/crew planos),
	// `aggregate_credits` (serie, cast com roles/crew com jobs agregados por episodio) e
	// `production_companies` (produtoras, ja vem no detalhe sem append). Mapeados para o
	// canonico cast/crew/companies por tmdb_creditos.go.
	Credits             creditsTMDB          `json:"credits"`
	AggregateCredits    aggregateCreditsTMDB `json:"aggregate_credits"`
	ProductionCompanies []companyTMDB        `json:"production_companies"`

	// Status é a SITUAÇÃO da série (só o /tv o traz: "Returning Series"|"Ended"|
	// "Canceled"|"In Production"|"Planned"). Vazio para filme. Valor CRU/neutro — a UI
	// traduz o rótulo (briefing 0045).
	Status string `json:"status"`
}

// generoTMDB é um item de `genres` (do detalhe ou da lista localizada). O id casa o
// gênero entre as listas por idioma; o nome é a tag naquele idioma.
type generoTMDB struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// traducoesTMDB é o bloco `translations` appended ao detalhe: o TMDB o entrega como
// um objeto que ENVELOPA o array `translations` (mais um `id`, ignorado aqui).
type traducoesTMDB struct {
	Translations []traducaoTMDB `json:"translations"`
}

// traducaoTMDB é uma tradução: o idioma (iso_639_1 + iso_3166_1) e os `data`
// traduzidos. Para filme, data traz `title`; para série, `name`.
type traducaoTMDB struct {
	Iso3166 string        `json:"iso_3166_1"`
	Iso639  string        `json:"iso_639_1"`
	Data    dadosTraducao `json:"data"`
}

// dadosTraducao são os campos traduzíveis de uma tradução (title/name/overview e,
// para o detalhe rico do 0022, o tagline/slogan por idioma).
type dadosTraducao struct {
	Title    string `json:"title"`
	Name     string `json:"name"`
	Overview string `json:"overview"`
	Tagline  string `json:"tagline"`
}

// MapearDetalhe traduz o payload do TMDB para o mapa CANÔNICO (nossos nomes). É
// PURO (sem rede): detecta o ENVELOPE novo (0020) vs o payload LEGADO (detalhe cru
// do 0015) e mapeia conforme. Só inclui as chaves que a fonte trouxe — assim o
// resolver distingue "campo ausente" de "campo vazio". SEM elenco/equipe (MD6).
func (c *Client) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	// SÉRIE-RICO (0045, rota C): o enrich do nó série grava o canônico rico JÁ MAPEADO
	// sob {ChaveEnvelopeSerieRico: ...}; aqui só o devolvemos como está (passthrough) —
	// vem antes dos demais ramos (a chave é exclusiva, não colide com o envelope/legado).
	if campos, ok := mapearSerieRico(raw); ok {
		return campos, nil
	}
	var env envelopeTMDB
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Detail) > 0 {
		return mapearEnvelope(env)
	}
	return mapearLegado(raw)
}

// mapearSerieRico detecta o envelope SÉRIE-RICO (0045) e devolve o canônico rico que
// ele carrega. ok=false para qualquer outro payload (envelope normal do 0020, legado do
// 0015): a chave ChaveEnvelopeSerieRico é exclusiva deste caminho.
func mapearSerieRico(raw json.RawMessage) (map[string]any, bool) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, false
	}
	blob, ok := env[provider.ChaveEnvelopeSerieRico]
	if !ok || len(blob) == 0 {
		return nil, false
	}
	var campos map[string]any
	if err := json.Unmarshal(blob, &campos); err != nil {
		return nil, false
	}
	return campos, true
}

// mapearEnvelope mapeia o PAYLOAD AUMENTADO (0020): title/description como
// map[idioma]valor (das translations, mais o original_title sob o idioma original,
// que é a única forma do topo garantidamente NO idioma original), tags como
// map[idioma][]string (do tags_by_lang), e os neutros escalares. Cada idioma é
// chaveado tanto pelo código COMBINADO (iso_639_1-iso_3166_1, ex. pt-BR) quanto pelo
// código-BASE (iso_639_1, ex. en) — assim o resolver casa tanto "pt-BR" quanto "en"
// (que no TMDB vem como en-US) sem conhecer a região configurada.
func mapearEnvelope(env envelopeTMDB) (map[string]any, error) {
	var d detalheTMDB
	if err := json.Unmarshal(env.Detail, &d); err != nil {
		return nil, fmt.Errorf("tmdb: detalhe do envelope invalido: %w", err)
	}

	out := map[string]any{}
	titulos, descricoes := traduzidosPorIdioma(d)
	if len(titulos) > 0 {
		out["title"] = titulos
	}
	if len(descricoes) > 0 {
		out["description"] = descricoes
	}
	if tags := tagsMap(env.TagsByLang); len(tags) > 0 {
		out["tags"] = tags
	}
	if orig := primeiroNaoVazio(d.OriginalTitle, d.OriginalName); orig != "" {
		out["original_title"] = orig
	}
	if data := primeiroNaoVazio(d.ReleaseDate, d.FirstAirDate); data != "" {
		out["release_date"] = data
	}
	if d.PosterPath != "" {
		out["poster_url"] = imagemBaseURL + d.PosterPath
	}
	if d.OriginalLanguage != "" {
		out["original_language"] = d.OriginalLanguage
	}
	// Campos do DETALHE RICO (0022): tagline/keywords traduzíveis (map por idioma) e
	// os neutros (runtime, classificação, backdrop, vídeos, coleção, similares, nota,
	// links). O resolver de entity IGNORA as chaves que não conhece (o registro só
	// itera title/description/tags/…), então enriquecer o canônico aqui é seguro para
	// o import; quem consome os campos ricos é o handler do detalhe externo.
	enriquecerCanonicoRico(out, d)
	return out, nil
}

// mapearLegado mapeia o payload LEGADO (detalhe cru do 0015, sem envelope): os
// campos traduzíveis saem ESCALARES; o resolver os trata como do idioma principal
// (tolerância do 0019). Preserva o comportamento pré-multi-idioma.
func mapearLegado(raw json.RawMessage) (map[string]any, error) {
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
	if d.OriginalLanguage != "" {
		out["original_language"] = d.OriginalLanguage
	}
	return out, nil
}

// traduzidosPorIdioma monta os mapas idioma→título e idioma→descrição a partir das
// translations do detalhe, chaveando cada idioma pelo código COMBINADO e pelo BASE
// (ver mapearEnvelope), e ancorando o idioma original com o original_title (o único
// campo do topo garantidamente no idioma original).
func traduzidosPorIdioma(d detalheTMDB) (titulos, descricoes map[string]any) {
	titulos = map[string]any{}
	descricoes = map[string]any{}
	for _, tr := range d.Translations.Translations {
		combinado := combinarIdioma(tr.Iso639, tr.Iso3166)
		if combinado == "" {
			continue
		}
		titulo := primeiroNaoVazio(tr.Data.Title, tr.Data.Name)
		porSeAusente(titulos, combinado, titulo)
		porSeAusente(titulos, tr.Iso639, titulo)
		porSeAusente(descricoes, combinado, tr.Data.Overview)
		porSeAusente(descricoes, tr.Iso639, tr.Data.Overview)
	}
	if orig := d.OriginalLanguage; orig != "" {
		porSeAusente(titulos, orig, primeiroNaoVazio(d.OriginalTitle, d.OriginalName))
	}
	return titulos, descricoes
}

// combinarIdioma junta iso_639_1 e iso_3166_1 no nosso código (ex. "pt"+"BR" →
// "pt-BR"); sem região devolve só o idioma ("en"). Vazio quando não há idioma.
func combinarIdioma(iso639, iso3166 string) string {
	if iso639 == "" {
		return ""
	}
	if iso3166 == "" {
		return iso639
	}
	return iso639 + "-" + iso3166
}

// porSeAusente grava valor não-vazio em chave não-vazia, SÓ se a chave ainda não
// existe (a primeira tradução por idioma vence — ordem estável do TMDB).
func porSeAusente(m map[string]any, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}

// tagsMap converte o tags_by_lang (idioma→[]string) no map[idioma]any que o
// resolver consome, descartando idiomas vazios/sem tags.
func tagsMap(byLang map[string][]string) map[string]any {
	out := map[string]any{}
	for lang, nomes := range byLang {
		if lang != "" && len(nomes) > 0 {
			out[lang] = nomes
		}
	}
	return out
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
