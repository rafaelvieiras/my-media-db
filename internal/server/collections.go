package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements Phase B of collections as an internal entity:
// the manual CREATION of a LOCAL collection (no external source) and the EDITING of its
// members (linking/unlinking library items). It is the write counterpart of the member
// read (getEntityMembers, in entities.go). Every write goes through the
// WRITE pool (ADR-0010) — since there is no external source, they do not go through enrich.
// There was no migration nor new sqlc: the schema already supports `entity type='collection'`
// (type is free TEXT) and the link through entity_relation(kind='collection'), whose CRUD
// already existed.

// createCollectionBody is the body of POST /api/v1/collections: the title (required) and the
// description (optional) of a collection assembled from scratch by the owner.
type createCollectionBody struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// addMemberBody is the body of POST /api/v1/entities/{id}/members: the id of the item to
// link.
type addMemberBody struct {
	ChildID string `json:"child_id"`
}

// createCollection handles POST /api/v1/collections: it creates a LOCAL
// `type='collection'` entity (no external_ids) with the owner's title/description and responds
// 201 with the resolved entity (the same shape as the detail). The status uses the schema
// DEFAULT ('cataloged'). 400 invalid JSON body; 422 empty title (whitespace only counts as
// empty).
func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()

	var body createCollectionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	titulo := strings.TrimSpace(body.Title)
	if titulo == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "título é obrigatório")
		return
	}

	id := uuid.NewString()
	if _, err := sqlcgen.New(s.db.Write).CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:          id,
		Type:        kindColecao,
		Title:       titulo,
		Description: descricaoOpcional(body.Description),
	}); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderResolvida(w, ctx, id, http.StatusCreated)
}

// addMember handles POST /api/v1/entities/{id}/members: it links an item (child_id) to the collection
// as a member (entity_relation kind='collection'). It validates that the parent EXISTS and is a
// collection, and that the child exists, is not itself and is not another collection (no
// nesting). It is idempotent: relinking an already linked item is a no-op (the UNIQUE also
// protects against a race). It responds 200 with the UPDATED member list (same shape/
// ordering as GET .../members) — the UI reflects it without a second round trip.
func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if !s.exigirColecao(w, ctx, id) {
		return
	}

	var body addMemberBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "corpo JSON inválido")
		return
	}
	childID := strings.TrimSpace(body.ChildID)
	if !s.validarFilho(w, ctx, id, childID) {
		return
	}

	if err := s.ligarMembroColecao(ctx, id, childID); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderMembros(w, ctx, id)
}

// removeMember handles DELETE /api/v1/entities/{id}/members/{childId}: it unlinks the member from the
// collection. Idempotent: if the item is not linked, it is a no-op (200 anyway). 404 when
// the collection does not exist. It responds 200 with the updated member list.
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	childID := chi.URLParam(r, "childId")
	ctx := r.Context()

	if _, err := s.leitura().ObterEntity(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "coleção não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}

	rels, err := s.leitura().ListarRelacoesFilhas(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	for _, rel := range rels {
		if rel.Kind == kindColecao && rel.ChildID == childID {
			if err := sqlcgen.New(s.db.Write).ApagarEntityRelation(ctx, rel.ID); err != nil {
				respondErroInterno(w)
				return
			}
			break // UNIQUE(parent,child,kind) guarantees at most one relation to delete
		}
	}
	s.responderMembros(w, ctx, id)
}

// exigirColecao validates that the {id} entity EXISTS and is a collection; it writes 404/422 and
// returns false otherwise. Common guard of the member mutations (the parent must be a
// collection to receive members).
func (s *Server) exigirColecao(w http.ResponseWriter, ctx context.Context, id string) bool {
	parent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "coleção não encontrada")
			return false
		}
		respondErroInterno(w)
		return false
	}
	if parent.Type != kindColecao {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "entidade não é uma coleção")
		return false
	}
	return true
}

// validarFilho validates the child_id of addMember: non-empty, distinct from the parent itself,
// existing and not a collection (no nested collections). It writes the appropriate 422 and returns
// false when any rule fails.
func (s *Server) validarFilho(w http.ResponseWriter, ctx context.Context, id, childID string) bool {
	if childID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "child_id é obrigatório")
		return false
	}
	if childID == id {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "uma coleção não pode conter a si mesma")
		return false
	}
	child, err := s.leitura().ObterEntity(ctx, childID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body", "item não encontrado")
			return false
		}
		respondErroInterno(w)
		return false
	}
	if child.Type == kindColecao {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "não é possível aninhar coleções")
		return false
	}
	return true
}

// ligarMembroColecao creates the parent->child relation (kind='collection') IDEMPOTENTLY:
// it loads the already linked ones and skips when the child is already a member (the UNIQUE still
// protects against a race). It mirrors the ligarMembros of enrich, but for the manual editing of
// Phase B.
func (s *Server) ligarMembroColecao(ctx context.Context, id, childID string) error {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, id)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		if rel.Kind == kindColecao && rel.ChildID == childID {
			return nil // already linked: no-op
		}
	}
	_, err = sqlcgen.New(s.db.Write).CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID:       uuid.NewString(),
		ParentID: id,
		ChildID:  childID,
		Kind:     kindColecao,
	})
	return err
}

// responderMembros builds and writes the updated member list (200) — the common
// response of add/removeMember. 500 on a database error.
func (s *Server) responderMembros(w http.ResponseWriter, ctx context.Context, id string) {
	// add/removeMember are COLLECTION operations; the list returned is always of members
	// by year (kindColecao), regardless of other parent types.
	out, err := s.montarMembros(ctx, id, "collection")
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// descricaoOpcional wraps the description in a VALID sql.NullString only when it is not
// empty (after trim); empty becomes NULL. Alias of nsOpcional (items.go), kept by name
// at the call site of createCollection.
func descricaoOpcional(desc string) sql.NullString {
	return nsOpcional(desc)
}
