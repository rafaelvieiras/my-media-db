// Package fake is a DETERMINISTIC fixture provider for the E2E tests. It
// implements provider.Provider + provider.ColecaoProvider serving an in-memory
// catalog (the Matrix franchise + its collection), multi-language, WITHOUT
// NETWORK and WITHOUT SECRETS. Nome() returns "tmdb" on purpose: that way the
// generated columns (external_id_tmdb) deduplicate and internal-first works the
// same as with the real source.
//
// Wired into the binary by MYMEDIADB_FAKE_PROVIDER=1 (see cmd/mymediadb) and
// injected straight into the Layer 1 harness (internal/e2e). The images point to
// ImageBase (a fixture server that the caller starts); empty => no poster
// (placeholder).
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// filme is an item fixture. The translatable fields are map[language]value (the
// resolver matches both the combined code -- pt-BR -- and the base one -- pt/en).
type filme struct {
	sourceID         string
	tipo             string // "movie"|"tv"
	title            map[string]string
	description      map[string]string
	tags             map[string][]string
	originalTitle    string
	originalLanguage string
	releaseDate      string // "YYYY-MM-DD"
	posterPath       string // relative; combined with ImageBase when non-empty
	colecaoID        string // "" when it does not belong to a collection
	// titulosAlternativos are the alternative/translated titles that
	// BuscarTitulosAlternativos returns for this item -- a data source SEPARATE
	// from the `title` map above (the real provider also brings them through a
	// distinct call, append_to_response=translations). Empty in most fixtures (it
	// degrades to the single-title score, like any source without a translation for
	// that candidate); the "605" fixture is the proof of the mechanism.
	titulosAlternativos []string
}

// colecaoFix is a collection fixture (a set of movies, ≠ playlist).
type colecaoFix struct {
	id         string
	name       string
	overview   string
	posterPath string
	parts      []string // sourceIDs of the members, in order
}

// Interface assertions: the fake is a complete Provider, a ColecaoProvider, a
// SerieProvider (the canned series tree) AND a TituloAlternativoProvider
// (alternative/translated titles).
var (
	_ provider.Provider                  = (*Provider)(nil)
	_ provider.ColecaoProvider           = (*Provider)(nil)
	_ provider.SerieProvider             = (*Provider)(nil)
	_ provider.TituloAlternativoProvider = (*Provider)(nil)
)

// SerieFixtureID is the id of the canned series served by BuscarSerieArvore
// (exposed so the tests can build the tree import without guessing the id).
const SerieFixtureID = "71663"

// Provider serves the fixture catalog. It is immutable after New (the catalog is
// compile-time data), so the methods are safe for concurrency.
type Provider struct {
	imageBase string
	filmes    map[string]filme
	colecoes  map[string]colecaoFix
	// buscas records the `lang` received in each Buscar call -- only so the E2E
	// tests can prove that the language resolved by the server (idiomaDeBusca)
	// really reaches the provider, without having to instrument the real network
	// nor make title matching language-sensitive (which would break the
	// multi-language behavior already covered by the existing journeys). Purely
	// observational -- it never influences the result of Buscar.
	buscas *logDeIdiomas
}

// New creates the provider with the default catalog. imageBase is the prefix of
// the image URLs (e.g. the URL of an httptest serving a PNG); empty => items
// without a poster.
func New(imageBase string) *Provider {
	return &Provider{
		imageBase: strings.TrimRight(imageBase, "/"),
		filmes:    catalogoFilmes(),
		colecoes:  catalogoColecoes(),
		buscas:    &logDeIdiomas{},
	}
}

// Nome identifies the source. "tmdb" to exercise dedup and internal-first like
// the real one.
func (p *Provider) Nome() string { return "tmdb" }

// IdiomasBuscados returns, in call order, the `lang` that each Buscar received
// since the provider was created -- used by the E2E journeys of preferred
// language per library to prove that automatch/retroactive/match-preview resolve
// and propagate the right language all the way to the provider, without exposing
// the real network.
func (p *Provider) IdiomasBuscados() []string { return p.buscas.todos() }

// Buscar filters the catalog by substring (case-insensitive) on the title of any
// language, optionally by type, in a stable order (by numeric/lexical sourceID).
// `lang` does NOT filter the match (the catalog already exposes every language it
// has, as before -- changing that would break the existing multi-language
// journeys); it is only RECORDED in p.buscas for the observability above.
func (p *Provider) Buscar(_ context.Context, query, tipo, lang string) ([]provider.ResultadoExterno, error) {
	p.buscas.registrar(lang)
	q := strings.ToLower(strings.TrimSpace(query))
	var out []provider.ResultadoExterno
	for _, f := range p.ordenados() {
		if tipo != "" && f.tipo != tipo {
			continue
		}
		if q != "" && !casaTitulo(f, q) {
			continue
		}
		out = append(out, p.resultadoDe(f))
	}
	return out, nil
}

// ResolverID satisfies provider.ResolvedorDeID: it resolves an id of the canned catalog,
// so tests can exercise the ID DOOR (search and folder-name import) with no network. An
// id outside the catalog is ErrNaoEncontrado, exactly like the real source answers 404.
func (p *Provider) ResolverID(_ context.Context, ref provider.ReferenciaDeID, _ string) ([]provider.ResultadoExterno, error) {
	f, ok := p.filmes[ref.ID]
	if !ok || (ref.Tipo != "" && ref.Tipo != f.tipo) {
		return nil, nil
	}
	return []provider.ResultadoExterno{p.resultadoDe(f)}, nil
}

// BuscarDetalhe returns the RAW (self-sufficient) payload of the item, which
// enrich persists and MapearDetalhe rebuilds. ErrNaoEncontrado when the id does
// not exist.
func (p *Provider) BuscarDetalhe(_ context.Context, sourceID, _ string, _ []string) (json.RawMessage, error) {
	f, ok := p.filmes[sourceID]
	if !ok {
		return nil, provider.ErrNaoEncontrado
	}
	raw := rawFixture{
		Title:            f.title,
		Description:      f.description,
		Tags:             f.tags,
		OriginalTitle:    f.originalTitle,
		OriginalLanguage: f.originalLanguage,
		ReleaseDate:      f.releaseDate,
		PosterURL:        p.posterURL(f.posterPath),
	}
	return json.Marshal(raw)
}

// MapearDetalhe translates the raw to the CANONICAL map with OUR names. It is
// PURE (it depends only on the raw). The translatable ones come out as
// map[string]any (the resolver requires that type); the neutral ones as scalars.
// It only includes the keys present.
func (p *Provider) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	// EPISODE: the fake tree writes the episode media_external under this source
	// (Nome="tmdb"); it recognizes the entity_kind discriminator and maps
	// title/description per language. Without the discriminator it falls into the
	// movie mapping below, without regression.
	if campos, ok := provider.MapearEpisodio(raw); ok {
		return campos, nil
	}
	var f rawFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if m := strMapAny(f.Title); len(m) > 0 {
		out["title"] = m
	}
	if m := strMapAny(f.Description); len(m) > 0 {
		out["description"] = m
	}
	if m := listaMapAny(f.Tags); len(m) > 0 {
		out["tags"] = m
	}
	porSeNaoVazio(out, "original_title", f.OriginalTitle)
	porSeNaoVazio(out, "original_language", f.OriginalLanguage)
	porSeNaoVazio(out, "release_date", f.ReleaseDate)
	porSeNaoVazio(out, "poster_url", f.PosterURL)
	return out, nil
}

// BuscarColecao returns the collection by id, with the parts in the defined
// order. ErrNaoEncontrado when the id does not exist.
func (p *Provider) BuscarColecao(_ context.Context, colecaoID, _ string) (provider.Colecao, error) {
	c, ok := p.colecoes[colecaoID]
	if !ok {
		return provider.Colecao{}, provider.ErrNaoEncontrado
	}
	col := provider.Colecao{
		Source:    p.Nome(),
		SourceID:  c.id,
		Name:      c.name,
		Overview:  ptrSeNaoVazio(c.overview),
		PosterURL: ptrSeNaoVazio(p.posterURL(c.posterPath)),
	}
	for _, id := range c.parts {
		if f, ok := p.filmes[id]; ok {
			col.Parts = append(col.Parts, p.resultadoDe(f))
		}
	}
	return col, nil
}

// BuscarTitulosAlternativos implements provider.TituloAlternativoProvider: it
// returns the CANNED alternative/translated titles of the fixture
// (titulosAlternativos field of `filme`) -- a data source of its OWN, independent
// of the `title` map. Most fixtures have none (nil, no error: "no translation
// here", the same best-effort stance of a real source without that data) -- only
// the "605" fixture ("A Pantera Cor-De-Rosa"/"The Rose Prowler", see
// catalogo.go) has an alternative, and it is DELIBERATELY different from the "en"
// title that Buscar returns as the main one (tituloPreferido always prefers
// "en"): it is the proof of side finding #2 -- the candidate only scores high
// against a file in Portuguese when compared against the alternative, never
// against the main one. An unknown sourceID returns nil without error (absence of
// alternatives is a valid result, not a failure).
func (p *Provider) BuscarTitulosAlternativos(_ context.Context, _, sourceID string) ([]string, error) {
	f, ok := p.filmes[sourceID]
	if !ok || len(f.titulosAlternativos) == 0 {
		return nil, nil
	}
	out := make([]string, len(f.titulosAlternativos))
	copy(out, f.titulosAlternativos)
	return out, nil
}

// BuscarSerieArvore returns the canned TREE of the fixture series (1 series, 2
// seasons of 3 episodes, "official"/"dvd"/"absolute" orders with an episode
// whose number diverges between them). Deterministic and without network -- it
// exercises the offline series import. ErrNaoEncontrado for any other id.
func (p *Provider) BuscarSerieArvore(_ context.Context, sourceID string, _ []string) (provider.SerieArvore, error) {
	if sourceID != SerieFixtureID {
		return provider.SerieArvore{}, provider.ErrNaoEncontrado
	}
	return provider.SerieArvore{
		DefaultSeasonType: "official",
		SeasonTypes:       []string{"official", "dvd", "absolute"},
		// The inventory of the groups the source published -- one that mapped and one that
		// anchored nothing (issue #113: the two must not look the same downstream).
		Groups: []provider.EpisodeGroup{
			{SourceID: "g-dvd", Name: "DVD Order", Kind: "3", Episodes: 4, Mapped: 4},
			{SourceID: "g-arc", Name: "Story Arc", Kind: "5", Episodes: 2, Mapped: 0,
				Note: "no item of the group is in the tree"},
		},
		Temporadas: []provider.TemporadaExterna{
			p.temporadaFake(1, "1989", nil, []provider.EpisodioExterno{
				// Matching orders: official/dvd are the same.
				p.episodioFake("101", "9101", 1, num(1, 1, 1, 1, 1, 1), "1989-12-17"),
				p.episodioFake("102", "9102", 2, num(1, 2, 1, 2, 1, 2), "1990-01-14"),
				// DIVERGING number: official S1E3, but dvd S1E5.
				p.episodioFake("103", "9103", 3, num(1, 3, 1, 5, 1, 3), "1990-01-21"),
			}),
			// Season without Name -> the title falls back to "Temporada N".
			p.temporadaFake(2, "1990", map[string]string{"en": "Second Season"}, []provider.EpisodioExterno{
				p.episodioFake("201", "9201", 4, num(2, 1, 2, 1, 1, 4), "1990-10-11"),
				p.episodioFake("202", "9202", 5, num(2, 2, 2, 2, 1, 5), "1990-10-18"),
				p.episodioFake("203", "9203", 6, num(2, 3, 2, 3, 1, 6), "1990-11-01"),
			}),
		},
	}, nil
}

// temporadaFake builds a TemporadaExterna of the default ordering ("official").
// A nil name leaves the season without a name (the import falls back to
// "Temporada N").
func (p *Provider) temporadaFake(n int, ano string, name map[string]string, eps []provider.EpisodioExterno) provider.TemporadaExterna {
	return provider.TemporadaExterna{
		Ordering:     "official",
		SeasonNumber: n,
		Name:         name,
		Year:         ano,
		PosterURL:    p.posterURL(fmt.Sprintf("/season-%d.png", n)),
		Episodios:    eps,
	}
}

// episodioFake builds an EpisodioExterno anchored on the id (sourceID), with
// {tvdb} in ExternalIDs, title/overview en+pt-BR+pt, fixed runtime and an
// optional still.
func (p *Provider) episodioFake(id, tvdb string, abs int, numbering map[string]provider.Numero, air string) provider.EpisodioExterno {
	absN, runtime := abs, 22
	return provider.EpisodioExterno{
		SourceID:    id,
		ExternalIDs: map[string]string{"tvdb": tvdb},
		Title: map[string]string{
			"en": fmt.Sprintf("Episode %s", id), "pt-BR": fmt.Sprintf("Episodio %s", id), "pt": fmt.Sprintf("Episodio %s", id),
		},
		Overview: map[string]string{
			"en": fmt.Sprintf("Overview of episode %s", id), "pt-BR": fmt.Sprintf("Sinopse do episodio %s", id),
		},
		AbsoluteNumber: &absN,
		Numbering:      numbering,
		AirDate:        air,
		Runtime:        &runtime,
		StillURL:       p.posterURL(fmt.Sprintf("/still-%s.png", id)),
	}
}

// num is a shortcut for the Numbering of the 3 orders: (official S,N), (dvd S,N),
// (absolute S,N).
func num(oS, oN, dS, dN, aS, aN int) map[string]provider.Numero {
	return map[string]provider.Numero{
		"official": {Season: oS, Number: oN},
		"dvd":      {Season: dS, Number: dN},
		"absolute": {Season: aS, Number: aN},
	}
}

// rawFixture is the persisted/re-composable payload: canonical and
// self-sufficient (poster_url already comes absolute), so that MapearDetalhe is
// pure.
type rawFixture struct {
	Title            map[string]string   `json:"title,omitempty"`
	Description      map[string]string   `json:"description,omitempty"`
	Tags             map[string][]string `json:"tags,omitempty"`
	OriginalTitle    string              `json:"original_title,omitempty"`
	OriginalLanguage string              `json:"original_language,omitempty"`
	ReleaseDate      string              `json:"release_date,omitempty"`
	PosterURL        string              `json:"poster_url,omitempty"`
}

// resultadoDe builds a ResultadoExterno (search card / collection part) from a
// movie, in the "en" title with a fallback to the first available one.
func (p *Provider) resultadoDe(f filme) provider.ResultadoExterno {
	r := provider.ResultadoExterno{
		Source:    p.Nome(),
		SourceID:  f.sourceID,
		Type:      f.tipo,
		Title:     tituloPreferido(f),
		Overview:  ptrSeNaoVazio(descricaoPreferida(f)),
		PosterURL: ptrSeNaoVazio(p.posterURL(f.posterPath)),
	}
	if len(f.releaseDate) >= 4 {
		ano := f.releaseDate[:4]
		r.Year = &ano
	}
	return r
}

// ordenados returns the movies in a stable order by sourceID (determinism).
func (p *Provider) ordenados() []filme {
	out := make([]filme, 0, len(p.filmes))
	for _, f := range p.filmes {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sourceID < out[j].sourceID })
	return out
}

// posterURL combines ImageBase with the relative path; "" when there is no base
// or no path (item without poster => placeholder in the UI).
func (p *Provider) posterURL(path string) string {
	if p.imageBase == "" || path == "" {
		return ""
	}
	return p.imageBase + "/" + strings.TrimLeft(path, "/")
}

// --- pure helpers ---

// logDeIdiomas records, under a mutex, each `lang` received by Buscar. Purely
// observational: no Provider method decides behavior from it, only the E2E tests
// read it through Provider.IdiomasBuscados().
type logDeIdiomas struct {
	mu    sync.Mutex
	langs []string
}

func (l *logDeIdiomas) registrar(lang string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.langs = append(l.langs, lang)
}

func (l *logDeIdiomas) todos() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.langs))
	copy(out, l.langs)
	return out
}

func casaTitulo(f filme, q string) bool {
	for _, t := range f.title {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(f.originalTitle), q)
}

func tituloPreferido(f filme) string {
	if t, ok := f.title["en"]; ok {
		return t
	}
	for _, t := range f.title {
		return t
	}
	return f.originalTitle
}

func descricaoPreferida(f filme) string {
	if d, ok := f.description["en"]; ok {
		return d
	}
	for _, d := range f.description {
		return d
	}
	return ""
}

// strMapAny copies a map[string]string to map[string]any (the type the resolver
// requires in the translatable containers).
func strMapAny(m map[string]string) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// listaMapAny copies a map[string][]string to map[string]any (values []string;
// the resolver coerces them via asStrSlice).
func listaMapAny(m map[string][]string) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func porSeNaoVazio(m map[string]any, chave, valor string) {
	if valor != "" {
		m[chave] = valor
	}
}

func ptrSeNaoVazio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
