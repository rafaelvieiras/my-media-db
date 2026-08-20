package e2e

// Jornada de MÚSICA (briefings 0106/0115): importa um artista pela busca manual
// (discografia inteira, ADR-0027) e completa o álbum com a faixa que ficou de
// fora via a tela externa de "faixas faltando" (0115 Parte 2, ADR-0029). Usa o
// fakeMusicbrainz (fake_musicbrainz_test.go) — o harness E2E desta camada não
// sobe o scanner, então o caminho testado é o de import HTTP direto (busca
// manual → importar artista; tela externa → importar faixa avulsa), não o
// gatilho automático do scan.
//
// As asserções por etapa vivem em helpers próprios (importarArtistaEAlbum/
// assertMembrosAlbum/assertFaixaFaltando/importarFaixaAvulsa) para manter o
// gocyclo da função de teste baixo (AGENTS.md) — cada helper cobre 1 branch de
// verificação, a função principal só encadeia a jornada.

import (
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/server"
)

func TestJornadaMusicaImportArtistaEFaixaFaltando(t *testing.T) {
	c := novoHarnessComOpcoes(t, server.WithSearchProvider(fakeMusicbrainz{}))

	// 1) Importa o artista inteiro (busca manual → POST /import type=artista,
	// 0106): cria artista + o único álbum + a faixa que a árvore fake traz.
	_, albumID := importarArtistaEAlbum(t, c)
	assertMembrosAlbum(t, c, albumID, 1) // só a faixa que a árvore trouxe

	// 2) A tela externa do álbum lista as faixas OFICIAIS do release
	// (GET /external/musicbrainz/album/{mbid}): a faixa 1 (já importada pela
	// árvore) sai com internal_id preenchido; a faixa 2 (fora da árvore) sai
	// faltando — mesmo contrato de interno-first (0026) que coleção/série já
	// usam.
	faixa2SourceID := assertFaixaFaltando(t, c, albumID)

	// 3) Importa a faixa avulsa faltando, ligando-a ao álbum PAI já interno
	// (POST /import type=faixa + parent_id, 0115 Parte 2).
	faixa2 := importarFaixaAvulsa(t, c, faixa2SourceID, albumID, http.StatusCreated)
	if got := str(t, faixa2, "title"); got != "Faixa Dois" {
		t.Fatalf("title da faixa avulsa = %q, quero %q", got, "Faixa Dois")
	}
	assertMembrosAlbum(t, c, albumID, 2)

	// Reimportar a mesma faixa avulsa é IDEMPOTENTE (find-or-create por MBID,
	// mesmo padrão de movie/collection) — não duplica o vínculo.
	faixa2Denovo := importarFaixaAvulsa(t, c, faixa2SourceID, albumID, http.StatusOK)
	if str(t, faixa2Denovo, "id") != str(t, faixa2, "id") {
		t.Fatalf("reimport da faixa avulsa criou outra entity")
	}
	assertMembrosAlbum(t, c, albumID, 2)
}

// importarArtistaEAlbum importa o artista fake (discografia inteira) e devolve
// (artistaID, albumID) — o álbum é o único membro do artista recém-criado.
func importarArtistaEAlbum(t *testing.T, c *cliente) (artistaID, albumID string) {
	t.Helper()
	artista := c.obj("POST", "/api/v1/import", map[string]any{
		"source":    "musicbrainz",
		"source_id": mbArtistaMBID,
		"type":      "artista",
	}, http.StatusCreated)
	artistaID = str(t, artista, "id")
	if got := str(t, artista, "title"); got != "Artista Fake" {
		t.Fatalf("title do artista = %q, quero %q", got, "Artista Fake")
	}
	membros := objs(t, c.arr("GET", "/api/v1/entities/"+artistaID+"/members", nil, http.StatusOK))
	if len(membros) != 1 {
		t.Fatalf("membros (álbuns) do artista = %d, quero 1", len(membros))
	}
	return artistaID, str(t, membros[0], "id")
}

// assertMembrosAlbum confere a contagem de faixas (members) do álbum.
func assertMembrosAlbum(t *testing.T, c *cliente, albumID string, quero int) {
	t.Helper()
	membros := c.arr("GET", "/api/v1/entities/"+albumID+"/members", nil, http.StatusOK)
	if len(membros) != quero {
		t.Fatalf("membros (faixas) do álbum = %d, quero %d", len(membros), quero)
	}
}

// assertFaixaFaltando confere o contrato da tela externa de álbum (2 faixas
// oficiais, a 1ª já interna e a 2ª faltando) e devolve o source_id MusicBrainz
// da faixa faltando, para o chamador importá-la em seguida.
func assertFaixaFaltando(t *testing.T, c *cliente, albumID string) string {
	t.Helper()
	albumExt := c.obj("GET", "/api/v1/external/musicbrainz/album/"+mbAlbumMBID, nil, http.StatusOK)
	if got := str(t, albumExt, "internal_id"); got != albumID {
		t.Fatalf("internal_id do álbum externo = %q, quero %q", got, albumID)
	}
	tracksRaw, ok := albumExt["tracks"].([]any)
	if !ok || len(tracksRaw) != 2 {
		t.Fatalf("tracks do álbum externo = %v, quero 2 (release oficial completo)", albumExt["tracks"])
	}
	tracks := objs(t, tracksRaw)
	if _, ok := tracks[0]["internal_id"].(string); !ok {
		t.Fatalf("track[0].internal_id ausente/vazio (deveria ser a faixa já importada): %v", tracks[0])
	}
	if tracks[1]["internal_id"] != nil {
		t.Fatalf("track[1].internal_id = %v, quero nil (faixa ainda não importada)", tracks[1]["internal_id"])
	}
	faixa2SourceID := str(t, tracks[1], "source_id")
	if faixa2SourceID != mbFaixa2MBID {
		t.Fatalf("track[1].source_id = %q, quero %q", faixa2SourceID, mbFaixa2MBID)
	}
	return faixa2SourceID
}

// importarFaixaAvulsa dispara POST /import type=faixa (0115 Parte 2), ligando
// ao álbum pai já interno, e exige o status HTTP esperado (201 na 1ª vez, 200
// no reimport idempotente).
func importarFaixaAvulsa(t *testing.T, c *cliente, sourceID, albumID string, statusEsperado int) map[string]any {
	t.Helper()
	return c.obj("POST", "/api/v1/import", map[string]any{
		"source":    "musicbrainz",
		"source_id": sourceID,
		"type":      "faixa",
		"parent_id": albumID,
	}, statusEsperado)
}
