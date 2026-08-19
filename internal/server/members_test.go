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

// TestMembersFiltraKindDiferente: so relacoes kind='collection' viram membros; um filho
// ligado por OUTRO kind (ex.: 'season') fica de fora.
func TestMembersFiltraKindDiferente(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "col", "collection", "Colecao")
	semearMembro(t, d, "m1", "Membro", "2001-01-01")
	semearMembro(t, d, "m2", "Nao-membro", "2002-01-01")
	ligarMembro(t, d, "r1", "col", "m1") // kind='collection'
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: "r2", ParentID: "col", ChildID: "m2", Kind: "season", // kind diferente -> ignorado
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

// TestMembers404: /members de uma entity inexistente e 404.
func TestMembers404(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/members", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
}

// TestMembersAlbumTracklistPorSortIndex: /members de um album (briefing 0111) devolve
// as faixas (kind=music_album) na ordem de sort_index (track_number) -- NAO por ano,
// ao contrario de colecao.
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

// TestMembersArtistaDiscografiaPorAno: /members de um artista (briefing 0111) devolve
// os filhos ligados por music_artista_principal, ordenados por ano (mesma logica de
// colecao) -- inclui album e faixa de colaboracao divergente, filtro por type fica a
// cargo do consumidor (DiscografiaArtista, no front).
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
