package server

// This file implements GET /api/v1/external/{source}/album/{id} (ADR-0029): the album's
// "missing tracks" screen -- a STRUCTURAL mold of external_colecao.go
// (GET .../collection/{id}), adapted to the music vertical: instead of
// collection→movies, it is album→official tracks of the release. The source is queried
// through the OPTIONAL capability provider.MusicaAlbumProvider (BuscarAlbumPorMbid, the
// DIRECT path by release-group MBID -- the internal `album` entity already stores that
// MBID in external_ids); sources without that capability degrade with 503. `internal_id`
// (internal-first) is filled in both on the album and on each already cataloged track,
// matching by MBID (idInternoPorMbid, interno.go) -- the same MBID that
// ImportarFaixaPossuida/importarFaixa/ImportarFaixaAvulsa write into external_ids when
// creating the track. The static "album" segment takes precedence over the {type} of
// GET /external/{source}/{type}/{id} in chi (the same mechanism as the "collection"
// segment, see server.go).

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// albumExternoDTO is the response of GET /external/{source}/album/{id}: the album
// metadata and its official tracks (the release chosen by the provider). `tracks` always
// comes out as `[]` (never null).
type albumExternoDTO struct {
	Source   string  `json:"source"`
	SourceID string  `json:"source_id"`
	Name     string  `json:"name"`
	Year     *string `json:"year"`
	Genre    *string `json:"genre"`
	// InternalID is the id of the internal album entity when this album was ALREADY
	// imported; nil otherwise. In practice this screen is only reached from an album that
	// is already internal (the TracklistAlbum link), but the field follows the same
	// collection/series contract (internal-first).
	InternalID *string           `json:"internal_id"`
	Tracks     []faixaExternaDTO `json:"tracks"`
}

// faixaExternaDTO is an official track of the release (a MusicBrainz recording).
type faixaExternaDTO struct {
	SourceID    string  `json:"source_id"`
	Title       string  `json:"title"`
	TrackNumber int     `json:"track_number"`
	DiscNumber  int     `json:"disc_number"`
	ISRC        *string `json:"isrc"`
	// InternalID is the id of the internal track entity when this track was ALREADY
	// imported (linked to the album through entity_relation kind=music_album); nil
	// otherwise -- the UI then offers "Importar" for that track.
	InternalID *string `json:"internal_id"`
}

// getExternalAlbum handles GET /api/v1/external/{source}/album/{id}?lang=. 503 without an
// external source (or a source without the selective album capability); 404 album that
// does not exist in the source; 502 the source failed; 200 with the album and its
// tracks.
func (s *Server) getExternalAlbum(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	source := chi.URLParam(r, "source")
	id := chi.URLParam(r, "id")

	prov, ok := s.providers.Get(source)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"album externo indisponivel (fonte externa nao configurada)")
		return
	}
	ap, ok := prov.(provider.MusicaAlbumProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "external_source_unavailable",
			"album externo indisponivel (fonte externa nao configurada)")
		return
	}

	ctx := r.Context()
	langs, err := s.idiomasPermitidos(ctx, w)
	if err != nil {
		return
	}

	alb, err := ap.BuscarAlbumPorMbid(ctx, id, langs)
	if err != nil {
		if errors.Is(err, provider.ErrNaoEncontrado) {
			writeError(w, http.StatusNotFound, "not_found", "album nao encontrado na fonte")
			return
		}
		respondFonteIndisponivel(w)
		return
	}

	writeJSON(w, http.StatusOK, s.montarAlbumExterno(ctx, source, alb))
}

// montarAlbumExterno composes the album DTO: it annotates the internal id of the album
// and of each already imported track (internal-first), matching by MBID.
func (s *Server) montarAlbumExterno(ctx context.Context, source string, alb provider.AlbumExterno) albumExternoDTO {
	faixas := make([]faixaExternaDTO, 0, len(alb.Faixas))
	for _, fx := range alb.Faixas {
		faixas = append(faixas, faixaExternaDTO{
			SourceID:    fx.MBID,
			Title:       fx.Title,
			TrackNumber: fx.TrackNumber,
			DiscNumber:  fx.DiscNumber,
			ISRC:        strOuNil(fx.ISRC),
			InternalID:  s.idInternoPorMbid(ctx, fx.MBID),
		})
	}
	return albumExternoDTO{
		Source:     source,
		SourceID:   alb.MBID,
		Name:       alb.Name,
		Year:       strOuNil(alb.Year),
		Genre:      strOuNil(alb.Genre),
		InternalID: s.idInternoPorMbid(ctx, alb.MBID),
		Tracks:     faixas,
	}
}

// strOuNil converts an empty string (a field absent from the source) into nil, so the
// JSON comes out as `null` instead of `""` -- the same optional-pointer contract that
// AlbumExterno.Year/Genre and FaixaExterna.ISRC use.
func strOuNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
