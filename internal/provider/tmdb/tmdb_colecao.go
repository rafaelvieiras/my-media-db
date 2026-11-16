package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// This file implements the COLLECTION capability of the provider
// (provider.ColecaoProvider): GET {base}/collection/{id}?language=<active lang>
// and the mapping of the parts to the canonical type. It is pragmatically
// MONO-LANGUAGE (searched in the active language, without the re-composable
// all-translations cache of the rich detail) -- the full multi-language support of
// the collection remains a follow-up. No network in the mapping (mapearColecao is
// PURE).

// colecaoDetalheTMDB mirrors /collection/{id} (only the fields used). The parts
// are the movies of the collection.
type colecaoDetalheTMDB struct {
	ID           int64              `json:"id"`
	Name         string             `json:"name"`
	Overview     string             `json:"overview"`
	PosterPath   string             `json:"poster_path"`
	BackdropPath string             `json:"backdrop_path"`
	Parts        []parteColecaoTMDB `json:"parts"`
}

// parteColecaoTMDB is an item (part) of the collection. media_type usually comes
// as "movie"; the title/release_date fields are movie ones (we keep
// name/first_air_date for robustness).
type parteColecaoTMDB struct {
	ID           int64  `json:"id"`
	MediaType    string `json:"media_type"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
}

// BuscarColecao pulls the collection by id from the source, localized in `lang`.
// It reuses getBytes (404 → provider.ErrNaoEncontrado; other statuses → error).
// Satisfies provider.ColecaoProvider.
func (c *Client) BuscarColecao(ctx context.Context, colecaoID, lang string) (provider.Colecao, error) {
	if colecaoID == "" {
		return provider.Colecao{}, fmt.Errorf("tmdb: colecaoID vazio")
	}
	params := url.Values{}
	if lang != "" {
		params.Set("language", lang)
	}
	body, err := c.getBytes(ctx, "/collection/"+url.PathEscape(colecaoID), params)
	if err != nil {
		return provider.Colecao{}, err
	}
	var d colecaoDetalheTMDB
	if err := json.Unmarshal(body, &d); err != nil {
		return provider.Colecao{}, fmt.Errorf("tmdb: colecao invalida: %w", err)
	}
	return mapearColecao(d), nil
}

// mapearColecao converts the raw collection into the canonical type. Each part
// becomes a ResultadoExterno (a movie navigable to the external detail). It
// discards parts without a title.
func mapearColecao(d colecaoDetalheTMDB) provider.Colecao {
	col := provider.Colecao{
		Source:      "tmdb",
		SourceID:    itoa(d.ID),
		Name:        d.Name,
		Overview:    ptrNaoVazio(d.Overview),
		PosterURL:   posterURL(d.PosterPath),
		BackdropURL: backdropURL(d.BackdropPath),
		Parts:       make([]provider.ResultadoExterno, 0, len(d.Parts)),
	}
	for _, p := range d.Parts {
		titulo := primeiroNaoVazio(p.Title, p.Name)
		if titulo == "" {
			continue
		}
		col.Parts = append(col.Parts, provider.ResultadoExterno{
			Source:    "tmdb",
			SourceID:  itoa(p.ID),
			Type:      tipoDaParte(p.MediaType),
			Title:     titulo,
			Year:      anoDe(primeiroNaoVazio(p.ReleaseDate, p.FirstAirDate)),
			Overview:  ptrNaoVazio(p.Overview),
			PosterURL: posterURL(p.PosterPath),
		})
	}
	return col
}

// tipoDaParte normalizes the media_type of the part; a TMDB collection is made of
// movies, so any value outside movie|tv falls back to "movie".
func tipoDaParte(mediaType string) string {
	if mediaType == "tv" {
		return "tv"
	}
	return "movie"
}

// backdropURL builds the direct URL of the collection backdrop on TMDB, or nil
// when there is no backdrop_path. An analogue of posterURL, but in the hero size.
func backdropURL(backdropPath string) *string {
	if backdropPath == "" {
		return nil
	}
	u := backdropBaseURL + backdropPath
	return &u
}
