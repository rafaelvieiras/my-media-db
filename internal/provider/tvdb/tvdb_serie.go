package tvdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// This file implements the provider.SerieProvider capability (ADR-0021): the TREE of
// the series -- seasons (only of the default ordering) + fine-grained episodes anchored
// on episode.id, each one with the numbering of ALL orders (Numbering).
//
// EFFICIENT PATH (invariant 1): it does NOT fetch each episode (N calls). It pulls the
// LIST of episodes per order -- /series/{id}/episodes/{order} (paginated 500/page) --
// once per order, and merges by episode.id. Cost = 1 (extended) + Σ pages per order +
// Σ pages per requested language. The numberings per order do NOT come in the seasons[]
// of the list (it comes empty); that is why the merge is by order-endpoint, not by
// episode.

// BuscarSerieArvore satisfies provider.SerieProvider: it returns the tree of the series
// `sourceID` in the `langs` languages (with fallback). Only the seasons of the DEFAULT
// ordering are materialized; the other orders become numbering in the episodes.
func (c *Client) BuscarSerieArvore(ctx context.Context, sourceID string, langs []string) (provider.SerieArvore, error) {
	if sourceID == "" {
		return provider.SerieArvore{}, fmt.Errorf("tvdb: sourceID vazio")
	}
	d, err := c.serieExtended(ctx, sourceID)
	if err != nil {
		return provider.SerieArvore{}, err
	}
	ordens := ordensDaSerie(d)
	ordemDef := ordemDefault(d, ordens)

	base, err := c.buscarEpisodios(ctx, sourceID, ordemDef, "")
	if err != nil {
		return provider.SerieArvore{}, err
	}
	episodios, indice := montarEpisodios(base, ordemDef, codigoApp(d.OriginalLanguage))

	for _, ord := range ordens {
		if ord == ordemDef {
			continue
		}
		if err := c.mesclarNumeracao(ctx, sourceID, ord, indice); err != nil {
			return provider.SerieArvore{}, err
		}
	}
	c.mesclarTraducoes(ctx, sourceID, ordemDef, langs, d.OriginalLanguage, indice)

	return provider.SerieArvore{
		DefaultSeasonType: ordemDef,
		SeasonTypes:       ordens,
		Temporadas:        montarTemporadas(d, ordemDef, episodios),
	}, nil
}

// serieExtended fetches and parses /series/{id}/extended (reused by the tree and by the
// detail). ErrNaoEncontrado when the series does not exist.
func (c *Client) serieExtended(ctx context.Context, sourceID string) (serieData, error) {
	body, err := c.getBytes(ctx, "/series/"+url.PathEscape(sourceID)+"/extended", nil)
	if err != nil {
		return serieData{}, err
	}
	var resp respSerie
	if err := json.Unmarshal(body, &resp); err != nil {
		return serieData{}, fmt.Errorf("tvdb: extended invalido: %w", err)
	}
	return resp.Data, nil
}

// ordensDaSerie returns the orderings (season types) that the series uses, in the order of
// the extended (e.g. ["official","dvd","absolute"]).
func ordensDaSerie(d serieData) []string {
	out := make([]string, 0, len(d.SeasonTypes))
	for _, st := range d.SeasonTypes {
		if st.Type != "" {
			out = append(out, st.Type)
		}
	}
	return out
}

// ordemDefault resolves the default ordering of the series: it matches defaultSeasonType
// (id) with the type in seasonTypes[]. It falls back to "official" (or to the first
// order) when it does not find it.
func ordemDefault(d serieData, ordens []string) string {
	for _, st := range d.SeasonTypes {
		if st.ID == d.DefaultSeasonType && st.Type != "" {
			return st.Type
		}
	}
	if len(ordens) > 0 {
		return ordens[0]
	}
	return "official"
}

// episodeRec mirrors the EpisodeBaseRecord of the /series/{id}/episodes/{order} list (only
// the fields used). name/overview come in the requested language (null when there is no
// translation).
type episodeRec struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Overview       string `json:"overview"`
	SeasonNumber   int    `json:"seasonNumber"`
	Number         int    `json:"number"`
	AbsoluteNumber int    `json:"absoluteNumber"`
	Aired          string `json:"aired"`
	Runtime        int    `json:"runtime"`
	Image          string `json:"image"`
}

// respEpisodios wraps the response of the episode list ({data:{episodes[]}, links}).
type respEpisodios struct {
	Data struct {
		Episodes []episodeRec `json:"episodes"`
	} `json:"data"`
	Links struct {
		Next *string `json:"next"`
	} `json:"links"`
}

// buscarEpisodios pulls ALL the episodes of an order (paginating 500/page through
// links.next). An empty `lang` brings the base language; a TheTVDB code brings the
// translation of that order (name/overview null when missing). Network errors propagate.
func (c *Client) buscarEpisodios(ctx context.Context, sourceID, ordem, lang string) ([]episodeRec, error) {
	path := "/series/" + url.PathEscape(sourceID) + "/episodes/" + url.PathEscape(ordem)
	if lang != "" {
		path += "/" + url.PathEscape(lang)
	}
	var todos []episodeRec
	for page := 0; ; page++ {
		body, err := c.getBytes(ctx, path, url.Values{"page": {strconv.Itoa(page)}})
		if err != nil {
			return nil, err
		}
		var resp respEpisodios
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("tvdb: episodios invalidos: %w", err)
		}
		todos = append(todos, resp.Data.Episodes...)
		if resp.Links.Next == nil || len(resp.Data.Episodes) == 0 {
			return todos, nil
		}
	}
}

// montarEpisodios creates the EpisodioExterno of the default ordering (the anchor),
// preserving the order of the source, and also returns the id→*episode index (for the
// following merges). The numbering of the default order already enters Numbering; the
// base content goes under the original language of the series.
func montarEpisodios(base []episodeRec, ordemDef, origLang string) ([]*provider.EpisodioExterno, map[int64]*provider.EpisodioExterno) {
	episodios := make([]*provider.EpisodioExterno, 0, len(base))
	indice := make(map[int64]*provider.EpisodioExterno, len(base))
	for _, r := range base {
		ep := &provider.EpisodioExterno{
			SourceID:    strconv.FormatInt(r.ID, 10),
			ExternalIDs: map[string]string{"tvdb": strconv.FormatInt(r.ID, 10)},
			Title:       map[string]string{},
			Overview:    map[string]string{},
			Numbering:   map[string]provider.Numero{ordemDef: {Season: r.SeasonNumber, Number: r.Number}},
			AirDate:     r.Aired,
			StillURL:    r.Image,
		}
		if r.AbsoluteNumber != 0 {
			abs := r.AbsoluteNumber
			ep.AbsoluteNumber = &abs
		}
		if r.Runtime != 0 {
			rt := r.Runtime
			ep.Runtime = &rt
		}
		setSeAusente(ep.Title, origLang, r.Name)
		setSeAusente(ep.Overview, origLang, r.Overview)
		episodios = append(episodios, ep)
		indice[r.ID] = ep
	}
	return episodios, indice
}

// mesclarNumeracao fetches the episodes of an ALTERNATIVE order and injects the numbering
// of that order into the SAME episode (by id). Episodes outside the default order are
// ignored (the orders are a subset of the default superset -- ADR-0021).
func (c *Client) mesclarNumeracao(ctx context.Context, sourceID, ordem string, indice map[int64]*provider.EpisodioExterno) error {
	recs, err := c.buscarEpisodios(ctx, sourceID, ordem, "")
	if err != nil {
		return err
	}
	for _, r := range recs {
		if ep, ok := indice[r.ID]; ok {
			ep.Numbering[ordem] = provider.Numero{Season: r.SeasonNumber, Number: r.Number}
		}
	}
	return nil
}

// mesclarTraducoes overlays title/overview per requested language, using the TRANSLATED
// LIST of the default order (/episodes/{order}/{code}) -- without N calls per episode. It
// is best-effort: the original language is skipped (it already is the base) and a language
// that fails degrades (fallback: the episode stays without that language in the map).
func (c *Client) mesclarTraducoes(ctx context.Context, sourceID, ordemDef string, langs []string, origTvdb string, indice map[int64]*provider.EpisodioExterno) {
	vistos := map[string]bool{origTvdb: true}
	for _, lang := range langs {
		code := codigoTVDB(lang)
		if code == "" || vistos[code] {
			continue
		}
		vistos[code] = true
		recs, err := c.buscarEpisodios(ctx, sourceID, ordemDef, code)
		if err != nil {
			continue
		}
		aplicarTraducao(recs, codigosApp(lang), indice)
	}
}

// aplicarTraducao writes the non-empty name/overview of each translated episode under the
// app language keys (combined + base).
func aplicarTraducao(recs []episodeRec, chaves []string, indice map[int64]*provider.EpisodioExterno) {
	for _, r := range recs {
		ep, ok := indice[r.ID]
		if !ok {
			continue
		}
		for _, chave := range chaves {
			setSeAusente(ep.Title, chave, r.Name)
			setSeAusente(ep.Overview, chave, r.Overview)
		}
	}
}

// montarTemporadas creates the TemporadaExterna ONLY of the default ordering (invariant
// 2), from extended.seasons[] (number, year, poster), attaching the episodes grouped by
// their season number IN the default order. A season present in the episodes but absent
// from seasons[] (rare) is synthesized so as not to lose episodes.
func montarTemporadas(d serieData, ordemDef string, episodios []*provider.EpisodioExterno) []provider.TemporadaExterna {
	porTemp := map[int][]provider.EpisodioExterno{}
	for _, ep := range episodios {
		n := ep.Numbering[ordemDef].Season
		porTemp[n] = append(porTemp[n], *ep)
	}
	temps := []provider.TemporadaExterna{}
	vistos := map[int]bool{}
	for _, s := range d.Seasons {
		if s.Type.Type != ordemDef || vistos[s.Number] {
			continue
		}
		vistos[s.Number] = true
		temps = append(temps, provider.TemporadaExterna{
			Ordering:     ordemDef,
			SeasonNumber: s.Number,
			Year:         s.Year,
			PosterURL:    s.Image,
			Episodios:    porTemp[s.Number],
		})
	}
	for n, eps := range porTemp {
		if !vistos[n] {
			temps = append(temps, provider.TemporadaExterna{Ordering: ordemDef, SeasonNumber: n, Episodios: eps})
		}
	}
	sort.Slice(temps, func(i, j int) bool { return temps[i].SeasonNumber < temps[j].SeasonNumber })
	return temps
}

// setSeAusente writes a non-empty value under a non-empty key only if it does not exist yet.
func setSeAusente(m map[string]string, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}
