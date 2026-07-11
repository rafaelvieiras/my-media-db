package server

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// ligarFilhoOrdenado liga parent->child com um kind e sort_index arbitrarios (arvore de
// series). Prova que a ordem de /members segue o sort_index, nao a de insercao.
func ligarFilhoOrdenado(t *testing.T, d *db.DB, id, parent, child, kind string, sortIndex int64) {
	t.Helper()
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: id, ParentID: parent, ChildID: child, Kind: kind,
		SortIndex: sql.NullInt64{Int64: sortIndex, Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntityRelation(%s): %v", id, err)
	}
}

// TestMembersSerieOrdenaTemporadasPorSortIndex: /members de uma SERIE devolve as
// temporadas na ordem de sort_index (numero da temporada), NAO por ano — ligadas fora
// de ordem e com anos que inverteriam a ordenacao por data.
func TestMembersSerieOrdenaTemporadasPorSortIndex(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	// Temporada 1 tem ANO mais tarde que a 2: se ordenasse por ano, viria depois.
	semearTemporada(t, d, "se1", "Temporada 1", "1995")
	semearTemporada(t, d, "se2", "Temporada 2", "1990")
	// Liga fora de ordem (se2 antes de se1) para provar que nao e ordem de insercao.
	ligarFilhoOrdenado(t, d, "r2", "s1", "se2", "tv_season", 2)
	ligarFilhoOrdenado(t, d, "r1", "s1", "se1", "tv_season", 1)

	cards := membrosDe(t, h, "s1")
	if len(cards) != 2 {
		t.Fatalf("temporadas = %d, quero 2", len(cards))
	}
	eq(t, "1a temporada (sort 1)", cards[0].ID, "se1")
	eq(t, "2a temporada (sort 2)", cards[1].ID, "se2")
	eq(t, "type do card", cards[0].Type, "season")
}

// TestMembersTemporadaOrdenaEpisodiosPorSortIndex: /members de uma TEMPORADA devolve os
// episodios numerados na ordem de sort_index, ligados fora de ordem.
func TestMembersTemporadaOrdenaEpisodiosPorSortIndex(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "e1", "episode", "Episodio 1")
	semearEntity(t, d, "e2", "episode", "Episodio 2")
	semearEntity(t, d, "e3", "episode", "Episodio 3")
	ligarFilhoOrdenado(t, d, "re3", "se1", "e3", "tv_episode", 3)
	ligarFilhoOrdenado(t, d, "re1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "re2", "se1", "e2", "tv_episode", 2)

	cards := membrosDe(t, h, "se1")
	if len(cards) != 3 {
		t.Fatalf("episodios = %d, quero 3", len(cards))
	}
	eq(t, "1o episodio", cards[0].ID, "e1")
	eq(t, "2o episodio", cards[1].ID, "e2")
	eq(t, "3o episodio", cards[2].ID, "e3")
	eq(t, "type do card", cards[0].Type, "episode")
}

// semearTemporada cria uma entity type='season' com release_date (ano) para provar que
// a ordenacao de temporadas NAO cai no caminho por-ano.
func semearTemporada(t *testing.T, d *db.DB, id, titulo, ano string) {
	t.Helper()
	p := sqlcgen.CriarEntityParams{ID: id, Type: "season", Title: titulo}
	if ano != "" {
		p.ReleaseDate = nsOf(ano)
	}
	if _, err := escrita(t, d).CriarEntity(context.Background(), p); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// membrosDe faz GET /entities/{id}/members e decodifica os cards (exige 200).
func membrosDe(t *testing.T, h http.Handler, id string) []entityCardDTO {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/members", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeCards(t, rec)
}
