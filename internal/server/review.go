package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file implements the Phase 3 CURATION: the review and orphan queues
// (queries), plus the confirm/correct actions and the PURGE
// (individual and in batch — the only destructive door, always explicit, ADR-0012).
// The reads go through the read pool; confirm/correct/purge write through the
// write pool (ADR-0010). Correction reuses the enrich service and the
// resolverEntity, without any new write path.

// limiarBaixaConfianca is the confidence cutoff that separates "high guess" from "low
// confidence" (0-100 scale of the scanner). It MIRRORS the LIMIAR_BAIXA_CONFIANCA=70 of the
// front end (web/src/lib/entity-badges.ts): the two MUST match. Annotated coupling —
// a candidate to become config/endpoint in the future (out of scope here). Single source in
// the backend: the ListarRevisao query receives it as a parameter.
const limiarBaixaConfianca = 70

// confiancaConfirmada is the confidence value that the "confirm" action writes into the
// entity's media_file rows: the owner's authority (100) takes the item out of the review
// queue.
const confiancaConfirmada = 100

// correctBody is the body of POST /entities/{id}/correct: the correct identity the
// owner reports. Without source, the default source is resolved by capability (fonteDefault);
// source_id is required (422 if empty).
type correctBody struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
}

// purgeBatchBody is the body of POST /entities/purge: the ids to purge in batch.
type purgeBatchBody struct {
	IDs []string `json:"ids"`
}

// purgaDTO is the response of the individual purge (DELETE /entities/{id}).
type purgaDTO struct {
	Purged bool `json:"purged"`
}

// purgaLoteDTO is the response of the batch purge (POST /entities/purge): how many
// entities were actually removed (idempotent by absent id).
type purgaLoteDTO struct {
	Removed int `json:"removed"`
}

// listReview handles GET /api/v1/review: the review queue (draft OR low confidence)
// with the same card shape as the list. 200 with [] when empty.
func (s *Server) listReview(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	rows, err := s.leitura().ListarRevisao(r.Context(),
		sql.NullFloat64{Float64: limiarBaixaConfianca, Valid: true})
	if err != nil {
		respondErroInterno(w)
		return
	}
	items := make([]entityCardDTO, 0, len(rows))
	caminhos := make([]string, 0, len(rows))
	for _, row := range rows {
		items = append(items, cardDTO(sqlcgen.ListarEntityCardsRow{
			ID: row.ID, Type: row.Type, Title: row.Title, Status: row.Status,
			Owned: row.Owned, FileCount: row.FileCount,
			MatchConfidence: row.MatchConfidence, HasPoster: row.HasPoster,
		}))
		caminhos = append(caminhos, row.Path)
	}
	// Grouping: annotation per item, without changing the predicate of the
	// queue. The library roots come in only so the label stays short
	// (path relative to the source); an error when listing degrades to long labels.
	anotarGruposRevisao(items, caminhos, s.raizesDeBiblioteca(r.Context()))
	writeJSON(w, http.StatusOK, items)
}

// listOrphans handles GET /api/v1/orphans: the orphan queue (auto-created, all
// missing, no investment from the owner). 200 with [] when empty.
func (s *Server) listOrphans(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	rows, err := s.leitura().ListarOrfas(r.Context())
	if err != nil {
		respondErroInterno(w)
		return
	}
	items := make([]entityCardDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, cardDTO(sqlcgen.ListarEntityCardsRow{
			ID: row.ID, Type: row.Type, Title: row.Title, Status: row.Status,
			Owned: row.Owned, FileCount: row.FileCount,
			MatchConfidence: row.MatchConfidence, HasPoster: row.HasPoster,
		}))
	}
	writeJSON(w, http.StatusOK, items)
}

// confirmEntity handles POST /api/v1/entities/{id}/confirm: the owner accepts the guess.
// It raises the confidence of the entity's media_file rows to 100 and responds with the
// resolved entity. 404 if the entity does not exist.
func (s *Server) confirmEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}
	if err := s.finalizarRevisao(ctx, id); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderResolvida(w, ctx, id, http.StatusOK)
}

// correctEntity handles POST /api/v1/entities/{id}/correct: the owner reports the
// correct identity. It writes external_ids[source]=source_id and RE-ENRICHES through
// the enrich service (identity wins, MD4; multi-language for free), then raises the
// confidence to 100. It does not touch override/artwork (enrich is non-destructive). 404 if
// the entity does not exist; 422 if source_id is empty; 502 if the source fails; 503 if
// enrich is off.
func (s *Server) correctEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	if s.enrichIndisponivel(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	var body correctBody
	if ok := decodeOpcional(w, r, &body); !ok {
		return
	}
	source, ok := s.resolverFonte(w, ent.Type, body.Source)
	if !ok {
		return
	}
	if body.SourceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "source_id é obrigatório")
		return
	}

	s.aplicarIdentidadeExterna(w, ctx, id, ent.Type, source, body.SourceID)
}

// aplicarIdentidadeExterna applies the external identity (source/sourceID) to an
// already existing entity through the canonical "Correct" sequence:
// resolverColisaoExterna (safety net -- merges without the owner's
// investment, or responds a real 409) → idiomasPermitidos → s.enrich.Enriquecer
// (identity wins, multi-language for free) → finalizarRevisao (raises
// confidence/promotes from draft). Extracted from correctEntity to
// be reused by the `external` branch of confirmCandidate, called with the SAME
// REAL ResponseWriter of the request in both cases -- a collision always becomes
// a real 409 in the response, it is never degraded silently (that
// "degrades without responding" behavior is exclusive to the unattended
// auto-match, via capturaResposta in aplicarCandidatoExterno). ok=false
// when any step has already written an error response -- the caller only
// needs to `return` without writing anything else.
func (s *Server) aplicarIdentidadeExterna(w http.ResponseWriter, ctx context.Context, id, tipo, source, sourceID string) bool {
	// Safety net against a duplicate: the corrected identity
	// may already belong to another entity (e.g.: imported by mistake before this
	// correction) -- it merges without the owner's investment, or aborts with 409.
	if _, ok := s.resolverColisaoExterna(w, ctx, id, tipo, source, sourceID); !ok {
		return false
	}

	langs, err := s.idiomasPermitidos(ctx, w)
	if err != nil {
		return false
	}
	// The new identity WINS: the ids of the OTHER sources belonged to the previous
	// (wrong) identity and must not survive a correction -- see the comment of
	// AplicarIdentidadeExclusiva. The enrichment right below writes back whatever
	// cross-id it can resolve.
	if err := s.enrich.AplicarIdentidadeExclusiva(ctx, id, source, sourceID); err != nil {
		respondErroInterno(w)
		return false
	}
	// A manual identity change is a RESET of what was derived from the previous one. FIRST the
	// data that would pass for the new identity: the downloaded artwork, the cached external
	// payloads and the exported-NFO memory (see enrich.InvalidarDerivados) -- measured in
	// production: without this the folder kept the previous show's poster.jpg/fanart.jpg and
	// the NFOs kept naming its episodes, while the completeness said "complete" and no job
	// ever rewrote them.
	if err := s.enrich.InvalidarDerivados(ctx, id); err != nil {
		respondErroInterno(w)
		return false
	}
	// Then the season/episode tree: it described the other show, so it goes away before the
	// new tree is imported (otherwise both sets live inside the same seasons -- see
	// enrich.DescartarArvoreSerie). The files stay: they hang on the series, and the
	// re-import re-links them by number.
	if tipo == "tv" {
		if err := s.enrich.DescartarArvoreSerie(ctx, id); err != nil {
			respondErroInterno(w)
			return false
		}
	}
	// force=true: the owner asked for this identity, so nothing from the previous one is
	// reused -- metadata, artwork and mediainfo are fetched again (same for a movie).
	if err := s.enrich.Enriquecer(ctx, id, source, sourceID, tipo, langs, true); err != nil {
		respondFonteIndisponivel(w)
		return false
	}
	// The identity changed, so the artwork JUST invalidated is missing again: materialize it
	// now instead of leaving the folder with the previous show's poster until somebody runs
	// the media sweep. Best-effort by construction -- the tree/identity are already written,
	// and a download failure must never turn a successful correction into an error.
	if _, _, err := s.MaterializarMidiasFaltantes(ctx, id); err != nil {
		log.Printf("review: materializar apos trocar a identidade de %s falhou (segue): %v", id, err)
	}
	// The item now has a REAL identity: the hint about the old (unresolved) state is over.
	s.esquecerSugestao(ctx, id)
	if err := s.finalizarRevisao(ctx, id); err != nil {
		respondErroInterno(w)
		return false
	}
	s.responderResolvida(w, ctx, id, http.StatusOK)
	return true
}

// deleteEntity handles DELETE /api/v1/entities/{id}: individual PURGE. It deletes the
// entity (ApagarEntity) — ON DELETE CASCADE takes media_file/media_external/media_override/
// asset along — and best-effort cleans the entity's assets binary directory.
// 200 with {purged:true}; 404 if the entity does not exist.
func (s *Server) deleteEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}
	if err := sqlcgen.New(s.db.Write).ApagarEntity(ctx, id); err != nil {
		respondErroInterno(w)
		return
	}
	s.limparBinariosAsset(id)
	writeJSON(w, http.StatusOK, purgaDTO{Purged: true})
}

// purgeBatch handles POST /api/v1/entities/purge: PURGE in batch. It purges each id of
// the body, ignoring absent ids (idempotent), and responds how many were removed.
// 422 invalid body.
func (s *Server) purgeBatch(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	ctx := r.Context()

	var body purgeBatchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "corpo JSON inválido")
		return
	}

	removed := 0
	for _, id := range body.IDs {
		ok, err := s.purgarSeExiste(ctx, id)
		if err != nil {
			respondErroInterno(w)
			return
		}
		if ok {
			removed++
		}
	}
	writeJSON(w, http.StatusOK, purgaLoteDTO{Removed: removed})
}

// ── Curation helpers ─────────────────────────────────────────────────────────

// finalizarRevisao is the final step shared by confirm and correct: it takes the
// item out of the review queue through BOTH branches of the predicate. (1) it raises the
// confidence of the media_file rows to 100 (kills the "low confidence" branch); (2) it
// promotes a draft to 'cataloged' (kills the "status='draft'" branch). Without (2), a
// confirmed/corrected draft raised the confidence but stayed stuck in the queue as
// unidentified. Both writes go through the write pool; PromoverDeDraft is a no-op outside
// draft.
func (s *Server) finalizarRevisao(ctx context.Context, id string) error {
	q := sqlcgen.New(s.db.Write)
	if err := q.AtualizarConfidencePorEntity(ctx, sqlcgen.AtualizarConfidencePorEntityParams{
		MatchConfidence: sql.NullFloat64{Float64: confiancaConfirmada, Valid: true},
		EntityID:        id,
	}); err != nil {
		return err
	}
	return q.PromoverDeDraft(ctx, id)
}

// purgarSeExiste deletes the entity and cleans its binaries IF it exists; it returns
// (true, nil) when it removed it, (false, nil) when it no longer existed (idempotent). A
// real database error is propagated.
func (s *Server) purgarSeExiste(ctx context.Context, id string) (bool, error) {
	if _, err := s.leitura().ObterEntity(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if err := sqlcgen.New(s.db.Write).ApagarEntity(ctx, id); err != nil {
		return false, err
	}
	s.limparBinariosAsset(id)
	return true, nil
}

// limparBinariosAsset best-effort removes the entity's assets binary directory
// ({assetsDir}/{id}). Without assetsDir configured it is a no-op; errors are
// ignored (the CASCADE already removed the rows — the disk is only cache).
func (s *Server) limparBinariosAsset(id string) {
	if s.assetsDir == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(s.assetsDir, id))
}
