package server

// This file implements GET /api/v1/external/{source}/collection/{id}: the external
// COLLECTION as its own page — name/synopsis/artwork of the set and the list of the
// PARTS (movies) navigable to the external detail. The source is queried through the
// OPTIONAL capability provider.ColecaoProvider (type-assert); sources without collections
// degrade with 503. Pragmatically single-language (the active language), with no
// re-composable cache — full multi-language support for collections is left as a
// follow-up.

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// colecaoExternaDTO is the response of GET /external/{source}/collection/{id}: the
// collection metadata and its parts. The HERO artwork (poster/background) goes through
// the local proxy; the part thumbnails stay DIRECT (consistent with the
// similars/collection of the external detail). `parts` always comes out as `[]` (never
// null).
type colecaoExternaDTO struct {
	Source      string  `json:"source"`
	SourceID    string  `json:"source_id"`
	Name        string  `json:"name"`
	Overview    *string `json:"overview"`
	PosterURL   *string `json:"poster_url"`
	BackdropURL *string `json:"backdrop_url"`
	// InternalID is the id of the internal collection entity when this collection was
	// ALREADY imported; nil otherwise. The UI then swaps "Importar coleção" for
	// "Ver coleção" (a link to the internal destination).
	InternalID *string           `json:"internal_id"`
	Parts      []parteColecaoDTO `json:"parts"`
}

// parteColecaoDTO is a collection item (the same lean shape as a search result): id at
// the source, type, title, year, synopsis and the direct thumbnail.
type parteColecaoDTO struct {
	SourceID  string  `json:"source_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Year      *string `json:"year"`
	Overview  *string `json:"overview"`
	PosterURL *string `json:"poster_url"`
	// InternalID is the id of the internal entity of this part when it was ALREADY
	// imported; nil otherwise. The UI then shows "na biblioteca" and links to the
	// internal destination instead of offering Importar.
	InternalID *string `json:"internal_id"`
}

// getExternalCollection handles GET /api/v1/external/{source}/collection/{id}?lang=. 503
// without an external source (or a source without the collection capability); 400 lang
// outside the allowed ones; 404 collection that does not exist in the source; 502 the
// source failed; 200 with the collection.
func (s *Server) getExternalCollection(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	source := chi.URLParam(r, "source")
	id := chi.URLParam(r, "id")

	prov, ok := s.providers.Get(source)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"colecao externa indisponivel (fonte externa nao configurada)")
		return
	}
	cp, ok := prov.(provider.ColecaoProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"colecao externa indisponivel (fonte externa nao configurada)")
		return
	}

	ctx := r.Context()
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	lang, ok := s.parseLang(w, cfg, r.URL.Query().Get("lang"))
	if !ok {
		return
	}

	col, err := cp.BuscarColecao(ctx, id, lang)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			writeError(w, http.StatusNotFound, "not_found", "colecao nao encontrada na fonte")
			return
		}
		respondFonteIndisponivel(w)
		return
	}

	writeJSON(w, http.StatusOK, s.montarColecaoExterna(ctx, source, col))
}

// montarColecaoExterna composes the collection DTO: it rewrites the hero artwork through
// the local proxy (urlImagemServida), annotates the internal id of the collection and of
// each already imported part (internal-first) and copies the parts with the direct
// thumbnail.
func (s *Server) montarColecaoExterna(ctx context.Context, source string, col provider.Colecao) colecaoExternaDTO {
	partes := make([]parteColecaoDTO, 0, len(col.Parts))
	for _, p := range col.Parts {
		partes = append(partes, parteColecaoDTO{
			SourceID:   p.SourceID,
			Type:       p.Type,
			Title:      p.Title,
			Year:       p.Year,
			Overview:   p.Overview,
			PosterURL:  p.PosterURL,
			InternalID: s.idInternoItem(ctx, source, p.SourceID),
		})
	}
	return colecaoExternaDTO{
		Source:      source,
		SourceID:    col.SourceID,
		Name:        col.Name,
		Overview:    col.Overview,
		PosterURL:   s.urlImagemServida(col.PosterURL),
		BackdropURL: s.urlImagemServida(col.BackdropURL),
		InternalID:  s.idInternoColecao(ctx, source, col.SourceID),
		Parts:       partes,
	}
}
