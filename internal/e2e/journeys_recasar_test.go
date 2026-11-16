package e2e

// journeys_recasar_test.go covers the case where the scanner finds a series
// BEFORE it has an identity/tree (draft, review queue): the files end up stuck
// directly on the series entity, with no episode link. The end-to-end journey
// below proves that "Corrigir" (POST /entities/{id}/correct) — which already
// synchronized the tree — now ALSO re-matches those pending files on the spot,
// with no need for a new scan.

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// TestJornadaCorrigirSerieRecasaArquivosPendentes: a series folder with a name the
// fake provider does not recognize ("Mystery Show") is scanned — the 2 files
// (S01E01/S01E02) get stuck in a DRAFT series, with no episode link (the same
// review path as the series scanner). The owner then Corrects the identity to the
// fake's real fixture (Springfield Tales, 71663): Enrich synchronizes the tree AND,
// in the same step, re-matches the 2 files that were already stuck — with no need
// for another scan.
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

	// (1) The scan did not resolve "Mystery Show" on the fake: it falls into the
	// review queue, a DRAFT series with the 2 files stuck directly on it, with no
	// episode link.
	serieID := acharSerieEmRevisao(t, c)
	if arqs := c.arquivosDaEntity(serieID); len(arqs) != 2 {
		t.Fatalf("arquivos presos na serie rascunho = %d, quero 2 (S01E01/S01E02 sem vinculo)", len(arqs))
	}

	// (2) The owner Corrects the identity — aplicarIdentidadeExterna calls Enrich,
	// which synchronizes the tree and, at the end of it, re-matches the pendings.
	c.obj(http.MethodPost, "/api/v1/entities/"+serieID+"/correct",
		map[string]string{"source": "tmdb", "source_id": fake.SerieFixtureID}, http.StatusOK)

	// (3) The tree was born (2 seasons) and the 2 files that WERE ALREADY in the
	// series — with no new scan at all — got linked to the right episodes by NUMBER:
	// ep 101 (S01E01) and ep 102 (S01E02); ep 103 (S01E03) still has no file at all.
	temporadas := c.idsDosMembros(serieID)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas apos corrigir = %d, quero 2 (arvore sincronizada)", len(temporadas))
	}
	epsT1 := c.idsDosMembros(temporadas[0])
	if len(epsT1) != 3 {
		t.Fatalf("episodios T1 = %d, quero 3", len(epsT1))
	}
	assertFileCount(t, c, epsT1[0], 1) // S01E01 -> ep 101, re-matched with no new scan
	assertFileCount(t, c, epsT1[1], 1) // S01E02 -> ep 102, re-matched with no new scan
	assertFileCount(t, c, epsT1[2], 0) // S01E03: no file covered that number
	assertPosse(t, c, temporadas[0], 2, 3)
}

// acharSerieEmRevisao returns the id of the (only) tv series in the review queue (GET
// /api/v1/review) — the draft the scanner created because it could not resolve the title.
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
