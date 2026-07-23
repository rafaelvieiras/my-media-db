package e2e

// journeys_recasar_test.go cobre o briefing 0069: quando o scanner encontra uma
// série ANTES dela ter identidade/árvore (draft, fila de revisão), os arquivos
// ficam presos direto na entity da série, sem vínculo de episódio. A jornada
// ponta-a-ponta abaixo prova que "Corrigir" (POST /entities/{id}/correct) — que já
// sincroniza a árvore desde o 0067 — agora TAMBÉM recasa esses arquivos pendentes
// no mesmo instante, sem precisar de um novo scan.

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// TestJornadaCorrigirSerieRecasaArquivosPendentes: uma pasta de série com nome que
// o provider fake não reconhece ("Mystery Show") é escaneada — os 2 arquivos
// (S01E01/S01E02) ficam presos numa série RASCUNHO, sem vínculo de episódio (mesmo
// caminho de revisão do 0034). O dono então Corrige a identidade para a fixture
// real do fake (Springfield Tales, 71663): Enriquecer sincroniza a árvore (0067) E,
// no mesmo passo, recasa os 2 arquivos que já estavam presos (0069) — sem precisar
// de outro scan.
func TestJornadaCorrigirSerieRecasaArquivosPendentes(t *testing.T) {
	c := novoHarness(t)

	root := t.TempDir()
	base := filepath.Join(root, "Mystery Show")
	escreverVideo(t, filepath.Join(base, "Season 01", "Mystery Show S01E01.mkv"))
	escreverVideo(t, filepath.Join(base, "Season 01", "Mystery Show S01E02.mkv"))

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "tv", "content_type": "series"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// (1) O scan não resolveu "Mystery Show" no fake: cai na fila de revisão, uma
	// série RASCUNHO com os 2 arquivos presos direto nela, sem vínculo de episódio.
	serieID := acharSerieEmRevisao(t, c)
	if arqs := c.arquivosDaEntity(serieID); len(arqs) != 2 {
		t.Fatalf("arquivos presos na serie rascunho = %d, quero 2 (S01E01/S01E02 sem vinculo)", len(arqs))
	}

	// (2) O dono Corrige a identidade — aplicarIdentidadeExterna chama Enriquecer,
	// que sincroniza a árvore (0067) e, ao final dela, recasa os pendentes (0069).
	c.obj(http.MethodPost, "/api/v1/entities/"+serieID+"/correct",
		map[string]string{"source": "tmdb", "source_id": fake.SerieFixtureID}, http.StatusOK)

	// (3) A árvore nasceu (2 temporadas) e os 2 arquivos JÁ ESTAVAM na série — sem
	// nenhum novo scan — ganharam vínculo aos episódios certos pelo NÚMERO: ep 101
	// (S01E01) e ep 102 (S01E02); ep 103 (S01E03) continua sem arquivo algum.
	temporadas := c.idsDosMembros(serieID)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas apos corrigir = %d, quero 2 (arvore sincronizada)", len(temporadas))
	}
	epsT1 := c.idsDosMembros(temporadas[0])
	if len(epsT1) != 3 {
		t.Fatalf("episodios T1 = %d, quero 3", len(epsT1))
	}
	assertFileCount(t, c, epsT1[0], 1) // S01E01 -> ep 101, recasado sem novo scan
	assertFileCount(t, c, epsT1[1], 1) // S01E02 -> ep 102, recasado sem novo scan
	assertFileCount(t, c, epsT1[2], 0) // S01E03: nenhum arquivo cobria esse numero
	assertPosse(t, c, temporadas[0], 2, 3)
}

// acharSerieEmRevisao devolve o id da (única) série tv na fila de revisão (GET
// /api/v1/review) — o rascunho que o scanner criou por não resolver o título.
func acharSerieEmRevisao(t *testing.T, c *cliente) string {
	t.Helper()
	fila := c.arr(http.MethodGet, "/api/v1/review", nil, http.StatusOK)
	for _, item := range fila {
		m, _ := item.(map[string]any)
		if str(t, m, "type") == "tv" {
			return str(t, m, "id")
		}
	}
	t.Fatalf("fila de revisao sem nenhuma serie tv: %v", fila)
	return ""
}
