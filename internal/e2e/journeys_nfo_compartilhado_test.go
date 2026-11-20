package e2e

// journeys_nfo_compartilhado_test.go covers the ONE .nfo per video FILE end to end
// (issue #80): a file that holds two episodes (the Kodi/Jellyfin multi-episode
// convention, "Show S01E01E02.mkv") is scanned, the two episodes are linked to it, and
// the job writes ONE document with one <episodedetails> block per episode -- then
// reports "nothing to do" on the second run, which is what the old per-entity writer
// could never do (the siblings overwrote each other and every run rewrote the file).

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// TestJornadaNFOCompartilhado proves the document of a multi-episode file, through the
// queue: scan -> job -> two blocks -> second run with no write.
func TestJornadaNFOCompartilhado(t *testing.T) {
	c := novoHarnessComOpcoes(t, server.WithMidiasFaltantesJob())

	root := t.TempDir()
	pasta := filepath.Join(root, "Springfield Tales", "Season 01")
	video := filepath.Join(pasta, "Springfield Tales S01E01E02.mkv")
	escreverVideo(t, video)

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "tv", "content_type": "series"}, http.StatusCreated)
	c.esperarScan(str(t, src, "id"))

	// (1) The job runs through the queue and finishes clean.
	if status := c.esperarJob("midias_faltantes"); status != "success" {
		t.Fatalf("status do job = %q, quero success", status)
	}

	// (2) ONE .nfo, named after the video, with one block per episode IN FILE ORDER.
	nfo := strings.TrimSuffix(video, filepath.Ext(video)) + ".nfo"
	corpo, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatalf("NFO do arquivo ausente em %s: %v", nfo, err)
	}
	texto := string(corpo)
	if blocos := strings.Count(texto, "<episodedetails>"); blocos != 2 {
		t.Fatalf("blocos <episodedetails> = %d, quero 2 (um por episodio do arquivo):\n%s", blocos, texto)
	}
	if cabecalhos := strings.Count(texto, "<?xml"); cabecalhos != 1 {
		t.Errorf("cabecalhos XML = %d, quero 1", cabecalhos)
	}
	for _, quero := range []string{"<episode>1</episode>", "<episode>2</episode>", "<season>1</season>"} {
		if !strings.Contains(texto, quero) {
			t.Errorf("NFO nao contem %q:\n%s", quero, texto)
		}
	}

	// (3) The queue item of the run reports the NFO it wrote, and a second run writes
	// NONE: the document of the file converged (the old writer never converged, so the
	// same files were rewritten on every single run).
	c.esperarJob("midias_faltantes")
	exec := c.obj(http.MethodGet, "/api/v1/jobs/midias_faltantes/executions?limit=1", nil, http.StatusOK)
	itens, _ := exec["items"].([]any)
	if len(itens) == 0 {
		t.Fatal("nenhuma execucao listada depois do segundo run")
	}
	ultima, _ := itens[0].(map[string]any)
	mensagem, _ := ultima["message"].(string)
	if !strings.Contains(mensagem, "0 NFO") {
		t.Errorf("message do 2o run = %q, quero 0 NFO (documento do arquivo ja converge)", mensagem)
	}
	depois, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatalf("reler o NFO: %v", err)
	}
	if string(depois) != texto {
		t.Error("o NFO mudou no 2o run -- a convergencia do documento compartilhado falhou")
	}
}
