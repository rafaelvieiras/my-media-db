package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// semearMembro cria um filme com release_date (para a ordenacao por ano dos membros).
func semearMembro(t *testing.T, d *db.DB, id, titulo, ano string) {
	t.Helper()
	p := sqlcgen.CriarEntityParams{ID: id, Type: "movie", Title: titulo}
	if ano != "" {
		p.ReleaseDate = nsOf(ano)
	}
	if _, err := escrita(t, d).CriarEntity(context.Background(), p); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// ligarMembro liga parent->child por entity_relation(kind='collection').
func ligarMembro(t *testing.T, d *db.DB, id, parent, child string) {
	t.Helper()
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: id, ParentID: parent, ChildID: child, Kind: "collection",
	}); err != nil {
		t.Fatalf("CriarEntityRelation(%s): %v", id, err)
	}
}

// TestMembersOrdenaPorAno: os membros saem como cards ordenados por ano ascendente.
func TestMembersOrdenaPorAno(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Matrix Colecao")
	semearMembro(t, d, "m2003", "Reloaded", "2003-05-15")
	semearMembro(t, d, "m1999", "Matrix", "1999-03-31")
	ligarMembro(t, d, "r1", "col", "m2003")
	ligarMembro(t, d, "r2", "col", "m1999")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/col/members", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	cards := decodeCards(t, rec)
	if len(cards) != 2 {
		t.Fatalf("membros = %d, quero 2", len(cards))
	}
	eq(t, "1o por ano", cards[0].ID, "m1999")
	eq(t, "2o por ano", cards[1].ID, "m2003")
	eq(t, "type do card", cards[0].Type, "movie")
	eq(t, "owned", cards[0].Owned, false)
	if cards[0].PosterURL != nil {
		t.Errorf("poster_url = %v, quero nil (sem arte)", *cards[0].PosterURL)
	}
}

// TestMembersNaoColecaoVazio: /members de uma entity que nao e colecao (sem filhos) e [].
func TestMembersNaoColecaoVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "filme", "movie", "Um Filme")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/filme/members", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200", rec.Code)
	}
	if cards := decodeCards(t, rec); len(cards) != 0 {
		t.Errorf("membros = %d, quero 0 (nao-colecao)", len(cards))
	}
}

// TestMembers404: /members de uma entity inexistente e 404.
func TestMembers404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/members", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
}
