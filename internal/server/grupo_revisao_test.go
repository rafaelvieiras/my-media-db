package server

// Tests of the review queue grouping: the key/label come
// from `media_file.path` (not from the tags, which 21 of the 185 real tracks do not
// have), and the grouping only ANNOTATES the card -- the queue predicate stays
// unchanged.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGrupoDeRevisaoPorTipo covers the three kinds and the case without a file.
func TestGrupoDeRevisaoPorTipo(t *testing.T) {
	raizes := []string{"/media/gdrive-media"}

	casos := []struct {
		nome        string
		tipo        string
		caminho     string
		queroKind   string
		queroLabel  string
		queroMesmaK string // comparison key: another path that must fall into the SAME group
	}{
		{
			nome:        "faixa (disco ignorado)",
			tipo:        tipoEntFaixa,
			caminho:     "/media/gdrive-media/Musicas/Duran Duran/Notorious (Deluxe Edition) (1984)/Disc 1/04 - A Matter of Feeling.mp3",
			queroKind:   grupoRevisaoAlbum,
			queroLabel:  "Duran Duran — Notorious (Deluxe Edition) (1984)",
			queroMesmaK: "/media/gdrive-media/Musicas/Duran Duran/Notorious (Deluxe Edition) (1984)/Disc 2/09 - Outra.mp3",
		},
		{
			nome:        "episódio (temporada ignorada)",
			tipo:        tipoEntEpisodio,
			caminho:     "/media/gdrive-media/Videos/Series/The Office/Season 6/S06E25.mkv",
			queroKind:   grupoRevisaoSerie,
			queroLabel:  "The Office",
			queroMesmaK: "/media/gdrive-media/Videos/Series/The Office/Season 3/S03E24.mkv",
		},
		{
			// A series still without identity holds the files of its seasons: the section
			// names the SERIES, never "Temporada 01" (the season is not an item waiting for
			// a series of its own -- the owner read it that way, and he was right).
			nome:        "série sem identidade (temporada ignorada)",
			tipo:        "tv",
			caminho:     "/media/gdrive-media/Videos/Series/C.S.I. NY/Temporada 01/CSI NY - S01E01 - Blink.mkv",
			queroKind:   grupoRevisaoPasta,
			queroLabel:  "C.S.I. NY",
			queroMesmaK: "/media/gdrive-media/Videos/Series/C.S.I. NY/Temporada 02/CSI NY - S02E01 - Outro.mkv",
		},
		{
			nome:        "filme (pasta da obra)",
			tipo:        "movie",
			caminho:     "/media/gdrive-media/Videos/Filmes/Familia Addams 3 (2025)/filme.mkv",
			queroKind:   grupoRevisaoPasta,
			queroLabel:  "Familia Addams 3 (2025)",
			queroMesmaK: "/media/gdrive-media/Videos/Filmes/Familia Addams 3 (2025)/extras.mkv",
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			g := grupoDeRevisao(c.tipo, c.caminho, raizes)
			if g.Kind != c.queroKind {
				t.Errorf("kind = %q, quero %q", g.Kind, c.queroKind)
			}
			if g.Label != c.queroLabel {
				t.Errorf("label = %q, quero %q", g.Label, c.queroLabel)
			}
			outro := grupoDeRevisao(c.tipo, c.queroMesmaK, raizes)
			if outro.Key != g.Key {
				t.Errorf("chave divergente entre arquivos do mesmo grupo: %q x %q", g.Key, outro.Key)
			}
		})
	}

	// No file is not a case of the queue anymore (ListarRevisao requires a file): the empty
	// path is only the defensive branch, and it must not invent a kind of its own.
	semArquivo := grupoDeRevisao("movie", "", raizes)
	if semArquivo.Kind != grupoRevisaoPasta {
		t.Errorf("entity sem arquivo: kind = %q, quero %q", semArquivo.Kind, grupoRevisaoPasta)
	}
}

// TestGrupoDeRevisaoNaoCortaNomeDeObra guarantees that the removal of the
// disc/season segment does not swallow a work name that STARTS with the same words.
func TestGrupoDeRevisaoNaoCortaNomeDeObra(t *testing.T) {
	casos := []struct{ tipo, caminho, queroLabel string }{
		{tipoEntFaixa, "/lib/Musicas/Banda/Disco Voador/01 - faixa.mp3", "Banda — Disco Voador"},
		{tipoEntFaixa, "/lib/Musicas/Banda/Season of the Witch/01 - faixa.mp3", "Banda — Season of the Witch"},
		// a library folder named "Series" cannot be mistaken for a season
		{tipoEntEpisodio, "/lib/Series/The Office/S06E25.mkv", "The Office"},
		{tipoEntEpisodio, "/lib/Videos/Series/Serie/Season 3/S01E01.mkv", "Serie"},
	}
	for _, c := range casos {
		if g := grupoDeRevisao(c.tipo, c.caminho, []string{"/lib"}); g.Label != c.queroLabel {
			t.Errorf("grupo de %q = %q, quero %q", c.caminho, g.Label, c.queroLabel)
		}
	}
}

// TestListReviewAnotaGrupo covers the HTTP contract: each item of GET /review comes out
// with review_group (kind/key/label/size) and the size counts the items of the SAME
// group.
func TestListReviewAnotaGrupo(t *testing.T) {
	s, d := setupSystemHealthServer(t)
	semearLibrarySource(t, d, "src-1", "/media/lib")

	// Two tracks of the SAME album (one on each disc) + one from another album.
	faixas := []struct{ id, caminho string }{
		{"fx-1", "/media/lib/Musicas/Duran Duran/Notorious/Disc 1/01 - A.mp3"},
		{"fx-2", "/media/lib/Musicas/Duran Duran/Notorious/Disc 2/09 - B.mp3"},
		{"fx-3", "/media/lib/Musicas/Sade/Stronger Than Pride/03 - C.mp3"},
	}
	for _, f := range faixas {
		semearEntity(t, d, f.id, tipoEntFaixa, "faixa "+f.id)
		definirStatus(t, d, f.id, "draft")
		semearFile(t, d, "mf-"+f.id, f.id, f.caminho, "present", 0, "")
	}

	rec := httptest.NewRecorder()
	s.listReview(rec, httptest.NewRequest(http.MethodGet, "/api/v1/review", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var cards []entityCardDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil {
		t.Fatalf("decodificar /review: %v", err)
	}
	if len(cards) != 3 {
		t.Fatalf("cards = %d, quero 3 (o agrupamento não pode mudar o predicado da fila)", len(cards))
	}
	porID := make(map[string]*reviewGroupDTO, len(cards))
	for _, card := range cards {
		if card.ReviewGroup == nil {
			t.Fatalf("card %s saiu sem review_group", card.ID)
		}
		porID[card.ID] = card.ReviewGroup
	}
	if porID["fx-1"].Key != porID["fx-2"].Key {
		t.Errorf("faixas do mesmo álbum em discos diferentes caíram em grupos distintos: %q x %q",
			porID["fx-1"].Key, porID["fx-2"].Key)
	}
	if porID["fx-1"].Size != 2 {
		t.Errorf("size do grupo do álbum = %d, quero 2", porID["fx-1"].Size)
	}
	if porID["fx-1"].Label != "Duran Duran — Notorious" {
		t.Errorf("label = %q, quero %q", porID["fx-1"].Label, "Duran Duran — Notorious")
	}
	if porID["fx-3"].Size != 1 || porID["fx-3"].Key == porID["fx-1"].Key {
		t.Errorf("grupo da outra faixa = %+v, quero size 1 e chave distinta", porID["fx-3"])
	}
}
