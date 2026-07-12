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

// TestMembersCardEpisodioPossuidoPelaJuncao cobre o bug de consistencia do card (0034):
// um arquivo de episodio tem media_file.entity_id = SERIE e liga ao EPISODIO so pela
// juncao media_file_entity. O CARD do episodio (via /members -> ObterEntityCard) precisa
// contar essa posse pela juncao -- antes vinha owned=false/file_count=0, divergindo do
// detalhe e da posse agregada da temporada. Tambem prova que NAO duplica a contagem: o
// card da SERIE (via a lista) conta o mesmo arquivo uma unica vez (pelo entity_id direto).
func TestMembersCardEpisodioPossuidoPelaJuncao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "ep1", "episode", "Ep 1")
	semearEntity(t, d, "ep2", "episode", "Ep 2")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-ep1", "se1", "ep1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-ep2", "se1", "ep2", "tv_episode", 2)
	// Arquivo com entity_id = SERIE, vinculado ao episodio 1 SO pela juncao.
	semearFile(t, d, "mf1", "s1", "/lib/serie/s01e01.mkv", "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "ep1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	// CARD do episodio via /members: ep1 possuido pela juncao, ep2 nao.
	cards := membrosDe(t, h, "se1")
	byID := map[string]entityCardDTO{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	eq(t, "ep1 card owned", byID["ep1"].Owned, true)
	eq(t, "ep1 card file_count", byID["ep1"].FileCount, 1)
	eq(t, "ep2 card owned", byID["ep2"].Owned, false)
	eq(t, "ep2 card file_count", byID["ep2"].FileCount, 0)

	// CARD da SERIE via a lista de topo: o MESMO arquivo (entity_id=s1) conta UMA vez.
	// A juncao liga o arquivo ao episodio (id != s1), entao nao ha dupla contagem no card
	// da serie. Prova a nota de "nao duplica" da query.
	rec := do(t, h, http.MethodGet, "/api/v1/entities?type=tv", nil)
	out := decodeList(t, rec.Body.Bytes())
	if len(out.Items) != 1 || out.Items[0].ID != "s1" {
		t.Fatalf("lista type=tv = %+v, quero so a serie s1", out.Items)
	}
	eq(t, "serie card owned", out.Items[0].Owned, true)
	eq(t, "serie card file_count (sem duplicar)", out.Items[0].FileCount, 1)
}

// TestCardFilmeInalteradoSemJuncao garante que a extensao pela juncao NAO muda o card do
// filme comum: sem linhas em media_file_entity, owned/file_count vem so do entity_id direto.
func TestCardFilmeInalteradoSemJuncao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "m1", "movie", "Filme")
	semearFile(t, d, "f1", "m1", "/lib/m1.mkv", "present", 0.9, "")
	semearFile(t, d, "f2", "m1", "/lib/m1-extra.mkv", "missing", 0.5, "")

	rec := do(t, h, http.MethodGet, "/api/v1/entities?type=movie", nil)
	out := decodeList(t, rec.Body.Bytes())
	if len(out.Items) != 1 || out.Items[0].ID != "m1" {
		t.Fatalf("lista type=movie = %+v, quero so o filme m1", out.Items)
	}
	eq(t, "filme card owned", out.Items[0].Owned, true)
	eq(t, "filme card file_count", out.Items[0].FileCount, 2)
}

// TestMembersSerieCardTemporadaTraeOwnership (0036): o card de cada TEMPORADA na lista
// de membros da serie carrega ownership {owned,total} — a posse agregada dos episodios,
// anotada no server (posseDaTemporada) sem mudar a query do card. Uma temporada com 1 de
// 2 episodios possuidos vem {1,2}; uma sem posse vem {0,1}; e os EPISODIOS (membros da
// temporada) seguem SEM ownership (null), provando que so o card season e anotado.
func TestMembersSerieCardTemporadaTraeOwnership(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "se2", "season", "Temporada 2")
	semearEntity(t, d, "e1", "episode", "Ep 1")
	semearEntity(t, d, "e2", "episode", "Ep 2")
	semearEntity(t, d, "e3", "episode", "Ep 3")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-se2", "s1", "se2", "tv_season", 2)
	ligarFilhoOrdenado(t, d, "r-e1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-e2", "se1", "e2", "tv_episode", 2)
	ligarFilhoOrdenado(t, d, "r-e3", "se2", "e3", "tv_episode", 1)
	// e1 possuido pela juncao: arquivo com entity_id=serie vinculado ao episodio.
	semearFile(t, d, "mf1", "s1", "/lib/s01e01.mkv", "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "e1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	cards := membrosDe(t, h, "s1")
	byID := map[string]entityCardDTO{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	// Temporada 1: 1 de 2 possuidos (parcial).
	if byID["se1"].Ownership == nil {
		t.Fatalf("se1 sem ownership; quero {1,2}")
	}
	eq(t, "se1 owned", byID["se1"].Ownership.OwnedCount, 1)
	eq(t, "se1 total", byID["se1"].Ownership.TotalCount, 2)
	// Temporada 2: 0 de 1 (vazia, mas total>0).
	if byID["se2"].Ownership == nil {
		t.Fatalf("se2 sem ownership; quero {0,1}")
	}
	eq(t, "se2 owned", byID["se2"].Ownership.OwnedCount, 0)
	eq(t, "se2 total", byID["se2"].Ownership.TotalCount, 1)

	// Episodios (membros da TEMPORADA) NAO carregam ownership (null): so o card season.
	for _, c := range membrosDe(t, h, "se1") {
		if c.Ownership != nil {
			t.Errorf("episodio %s com ownership %+v; quero nil", c.ID, c.Ownership)
		}
	}
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
