package server

// Testes do agrupamento da fila de revisão (briefing 0137): a chave/rótulo saem
// do `media_file.path` (não das tags, que 21 das 185 faixas reais não têm), e o
// agrupamento só ANOTA o card -- o predicado da fila continua o do 0018.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGrupoDeRevisaoPorTipo cobre os três kinds e o caso sem arquivo.
func TestGrupoDeRevisaoPorTipo(t *testing.T) {
	raizes := []string{"/media/gdrive-media"}

	casos := []struct {
		nome        string
		tipo        string
		caminho     string
		queroKind   string
		queroLabel  string
		queroMesmaK string // chave de comparação: outro caminho que deve cair no MESMO grupo
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

	semArquivo := grupoDeRevisao("movie", "", raizes)
	if semArquivo.Kind != grupoRevisaoSemArquivo {
		t.Errorf("entity sem arquivo: kind = %q, quero %q", semArquivo.Kind, grupoRevisaoSemArquivo)
	}
}

// TestGrupoDeRevisaoNaoCortaNomeDeObra garante que a remoção de segmento de
// disco/temporada não engole nome de obra que COMEÇA com as mesmas palavras.
func TestGrupoDeRevisaoNaoCortaNomeDeObra(t *testing.T) {
	casos := []struct{ tipo, caminho, queroLabel string }{
		{tipoEntFaixa, "/lib/Musicas/Banda/Disco Voador/01 - faixa.mp3", "Banda — Disco Voador"},
		{tipoEntFaixa, "/lib/Musicas/Banda/Season of the Witch/01 - faixa.mp3", "Banda — Season of the Witch"},
		// pasta de biblioteca chamada "Series" não pode ser confundida com temporada
		{tipoEntEpisodio, "/lib/Series/The Office/S06E25.mkv", "The Office"},
		{tipoEntEpisodio, "/lib/Videos/Series/Serie/Season 3/S01E01.mkv", "Serie"},
	}
	for _, c := range casos {
		if g := grupoDeRevisao(c.tipo, c.caminho, []string{"/lib"}); g.Label != c.queroLabel {
			t.Errorf("grupo de %q = %q, quero %q", c.caminho, g.Label, c.queroLabel)
		}
	}
}

// TestListReviewAnotaGrupo cobre o contrato HTTP: cada item do GET /review sai
// com review_group (kind/key/label/size) e o size conta os itens do MESMO grupo.
func TestListReviewAnotaGrupo(t *testing.T) {
	s, d := setupSystemHealthServer(t)
	semearLibrarySource(t, d, "src-1", "/media/lib")

	// Duas faixas do MESMO álbum (uma em cada disco) + uma de outro álbum.
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
