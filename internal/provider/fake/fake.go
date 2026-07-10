// Package fake é um provider DETERMINÍSTICO de fixtures para os testes E2E
// (briefing 0028). Implementa provider.Provider + provider.ColecaoProvider servindo
// um catálogo em memória (a franquia Matrix + a coleção dela), multi-idioma, SEM
// REDE e SEM SEGREDO. Nome() devolve "tmdb" de propósito: assim as colunas geradas
// (external_id_tmdb) deduplicam e o interno-first funciona igual à fonte real.
//
// Ligado no binário por MYMEDIADB_FAKE_PROVIDER=1 (ver cmd/mymediadb) e injetado
// direto no harness da Camada 1 (internal/e2e). As imagens apontam para ImageBase
// (um servidor de fixtures que o chamador sobe); vazio => sem pôster (placeholder).
package fake

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// filme é uma fixture de item. Os campos traduzíveis são map[idioma]valor (o
// resolver casa tanto o código combinado — pt-BR — quanto o base — pt/en).
type filme struct {
	sourceID         string
	tipo             string // "movie"|"tv"
	title            map[string]string
	description      map[string]string
	tags             map[string][]string
	originalTitle    string
	originalLanguage string
	releaseDate      string // "AAAA-MM-DD"
	posterPath       string // relativo; combinado com ImageBase quando não-vazio
	colecaoID        string // "" quando não pertence a coleção
}

// colecaoFix é uma fixture de coleção (conjunto de filmes, ≠ playlist).
type colecaoFix struct {
	id         string
	name       string
	overview   string
	posterPath string
	parts      []string // sourceIDs dos membros, em ordem
}

// Asserções de interface: o fake é um Provider completo E um ColecaoProvider.
var (
	_ provider.Provider        = (*Provider)(nil)
	_ provider.ColecaoProvider = (*Provider)(nil)
)

// Provider serve o catálogo de fixtures. É imutável após New (o catálogo é dado de
// compilação), então os métodos são seguros para concorrência.
type Provider struct {
	imageBase string
	filmes    map[string]filme
	colecoes  map[string]colecaoFix
}

// New cria o provider com o catálogo padrão. imageBase é o prefixo das URLs de
// imagem (ex.: o URL de um httptest servindo um PNG); vazio => itens sem pôster.
func New(imageBase string) *Provider {
	return &Provider{
		imageBase: strings.TrimRight(imageBase, "/"),
		filmes:    catalogoFilmes(),
		colecoes:  catalogoColecoes(),
	}
}

// Nome identifica a fonte. "tmdb" para exercitar dedup e interno-first como a real.
func (p *Provider) Nome() string { return "tmdb" }

// Buscar filtra o catálogo por substring (case-insensitive) no título de qualquer
// idioma, opcionalmente por tipo, em ordem estável (por sourceID numérico/lexical).
func (p *Provider) Buscar(_ context.Context, query, tipo, _ string) ([]provider.ResultadoExterno, error) {
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

// BuscarDetalhe devolve o payload CRU (auto-suficiente) do item, que o enrich
// persiste e o MapearDetalhe reconstrói. ErrNaoEncontrado quando o id não existe.
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

// MapearDetalhe traduz o raw para o mapa CANÔNICO com os NOSSOS nomes. É PURO
// (só depende do raw). Os traduzíveis saem como map[string]any (o resolver exige
// esse tipo); os neutros como escalares. Só inclui as chaves presentes.
func (p *Provider) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
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

// BuscarColecao devolve a coleção pelo id, com as partes na ordem definida.
// ErrNaoEncontrado quando o id não existe.
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

// rawFixture é o payload persistido/re-componível: canônico e auto-suficiente (o
// poster_url já vem absoluto), para MapearDetalhe ser puro.
type rawFixture struct {
	Title            map[string]string   `json:"title,omitempty"`
	Description      map[string]string   `json:"description,omitempty"`
	Tags             map[string][]string `json:"tags,omitempty"`
	OriginalTitle    string              `json:"original_title,omitempty"`
	OriginalLanguage string              `json:"original_language,omitempty"`
	ReleaseDate      string              `json:"release_date,omitempty"`
	PosterURL        string              `json:"poster_url,omitempty"`
}

// resultadoDe monta um ResultadoExterno (card de busca / parte de coleção) a partir
// de um filme, no título "en" com fallback ao primeiro disponível.
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

// ordenados devolve os filmes em ordem estável por sourceID (determinismo).
func (p *Provider) ordenados() []filme {
	out := make([]filme, 0, len(p.filmes))
	for _, f := range p.filmes {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sourceID < out[j].sourceID })
	return out
}

// posterURL combina o ImageBase com o caminho relativo; "" quando não há base ou
// caminho (item sem pôster => placeholder na UI).
func (p *Provider) posterURL(path string) string {
	if p.imageBase == "" || path == "" {
		return ""
	}
	return p.imageBase + "/" + strings.TrimLeft(path, "/")
}

// --- helpers puros ---

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

// strMapAny copia um map[string]string para map[string]any (tipo que o resolver
// exige nos containers traduzíveis).
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

// listaMapAny copia um map[string][]string para map[string]any (valores []string;
// o resolver os coage via asStrSlice).
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
