package server

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the RECONCILIATION OF ORPHAN CHILDREN, a complement to the
// retroactive enrichment: a track/series that got stuck in draft because the textual/
// series match failed at scan time (or the parent tree did not exist yet at that
// moment) is never retried by the scan itself -- the match pipeline runs a SINGLE
// time, per file, at scan time. This step tries again, in a small batch throttled per
// item, reusing Manager.ReconciliarFaixaDraft/Manager.ReconciliarSerieDraft
// (internal/scanner/reconciliar.go, already implemented).

// limiteReconciliacaoOrfaosPorExecucao is the ceiling of items checked PER TYPE
// (track and series, separately) per job run -- same reasoning/value as
// limiteReprocessamentoArtesPorExecucao/
// limiteEnriquecimentoRetroativoPorExecucao: throttling of cost/rate-limit
// in a library with a large backlog on the first time this runs.
const limiteReconciliacaoOrfaosPorExecucao = 15

// ReconciliarFilhosOrfaos sweeps orphan draft tracks (without an entity_relation as
// a child of any album) and draft series with a file, and tries to relink them
// against the already resolved identity tree (or one that came to be resolved
// later). It satisfies libraryhealth.Matcher (called by the saude_biblioteca job,
// after DescobrirArteLocalMusica). err is ONLY a real database error in the
// LISTING; a failure in an individual item is absorbed (best-effort, it logs and
// moves on). reconciliados only counts SUCCESS (linked==true) -- unlike
// ReprocessarArtesFaltantes/EnriquecerRetroativo, which count every attempt:
// the final message of this step has to say how many items actually left the
// review queue, not how many were merely checked.
func (s *Server) ReconciliarFilhosOrfaos(ctx context.Context) (reconciliados int, err error) {
	// Without a scanner, ReconciliarFaixaDraft/ReconciliarSerieDraft would not even
	// exist to call -- same guard as DescobrirArteLocalMusica.
	if s.scanner == nil {
		return 0, nil
	}

	cfgRetry, err := s.obterRetroactiveEnrichmentSettings(ctx)
	if err != nil {
		return 0, err
	}
	langCfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return 0, err
	}

	faixas, err := s.leitura().ListarFaixasDraftOrfas(ctx, sqlcgen.ListarFaixasDraftOrfasParams{
		DiasGlobal: int64(cfgRetry.RetryDays),
		Limite:     limiteReconciliacaoOrfaosPorExecucao,
	})
	if err != nil {
		return 0, err
	}
	for _, row := range faixas {
		if s.reconciliarECarimbar(ctx, row.ID, langCfg.Allowed, "faixa", s.scanner.ReconciliarFaixaDraft) {
			reconciliados++
		}
	}

	series, err := s.leitura().ListarSeriesDraftComArquivo(ctx, sqlcgen.ListarSeriesDraftComArquivoParams{
		DiasGlobal: int64(cfgRetry.RetryDays),
		Limite:     limiteReconciliacaoOrfaosPorExecucao,
	})
	if err != nil {
		return reconciliados, err
	}
	for _, row := range series {
		if s.reconciliarECarimbar(ctx, row.ID, langCfg.Allowed, "série", s.scanner.ReconciliarSerieDraft) {
			reconciliados++
		}
	}

	return reconciliados, nil
}

// reconciliarECarimbar calls `tentar` (Manager.ReconciliarFaixaDraft or
// Manager.ReconciliarSerieDraft) for ONE item, logs a best-effort failure and
// always STAMPS the attempt (same pattern as ReprocessarArtesFaltantes/
// EnriquecerRetroativo: the item was checked in this run, even without having
// relinked -- otherwise the next batch would try again the next day, without
// respecting the throttle). `rotulo` is only for the log message ("faixa"/
// "série").
func (s *Server) reconciliarECarimbar(ctx context.Context, id string, langs []string, rotulo string, tentar func(context.Context, string, []string) (bool, error)) bool {
	linked, err := tentar(ctx, id, langs)
	if err != nil {
		log.Printf("saude_biblioteca: reconciliação de %s órfã falhou para %s (best-effort, ignorado): %v", rotulo, id, err)
	}
	if werr := s.carimbarTentativaExterna(ctx, id); werr != nil {
		log.Printf("saude_biblioteca: gravar tentativa de reconciliação falhou para %s: %v", id, werr)
	}
	return linked
}

// forcarReconciliacaoResultDTO is the response of POST
// /entities/{id}/force-reconcile (openapi.yaml
// ForceReconcileResult) -- a single field, `linked`, mirroring the return of
// ReconciliarFaixaDraft/ReconciliarSerieDraft.
type forcarReconciliacaoResultDTO struct {
	Linked bool `json:"linked"`
}

// forcarReconciliacao handles POST /api/v1/entities/{id}/force-reconcile:
// it triggers the SAME parent-child reconciliation of the batch job
// (ReconciliarFilhosOrfaos) for ONE entity, but IGNORING the
// last_external_match_attempt_at throttle completely -- a manual escape
// hatch for when the owner wants immediate feedback (e.g.: they just
// corrected a file tag, or know that a backend fix was just
// released) without waiting for the next saude_biblioteca cycle. Dispatch
// by ent.Type: "faixa" -> ReconciliarFaixaDraft, "tv" ->
// ReconciliarSerieDraft; any other type is a no-op (200, linked=false, WITHOUT
// stamping -- there was no real attempt at anything, same "type not
// applicable" criterion). In both applicable cases, it always stamps the
// attempt at the end (even on a try error), same semantics as
// enrichMissing (retroativo.go): it coordinates with the NEXT job batch so it
// does not compete for the item the owner just resolved manually. 404
// entity does not exist; 500 only on a real database/infrastructure error (in the
// reconciliation OR in the stamping) -- never for "no candidate found"
// (linked=false is a valid result, not an error).
func (s *Server) forcarReconciliacao(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	// Without a scanner configured, ReconciliarFaixaDraft/ReconciliarSerieDraft would
	// not even exist to call -- same guard as ReconciliarFilhosOrfaos:
	// it degrades (200, linked=false) instead of 503/500.
	if s.scanner == nil {
		writeJSON(w, http.StatusOK, forcarReconciliacaoResultDTO{Linked: false})
		return
	}

	langCfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}

	var tentar func(context.Context, string, []string) (bool, error)
	switch ent.Type {
	case tipoEntFaixa:
		tentar = s.scanner.ReconciliarFaixaDraft
	case tipoEntSerie:
		tentar = s.scanner.ReconciliarSerieDraft
	default:
		// Type not applicable (e.g.: movie): no-op, without stamping -- there was no
		// real parent-child reconciliation attempt at all.
		writeJSON(w, http.StatusOK, forcarReconciliacaoResultDTO{Linked: false})
		return
	}

	linked, tentarErr := tentar(ctx, ent.ID, langCfg.Allowed)
	// Always STAMPED, even if tentarErr != nil -- the item was actually
	// checked in this call (same semantics as enrichMissing/
	// EnriquecerRetroativo). Unlike the batch job (best-effort, logs and
	// moves on), here a real database error in ANY of the two steps becomes
	// 500 -- it is an individual HTTP call, not a batch.
	carimbarErr := s.carimbarTentativaExterna(ctx, ent.ID)
	if tentarErr != nil || carimbarErr != nil {
		respondErroInterno(w)
		return
	}

	writeJSON(w, http.StatusOK, forcarReconciliacaoResultDTO{Linked: linked})
}
