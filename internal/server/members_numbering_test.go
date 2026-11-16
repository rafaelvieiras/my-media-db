package server

import (
	"context"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// members_numbering_test covers the REAL numbering of the episode card: the card
// carries {season,number,absolute_number?} read from the episode's extension_data
// core.tv@1, projected onto the ordering of the parent season -- NOT the position in the
// list. It proves that a sparse library (E01+E05) shows the real numbers, that a
// non-episode and a draft episode stay without numbering, and the fallback to the
// absolute one.

// semearExtension writes the raw extension_data_json of an already seeded entity.
func semearExtension(t *testing.T, d *db.DB, id, extJSON string) {
	t.Helper()
	if err := escrita(t, d).AtualizarExtensionData(context.Background(), sqlcgen.AtualizarExtensionDataParams{
		ExtensionDataJson: nsOf(extJSON),
		ID:                id,
	}); err != nil {
		t.Fatalf("AtualizarExtensionData(%s): %v", id, err)
	}
}

// TestMembersNumberingRealDoEpisodio: the episode cards bring the REAL numbering of the
// parent season ordering, even with gaps in the library (E01+E05, without E02-E04).
// The E05 card must say number=5 (not the 2nd position) and expose the absolute one when
// it differs.
func TestMembersNumberingRealDoEpisodio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	semearEntity(t, d, "e1", "episode", "Ep 1")
	semearExtension(t, d, "e1", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":1}},"absolute_number":1}}`)
	semearEntity(t, d, "e5", "episode", "Ep 5")
	semearExtension(t, d, "e5", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":5}},"absolute_number":128}}`)
	// Linked by sort_index 1 and 5: the library only has the 1st and the 5th
	// episode.
	ligarFilhoOrdenado(t, d, "re1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "re5", "se1", "e5", "tv_episode", 5)

	cards := membrosDe(t, h, "se1")
	if len(cards) != 2 {
		t.Fatalf("episodios = %d, quero 2", len(cards))
	}
	byID := map[string]entityCardDTO{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	// E01: number 1; absolute equal to the number => omitted (nil) in the DTO.
	if byID["e1"].Numbering == nil {
		t.Fatalf("e1 sem numbering; quero {1,1}")
	}
	eq(t, "e1 season", byID["e1"].Numbering.Season, 1)
	eq(t, "e1 number", byID["e1"].Numbering.Number, 1)
	if byID["e1"].Numbering.AbsoluteNumber == nil {
		t.Fatalf("e1 absolute_number nil; quero 1 (presente)")
	}
	eq(t, "e1 absolute", *byID["e1"].Numbering.AbsoluteNumber, 1)
	// E05: REAL number 5 (not the 2nd position); distinct absolute (128).
	if byID["e5"].Numbering == nil {
		t.Fatalf("e5 sem numbering; quero {1,5,#128}")
	}
	eq(t, "e5 season", byID["e5"].Numbering.Season, 1)
	eq(t, "e5 number (real, nao posicao)", byID["e5"].Numbering.Number, 5)
	if byID["e5"].Numbering.AbsoluteNumber == nil {
		t.Fatalf("e5 absolute_number nil; quero 128")
	}
	eq(t, "e5 absolute", *byID["e5"].Numbering.AbsoluteNumber, 128)
}

// TestMembersNumberingAusenteParaNaoEpisodio: the SEASON cards (members of the series)
// do NOT carry numbering -- only the episode card gets the annotation.
func TestMembersNumberingAusenteParaNaoEpisodio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)

	for _, c := range membrosDe(t, h, "s1") {
		if c.Numbering != nil {
			t.Errorf("card %s (%s) com numbering %+v; quero nil (nao-episodio)", c.ID, c.Type, c.Numbering)
		}
	}
}

// TestMembersNumberingDraftSemNumbering: a draft episode (without extension_data) stays
// WITHOUT numbering -- the UI falls back to the neutral label, without inventing a
// position.
func TestMembersNumberingDraftSemNumbering(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	semearEntity(t, d, "e1", "episode", "Ep 1 (draft)")
	ligarFilhoOrdenado(t, d, "re1", "se1", "e1", "tv_episode", 1)

	cards := membrosDe(t, h, "se1")
	if len(cards) != 1 {
		t.Fatalf("episodios = %d, quero 1", len(cards))
	}
	if cards[0].Numbering != nil {
		t.Errorf("episodio draft com numbering %+v; quero nil", cards[0].Numbering)
	}
}

// TestMembersNumberingFallbackAbsoluto: an episode WITHOUT an entry for the season
// ordering, but WITH absolute_number, falls back to the absolute one as the number
// (season = the parent season's). It covers the fallback (the ordering does not match,
// but the absolute one does).
func TestMembersNumberingFallbackAbsoluto(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "se1", "season", "Temporada 2")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":2}}`)
	semearEntity(t, d, "e1", "episode", "Ep so-absoluto")
	// Numbering only in the "dvd" order (not the season's "official") + absolute.
	semearExtension(t, d, "e1", `{"core.tv@1":{"numbering":{"dvd":{"season":2,"number":9}},"absolute_number":42}}`)
	ligarFilhoOrdenado(t, d, "re1", "se1", "e1", "tv_episode", 1)

	cards := membrosDe(t, h, "se1")
	if len(cards) != 1 || cards[0].Numbering == nil {
		t.Fatalf("cards = %+v; quero 1 com numbering (fallback absoluto)", cards)
	}
	eq(t, "fallback season (da temporada-pai)", cards[0].Numbering.Season, 2)
	eq(t, "fallback number (= absoluto)", cards[0].Numbering.Number, 42)
}
