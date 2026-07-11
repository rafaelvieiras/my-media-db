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

// Este arquivo implementa a Fase B do briefing 0025 (coleção como entity interna):
// a CRIAÇÃO manual de uma coleção LOCAL (sem fonte externa) e a EDIÇÃO de seus
// membros (ligar/desligar itens da biblioteca). É a contraparte de escrita da leitura
// de membros (getEntityMembers, em entities.go). Todas as escritas passam pelo pool de
// ESCRITA (ADR-0010) — como não há fonte externa, não passam pelo enrich. Não houve
// migração nem sqlc novo: o schema já suporta `entity type='collection'` (type é TEXT
// livre) e a ligação por entity_relation(kind='collection'), cujo CRUD já existia.

// createCollectionBody é o corpo de POST /api/v1/collections: o título (obrigatório) e a
// descrição (opcional) de uma coleção montada do zero pelo dono.
type createCollectionBody struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// addMemberBody é o corpo de POST /api/v1/entities/{id}/members: o id do item a ligar.
type addMemberBody struct {
	ChildID string `json:"child_id"`
}

// createCollection trata POST /api/v1/collections: cria uma entity `type='collection'`
// LOCAL (sem external_ids) com o título/descrição do dono e responde 201 com a entity
// resolvida (o mesmo shape do detalhe). O status usa o DEFAULT do schema ('cataloged').
// 400 corpo JSON inválido; 422 título vazio (só espaços conta como vazio).
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

// addMember trata POST /api/v1/entities/{id}/members: liga um item (child_id) à coleção
// como membro (entity_relation kind='collection'). Valida que o pai EXISTE e é uma
// coleção, e que o filho existe, não é ele mesmo e não é outra coleção (sem
// aninhamento). É idempotente: religar um item já ligado é no-op (o UNIQUE também
// protege de corrida). Responde 200 com a lista de membros ATUALIZADA (mesmo shape/
// ordenação de GET .../members) — a UI reflete sem uma segunda ida.
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

// removeMember trata DELETE /api/v1/entities/{id}/members/{childId}: desliga o membro da
// coleção. Idempotente: se o item não está ligado, é no-op (200 mesmo assim). 404 quando
// a coleção não existe. Responde 200 com a lista de membros atualizada.
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
			break // UNIQUE(parent,child,kind) garante no máximo uma relação a apagar
		}
	}
	s.responderMembros(w, ctx, id)
}

// exigirColecao valida que a entity {id} EXISTE e é uma coleção; escreve 404/422 e
// devolve false quando não. Guarda comum das mutações de membros (o pai precisa ser
// coleção para receber membros).
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

// validarFilho valida o child_id de addMember: não-vazio, distinto do próprio pai,
// existente e não-coleção (sem aninhar coleções). Escreve o 422 apropriado e devolve
// false quando alguma regra falha.
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

// ligarMembroColecao cria a relação pai->filho (kind='collection') de forma IDEMPOTENTE:
// carrega os já-ligados e pula quando o filho já é membro (o UNIQUE ainda protege de
// corrida). Espelha o ligarMembros do enrich, mas para a edição manual da Fase B.
func (s *Server) ligarMembroColecao(ctx context.Context, id, childID string) error {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, id)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		if rel.Kind == kindColecao && rel.ChildID == childID {
			return nil // já ligado: no-op
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

// responderMembros monta e escreve a lista de membros atualizada (200) — a resposta
// comum de add/removeMember. 500 em erro de banco.
func (s *Server) responderMembros(w http.ResponseWriter, ctx context.Context, id string) {
	// add/removeMember são operações de COLEÇÃO; a lista devolvida é sempre de membros
	// por ano (kindColecao), independente de outros tipos de pai.
	out, err := s.montarMembros(ctx, id, "collection")
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// descricaoOpcional embrulha a descrição num sql.NullString VÁLIDO só quando ela não é
// vazia (após trim); vazia vira NULL. Alias de nsOpcional (items.go), mantido pelo nome
// no ponto de chamada do createCollection.
func descricaoOpcional(desc string) sql.NullString {
	return nsOpcional(desc)
}
