package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// Tests of Phase B of the internal collection (manual creation + member editing). They
// reuse the helpers of entities_test.go (setupEntitiesServer, semearEntity, escrita, do,
// decodeCards, eq) and of members_test.go (semearMembro, ligarMembro).

// criarColecaoOK POSTs /api/v1/collections expecting 201 and returns the resolved entity.
func criarColecaoOK(t *testing.T, h http.Handler, body any) resolver.Entidade {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/collections", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /collections status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeEntidade(t, rec)
}

// TestCriarColecao: creates a LOCAL collection (type='collection', title and description
// written, no external source) and answers 201 with the resolved entity.
func TestCriarColecao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	ent := criarColecaoOK(t, h, map[string]string{
		"title": "Maratona de Terror", "description": "Meus favoritos",
	})
	eq(t, "type resolvido", ent.Type, "collection")
	eq(t, "title resolvido", ent.Title, "Maratona de Terror")

	// Checks the database: entity persisted as a collection with the description written.
	row, err := escrita(t, d).ObterEntity(context.Background(), ent.ID)
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	eq(t, "type base", row.Type, "collection")
	eq(t, "title base", row.Title, "Maratona de Terror")
	if row.Description.String != "Meus favoritos" {
		t.Errorf("description = %q, quero 'Meus favoritos'", row.Description.String)
	}
	if row.ExternalIdsJson.Valid {
		t.Errorf("external_ids = %q, quero NULL (coleção local sem fonte)", row.ExternalIdsJson.String)
	}
}

// TestCriarColecaoSemDescricao: a missing description becomes NULL (not an empty string).
func TestCriarColecaoSemDescricao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	ent := criarColecaoOK(t, h, map[string]string{"title": "Só Título"})
	row, err := escrita(t, d).ObterEntity(context.Background(), ent.ID)
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if row.Description.Valid {
		t.Errorf("description = %q, quero NULL", row.Description.String)
	}
}

// TestCriarColecaoTituloVazio: an empty or whitespace-only title is 422.
func TestCriarColecaoTituloVazio(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	for _, titulo := range []string{"", "   "} {
		rec := do(t, h, http.MethodPost, "/api/v1/collections", map[string]string{"title": titulo})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("title=%q status = %d, quero 422", titulo, rec.Code)
		}
	}
}

// TestCriarColecaoCorpoInvalido: malformed JSON is 400.
func TestCriarColecaoCorpoInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/collections", []byte("{nao-json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
}

// TestAddMember: links an item to the collection; the 200 response brings the updated
// list, ordered by year.
func TestAddMember(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção")
	semearMembro(t, d, "m2003", "Reloaded", "2003-05-15")
	semearMembro(t, d, "m1999", "Matrix", "1999-03-31")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/col/members", map[string]string{"child_id": "m2003"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if cards := decodeCards(t, rec); len(cards) != 1 || cards[0].ID != "m2003" {
		t.Fatalf("membros = %v, quero [m2003]", cards)
	}

	// Second member, older: it must come FIRST in the list (ordering by year).
	rec = do(t, h, http.MethodPost, "/api/v1/entities/col/members", map[string]string{"child_id": "m1999"})
	cards := decodeCards(t, rec)
	if len(cards) != 2 {
		t.Fatalf("membros = %d, quero 2", len(cards))
	}
	eq(t, "1o por ano", cards[0].ID, "m1999")
	eq(t, "2o por ano", cards[1].ID, "m2003")
}

// TestAddMemberIdempotente: linking the same item again does not duplicate the relation.
func TestAddMemberIdempotente(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção")
	semearMembro(t, d, "m1", "Filme", "2001-01-01")

	for i := 0; i < 3; i++ {
		rec := do(t, h, http.MethodPost, "/api/v1/entities/col/members", map[string]string{"child_id": "m1"})
		if rec.Code != http.StatusOK {
			t.Fatalf("religar #%d status = %d, quero 200", i, rec.Code)
		}
	}
	rels := relacoesFilhas(t, d, "col")
	if rels != 1 {
		t.Errorf("relações = %d, quero 1 (sem duplicar)", rels)
	}
}

// TestAddMemberParentNaoColecao: linking an item to an entity that is not a collection is
// 422.
func TestAddMemberParentNaoColecao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "filme", "movie", "Um Filme")
	semearMembro(t, d, "m1", "Outro", "2001-01-01")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/filme/members", map[string]string{"child_id": "m1"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
}

// TestAddMemberSelf: a collection cannot contain itself (422).
func TestAddMemberSelf(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/col/members", map[string]string{"child_id": "col"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
}

// TestAddMemberAninhamento: a collection cannot contain another collection (422).
func TestAddMemberAninhamento(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção A")
	semearEntity(t, d, "col2", "collection", "Coleção B")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/col/members", map[string]string{"child_id": "col2"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
}

// TestAddMemberFilhoInexistente: linking an item that does not exist is 422.
func TestAddMemberFilhoInexistente(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/col/members", map[string]string{"child_id": "fantasma"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
}

// TestAddMemberChildVazio: a missing child_id is 422.
func TestAddMemberChildVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/col/members", map[string]string{"child_id": "  "})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
}

// TestAddMemberCorpoInvalido: malformed JSON is 400 (after validating that the parent is a
// collection).
func TestAddMemberCorpoInvalido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção")
	rec := do(t, h, http.MethodPost, "/api/v1/entities/col/members", []byte("{nao-json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
}

// TestAddMemberColecaoInexistente: linking into a collection that does not exist is 404.
func TestAddMemberColecaoInexistente(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/members", map[string]string{"child_id": "x"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
}

// TestRemoveMember: unlinks a member; the list shrinks in the response.
func TestRemoveMember(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção")
	semearMembro(t, d, "m1", "Fica", "2001-01-01")
	semearMembro(t, d, "m2", "Sai", "2002-01-01")
	ligarMembro(t, d, "r1", "col", "m1")
	ligarMembro(t, d, "r2", "col", "m2")

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/col/members/m2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	cards := decodeCards(t, rec)
	if len(cards) != 1 || cards[0].ID != "m1" {
		t.Fatalf("membros = %v, quero [m1]", cards)
	}
	if n := relacoesFilhas(t, d, "col"); n != 1 {
		t.Errorf("relações = %d, quero 1", n)
	}
}

// TestRemoveMemberIdempotente: unlinking an item that is not a member is a no-op (200,
// list intact) — it does not error.
func TestRemoveMemberIdempotente(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Coleção")
	semearMembro(t, d, "m1", "Membro", "2001-01-01")
	ligarMembro(t, d, "r1", "col", "m1")

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/col/members/fantasma", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	if cards := decodeCards(t, rec); len(cards) != 1 {
		t.Errorf("membros = %d, quero 1 (intacto)", len(cards))
	}
}

// TestRemoveMemberColecaoInexistente: unlinking from a collection that does not exist is
// 404.
func TestRemoveMemberColecaoInexistente(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodDelete, "/api/v1/entities/naoexiste/members/x", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
}

// relacoesFilhas counts the kind='collection' relations under a parent (for dedup asserts).
func relacoesFilhas(t *testing.T, d *db.DB, parentID string) int {
	t.Helper()
	rels, err := escrita(t, d).ListarRelacoesFilhas(context.Background(), parentID)
	if err != nil {
		t.Fatalf("ListarRelacoesFilhas: %v", err)
	}
	n := 0
	for _, rel := range rels {
		if rel.Kind == "collection" {
			n++
		}
	}
	return n
}
