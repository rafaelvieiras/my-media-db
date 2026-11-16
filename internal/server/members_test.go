package server

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
)

// semearMembro creates a movie with a release_date (for the year ordering of the members).
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

// ligarMembro links parent->child through entity_relation(kind='collection').
func ligarMembro(t *testing.T, d *db.DB, id, parent, child string) {
	t.Helper()
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: id, ParentID: parent, ChildID: child, Kind: "collection",
	}); err != nil {
		t.Fatalf("CriarEntityRelation(%s): %v", id, err)
	}
}

// TestMembersOrdenaPorAno: the members come out as cards ordered by ascending year.
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

// TestMembersNaoColecaoVazio: /members of an entity that is not a collection (no children)
// is [].
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

// TestMembersFiltraKindDiferente: only kind='collection' relations become members; a child
// linked by ANOTHER kind (e.g. 'season') is left out.
func TestMembersFiltraKindDiferente(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Colecao")
	semearMembro(t, d, "m1", "Membro", "2001-01-01")
	semearMembro(t, d, "m2", "Nao-membro", "2002-01-01")
	ligarMembro(t, d, "r1", "col", "m1") // kind='collection'
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: "r2", ParentID: "col", ChildID: "m2", Kind: "season", // different kind -> ignored
	}); err != nil {
		t.Fatalf("CriarEntityRelation season: %v", err)
	}
	rec := do(t, h, http.MethodGet, "/api/v1/entities/col/members", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	cards := decodeCards(t, rec)
	if len(cards) != 1 || cards[0].ID != "m1" {
		t.Fatalf("membros = %v, quero so [m1] (kind 'season' ignorado)", cards)
	}
}

// TestMembers404: /members of an entity that does not exist is 404.
func TestMembers404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/members", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
}

// TestMembersAlbumTracklistPorSortIndex: /members of an album returns the tracks
// (kind=music_album) in sort_index order (track_number) -- NOT by year, unlike a
// collection.
func TestMembersAlbumTracklistPorSortIndex(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "alb1", "album", "Rio")
	semearMembro(t, d, "fx2", "Save A Prayer", "")
	semearMembro(t, d, "fx1", "Rio", "")
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: "r1", ParentID: "alb1", ChildID: "fx2", Kind: musicext.KindAlbum, SortIndex: sql.NullInt64{Int64: 2, Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntityRelation fx2: %v", err)
	}
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: "r2", ParentID: "alb1", ChildID: "fx1", Kind: musicext.KindAlbum, SortIndex: sql.NullInt64{Int64: 1, Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntityRelation fx1: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/alb1/members", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	cards := decodeCards(t, rec)
	if len(cards) != 2 {
		t.Fatalf("membros = %d, quero 2", len(cards))
	}
	eq(t, "1a faixa por track_number", cards[0].ID, "fx1")
	eq(t, "2a faixa por track_number", cards[1].ID, "fx2")
}

// TestMembersArtistaDiscografiaPorAno: /members of an artist returns the children linked by
// music_artista_principal, ordered by year (the same logic as a collection) -- it includes
// album and divergent collaboration track, the filter by type being the consumer's job
// (DiscografiaArtista, in the frontend).
func TestMembersArtistaDiscografiaPorAno(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "art1", "artista", "Duran Duran")
	semearMembro(t, d, "alb2015", "Paper Gods", "2015-09-11")
	semearMembro(t, d, "alb1982", "Rio", "1982-05-10")
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: "r1", ParentID: "art1", ChildID: "alb2015", Kind: musicext.KindArtistaPrincipal,
	}); err != nil {
		t.Fatalf("CriarEntityRelation alb2015: %v", err)
	}
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: "r2", ParentID: "art1", ChildID: "alb1982", Kind: musicext.KindArtistaPrincipal,
	}); err != nil {
		t.Fatalf("CriarEntityRelation alb1982: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/art1/members", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	cards := decodeCards(t, rec)
	if len(cards) != 2 {
		t.Fatalf("membros = %d, quero 2", len(cards))
	}
	eq(t, "1o album por ano", cards[0].ID, "alb1982")
	eq(t, "2o album por ano", cards[1].ID, "alb2015")
}
