package e2e

// journeys_reconciliar_test.go covers the reconciliation of orphan music/series
// children: a series that got stuck as a DRAFT because the match by name failed at
// scan time (files stuck directly on the draft entity, with no episode link) is
// resolved ON ITS OWN by the `saude_biblioteca` job, without the owner having to
// click anything -- unlike TestJornadaCorrigirSerieRecasaArquivosPendentes
// (journeys_recasar_test.go), which proves the SAME situation resolved manually
// through POST /entities/{id}/correct.
//
// Challenge (the same static fake.Provider catalog as the other E2E tests): a name
// the fake recognizes right away (the exact title of a fixture, e.g. "Springfield
// Tales") would resolve on the FIRST scan -- it would never become a draft; a name
// it does NOT recognize (e.g. "Mystery Show", used in the re-match test) never
// resolves, not even in the reconciliation (same catalog). To prove SUCCESS end to
// end, the wrapper below makes the fake.Provider FAIL on the THREE first Buscar
// calls (tipo="tv", nome=fixture), one for each step that runs BEFORE the
// reconciliation itself (see libraryhealth.Handler.Executar):
//
//  1. the manual scan (esperarScan, through ImportarSeriePorNome);
//  2. the internal RE-SCAN that the saude_biblioteca job itself fires for EVERY
//     registered library_source (escanearTodas, also through ImportarSeriePorNome);
//  3. the AUTO-MATCH the job runs next: it derives the term from the series FOLDER
//     (clean title, score 100) instead of the episode file name, so it resolves the
//     draft IN PLACE (mesclarBuscas) and the reconciliation would never have
//     anything to do.
//
// Only from the FOURTH call onward (the reconciliation one, tentarImportarSerie with
// the entity title) does the wrapper let it through to the real fake.

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/server"
)

// serieProviderFalhaDuasVezes embeds *fake.Provider (inheriting BuscarSerieArvore/
// BuscarDetalhe/MapearDetalhe/Nome through method promotion) and overrides only
// Buscar: the THREE first calls with tipo="tv" and nome==nomeAlvo return "no
// candidates" (simulating a transient match failure); from the fourth call onward
// for that name/type -- and ALWAYS for any other name/type -- it falls back to the
// real fake. See the top comment of the file about the reason for needing 3
// failures (scan, re-scan and the auto-match of the job), not 1.
type serieProviderFalhaDuasVezes struct {
	*fake.Provider
	nomeAlvo string
	chamadas int
}

func (p *serieProviderFalhaDuasVezes) Buscar(ctx context.Context, query, tipo, lang string) ([]provider.ResultadoExterno, error) {
	if tipo == "tv" && strings.EqualFold(query, p.nomeAlvo) {
		p.chamadas++
		if p.chamadas <= 3 {
			return nil, nil
		}
	}
	return p.Provider.Buscar(ctx, query, tipo, lang)
}

// esperarJob fires POST /jobs/{id}/run (202) and polls
// GET /jobs/{id}/executions?limit=1 until the most recent execution leaves
// queued/running. It returns the final status ("success"/"error"). Same
// spirit/deadline as esperarScan (journeys_test.go).
func (c *cliente) esperarJob(jobID string) string {
	c.t.Helper()
	c.obj(http.MethodPost, "/api/v1/jobs/"+jobID+"/run", nil, http.StatusAccepted)
	prazo := time.Now().Add(30 * time.Second)
	for time.Now().Before(prazo) {
		resp := c.obj(http.MethodGet, "/api/v1/jobs/"+jobID+"/executions?limit=1", nil, http.StatusOK)
		itens, _ := resp["items"].([]any)
		if len(itens) > 0 {
			exec, _ := itens[0].(map[string]any)
			status, _ := exec["status"].(string)
			if status != "" && status != "queued" && status != "running" {
				return status
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	c.t.Fatalf("job %s nao concluiu no prazo", jobID)
	return ""
}

// TestJornadaReconciliacaoAutomaticaSerieDraft proves the automatic reconciliation
// end to end: a series folder with the EXACT fixture title of the fake ("Springfield
// Tales", fake.SerieFixtureID) scanned when the provider still "does not know how to
// resolve" it (the 2 first attempts fail, see the wrapper above) gets stuck as a
// draft -- and the `saude_biblioteca` job resolves it on its own, with no POST
// /correct at all.
func TestJornadaReconciliacaoAutomaticaSerieDraft(t *testing.T) {
	fakeReal := fake.New("")
	wrapper := &serieProviderFalhaDuasVezes{Provider: fakeReal, nomeAlvo: "Springfield Tales"}
	c := novoHarnessComOpcoes(t, server.WithLibraryHealthJob(), server.WithSearchProvider(wrapper))

	root := t.TempDir()
	base := filepath.Join(root, "Springfield Tales")
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E01.mkv"))
	escreverVideo(t, filepath.Join(base, "Season 01", "Springfield Tales S01E02.mkv"))

	src := c.obj(http.MethodPost, "/api/v1/library",
		map[string]string{"path": root, "type": "tv", "content_type": "series"}, http.StatusCreated)
	srcID := str(t, src, "id")
	c.esperarScan(srcID)

	// (1) The 1st Buscar call (inside the manual scan) fails on purpose: the series
	// falls into the review queue, a draft with the 2 files stuck directly on it, with
	// no episode link (the same initial state as the re-match test).
	serieID := acharSerieEmRevisao(t, c)
	if arqs := c.arquivosDaEntity(serieID); len(arqs) != 2 {
		t.Fatalf("arquivos presos na serie rascunho = %d, quero 2 (S01E01/S01E02 sem vinculo)", len(arqs))
	}

	// (2) Fires the saude_biblioteca job: internally it re-scans the registered
	// source (2nd Buscar call, still failing) and, in the orphan children
	// reconciliation step (after the re-scan/auto-match/retroactive enrichment),
	// tries again (3rd call, now succeeding) through Manager.ReconciliarSerieDraft --
	// without the owner clicking anything.
	status := c.esperarJob("saude_biblioteca")
	if status != "success" {
		t.Fatalf("status da execucao do job saude_biblioteca = %q, quero \"success\"", status)
	}

	// (3) The resolved series is found through the same INTERNAL-FIRST path as
	// always: POST /import with the SAME source/source_id is idempotent
	// (find-or-create) and returns 200 with the entity that holds the identity.
	// The id may be the original draft's: ImportarSeriePorNome does find-or-create BY
	// NAME, so when the draft title already matches the name being looked up the
	// existing entity is reused (promoted in place) instead of creating a second
	// "Springfield Tales". What this test enforces is the RESULT (identity applied,
	// tree created, files relinked to the episodes) -- not through which of the two
	// ids the link went.
	resolvida := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": fake.SerieFixtureID, "type": "tv"}, http.StatusOK)
	serieResolvidaID := str(t, resolvida, "id")

	// (3b) The 2 files are on the resolved series and none was left stuck on an
	// entity that is not it (the draft, when not reused, ends up with no media_file
	// at all -- orphan, not deleted).
	if arqs := c.arquivosDaEntity(serieResolvidaID); len(arqs) != 2 {
		t.Fatalf("arquivos na serie resolvida = %d, quero 2 (repontados pela reconciliacao)", len(arqs))
	}
	if serieResolvidaID != serieID {
		if arqs := c.arquivosDaEntity(serieID); len(arqs) != 0 {
			t.Fatalf("arquivos ainda presos no rascunho original = %d, quero 0 (repontados pela reconciliacao)", len(arqs))
		}
	}

	// (3c) The same assertions as TestJornadaCorrigirSerieRecasaArquivosPendentes:
	// the tree was born (2 seasons) and the 2 files that were already stuck got
	// linked to the right episodes by NUMBER -- only getting there through the
	// automatic job, not through /correct.
	temporadas := c.idsDosMembros(serieResolvidaID)
	if len(temporadas) != 2 {
		t.Fatalf("temporadas apos reconciliar = %d, quero 2 (arvore sincronizada)", len(temporadas))
	}
	epsT1 := c.idsDosMembros(temporadas[0])
	if len(epsT1) != 3 {
		t.Fatalf("episodios T1 = %d, quero 3", len(epsT1))
	}
	assertFileCount(t, c, epsT1[0], 1) // S01E01 -> ep 101, relinked by the reconciliation
	assertFileCount(t, c, epsT1[1], 1) // S01E02 -> ep 102, relinked by the reconciliation
	assertFileCount(t, c, epsT1[2], 0) // S01E03: no file covered that number
	assertPosse(t, c, temporadas[0], 2, 3)

	// (4) Finding from running this journey: UNLIKE /correct (which resolves the
	// SAME entity in place and therefore makes it disappear from /review), the
	// reconciliation leaves the ORIGINAL draft (now an orphan, with no media_file) in
	// the review queue forever -- ListarRevisao (review.sql) lists EVERY
	// status='draft' unconditionally, without looking at file_count; and
	// ReconciliarSerieDraft never touches the status/existence of the old entity (it
	// only repoints media_file.entity_id -- non-destructive, principle 5, but it also
	// does not promote/delete the empty draft). What the reconciliation GUARANTEES --
	// and what this assertion proves -- is that NO tv series in the review queue still
	// has a stuck file (owned=true): the real problem (a file glued to the wrong
	// entity, with no episode link) is indeed solved; the remaining draft is just an
	// empty record, waiting for the owner to delete/merge it manually if they want to.
	fila := c.arr(http.MethodGet, "/api/v1/review", nil, http.StatusOK)
	for _, item := range fila {
		m, _ := item.(map[string]any)
		if str(t, m, "type") != "tv" {
			continue
		}
		if owned, _ := m["owned"].(bool); owned {
			t.Fatalf("serie tv na fila de revisao ainda com arquivo preso (owned=true) apos a reconciliacao automatica: %v", m)
		}
	}
}
