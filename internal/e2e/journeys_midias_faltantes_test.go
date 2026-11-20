package e2e

// journeys_midias_faltantes_test.go covers the `midias_faltantes` JOB end to end, through
// the queue, and not only its handler body: until now every journey reached the
// materialization through the manual `POST /entities/{id}/materialize-missing-media`, so
// the job itself (internal/job/midiasfaltantes) had no journey coverage at all.
//
// It also covers what issue #69 added: the run NARRATES itself to the queue item (the
// step, the total announced before the loop, the item counter and one trail row per
// entity), and the counters are the only way the owner has to read a run that lasts hours.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// TestJornadaJobMidiasFaltantes proves the job writes what is missing, reports what it did
// and then reports "nothing to do" on a second run -- the three things the nightly
// execution depends on.
func TestJornadaJobMidiasFaltantes(t *testing.T) {
	c := novoHarnessComOpcoes(t, server.WithMidiasFaltantesJob())

	// A movie with a real file in an exclusive folder: that is what makes the entity
	// eligible (a present media_file) and gives the job a destination to write into.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")
	root := t.TempDir()
	escreverVideo(t, filepath.Join(root, "Matrix (1999) [tmdbid-603].mkv"))
	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "movie"}, http.StatusCreated)
	c.esperarScan(str(t, src, "id"))

	// Artwork in the internal cache, not on disk: exactly the state the job exists for.
	c.uploadAsset(id, "poster", pngFixture)

	// (1) The job runs through the queue and finishes clean.
	if status := c.esperarJob("midias_faltantes"); status != "success" {
		t.Fatalf("status do job = %q, quero success", status)
	}
	exec := c.obj(http.MethodGet, "/api/v1/jobs/midias_faltantes/executions?limit=1", nil, http.StatusOK)
	itens, _ := exec["items"].([]any)
	if len(itens) == 0 {
		t.Fatal("nenhuma execucao listada depois do run")
	}
	primeira, _ := itens[0].(map[string]any)
	mensagem, _ := primeira["message"].(string)
	if !strings.Contains(mensagem, "1 entity") {
		t.Errorf("message da execucao = %q, quero o resumo com 1 entity elegivel", mensagem)
	}
	if !strings.Contains(mensagem, "1 arte") {
		t.Errorf("message da execucao = %q, quero a contagem de artes materializadas", mensagem)
	}

	// The poster actually landed next to the video, with the bare name of an exclusive
	// folder (the same rule the manual path uses -- one implementation, two callers).
	poster := filepath.Join(root, "poster.png")
	if _, err := os.Stat(poster); err != nil {
		t.Errorf("poster nao foi materializado em %s: %v", poster, err)
	}
	// And so did the NFO, named after the video.
	nfo := filepath.Join(root, "Matrix (1999) [tmdbid-603].nfo")
	if _, err := os.Stat(nfo); err != nil {
		t.Fatalf("NFO nao foi gravado em %s: %v", nfo, err)
	}

	// (2) The NARRATION (issue #69): the queue item of that execution knows the step, the
	// total announced before the loop, and the trail row of the entity it handled.
	fila := c.obj(http.MethodGet, "/api/v1/queue?recent=5", nil, http.StatusOK)
	recentes, _ := fila["recent"].([]any)
	var itemID string
	for _, r := range recentes {
		item, _ := r.(map[string]any)
		if kind, _ := item["kind"].(string); kind != "job" {
			continue
		}
		if ref, _ := item["ref_id"].(string); ref != "midias_faltantes" {
			continue
		}
		if st, _ := item["status"].(string); st != "success" {
			continue
		}
		itemID, _ = item["id"].(string)
		if total, _ := item["total"].(float64); total != 1 {
			t.Errorf("total narrado = %v, quero 1 (o universo e conhecido antes do loop)", item["total"])
		}
		if done, _ := item["done"].(float64); done != 1 {
			t.Errorf("done narrado = %v, quero 1", item["done"])
		}
		if step, _ := item["step_label"].(string); step == "" {
			t.Error("step_label vazio: o job deveria anunciar a etapa")
		}
	}
	if itemID == "" {
		t.Fatal("o run do job nao apareceu em GET /queue?recent")
	}
	detalhe := c.obj(http.MethodGet, "/api/v1/queue/"+itemID+"?events=50", nil, http.StatusOK)
	eventos, _ := detalhe["events"].([]any)
	if len(eventos) == 0 {
		t.Fatal("trilha vazia: o job deveria registrar um desfecho por entity")
	}
	if ev, _ := eventos[0].(map[string]any); ev["status"] == nil {
		t.Errorf("evento da trilha sem status: %v", ev)
	}

	// (3) A SECOND run is a no-op: everything is on disk and the NFO is convergent. This
	// is the answer the nightly job gives for almost every entity of a real library.
	if status := c.esperarJob("midias_faltantes"); status != "success" {
		t.Fatalf("status do 2o run = %q, quero success", status)
	}
	exec2 := c.obj(http.MethodGet, "/api/v1/jobs/midias_faltantes/executions?limit=1", nil, http.StatusOK)
	itens2, _ := exec2["items"].([]any)
	segunda, _ := itens2[0].(map[string]any)
	mensagem2, _ := segunda["message"].(string)
	if !strings.Contains(mensagem2, "0 arte") {
		t.Errorf("message da 2a execucao = %q, quero 0 arte (nada faltando)", mensagem2)
	}
	if !strings.Contains(mensagem2, "0 NFO") {
		t.Errorf("message da 2a execucao = %q, quero 0 NFO (convergente, nao regrava)", mensagem2)
	}
}
