package tmdb

// This file implements the RICH ENRICHMENT OF THE SERIES NODE (route C): TMDB does
// not model the series tree (that is TVDB's job), but it has the rich fields that
// TVDB does not give -- tagline, genres, rating, keywords, similar and STATUS. The
// path: cross the TVDB id of the series to the TMDB id via /find, pull the rich
// /tv/{id} and map it REUSING the helpers of the rich movie detail (tmdb_rico.go).
// It returns ONLY the rich keys -- never title/overview/original_language -- so that
// the layer composition does not clobber the identity of the TVDB tree. It is the
// provider.SerieRicoProvider capability.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// BuscarSerieDetalheRico crosses the `tvdbID` of the series to the TMDB series id
// (/find?external_source=tvdb_id) and, if there is a match, pulls the rich /tv/{id}
// and maps it to the RICH canonical (only the rich keys). ok=false without a match
// (gracefully, without error); a real error (network/status) propagates so that
// enrich can log it and go on TVDB-only. Satisfies provider.SerieRicoProvider.
func (c *Client) BuscarSerieDetalheRico(ctx context.Context, tvdbID string, langs []string) (map[string]any, bool, error) {
	if tvdbID == "" {
		return nil, false, nil
	}
	tmdbID, ok, err := c.acharSeriePorTvdb(ctx, tvdbID)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil // no match in TMDB: gracefully (TVDB-only)
	}
	d, err := c.buscarDetalheSerieTMDB(ctx, tmdbID, langs)
	if err != nil {
		return nil, false, err
	}
	return c.mapearSerieRicoCanonico(ctx, d, langs), true, nil
}

// acharSeriePorTvdb resolves the TMDB series id from the TVDB id via
// /find/{tvdbID}?external_source=tvdb_id (the first tv_results wins). ok=false when
// TMDB does not know that TVDB id (empty tv_results) -- a TVDB-exclusive series.
func (c *Client) acharSeriePorTvdb(ctx context.Context, tvdbID string) (string, bool, error) {
	body, err := c.getBytes(ctx, "/find/"+url.PathEscape(tvdbID), url.Values{"external_source": {"tvdb_id"}})
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			return "", false, nil // id unknown in TMDB: gracefully
		}
		return "", false, err
	}
	var resp struct {
		TVResults []struct {
			ID int64 `json:"id"`
		} `json:"tv_results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", false, fmt.Errorf("tmdb: find de serie invalido: %w", err)
	}
	if len(resp.TVResults) == 0 || resp.TVResults[0].ID == 0 {
		return "", false, nil
	}
	return itoa(resp.TVResults[0].ID), true, nil
}

// buscarDetalheSerieTMDB pulls /tv/{id} with the append_to_response of the rich
// series blocks (content_ratings = rating; keywords; translations = tagline per
// language; recommendations (see tmdb.go); external_ids = imdb) in the main
// language. It reuses the same client/auth/base of the movie (getBytes/novoGet). The
// `status` (situation) already comes in the body of /tv, without append.
func (c *Client) buscarDetalheSerieTMDB(ctx context.Context, tmdbID string, langs []string) (detalheTMDB, error) {
	params := url.Values{"append_to_response": {"content_ratings,keywords,translations,recommendations,external_ids,aggregate_credits"}}
	if len(langs) > 0 && langs[0] != "" {
		params.Set("language", langs[0])
	}
	body, err := c.getBytes(ctx, "/tv/"+url.PathEscape(tmdbID), params)
	if err != nil {
		return detalheTMDB{}, err
	}
	var d detalheTMDB
	if err := json.Unmarshal(body, &d); err != nil {
		return detalheTMDB{}, fmt.Errorf("tmdb: detalhe de serie invalido: %w", err)
	}
	return d, nil
}

// mapearSerieRicoCanonico maps the rich /tv to the canonical, REUSING
// enriquecerCanonicoRico (tagline/keywords/certification_by_region/backdrop_url/
// similar/runtime/vote_average/external_links) and tagsPorIdioma (genres →
// tags/tags_by_lang, with the localized genre lists cached). It adds the SITUATION
// (airing_status, the RAW TMDB value). It does NOT write
// title/overview/original_language/poster_url/release_date -- the identity and the
// art of the series come from the TVDB tree (additive, it does not clobber the
// layer composition).
func (c *Client) mapearSerieRicoCanonico(ctx context.Context, d detalheTMDB, langs []string) map[string]any {
	out := map[string]any{}
	enriquecerCanonicoRico(out, d)
	if tags := tagsMap(c.tagsPorIdioma(ctx, "tv", d, langs)); len(tags) > 0 {
		out["tags"] = tags
	}
	if d.Status != "" {
		out["airing_status"] = d.Status
	}
	return out
}
