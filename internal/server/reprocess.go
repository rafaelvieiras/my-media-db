package server

// reprocess.go implements the owner-initiated REPROCESS of one library
// (POST /api/v1/library/{id}/reprocess, GET for the progress).
//
// Why it exists: the post-scan hook (ADR-0031, posscan.go) already reconciles the
// drafts and purges the empty ones, but it runs inside a 4-minute budget and behind
// an hour throttle, because it lives at the end of a scan and must not hold the
// queue. That is right for a routine scan and wrong for the owner who just fixed
// the parser (or a provider) and wants the EXISTING catalog corrected now: with a
// library holding hundreds of drafts, each scan advances a slice and there is no
// "resolve it all" button.
//
// So this is the same reconciliation, with the opposite trade-off: no budget, no
// throttle, real progress, and a MANDATORY dry-run (dry_run defaults to true) that
// answers what would change before anything is written.
//
// Two modes:
//   - fast: only the catalog already in the database. No disk walk, no ffprobe.
//     The drafts are retried against the provider and the empty drafts are purged.
//   - full: enqueues the normal scan first -- which re-parses every name and
//     re-links every file (gravarArquivoSerie removes and recreates the episode
//     links on each pass, so a parser fix lands on the existing rows) -- and then
//     runs the same unbounded reconciliation.
//
// Nothing here deletes media. The only deletion is the purge of DRAFT entities that
// have no file, no relation and no owner override: the leftovers of a re-link.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

const (
	reprocessModeFast = "fast"
	reprocessModeFull = "full"
	// reprocessLimite is the ceiling of each listing. It exists only to bound a
	// pathological library; the throttle is ZERO (everything is eligible) because
	// the owner asked for it now.
	reprocessLimite = 100000
	// reprocessPollScan is how often the full mode checks whether the scan it
	// enqueued has finished.
	reprocessPollScan = 3 * time.Second
)

// reprocessRequest is the body of POST /library/{id}/reprocess. Every field is
// optional: the defaults are the SAFE ones (fast, dry_run, delete_orphans).
type reprocessRequest struct {
	Mode          string `json:"mode"`
	DryRun        *bool  `json:"dry_run"`
	DeleteOrphans *bool  `json:"delete_orphans"`
}

// reprocessReport is both the dry-run answer and the result of an applied run.
// Promoted counts the drafts that gained an external identity in this pass;
// OrphansDeleted counts the empty drafts removed by the purge.
type reprocessReport struct {
	Mode          string `json:"mode"`
	DryRun        bool   `json:"dry_run"`
	SeriesDraft   int64  `json:"series_draft"`
	MoviesPending int64  `json:"movies_pending"`
	DraftEpisodes int64  `json:"draft_episodes"`
	// DraftEpisodesAfter is the same count measured at the END of an applied run.
	// It exists because "promoted" alone lies: promoting a show to a provider whose
	// tree numbers the episodes differently turns MATCHED episodes into provisional
	// ones, and the owner has to see that in the report (measured: +302 on one
	// library after 11 promotions).
	DraftEpisodesAfter int64 `json:"draft_episodes_after,omitempty"`
	Orphans            int64 `json:"orphans"`
	Promoted           int64 `json:"promoted"`
	OrphansDeleted     int64 `json:"orphans_deleted"`
	ScanQueued         bool  `json:"scan_queued,omitempty"`
}

// reprocessState is the progress of one source. Guarded by Server.reprocessMu.
type reprocessState struct {
	Running   bool             `json:"running"`
	Mode      string           `json:"mode"`
	Phase     string           `json:"phase"` // scan | reconcile | purge | done
	Done      int64            `json:"done"`
	Total     int64            `json:"total"`
	Report    *reprocessReport `json:"report,omitempty"`
	LastError string           `json:"last_error,omitempty"`
	StartedAt time.Time        `json:"started_at"`
	Finished  bool             `json:"finished"`
}

// reprocessStatus is the response of GET /library/{id}/reprocess.
type reprocessStatus struct {
	Running   bool             `json:"running"`
	Mode      string           `json:"mode,omitempty"`
	Phase     string           `json:"phase,omitempty"`
	Done      int64            `json:"done"`
	Total     int64            `json:"total"`
	Report    *reprocessReport `json:"report,omitempty"`
	LastError string           `json:"last_error,omitempty"`
}

func (s *Server) estadoReprocess(sourceID string) (reprocessState, bool) {
	s.reprocessMu.Lock()
	defer s.reprocessMu.Unlock()
	st, ok := s.reprocess[sourceID]
	if !ok || st == nil {
		return reprocessState{}, false
	}
	return *st, true
}

func (s *Server) gravarEstadoReprocess(sourceID string, st reprocessState) {
	s.reprocessMu.Lock()
	defer s.reprocessMu.Unlock()
	if s.reprocess == nil {
		s.reprocess = make(map[string]*reprocessState)
	}
	copia := st
	s.reprocess[sourceID] = &copia
}

// startReprocess handles POST /api/v1/library/{id}/reprocess.
//
// Sequence: validate the source, refuse when a scan or another reprocess of the
// same source is running (they would fight over the same rows), then either answer
// the dry-run or start the background run.
func (s *Server) startReprocess(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	if s.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner_unavailable",
			"scanner indisponível nesta instância")
		return
	}
	id := chi.URLParam(r, "id")
	src, err := s.library.Obter(r.Context(), id)
	if err != nil {
		respondLibraryError(w, err)
		return
	}

	var req reprocessRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_body",
			"corpo da requisição inválido: "+err.Error())
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = reprocessModeFast
	}
	if mode != reprocessModeFast && mode != reprocessModeFull {
		writeError(w, http.StatusUnprocessableEntity, "invalid_mode",
			"mode inválido: use fast ou full")
		return
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	deleteOrphans := true
	if req.DeleteOrphans != nil {
		deleteOrphans = *req.DeleteOrphans
	}

	if st := s.scanner.Status(id); st.Running {
		writeError(w, http.StatusConflict, "scan_in_progress",
			"há um scan em andamento nesta biblioteca; espere ele terminar")
		return
	}
	if st, ok := s.estadoReprocess(id); ok && st.Running {
		writeError(w, http.StatusConflict, "reprocess_in_progress",
			"já existe um reprocessamento em andamento nesta biblioteca")
		return
	}

	relatorio, err := s.analisarReprocess(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error",
			"erro ao analisar a biblioteca: "+err.Error())
		return
	}
	relatorio.Mode = mode
	relatorio.DryRun = dryRun

	if dryRun {
		// Dry-run is the DEFAULT and the contract: no write, no provider call.
		writeJSON(w, http.StatusOK, relatorio)
		return
	}

	if mode == reprocessModeFull {
		relatorio.ScanQueued = true
	}
	st := reprocessState{
		Running: true, Mode: mode, Phase: phaseInicial(mode),
		Total:  relatorio.SeriesDraft + relatorio.MoviesPending,
		Report: relatorio, StartedAt: time.Now().UTC(),
	}
	s.gravarEstadoReprocess(id, st)

	go s.executarReprocess(context.WithoutCancel(r.Context()), src, mode, deleteOrphans, relatorio)

	writeJSON(w, http.StatusAccepted, startScanResponse{JobID: id, Status: "accepted"})
}

func phaseInicial(mode string) string {
	if mode == reprocessModeFull {
		return "scan"
	}
	return "reconcile"
}

// getReprocess handles GET /api/v1/library/{id}/reprocess: the progress, or the
// report of the last run when it already finished.
func (s *Server) getReprocess(w http.ResponseWriter, r *http.Request) {
	if s.libraryUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := s.library.Obter(r.Context(), id); err != nil {
		respondLibraryError(w, err)
		return
	}
	st, ok := s.estadoReprocess(id)
	if !ok {
		writeJSON(w, http.StatusOK, reprocessStatus{Running: false})
		return
	}
	writeJSON(w, http.StatusOK, reprocessStatus{
		Running: st.Running, Mode: st.Mode, Phase: st.Phase,
		Done: st.Done, Total: st.Total, Report: st.Report, LastError: st.LastError,
	})
}

// analisarReprocess counts what the reprocess would touch. It reads only; it never
// writes and never calls a provider, which is what makes dry-run honest.
func (s *Server) analisarReprocess(ctx context.Context, sourceID string) (*reprocessReport, error) {
	q := s.leitura()
	source := sql.NullString{String: sourceID, Valid: true}

	series, err := q.ListarSeriesDraftPorSource(ctx, sqlcgen.ListarSeriesDraftPorSourceParams{
		SourceID:     source,
		HorasMinimas: 0,
		Limite:       reprocessLimite,
	})
	if err != nil {
		return nil, fmt.Errorf("listar séries em draft: %w", err)
	}
	filmes, err := q.ListarFilmesPendentesPorSource(ctx, sqlcgen.ListarFilmesPendentesPorSourceParams{
		SourceID: source,
		Limite:   reprocessLimite,
	})
	if err != nil {
		return nil, fmt.Errorf("listar filmes pendentes: %w", err)
	}
	// Per source, with an indexed join: the review listing (ListarRevisao) is the
	// obvious source of this number and the wrong one here — its five correlated
	// subqueries per entity cost ~60 s on this catalog, which is fine for the screen
	// that asks once and absurd for a preview.
	direto, err := q.ContarEpisodiosDraftDaFonteDireto(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("contar episódios em draft (direto): %w", err)
	}
	viaTemporada, err := q.ContarEpisodiosDraftDaFonteViaTemporada(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("contar episódios em draft (temporada): %w", err)
	}
	episodios := direto + viaTemporada
	orfaos, err := q.ContarRascunhosVazios(ctx)
	if err != nil {
		return nil, fmt.Errorf("contar rascunhos vazios: %w", err)
	}
	return &reprocessReport{
		SeriesDraft:   int64(len(series)),
		MoviesPending: int64(len(filmes)),
		DraftEpisodes: episodios,
		Orphans:       orfaos,
	}, nil
}

// executarReprocess is the background run. Mode full first waits for the scan it
// enqueued; both modes then reconcile everything eligible and purge the empties.
func (s *Server) executarReprocess(ctx context.Context, src library.Source, mode string, deleteOrphans bool, relatorio *reprocessReport) {
	id := src.ID
	falhou := func(etapa string, err error) {
		log.Printf("reprocess: %s de %q falhou: %v", etapa, id, err)
		st, ok := s.estadoReprocess(id)
		if !ok {
			return
		}
		st.Running = false
		st.Finished = true
		st.LastError = etapa + ": " + err.Error()
		s.gravarEstadoReprocess(id, st)
	}

	if mode == reprocessModeFull {
		if _, err := s.scanner.Start(src, scanner.TriggerManual); err != nil {
			if !errors.Is(err, scanner.ErrScanEmAndamento) {
				falhou("scan", err)
				return
			}
		}
		for {
			if ctx.Err() != nil {
				falhou("scan", ctx.Err())
				return
			}
			st := s.scanner.Status(id)
			if !st.Running {
				break
			}
			time.Sleep(reprocessPollScan)
		}
	}

	q := s.leitura()
	source := sql.NullString{String: id, Valid: true}

	series, err := q.ListarSeriesDraftPorSource(ctx, sqlcgen.ListarSeriesDraftPorSourceParams{
		SourceID: source, HorasMinimas: 0, Limite: reprocessLimite,
	})
	if err != nil {
		falhou("listar séries em draft", err)
		return
	}
	filmes, err := q.ListarFilmesPendentesPorSource(ctx, sqlcgen.ListarFilmesPendentesPorSourceParams{
		SourceID: source, Limite: reprocessLimite,
	})
	if err != nil {
		falhou("listar filmes pendentes", err)
		return
	}

	total := int64(len(series) + len(filmes))
	var done, promovidos int64
	s.atualizarProgressoReprocess(id, func(st *reprocessState) {
		st.Phase = "reconcile"
		st.Total = total
		st.Done = 0
	})

	// The retry uses tentarAutoMatch -- the SAME path the "Identificar" action of the
	// UI takes (local-first, then the external candidate above LimiarAutoMatch,
	// followed by Enriquecer, which imports the season tree). The conservative
	// reconciler of the post-scan is NOT the right tool here: measured on a real
	// library with 59 series in draft it returned "ambígua no provider" or "não
	// encontrada" for ALL of them -- 0 promoted -- while the auto-match path resolves
	// the same titles with score 100. What the post-scan needs (never touch an item
	// the owner is watching) is not what an owner-initiated action needs.
	tentar := func(entityID string) bool {
		ent, err := s.leitura().ObterEntity(ctx, entityID)
		if err != nil {
			log.Printf("reprocess: entidade %q ilegível (ignorada): %v", entityID, err)
			return false
		}
		res, err := s.tentarAutoMatch(ctx, ent)
		if err != nil {
			log.Printf("reprocess: auto-match de %q falhou (best-effort, ignorado): %v", entityID, err)
			return false
		}
		return res.Resolved
	}
	for _, row := range series {
		if ctx.Err() != nil {
			return
		}
		if tentar(row.ID) {
			promovidos++
		}
		done++
		s.atualizarProgressoReprocess(id, func(st *reprocessState) { st.Done = done })
	}
	for _, row := range filmes {
		if ctx.Err() != nil {
			return
		}
		if tentar(row.ID) {
			promovidos++
		}
		done++
		s.atualizarProgressoReprocess(id, func(st *reprocessState) { st.Done = done })
	}

	var apagados int64
	if deleteOrphans {
		s.atualizarProgressoReprocess(id, func(st *reprocessState) { st.Phase = "purge" })
		n, err := sqlcgen.New(s.db.Write).ApagarRascunhosVazios(ctx)
		if err != nil {
			log.Printf("reprocess: purga de rascunhos vazios falhou (best-effort, ignorado): %v", err)
		} else {
			apagados = n
		}
	}

	depois, errConferir := q.ContarEpisodiosDraftDaFonteViaTemporada(ctx, source)
	if errConferir != nil {
		log.Printf("reprocess: contagem final de episódios em draft falhou (ignorado): %v", errConferir)
	}
	diretoDepois, errDireto := q.ContarEpisodiosDraftDaFonteDireto(ctx, source)
	if errDireto == nil {
		depois += diretoDepois
	}

	final := &reprocessReport{
		Mode: mode, DryRun: false,
		SeriesDraft:        int64(len(series)),
		MoviesPending:      int64(len(filmes)),
		DraftEpisodes:      relatorio.DraftEpisodes,
		Orphans:            relatorio.Orphans,
		Promoted:           promovidos,
		OrphansDeleted:     apagados,
		DraftEpisodesAfter: depois,
		ScanQueued:         mode == reprocessModeFull,
	}
	s.atualizarProgressoReprocess(id, func(st *reprocessState) {
		st.Running = false
		st.Finished = true
		st.Phase = "done"
		st.Done = done
		st.Report = final
	})
	log.Printf("reprocess: %q concluído (mode=%s): %d séries e %d filmes retentados, %d promovidos, %d rascunhos vazios apagados",
		id, mode, len(series), len(filmes), promovidos, apagados)
}

// atualizarProgressoReprocess applies a mutation to the state of a source, keeping
// the mutex in one place.
func (s *Server) atualizarProgressoReprocess(sourceID string, mut func(*reprocessState)) {
	s.reprocessMu.Lock()
	defer s.reprocessMu.Unlock()
	st, ok := s.reprocess[sourceID]
	if !ok || st == nil {
		return
	}
	mut(st)
}
