// Package tmdb implements provider.Provider for The Movie Database (TMDB), the
// first provider of the project. It covers movies and series in the MVP, via
// /search/multi filtered by media_type. The credential is read at boot (main) and
// injected here -- NEVER hardcoded, never logged.
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
	// baseURLPadrao is the stable base of the TMDB API (hardcoded config -- the
	// decision: it avoids a call to /configuration in the MVP).
	baseURLPadrao = "https://api.themoviedb.org/3"
	// imagemBaseURL is the base of the TMDB images, w342 size for the card poster.
	imagemBaseURL = "https://image.tmdb.org/t/p/w342"
	// backdropBaseURL is the base of the backdrop of the rich detail, in a bigger
	// size (w780) because it is hero art, not a card thumbnail.
	backdropBaseURL = "https://image.tmdb.org/t/p/w780"
	// timeoutPadrao bounds each request to TMDB; an overrun becomes an error (the
	// handler degrades to local results only).
	timeoutPadrao = 8 * time.Second
)

// Credencial is the resolved TMDB credential. The preference was decided in the
// design: Token (Read Access Token v4 → Authorization: Bearer header) over APIKey
// (v3 → api_key query). Whoever reads the env is main; here it already arrives
// resolved.
type Credencial struct {
	// Token is the Read Access Token v4 (preferred).
	Token string
	// APIKey is the api_key v3 (alternative for whoever only has it).
	APIKey string
}

// Configurada reports whether at least one credential is filled in. Without any,
// main does not even create the provider (external search off -- not an error).
func (c Credencial) Configurada() bool {
	return c.Token != "" || c.APIKey != ""
}

// Client is the TMDB provider. httpClient and baseURL are injectable for testing
// (point at an httptest.Server without the real network).
type Client struct {
	cred       Credencial
	httpClient *http.Client
	baseURL    string

	// genreMu protects genreCache. The localized genre lists change very rarely, so
	// they are cached in memory by (type|language) and reused between items -- they do
	// not multiply calls per imported item.
	genreMu    sync.Mutex
	genreCache map[string]map[int64]string
}

// New creates the TMDB client. A nil httpClient uses a default with timeout; an
// empty baseURL uses the public TMDB base.
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

// Nome identifies the source.
func (c *Client) Nome() string { return "tmdb" }

// respostaMulti mirrors the /search/multi payload (only the fields used).
type respostaMulti struct {
	Results []itemMulti `json:"results"`
}

// itemMulti is a raw result of /search/multi. media_type discriminates the type
// (movie|tv|person|…); only movie and tv interest us.
type itemMulti struct {
	ID            int64  `json:"id"`
	MediaType     string `json:"media_type"`
	Title         string `json:"title"`          // movies
	Name          string `json:"name"`           // series
	OriginalTitle string `json:"original_title"` // movies: the title in the original language
	OriginalName  string `json:"original_name"`  // series: the title in the original language
	Overview      string `json:"overview"`
	PosterPath    string `json:"poster_path"`
	ReleaseDate   string `json:"release_date"`   // movies
	FirstAirDate  string `json:"first_air_date"` // series
}

// Buscar queries /search/multi and maps the results to ResultadoExterno, discarding
// media_type outside {movie, tv}. If `tipo` is movie/tv, it filters by it as well.
// Error/timeout is propagated (the handler degrades).
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
			continue // discards person and other media_type
		}
		if tipo != "" && res.Type != tipo {
			continue // optional filter by type
		}
		out = append(out, res)
	}
	return out, nil
}

// montarRequisicao builds the GET /search/multi with the search term and, when
// informed, the reading language (`language=`, e.g. pt-BR) -- so the titles/overviews
// of the search come in the active language. Authentication is applied by novoGet
// (common to all calls).
func (c *Client) montarRequisicao(ctx context.Context, query, lang string) (*http.Request, error) {
	params := url.Values{"query": {query}}
	if lang != "" {
		params.Set("language", lang)
	}
	return c.novoGet(ctx, "/search/multi", params)
}

// novoGet builds a GET for `path` (relative to the base) with the `params` query and
// the authentication chosen at boot: Token v4 → Authorization: Bearer header;
// otherwise APIKey v3 → api_key query. The SINGLE auth point -- search and detail go
// through here (the credential is never logged).
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

// BuscarDetalhe pulls the detail of the item from the source already MULTI-LANGUAGE:
// GET {base}/{movie|tv}/{id}?append_to_response=translations (base language = the
// first of `langs`) in a single round trip, and fetches/reuses the localized genre
// lists (one per allowed language, cached) to translate the genres of the item. It
// returns the AUGMENTED PAYLOAD `{detail, tags_by_lang}` (still a re-composable cache
// -- ADR-0011): `detail` is the raw response with the translations; `tags_by_lang` is
// the derived language→[genre names] map. Without `langs`, it degrades to
// mono-language (envelope with only the original language of the item).
// Error/timeout is propagated.
func (c *Client) BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("tmdb: sourceID vazio")
	}
	if tipo != "movie" && tipo != "tv" {
		return nil, fmt.Errorf("tmdb: tipo inválido %q (use movie|tv)", tipo)
	}

	// append_to_response in a single round trip: translations (title/overview/tagline
	// translations), videos, keywords, release_dates (movie rating), content_ratings
	// (series rating), external_ids (imdb…) and recommendations. TMDB ignores the
	// appends that do not apply to the type (e.g. content_ratings on a movie).
	// belongs_to_collection already comes in the movie detail, without append.
	//
	// RECOMMENDATIONS: switched from `similar` (generic matching by genre/keyword,
	// noisy -- a report from the owner about a title with no relation of theme/tone)
	// to `recommendations` (behavior signal, "whoever watched this also watched",
	// more relevant). The canonical filters/sorts the result (see tmdb_rico.go).
	//
	// CREDITS (MD6): `credits` brings the cast/crew of the MOVIE; `aggregate_credits`
	// the one of the SERIES (aggregated per episode). We request both -- the source
	// ignores what does not apply to the type; the mapping prefers the aggregated one
	// when present (series).
	params := url.Values{"append_to_response": {"translations,videos,keywords,release_dates,content_ratings,external_ids,recommendations,credits,aggregate_credits"}}
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

// BuscarTitulosAlternativos implements provider.TituloAlternativoProvider: GET
// {base}/{movie|tv}/{id}?append_to_response=translations -- a NEW and lighter call
// than BuscarDetalhe, WITHOUT the heavy appends (credits/keywords/similar/etc), used
// only to rescore the TOP-3 candidates of the auto-match against the range of titles
// that TMDB knows. It extracts `title` (movie) or `name` (series) from each item of
// `translations.translations[].data` -- there may be empty/duplicate strings, the
// caller does not require perfect dedupe. Error/timeout propagates (the caller
// degrades, best-effort).
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

// getBytes performs an authenticated GET to `path` with the `params` and returns the
// body, with a status check. The single reading I/O point (detail and genre lists).
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
		// Item that does not exist in the source: sentinel for the handler to answer 404 (not 502).
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

// tagsPorIdioma maps the genre ids of the item to the names in EACH allowed language,
// through the localized genre lists (cached). A language whose list fails is SKIPPED
// (it degrades that language, not the search). Without `langs`, it degrades using the
// genre names embedded in the detail itself, under the original language of the item.
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
			continue // degrades this language; the others go on
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

// traduzirGeneros maps the genre ids of the item to the names in the catalog of a
// language (an id missing from the catalog is discarded). Stable order = order of the
// detail.
func traduzirGeneros(generos []generoTMDB, catalogo map[int64]string) []string {
	nomes := make([]string, 0, len(generos))
	for _, g := range generos {
		if nome, ok := catalogo[g.ID]; ok && nome != "" {
			nomes = append(nomes, nome)
		}
	}
	return nomes
}

// listaDeGeneros returns the id→name map of the genres of `tipo` in `lang`, from the
// in-memory cache when present (it rarely goes stale) or fetching
// GET /genre/{movie|tv}/list and caching it. The network runs OUTSIDE the lock (it
// allows concurrency); a race tie only refetches, without corrupting the cache.
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

// envelopeTMDB is the AUGMENTED PAYLOAD written to media_external: the raw detail
// (with the translations appended) plus the tags already translated per language
// (derived in enrich, which does network -- read-time is pure). Detecting `detail` !=
// empty distinguishes the new envelope from the LEGACY payload (raw detail).
type envelopeTMDB struct {
	Detail     json.RawMessage     `json:"detail"`
	TagsByLang map[string][]string `json:"tags_by_lang"`
}

// detalheTMDB mirrors the /movie/{id} and /tv/{id} payload (only the fields used).
// title/release_date are movie ones; name/first_air_date series ones -- we map both.
// Translations comes from append_to_response=translations; original_language is the
// anchor of the per-language fallback.
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

	// Fields of the RICH DETAIL, filled in by the widened append_to_response.
	Tagline             string              `json:"tagline"`
	BackdropPath        string              `json:"backdrop_path"`
	VoteAverage         float64             `json:"vote_average"`
	Runtime             int                 `json:"runtime"`          // movie (minutes)
	EpisodeRunTime      []int               `json:"episode_run_time"` // series (minutes/ep)
	BelongsToCollection *colecaoTMDB        `json:"belongs_to_collection"`
	Videos              videosTMDB          `json:"videos"`
	Keywords            keywordsTMDB        `json:"keywords"`
	ReleaseDates        listaPaisTMDB       `json:"release_dates"`   // movie rating
	ContentRatings      contentRatingsTMDB  `json:"content_ratings"` // series rating
	ExternalIds         externalIdsTMDB     `json:"external_ids"`
	Recommendations     recommendationsTMDB `json:"recommendations"`

	// CREDITS blocks (MD6): `credits` (movie, flat cast/crew), `aggregate_credits`
	// (series, cast with roles/crew with jobs aggregated per episode) and
	// `production_companies` (production companies, already comes in the detail without
	// append). Mapped to the canonical cast/crew/companies by tmdb_creditos.go.
	Credits             creditsTMDB          `json:"credits"`
	AggregateCredits    aggregateCreditsTMDB `json:"aggregate_credits"`
	ProductionCompanies []companyTMDB        `json:"production_companies"`

	// Status is the SITUATION of the series (only /tv brings it: "Returning Series"|
	// "Ended"|"Canceled"|"In Production"|"Planned"). Empty for a movie. RAW/neutral
	// value -- the UI translates the label.
	Status string `json:"status"`
}

// generoTMDB is an item of `genres` (from the detail or from the localized list). The
// id matches the genre between the per-language lists; the name is the tag in that
// language.
type generoTMDB struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// traducoesTMDB is the `translations` block appended to the detail: TMDB delivers it
// as an object that WRAPS the `translations` array (plus an `id`, ignored here).
type traducoesTMDB struct {
	Translations []traducaoTMDB `json:"translations"`
}

// traducaoTMDB is a translation: the language (iso_639_1 + iso_3166_1) and the
// translated `data`. For a movie, data brings `title`; for a series, `name`.
type traducaoTMDB struct {
	Iso3166 string        `json:"iso_3166_1"`
	Iso639  string        `json:"iso_639_1"`
	Data    dadosTraducao `json:"data"`
}

// dadosTraducao are the translatable fields of a translation (title/name/overview
// and, for the rich detail, the tagline/slogan per language).
type dadosTraducao struct {
	Title    string `json:"title"`
	Name     string `json:"name"`
	Overview string `json:"overview"`
	Tagline  string `json:"tagline"`
}

// MapearDetalhe translates the TMDB payload to the CANONICAL map (our names). It is
// PURE (no network): it detects the new ENVELOPE vs the LEGACY payload (raw detail)
// and maps accordingly. It only includes the keys that the source brought -- so the
// resolver distinguishes "absent field" from "empty field". WITHOUT cast/crew.
func (c *Client) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	// RICH SERIES (route C): the enrich of the series node writes the rich canonical
	// ALREADY MAPPED under {ChaveEnvelopeSerieRico: ...}; here we only return it as it
	// is (passthrough) -- it comes before the other branches (the key is exclusive, it
	// does not collide with the envelope/legacy).
	if campos, ok := mapearSerieRico(raw); ok {
		return campos, nil
	}
	var env envelopeTMDB
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Detail) > 0 {
		return mapearEnvelope(env)
	}
	return mapearLegado(raw)
}

// detalheBrutoDoRaw extracts the bytes of the RAW DETAIL from a `raw`: the AUGMENTED
// envelope {detail,tags_by_lang} when present, or `raw` itself when it is the LEGACY
// payload without an envelope -- the same detection that MapearDetalhe already does
// (mapearEnvelope/mapearLegado). Extracted so that the new Similares method
// (tmdb_rico.go) can reuse it without duplicating the parsing of the envelope.
func detalheBrutoDoRaw(raw json.RawMessage) json.RawMessage {
	var env envelopeTMDB
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Detail) > 0 {
		return env.Detail
	}
	return raw
}

// mapearSerieRico detects the RICH SERIES envelope and returns the rich canonical that
// it carries. ok=false for any other payload (normal envelope, legacy): the
// ChaveEnvelopeSerieRico key is exclusive to this path.
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

// mapearEnvelope maps the AUGMENTED PAYLOAD: title/description as map[language]value
// (from the translations, plus the original_title under the original language, which is
// the only form of the top guaranteed to be IN the original language), tags as
// map[language][]string (from tags_by_lang), and the neutral ones as scalars. Each
// language is keyed both by the COMBINED code (iso_639_1-iso_3166_1, e.g. pt-BR) and by
// the BASE code (iso_639_1, e.g. en) -- so the resolver matches both "pt-BR" and "en"
// (which in TMDB comes as en-US) without knowing the configured region.
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
	// Fields of the RICH DETAIL: tagline/keywords translatable (map by language) and
	// the neutral ones (runtime, rating, backdrop, videos, collection, similar, score,
	// links). The entity resolver IGNORES the keys it does not know (the registry only
	// iterates title/description/tags/…), so enriching the canonical here is safe for
	// the import; whoever consumes the rich fields is the external detail handler.
	enriquecerCanonicoRico(out, d)
	return out, nil
}

// mapearLegado maps the LEGACY payload (raw detail, without envelope): the
// translatable fields come out as SCALARS; the resolver treats them as belonging to
// the main language (tolerance). It preserves the pre-multi-language behavior.
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

// traduzidosPorIdioma builds the language→title and language→description maps from
// the translations of the detail, keying each language by the COMBINED code and by the
// BASE one (see mapearEnvelope), and anchoring the original language with the
// original_title (the only field of the top guaranteed to be in the original
// language).
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

// combinarIdioma joins iso_639_1 and iso_3166_1 into our code (e.g. "pt"+"BR" →
// "pt-BR"); without a region it returns only the language ("en"). Empty when there is
// no language.
func combinarIdioma(iso639, iso3166 string) string {
	if iso639 == "" {
		return ""
	}
	if iso3166 == "" {
		return iso639
	}
	return iso639 + "-" + iso3166
}

// porSeAusente writes a non-empty value under a non-empty key, ONLY if the key does
// not exist yet (the first translation per language wins -- stable TMDB order).
func porSeAusente(m map[string]any, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}

// tagsMap converts the tags_by_lang (language→[]string) into the map[language]any that
// the resolver consumes, discarding empty languages/ones without tags.
func tagsMap(byLang map[string][]string) map[string]any {
	out := map[string]any{}
	for lang, nomes := range byLang {
		if lang != "" && len(nomes) > 0 {
			out[lang] = nomes
		}
	}
	return out
}

// primeiroNaoVazio returns the first non-empty argument (TMDB uses distinct fields
// for movie and series).
func primeiroNaoVazio(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// nomesDosGeneros extracts the names from `genres` as a canonical tag list.
func nomesDosGeneros(gs []generoTMDB) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}

// mapearItem converts a raw item into the canonical ResultadoExterno. It returns
// ok=false for a media_type that is not movie/tv. title/date vary by type
// (title/release_date for a movie, name/first_air_date for a series).
func mapearItem(it itemMulti) (provider.ResultadoExterno, bool) {
	var titulo, original, data string
	switch it.MediaType {
	case "movie":
		titulo, original, data = it.Title, it.OriginalTitle, it.ReleaseDate
	case "tv":
		titulo, original, data = it.Name, it.OriginalName, it.FirstAirDate
	default:
		return provider.ResultadoExterno{}, false
	}

	res := provider.ResultadoExterno{
		Source:   "tmdb",
		SourceID: fmt.Sprintf("%d", it.ID),
		Type:     it.MediaType,
		Title:    titulo,
		// The original title comes in the SAME search payload -- no extra request. It is
		// what the local folder name usually matches ("White Collar" against the pt-BR
		// "Crimes do Colarinho Branco" is a ~0 score).
		TituloOriginal: original,
		Year:           anoDe(data),
		Overview:       ptrNaoVazio(it.Overview),
		PosterURL:      posterURL(it.PosterPath),
		Saved:          false,
	}
	return res, true
}

// anoDe extracts the first 4 characters (year) of an ISO date (YYYY-MM-DD). It
// returns nil when the date is empty or too short.
func anoDe(data string) *string {
	if len(data) < 4 {
		return nil
	}
	ano := data[:4]
	return &ano
}

// posterURL builds the direct art URL on TMDB, or nil when there is no poster_path.
func posterURL(posterPath string) *string {
	if posterPath == "" {
		return nil
	}
	u := imagemBaseURL + posterPath
	return &u
}

// ptrNaoVazio returns *string for a string, or nil when it is empty.
func ptrNaoVazio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
