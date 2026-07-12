package tmdb

// Este arquivo implementa o ENRIQUECIMENTO RICO DO NÓ SÉRIE (briefing 0045, rota C):
// o TMDB não modela a árvore de série (isso é do TVDB), mas tem os campos ricos que o
// TVDB não dá — slogan, gêneros, classificação, palavras-chave, similares e SITUAÇÃO.
// O caminho: cruzar o id TVDB da série para o id TMDB via /find, puxar o /tv/{id} rico
// e mapeá-lo REUSANDO os helpers do detalhe rico de filme (tmdb_rico.go). Devolve SÓ as
// chaves ricas — nunca title/overview/original_language — para a composição de camadas
// não clobbar a identidade da árvore TVDB. É a capacidade provider.SerieRicoProvider.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// BuscarSerieDetalheRico cruza o `tvdbID` da série para o id de série do TMDB
// (/find?external_source=tvdb_id) e, havendo match, puxa o /tv/{id} rico e o mapeia
// para o canônico RICO (só as chaves ricas). ok=false sem match (graça, sem erro);
// erro real (rede/status) propaga para o enrich logar e seguir só-TVDB. Satisfaz
// provider.SerieRicoProvider.
func (c *Client) BuscarSerieDetalheRico(ctx context.Context, tvdbID string, langs []string) (map[string]any, bool, error) {
	if tvdbID == "" {
		return nil, false, nil
	}
	tmdbID, ok, err := c.acharSeriePorTvdb(ctx, tvdbID)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil // sem match no TMDB: graca (so-TVDB)
	}
	d, err := c.buscarDetalheSerieTMDB(ctx, tmdbID, langs)
	if err != nil {
		return nil, false, err
	}
	return c.mapearSerieRicoCanonico(ctx, d, langs), true, nil
}

// acharSeriePorTvdb resolve o id de série do TMDB a partir do id TVDB via
// /find/{tvdbID}?external_source=tvdb_id (o primeiro tv_results vence). ok=false quando
// o TMDB não conhece aquele id TVDB (tv_results vazio) — série exclusiva do TVDB.
func (c *Client) acharSeriePorTvdb(ctx context.Context, tvdbID string) (string, bool, error) {
	body, err := c.getBytes(ctx, "/find/"+url.PathEscape(tvdbID), url.Values{"external_source": {"tvdb_id"}})
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			return "", false, nil // id desconhecido no TMDB: graca
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

// buscarDetalheSerieTMDB puxa o /tv/{id} com o append_to_response dos blocos ricos de
// série (content_ratings = classificação; keywords; translations = slogan por idioma;
// similar; external_ids = imdb) no idioma principal. Reusa o mesmo cliente/auth/base do
// filme (getBytes/novoGet). O `status` (situação) já vem no corpo do /tv, sem append.
func (c *Client) buscarDetalheSerieTMDB(ctx context.Context, tmdbID string, langs []string) (detalheTMDB, error) {
	params := url.Values{"append_to_response": {"content_ratings,keywords,translations,similar,external_ids"}}
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

// mapearSerieRicoCanonico mapeia o /tv rico para o canônico, REUSANDO enriquecerCanonicoRico
// (tagline/keywords/certification_by_region/backdrop_url/similar/runtime/vote_average/
// external_links) e tagsPorIdioma (gêneros → tags/tags_by_lang, com as listas de gênero
// localizadas cacheadas). Acrescenta a SITUAÇÃO (airing_status, valor CRU do TMDB). NÃO
// grava title/overview/original_language/poster_url/release_date — a identidade e a arte
// da série seguem da árvore TVDB (aditivo, não clobba a composição de camadas).
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
