package server

// This file exposes the SUGGESTED MATCH (migration 0028): the "did you mean this?" hint the
// scanner persists when the name on disk found nothing and a cheaper variant of it did.
//
// It is a HINT, not an identity: nothing here applies anything. The action behind the
// warning is the endpoint that already exists (POST /entities/{id}/confirm-candidate), and
// the owner may also dismiss the hint (DELETE /entities/{id}/match-suggestion, 204,
// idempotent) -- a hint with no way out would become permanent noise.

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// sugestaoDeMatch returns the "did you mean this?" hint of an item, computing it ON FIRST
// OPEN when there is none yet.
//
// WHY HERE and not at scan time: the fallback costs up to four source queries per name (the
// cheaper variants). Firing that for every draft during a scan would multiply the requests
// by the size of the review queue -- the exact burst that already earned a 429 from TMDb.
// Computed on open, the cost is paid only for the item the owner is actually looking at, and
// the result is PERSISTED, so the next open is a single row read.
//
// It only runs for an item that still has NO identity (no external id) -- a resolved item
// needs no hint -- and only for series, which is the vertical the fallback was designed and
// measured for. A failure of any kind returns nil: the hint is decoration next to the real
// data and must never break the screen.
func (s *Server) sugestaoDeMatch(ctx context.Context, ent resolver.Entidade) *resolver.MatchSuggestion {
	row, err := s.leitura().ObterMatchSuggestion(ctx, ent.ID)
	switch {
	case err == nil:
		if row.Dismissed != 0 {
			return nil // the owner already said no: do not ask again, do not query again
		}
		return sugestaoDTO(row, ent)
	case !errors.Is(err, sql.ErrNoRows):
		log.Printf("server: ler sugestão de match de %s falhou (%v)", ent.ID, err)
		return nil
	}
	if !s.podeSugerir(ent) {
		return nil
	}
	sug, ok := s.enrich.SugerirSerie(ctx, ent.Title, s.idiomasDaEntity(ctx, ent))
	if !ok {
		return nil
	}
	criada, err := s.gravarSugestao(ctx, ent.ID, sug)
	if err != nil {
		log.Printf("server: gravar sugestão de match de %s falhou (%v)", ent.ID, err)
		return nil
	}
	return sugestaoDTO(criada, ent)
}

// podeSugerir says whether the fallback is worth running for this item: a SERIES that still
// has no external id and carries a name to work with. Everything else (a resolved item, a
// movie, an episode) gets no hint.
func (s *Server) podeSugerir(ent resolver.Entidade) bool {
	// s.enrich is nil when the server runs WITHOUT a source configured (a read-only
	// instance, and several tests): there is nothing to ask, so there is no hint.
	return s.enrich != nil && ent.Type == "tv" && len(ent.ExternalIDs) == 0 && ent.Title != ""
}

// idiomasDaEntity returns the allowed languages for the query, degrading to the global list
// when the settings cannot be read (the hint is not worth an error).
func (s *Server) idiomasDaEntity(ctx context.Context, _ resolver.Entidade) []string {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return nil
	}
	// The allowed languages, primary first: the same list the search uses.
	idiomas := make([]string, 0, len(cfg.Allowed)+1)
	idiomas = append(idiomas, cfg.Primary)
	for _, l := range cfg.Allowed {
		if l != cfg.Primary {
			idiomas = append(idiomas, l)
		}
	}
	return idiomas
}

// gravarSugestao persists the computed hint and returns the stored row, so the DTO the
// screen receives is exactly what the next open will read.
func (s *Server) gravarSugestao(ctx context.Context, id string, sug enrich.SugestaoDeSerie) (sqlcgen.MatchSuggestion, error) {
	params := sqlcgen.UpsertMatchSuggestionParams{
		EntityID:  id,
		Source:    sug.Resultado.Source,
		SourceID:  sug.Resultado.SourceID,
		Title:     sug.Resultado.Title,
		Year:      nsDeTexto(sug.Resultado.Year),
		PosterUrl: nsDeTexto(sug.Resultado.PosterURL),
		Score:     int64(sug.Score + 0.5),
		Query:     sug.Consulta,
		Reason:    enrich.MotivoNome,
	}
	if err := sqlcgen.New(s.db.Write).UpsertMatchSuggestion(ctx, params); err != nil {
		return sqlcgen.MatchSuggestion{}, err
	}
	return s.leitura().ObterMatchSuggestion(ctx, id)
}

// nsDeTexto converts an optional string to a nullable column value.
func nsDeTexto(p *string) sql.NullString {
	if p == nil || *p == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

// sugestaoDTO maps the stored row to the response shape. The entity is needed only to
// answer CanApply: a row whose candidate is the identity the entity ALREADY has must not
// offer an apply button (that is the 'tree_mismatch' case, where the row describes the
// current match on purpose).
func sugestaoDTO(row sqlcgen.MatchSuggestion, ent resolver.Entidade) *resolver.MatchSuggestion {
	return &resolver.MatchSuggestion{
		Source:         row.Source,
		SourceID:       row.SourceID,
		Title:          row.Title,
		Year:           ptrDeNull(row.Year),
		PosterURL:      ptrDeNull(row.PosterUrl),
		Score:          int(row.Score),
		Query:          row.Query,
		Reason:         row.Reason,
		CanApply:       ent.ExternalIDs[row.Source] != row.SourceID,
		FilesUnmatched: int(row.FilesUnmatched),
		FilesTotal:     int(row.FilesTotal),
	}
}

// ptrDeNull converts a nullable column to *string, nil when NULL (so the JSON says null
// instead of "").
func ptrDeNull(v sql.NullString) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

// esquecerSugestao drops the hint of an entity. It is called whenever the item gets a real
// identity (a correction, an applied candidate, an import), because the hint described the
// previous state -- keeping it would offer the owner a "fix" for something already fixed.
// Best-effort: a failure is logged and never fails the operation that succeeded.
func (s *Server) esquecerSugestao(ctx context.Context, id string) {
	if err := sqlcgen.New(s.db.Write).DeletarMatchSuggestion(ctx, id); err != nil {
		log.Printf("server: apagar sugestão de match de %s falhou (%v)", id, err)
	}
}

// deleteMatchSuggestion handles DELETE /api/v1/entities/{id}/match-suggestion: the owner
// ignores the hint. The row is KEPT (dismissed=1) instead of deleted, so opening the item
// again neither re-queries the sources nor brings the warning back.
// 204 always (idempotent, like the other DELETEs of the API); 404 only when the ENTITY does
// not exist.
func (s *Server) deleteMatchSuggestion(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}
	if err := sqlcgen.New(s.db.Write).DescartarMatchSuggestion(ctx, id); err != nil {
		respondErroInterno(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
