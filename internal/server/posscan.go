package server

// posscan.go implements the POST-SCAN per library (ADR-0031):
// the hook the scanner calls at the end of the scan of ONE source, after all
// batches are committed. It exists because the match pipeline runs a single time per
// file: whatever failed on the spot (network, the artist tree that did not exist yet,
// a series import that arrived after the episodes) stayed waiting for the cron of the
// saude_biblioteca job -- 15 items per type, 15-day throttle.
//
// There is NO new matching logic here: each item reuses the existing path
// (scanner.ReconciliarFaixaDraft / scanner.ReconciliarSerieDraft /
// Server.TentarAutoMatchFilme). What this file adds is (1) the slice per
// library_source, (2) the absence of the day throttle (it is the scan retry the
// owner just asked for) and (3) the optional materialization of whoever gained an
// identity, when the auto-download toggle is on.
//
// Best-effort from start to finish: a database error in the LISTING propagates (the scanner only
// logs it); an error in an individual item is absorbed and logged -- a bad item never
// aborts the reconciliation of the others.

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// posScanBudget is the WALL-clock ceiling of the post-scan of ONE scan.
// Each track attempt costs external calls at 1 req/s; without a ceiling, a
// scan with 200 tracks in draft blocked the scan queue for 45+ min (measured
// in production). It blew up: it stops where it is and leaves the rest for the next scan --
// the queries already sort by "never tried first", so the queue advances.
const posScanBudget = 4 * time.Minute

// esgotou says whether the post-scan time budget is over (see posScanBudget).
func esgotou(limite time.Time) bool { return time.Now().After(limite) }

// ProcessarPosScan satisfies scanner.PosScan: it reconciles the
// drafts and retries the pending items OF THE SOURCE `sourceID`. It returns an error only on a real
// READ failure (database/config); the rest is best-effort.
func (s *Server) ProcessarPosScan(ctx context.Context, sourceID string) error {
	// Without a scanner or without a provider/importer there is nothing to reconcile: the two
	// reconcilers and auto-match degrade on their own, but we avoid the whole database
	// sweep in that case.
	if s.scanner == nil || sourceID == "" {
		return nil
	}
	langCfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return err
	}

	limite := time.Now().Add(posScanBudget)

	faixas, err := s.leitura().ListarFaixasDraftOrfasPorSource(ctx, sqlcgen.ListarFaixasDraftOrfasPorSourceParams{
		SourceID:     sql.NullString{String: sourceID, Valid: true},
		HorasMinimas: scanner.PosScanHorasMinimas,
		Limite:       scanner.CapReconciliacaoFaixas,
	})
	if err != nil {
		return err
	}
	albumMaterializar := make(map[string]bool)
	faixasOk := 0
	for _, row := range faixas {
		if ctx.Err() != nil || esgotou(limite) {
			break
		}
		if !s.reconciliarECarimbar(ctx, row.ID, langCfg.Allowed, "faixa", s.scanner.ReconciliarFaixaDraft) {
			continue
		}
		faixasOk++
		// The cover lives in the ALBUM (ADR-0027) -- it materializes the parent of the track
		// just linked, once per album in this pass.
		for _, albumID := range s.albumsDaEntity(ctx, row.ID) {
			if albumMaterializar[albumID] {
				continue
			}
			albumMaterializar[albumID] = true
			if merr := s.MaterializarSeAutoDownloadLigado(ctx, albumID); merr != nil {
				log.Printf("pos-scan: materialização do álbum %q falhou (best-effort, ignorado): %v", albumID, merr)
			}
		}
	}

	series, err := s.leitura().ListarSeriesDraftPorSource(ctx, sqlcgen.ListarSeriesDraftPorSourceParams{
		SourceID:     sql.NullString{String: sourceID, Valid: true},
		HorasMinimas: scanner.PosScanHorasMinimas,
		Limite:       scanner.CapReconciliacaoSeries,
	})
	if err != nil {
		return err
	}
	seriesOk := 0
	for _, row := range series {
		if ctx.Err() != nil || esgotou(limite) {
			break
		}
		if s.reconciliarECarimbar(ctx, row.ID, langCfg.Allowed, "série", s.scanner.ReconciliarSerieDraft) {
			seriesOk++
		}
	}

	filmes, err := s.leitura().ListarFilmesPendentesPorSource(ctx, sqlcgen.ListarFilmesPendentesPorSourceParams{
		SourceID: sql.NullString{String: sourceID, Valid: true},
		Limite:   scanner.CapAutoMatchPendentes,
	})
	if err != nil {
		return err
	}
	filmesOk := 0
	for _, row := range filmes {
		if ctx.Err() != nil || esgotou(limite) {
			break
		}
		if s.autoMatchPendentePosScan(ctx, row.ID) {
			filmesOk++
		}
	}

	// Stock purge: the drafts that lost the file to the
	// definitive entity stayed forever in the review queue as "no file". Idempotent --
	// 0 when there is nothing to clean.
	purgados := int64(0)
	if n, err := sqlcgen.New(s.db.Write).ApagarRascunhosVazios(ctx); err != nil {
		log.Printf("pos-scan: purga de rascunhos vazios falhou (best-effort, ignorado): %v", err)
	} else {
		purgados = n
	}

	// Match alerts (ADR-0038): series that HAVE an identity but whose tree does not fit the
	// files. Deliberately LAST and per LIBRARY-SCAN, not per series: it is one query over the
	// whole catalog (measured: 10 series in a production library with 480k relations, and
	// loosening the cut would pull in series whose extra files are legitimate specials).
	// Best-effort -- a failure here never aborts the post-scan.
	alertas := s.sinalizarArvoresIncompativeis(ctx)

	if faixasOk+seriesOk+filmesOk > 0 || purgados > 0 || alertas > 0 {
		log.Printf("pos-scan: biblioteca %q -- %d faixa(s), %d série(s), %d filme(s) resolvidos depois do scan, %d rascunho(s) vazio(s) purgado(s), %d alerta(s) de match",
			sourceID, faixasOk, seriesOk, filmesOk, purgados, alertas)
	}
	return nil
}

// sinalizarArvoresIncompativeis writes the 'tree_mismatch' alert of every series whose files
// mostly did not fit the tree. It returns how many alerts it wrote (for the summary line).
//
// WHY the signal comes from the CENSUS and not from the scan loop: the scanner knows how
// many files it placed, but not whether the series is "mostly unmatched" -- the ratio needs
// the whole tree of the series, which the database already has after the scan. Reading it
// here also covers series scanned by an EARLIER run, so an existing library gets the alert
// without waiting for a re-scan.
func (s *Server) sinalizarArvoresIncompativeis(ctx context.Context) int {
	if s.enrich == nil {
		return 0
	}
	linhas, err := s.leitura().ListarSeriesComArvoreIncompativel(ctx)
	if err != nil {
		log.Printf("pos-scan: listar series com arvore incompativel falhou (best-effort, ignorado): %v", err)
		return 0
	}
	escritos := 0
	for _, row := range linhas {
		if ctx.Err() != nil {
			break
		}
		s.enrich.SinalizarArvoreIncompativel(ctx, row.EntityID, int(row.Provisional), int(row.Cataloged))
		escritos++
	}
	return escritos
}

// autoMatchPendentePosScan tries to match+enrich ONE pending movie and, on a match,
// materializes the artwork when the toggle is on -- the same pair the scan uses
// for a new item (dispararAutoMatchFilme + MaterializarSeAutoDownloadLigado),
// now for what was left behind.
func (s *Server) autoMatchPendentePosScan(ctx context.Context, entityID string) bool {
	matched, err := s.TentarAutoMatchFilme(ctx, entityID)
	if err != nil {
		log.Printf("pos-scan: auto-match do filme %q falhou (best-effort, ignorado): %v", entityID, err)
		return false
	}
	if !matched {
		return false
	}
	if merr := s.MaterializarSeAutoDownloadLigado(ctx, entityID); merr != nil {
		log.Printf("pos-scan: materialização do filme %q falhou (best-effort, ignorado): %v", entityID, merr)
	}
	return true
}

// albumsDaEntity returns the parents (albums) of a track -- a simple read of
// the relations tree; empty when there is no link or the database fails (the
// materialization is optional, it is never worth aborting because of it).
func (s *Server) albumsDaEntity(ctx context.Context, entityID string) []string {
	rels, err := s.leitura().ListarRelacoesPais(ctx, entityID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	ids := make([]string, 0, len(rels))
	for _, rel := range rels {
		if rel.ParentID != "" {
			ids = append(ids, rel.ParentID)
		}
	}
	return ids
}
