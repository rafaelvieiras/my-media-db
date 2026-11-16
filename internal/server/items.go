package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the creation of a manual LOCAL ITEM: POST /api/v1/entities creates
// an entity `type=movie|tv` WITHOUT an external source — the owner catalogs something the
// source does not have (or has not matched yet). It combines with the editing of
// external_ids (external_ids.go): the item is created and, later, the source id is fixed
// for a future synchronization. It is the analog of POST /collections for movie/series.
// Written through the WRITE pool (ADR-0010); no enrich (there is no source).

// tiposItemManual are the types accepted in the manual creation: a collection has its own
// endpoint (POST /collections), so here it is media content only.
var tiposItemManual = map[string]bool{"movie": true, "tv": true}

// createItemBody is the body of POST /api/v1/entities: type (movie|tv) and title are
// required; the other base fields are optional (empty = inherits/NULL).
type createItemBody struct {
	Type          string `json:"type"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	Description   string `json:"description"`
	ReleaseDate   string `json:"release_date"`
}

// createItem handles POST /api/v1/entities: it creates a LOCAL media entity (movie|tv)
// without external_ids, with the DEFAULT status ('cataloged'). 400 invalid JSON body; 422
// type outside movie|tv or an empty title (whitespace only counts as empty). 201 with the
// resolved entity (same shape as the detail).
func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()

	var body createItemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	if !tiposItemManual[body.Type] {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "type inválido (use movie ou tv)")
		return
	}
	titulo := strings.TrimSpace(body.Title)
	if titulo == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "título é obrigatório")
		return
	}

	id := uuid.NewString()
	if _, err := sqlcgen.New(s.db.Write).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:            id,
		Type:          body.Type,
		Title:         titulo,
		OriginalTitle: nsOpcional(body.OriginalTitle),
		Description:   nsOpcional(body.Description),
		ReleaseDate:   nsOpcional(body.ReleaseDate),
	}); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderResolvida(w, ctx, id, http.StatusCreated)
}

// nsOpcional wraps an optional text field in a VALID sql.NullString only when it is not
// empty (after trim); empty becomes NULL (it inherits the base, without writing an empty
// string). Shared by the local creations (manual item and collection).
func nsOpcional(v string) sql.NullString {
	limpo := strings.TrimSpace(v)
	if limpo == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: limpo, Valid: true}
}
