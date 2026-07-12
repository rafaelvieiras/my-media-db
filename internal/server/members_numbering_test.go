package server

import (
	"context"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// members_numbering_test cobre o numbering REAL do card de episodio (0039): o card
// carrega {season,number,absolute_number?} lido do extension_data core.tv@1 do
// episodio, projetado na ordenacao da temporada-pai -- NAO a posicao na lista. Prova
// que biblioteca esparsa (E01+E05) mostra os numeros reais, que nao-episodio e episodio
// draft ficam sem numbering, e o fallback ao absoluto.

// semearExtension grava o extension_data_json cru de uma entity ja semeada.
func semearExtension(t *testing.T, d *db.DB, id, extJSON string) {
	t.Helper()
	if err := escrita(t, d).AtualizarExtensionData(context.Background(), sqlcgen.AtualizarExtensionDataParams{
		ExtensionDataJson: nsOf(extJSON),
		ID:                id,
	}); err != nil {
		t.Fatalf("AtualizarExtensionData(%s): %v", id, err)
	}
}

// TestMembersNumberingRealDoEpisodio: os cards de episodio trazem o numbering REAL da
// ordenacao da temporada-pai, mesmo com buracos na biblioteca (E01+E05, sem E02-E04).
// O card do E05 deve dizer number=5 (nao a 2a posicao) e expor o absoluto quando difere.
func TestMembersNumberingRealDoEpisodio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":1}}`)
	semearEntity(t, d, "e1", "episode", "Ep 1")
	semearExtension(t, d, "e1", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":1}},"absolute_number":1}}`)
	semearEntity(t, d, "e5", "episode", "Ep 5")
	semearExtension(t, d, "e5", `{"core.tv@1":{"numbering":{"official":{"season":1,"number":5}},"absolute_number":128}}`)
	// Ligados por sort_index 1 e 5: a biblioteca so tem o 1o e o 5o episodio.
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
	// E01: numero 1; absoluto igual ao numero => omitido (nil) no DTO.
	if byID["e1"].Numbering == nil {
		t.Fatalf("e1 sem numbering; quero {1,1}")
	}
	eq(t, "e1 season", byID["e1"].Numbering.Season, 1)
	eq(t, "e1 number", byID["e1"].Numbering.Number, 1)
	if byID["e1"].Numbering.AbsoluteNumber == nil {
		t.Fatalf("e1 absolute_number nil; quero 1 (presente)")
	}
	eq(t, "e1 absolute", *byID["e1"].Numbering.AbsoluteNumber, 1)
	// E05: numero REAL 5 (nao a 2a posicao); absoluto distinto (128).
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

// TestMembersNumberingAusenteParaNaoEpisodio: os cards de TEMPORADA (membros da serie)
// NAO carregam numbering -- so o card de episodio recebe a anotacao.
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

// TestMembersNumberingDraftSemNumbering: episodio draft (sem extension_data) fica SEM
// numbering -- a UI cai no rotulo neutro, sem inventar posicao.
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

// TestMembersNumberingFallbackAbsoluto: episodio SEM entrada para a ordenacao da
// temporada, mas COM absolute_number, cai no absoluto como numero (season = o da
// temporada-pai). Cobre o fallback do 0039 (ordenacao nao casa, mas o absoluto casa).
func TestMembersNumberingFallbackAbsoluto(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "se1", "season", "Temporada 2")
	semearExtension(t, d, "se1", `{"core.tv@1":{"ordering":"official","season_number":2}}`)
	semearEntity(t, d, "e1", "episode", "Ep so-absoluto")
	// Numbering so na ordem "dvd" (nao a "official" da temporada) + absoluto.
	semearExtension(t, d, "e1", `{"core.tv@1":{"numbering":{"dvd":{"season":2,"number":9}},"absolute_number":42}}`)
	ligarFilhoOrdenado(t, d, "re1", "se1", "e1", "tv_episode", 1)

	cards := membrosDe(t, h, "se1")
	if len(cards) != 1 || cards[0].Numbering == nil {
		t.Fatalf("cards = %+v; quero 1 com numbering (fallback absoluto)", cards)
	}
	eq(t, "fallback season (da temporada-pai)", cards[0].Numbering.Season, 2)
	eq(t, "fallback number (= absoluto)", cards[0].Numbering.Number, 42)
}
