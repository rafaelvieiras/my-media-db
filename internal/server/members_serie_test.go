package server

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// ligarFilhoOrdenado links parent->child with an arbitrary kind and sort_index (series
// tree). It proves that the order of /members follows the sort_index, not the insertion
// one.
func ligarFilhoOrdenado(t *testing.T, d *db.DB, id, parent, child, kind string, sortIndex int64) {
	t.Helper()
	if _, err := escrita(t, d).CriarEntityRelation(context.Background(), sqlcgen.CriarEntityRelationParams{
		ID: id, ParentID: parent, ChildID: child, Kind: kind,
		SortIndex: sql.NullInt64{Int64: sortIndex, Valid: true},
	}); err != nil {
		t.Fatalf("CriarEntityRelation(%s): %v", id, err)
	}
}

// TestMembersSerieOrdenaTemporadasPorSortIndex: /members of a SERIES returns the seasons in
// sort_index order (season number), NOT by year — linked out of order and with years that
// would invert the date ordering.
func TestMembersSerieOrdenaTemporadasPorSortIndex(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	// Season 1 has a LATER YEAR than season 2: if it ordered by year, it would come after.
	semearTemporada(t, d, "se1", "Temporada 1", "1995")
	semearTemporada(t, d, "se2", "Temporada 2", "1990")
	// Links out of order (se2 before se1) to prove that it is not insertion order.
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

// TestMembersTemporadaOrdenaEpisodiosPorSortIndex: /members of a SEASON returns the numbered
// episodes in sort_index order, linked out of order.
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

// TestMembersCardEpisodioPossuidoPelaJuncao covers the card consistency bug: an episode file
// has media_file.entity_id = SERIES and links to the EPISODE only through the
// media_file_entity join. The episode CARD (through /members -> ObterEntityCard) has to count
// that ownership through the join -- before, it came out owned=false/file_count=0, diverging
// from the detail and from the season's aggregate ownership. It also proves that it does NOT
// duplicate the count: the SERIES card (through the list) counts the same file a single time
// (through the direct entity_id).
func TestMembersCardEpisodioPossuidoPelaJuncao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "ep1", "episode", "Ep 1")
	semearEntity(t, d, "ep2", "episode", "Ep 2")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-ep1", "se1", "ep1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-ep2", "se1", "ep2", "tv_episode", 2)
	// File with entity_id = SERIES, linked to episode 1 ONLY through the join.
	semearFile(t, d, "mf1", "s1", "/lib/serie/s01e01.mkv", "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "ep1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	// Episode CARD through /members: ep1 owned through the join, ep2 not.
	cards := membrosDe(t, h, "se1")
	byID := map[string]entityCardDTO{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	eq(t, "ep1 card owned", byID["ep1"].Owned, true)
	eq(t, "ep1 card file_count", byID["ep1"].FileCount, 1)
	eq(t, "ep2 card owned", byID["ep2"].Owned, false)
	eq(t, "ep2 card file_count", byID["ep2"].FileCount, 0)

	// SERIES CARD through the top list: the SAME file (entity_id=s1) counts ONCE. The join
	// links the file to the episode (id != s1), so there is no double counting on the series
	// card. It proves the query's "does not duplicate" note.
	rec := do(t, h, http.MethodGet, "/api/v1/entities?type=tv", nil)
	out := decodeList(t, rec.Body.Bytes())
	if len(out.Items) != 1 || out.Items[0].ID != "s1" {
		t.Fatalf("lista type=tv = %+v, quero so a serie s1", out.Items)
	}
	eq(t, "serie card owned", out.Items[0].Owned, true)
	eq(t, "serie card file_count (sem duplicar)", out.Items[0].FileCount, 1)
}

// TestCardFilmeInalteradoSemJuncao ensures that the join extension does NOT change the card
// of a regular movie: without rows in media_file_entity, owned/file_count come only from the
// direct entity_id.
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

// TestMembersSerieCardTemporadaTraeOwnership: the card of each SEASON in the series members
// list carries ownership {owned,total} — the aggregate ownership of the episodes, annotated
// on the server (posseDaTemporada) without changing the card query. A season with 1 of 2
// episodes owned comes out {1,2}; one without ownership comes out {0,1}; and the EPISODES
// (members of the season) stay WITHOUT ownership (null), proving that only the season card is
// annotated.
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
	// e1 owned through the join: file with entity_id=series linked to the episode.
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
	// Season 1: 1 of 2 owned (partial).
	if byID["se1"].Ownership == nil {
		t.Fatalf("se1 sem ownership; quero {1,2}")
	}
	eq(t, "se1 owned", byID["se1"].Ownership.OwnedCount, 1)
	eq(t, "se1 total", byID["se1"].Ownership.TotalCount, 2)
	// Season 2: 0 of 1 (empty, but total>0).
	if byID["se2"].Ownership == nil {
		t.Fatalf("se2 sem ownership; quero {0,1}")
	}
	eq(t, "se2 owned", byID["se2"].Ownership.OwnedCount, 0)
	eq(t, "se2 total", byID["se2"].Ownership.TotalCount, 1)

	// Episodes (members of the SEASON) do NOT carry ownership (null): only the season card.
	for _, c := range membrosDe(t, h, "se1") {
		if c.Ownership != nil {
			t.Errorf("episodio %s com ownership %+v; quero nil", c.ID, c.Ownership)
		}
	}
}

// TestListEntitiesCardSerieOwnershipRaso: the SERIES card in the LISTING
// (GET /api/v1/entities) carries SHALLOW ownership = seasons WITH content / total of seasons,
// NOT the deep sum of episodes. Scenario: series with 2 seasons; se1 has ep1(owned)+ep2, se2
// has ep3(empty). Shallow card -> {1,2} (only se1 has content); the DETAIL of the same series
// stays DEEP -> {1,3} episodes (obterResolvida), proving the intentional divergence
// card(shallow) x detail(deep). The listing only brings TOP-LEVEL WORKS (season/episode are
// internal nodes, outside the grid); the movie stays without ownership.
func TestListEntitiesCardSerieOwnershipRaso(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "se2", "season", "Temporada 2")
	semearEntity(t, d, "e1", "episode", "Ep 1")
	semearEntity(t, d, "e2", "episode", "Ep 2")
	semearEntity(t, d, "e3", "episode", "Ep 3")
	semearEntity(t, d, "m1", "movie", "Filme")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-se2", "s1", "se2", "tv_season", 2)
	ligarFilhoOrdenado(t, d, "r-e1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-e2", "se1", "e2", "tv_episode", 2)
	ligarFilhoOrdenado(t, d, "r-e3", "se2", "e3", "tv_episode", 1)
	// Only ep1 owned: only season 1 has content.
	semearFile(t, d, "mf1", "s1", "/lib/s01e01.mkv", "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "e1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	byID := map[string]entityCardDTO{}
	for _, it := range decodeList(t, rec.Body.Bytes()).Items {
		byID[it.ID] = it
	}
	// SERIES card: SHALLOW -> 1 of 2 seasons with content (NOT 1/3 episodes).
	if byID["s1"].Ownership == nil {
		t.Fatalf("s1 (card) sem ownership; quero {1,2} temporadas")
	}
	eq(t, "s1 card owned (temporadas com conteudo)", byID["s1"].Ownership.OwnedCount, 1)
	eq(t, "s1 card total (temporadas)", byID["s1"].Ownership.TotalCount, 2)
	// The listing only brings top-level works: season/episode never show up in the grid.
	if _, ok := byID["se1"]; ok {
		t.Errorf("se1 (season) nao deveria aparecer na listagem de topo")
	}
	if _, ok := byID["e1"]; ok {
		t.Errorf("e1 (episode) nao deveria aparecer na listagem de topo")
	}
	// Movie in the listing stays WITHOUT ownership.
	if byID["m1"].Ownership != nil {
		t.Errorf("m1 card ownership = %+v, quero nil (filme)", byID["m1"].Ownership)
	}

	// The DETAIL of the SAME series stays DEEP (episodes): 1 of 3.
	s1 := obterResolvida(t, h, "s1")
	if s1.Ownership == nil {
		t.Fatalf("s1 (detalhe) sem ownership; quero {1,3} episodios")
	}
	eq(t, "s1 detalhe owned (episodios profundos)", s1.Ownership.OwnedCount, 1)
	eq(t, "s1 detalhe total (episodios profundos)", s1.Ownership.TotalCount, 3)
}

// semearTemporada creates a type='season' entity with a release_date (year) to prove that the
// season ordering does NOT fall into the by-year path.
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

// membrosDe does GET /entities/{id}/members and decodes the cards (it requires 200).
func membrosDe(t *testing.T, h http.Handler, id string) []entityCardDTO {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/members", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeCards(t, rec)
}
