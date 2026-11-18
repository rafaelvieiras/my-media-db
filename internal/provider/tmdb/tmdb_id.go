package tmdb

// This file implements the ID DOOR for the external search: resolving an item from its
// id (or from a source URL) instead of from its title. See
// internal/provider/idref.go for WHY (a typo in the folder name makes /search/multi
// return nothing, and the sources do not spell-correct).
//
// It satisfies provider.ResolvedorDeID. Three routes, one per source that owns ids:
//
//	tmdb  -> GET /tv/{id} or /movie/{id}           (the id IS ours)
//	tvdb  -> GET /find/{id}?external_source=tvdb_id (cross the id to the TMDB one)
//	imdb  -> GET /find/{id}?external_source=imdb_id
//
// The /find route is the same one BuscarSerieDetalheRico already uses for the series
// tree, so there is a single place that knows how to cross external ids.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// ResolverID turns a ReferenciaDeID into search results, shaped exactly like the ones
// from /search/multi (same ResultadoExterno), so the handler and the UI serve both
// without a new type. An id that does not exist in the source degrades to ZERO results
// (not an error): a wrong id is a "found nothing", same as a wrong title.
func (c *Client) ResolverID(ctx context.Context, ref provider.ReferenciaDeID, lang string) ([]provider.ResultadoExterno, error) {
	switch ref.Fonte {
	case "tmdb":
		return c.resolverPorIDTMDB(ctx, ref, lang)
	case "tvdb":
		return c.resolverPorFind(ctx, ref, "tvdb_id", lang)
	case "imdb":
		return c.resolverPorFind(ctx, ref, "imdb_id", lang)
	}
	return nil, nil
}

// resolverPorIDTMDB pulls /tv/{id} and/or /movie/{id}. TMDb ids are NOT unique across
// types (the same number can belong to a series and to a film), so without a type in the
// input both are tried and BOTH come back -- the search UI shows the type, and picking
// the wrong row silently is worse than showing two. When the input already carries the
// type (a themoviedb.org URL, or ?type=), only that route runs.
func (c *Client) resolverPorIDTMDB(ctx context.Context, ref provider.ReferenciaDeID, lang string) ([]provider.ResultadoExterno, error) {
	tipos := []string{ref.Tipo}
	if ref.Tipo == "" {
		tipos = []string{"tv", "movie"}
	}
	out := make([]provider.ResultadoExterno, 0, len(tipos))
	for _, tipo := range tipos {
		res, ok, err := c.buscarItemPorID(ctx, tipo, ref.ID, lang)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, res)
		}
	}
	return out, nil
}

// buscarItemPorID does ONE detail request and maps it through mapearItem, so the result
// carries the same fields (title, original title, year, overview, poster) as a search hit.
// ok=false when the id does not exist for that type.
func (c *Client) buscarItemPorID(ctx context.Context, tipo, id, lang string) (provider.ResultadoExterno, bool, error) {
	params := url.Values{}
	if lang != "" {
		params.Set("language", lang)
	}
	body, err := c.getBytes(ctx, "/"+tipo+"/"+url.PathEscape(id), params)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			return provider.ResultadoExterno{}, false, nil
		}
		return provider.ResultadoExterno{}, false, err
	}
	// The detail payload uses the SAME field names as a /search/multi hit (name/title,
	// first_air_date/release_date, poster_path...); only media_type is missing there, and
	// we already know it from the route.
	var it itemMulti
	if err := json.Unmarshal(body, &it); err != nil {
		return provider.ResultadoExterno{}, false, fmt.Errorf("tmdb: resposta inválida em %s/%s: %w", tipo, id, err)
	}
	it.MediaType = tipo
	res, ok := mapearItem(it)
	return res, ok, nil
}

// resolverPorFind crosses an EXTERNAL id (tvdb/imdb) to the TMDB item via
// /find/{id}?external_source=..., whose payload already brings the full objects for films
// and series. A person result is ignored (the search only imports works).
func (c *Client) resolverPorFind(ctx context.Context, ref provider.ReferenciaDeID, fonteExterna, lang string) ([]provider.ResultadoExterno, error) {
	params := url.Values{"external_source": {fonteExterna}}
	if lang != "" {
		params.Set("language", lang)
	}
	body, err := c.getBytes(ctx, "/find/"+url.PathEscape(ref.ID), params)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			return nil, nil
		}
		return nil, err
	}
	var resp struct {
		TVResults    []itemMulti `json:"tv_results"`
		MovieResults []itemMulti `json:"movie_results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("tmdb: resposta inválida em /find/%s: %w", ref.ID, err)
	}
	out := make([]provider.ResultadoExterno, 0, len(resp.TVResults)+len(resp.MovieResults))
	out = anexarMapeados(out, resp.TVResults, "tv", ref.Tipo)
	out = anexarMapeados(out, resp.MovieResults, "movie", ref.Tipo)
	return out, nil
}

// anexarMapeados maps a /find array, stamping the type (the /find payload does not carry
// media_type) and honoring a type filter from the input.
func anexarMapeados(out []provider.ResultadoExterno, itens []itemMulti, tipo, filtro string) []provider.ResultadoExterno {
	if filtro != "" && filtro != tipo {
		return out
	}
	for _, it := range itens {
		it.MediaType = tipo
		if res, ok := mapearItem(it); ok {
			out = append(out, res)
		}
	}
	return out
}
