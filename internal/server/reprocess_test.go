package server

// reprocess_test.go covers the owner-initiated reprocess (reprocess.go): the
// MANDATORY dry-run, the refusal while a scan runs and the applied run that retries
// the drafts and purges the empty ones.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// comParametroID injeta o {id} da rota chi no request (the handlers read it with
// chi.URLParam, so the test must build the same context the router would).
func comParametroID(r *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// semearFonteCriada creates a real datasource through the service, so the handler
// validates it exactly as in production.
func semearFonteCriada(t *testing.T, s *Server, tipo, contentType string) string {
	t.Helper()
	src, err := s.library.Criar(context.Background(), t.TempDir(), tipo, contentType, nil, nil)
	if err != nil {
		t.Fatalf("Criar source: %v", err)
	}
	return src.ID
}

// semearArquivoDaFonte creates a media_file tied to the datasource (the reprocess
// listings filter by library_source_id, which semearFile does not set).
func semearArquivoDaFonte(t *testing.T, d *db.DB, id, entityID, sourceID, path string) {
	t.Helper()
	_, err := escrita(t, d).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID:              id,
		EntityID:        entityID,
		Path:            path,
		State:           "present",
		LibrarySourceID: nsOf(sourceID),
	})
	if err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

func chamarReprocess(t *testing.T, s *Server, sourceID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/library/"+sourceID+"/reprocess", bytes.NewReader(raw))
	r = comParametroID(r, sourceID)
	w := httptest.NewRecorder()
	s.startReprocess(w, r)
	return w
}

// TestReprocessDryRunEhPadraoENaoEscreve: with no body at all the handler answers
// the dry-run, counts the drafts of the source and touches nothing.
func TestReprocessDryRunEhPadraoENaoEscreve(t *testing.T) {
	s, d := setupReconciliarOrfaosServer(t, nil, nil)
	fonte := semearFonteCriada(t, s, "tv", "series")

	semearEntity(t, d, "serie-draft", "tv", "Série Travada")
	semearArquivoDaFonte(t, d, "mf-serie", "serie-draft", fonte, "/media/Series/Série Travada/S01E01.mkv")
	rebaixarParaDraft(t, d, "serie-draft", "tv", "Série Travada")

	// An empty draft: the leftover a purge is expected to remove.
	semearEntity(t, d, "rascunho-vazio", "tv", "Fantasma")
	rebaixarParaDraft(t, d, "rascunho-vazio", "tv", "Fantasma")

	w := chamarReprocess(t, s, fonte, map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo: %s)", w.Code, w.Body.String())
	}
	var rel reprocessReport
	if err := json.Unmarshal(w.Body.Bytes(), &rel); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !rel.DryRun {
		t.Error("dry_run deveria ser o PADRÃO")
	}
	if rel.Mode != reprocessModeFast {
		t.Errorf("mode = %q, quero %q", rel.Mode, reprocessModeFast)
	}
	if rel.SeriesDraft < 1 {
		t.Errorf("series_draft = %d, quero ao menos a série semeada", rel.SeriesDraft)
	}
	if rel.Orphans < 1 {
		t.Errorf("orphans = %d, quero ao menos o rascunho vazio", rel.Orphans)
	}

	// Nothing was written: the draft is still a draft and the orphan still exists.
	if e, err := escrita(t, d).ObterEntity(context.Background(), "serie-draft"); err != nil || e.Status != "draft" {
		t.Errorf("a entidade saiu de draft no dry-run (status=%v, err=%v)", e.Status, err)
	}
	if _, err := escrita(t, d).ObterEntity(context.Background(), "rascunho-vazio"); err != nil {
		t.Errorf("o rascunho vazio foi apagado no dry-run: %v", err)
	}
}

// TestReprocessRecusaEnquantoHaScan: two writers over the same rows is a conflict.
func TestReprocessRecusaEnquantoHaScan(t *testing.T) {
	s, _ := setupReconciliarOrfaosServer(t, nil, nil)
	fonte := semearFonteCriada(t, s, "tv", "series")

	// Simulates the scan in progress by marking the state the handler reads.
	s.gravarEstadoReprocess(fonte, reprocessState{Running: true, Mode: reprocessModeFast})

	w := chamarReprocess(t, s, fonte, map[string]any{"dry_run": false})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, quero 409 (corpo: %s)", w.Code, w.Body.String())
	}
}

// TestReprocessAplicadoPurgaRascunhoVazio runs the real thing: the empty draft is
// purged, and the run reports it. The provider is absent, so the series draft is
// retried and simply stays a draft -- which is the honest expectation here.
func TestReprocessAplicadoPurgaRascunhoVazio(t *testing.T) {
	s, d := setupReconciliarOrfaosServer(t, nil, nil)
	fonte := semearFonteCriada(t, s, "tv", "series")

	semearEntity(t, d, "rascunho-vazio", "tv", "Fantasma")
	rebaixarParaDraft(t, d, "rascunho-vazio", "tv", "Fantasma")

	w := chamarReprocess(t, s, fonte, map[string]any{"dry_run": false})
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, quero 202 (corpo: %s)", w.Code, w.Body.String())
	}

	// The run is a goroutine: wait for the state to settle.
	prazo := time.Now().Add(20 * time.Second)
	var st reprocessState
	for {
		var ok bool
		st, ok = s.estadoReprocess(fonte)
		if ok && !st.Running {
			break
		}
		if time.Now().After(prazo) {
			t.Fatalf("o reprocessamento não terminou: %+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st.LastError != "" {
		t.Fatalf("erro no reprocessamento: %s", st.LastError)
	}
	if st.Report == nil {
		t.Fatal("relatório ausente ao final")
	}
	if st.Report.DryRun {
		t.Error("o relatório aplicado não pode se declarar dry-run")
	}
	if st.Report.OrphansDeleted < 1 {
		t.Errorf("orphans_deleted = %d, quero ao menos 1", st.Report.OrphansDeleted)
	}
	if _, err := escrita(t, d).ObterEntity(context.Background(), "rascunho-vazio"); err == nil {
		t.Error("o rascunho vazio deveria ter sido apagado")
	}
}

// TestReprocessModoInvalido: the mode is a closed enum.
func TestReprocessModoInvalido(t *testing.T) {
	s, _ := setupReconciliarOrfaosServer(t, nil, nil)
	fonte := semearFonteCriada(t, s, "tv", "series")

	w := chamarReprocess(t, s, fonte, map[string]any{"mode": "turbo"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo: %s)", w.Code, w.Body.String())
	}
}
