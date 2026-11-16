package server

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// This file resolves the EXTERNAL identity (source, source_id) to the corresponding
// INTERNAL entity: the basis of the "internal-first centralization". When an external item
// (a collection or one of its parts) was already imported, the UI must link to the internal
// destination (/entities/{id}) and not offer to reimport — for that, the external DTOs get
// an `internal_id`, filled in by these helpers.
//
// The resolution matches by (source, source_id) against the AUTHORITATIVE identity of the
// entity (generated columns external_id_tmdb/tvdb; see ADR-0015/ADR-0021/ADR-0011). It is
// generalized: ANY source with a generated column (tmdb AND tvdb today) resolves the
// internal-first, not only tmdb — so an item imported from TheTVDB also shows up "na
// biblioteca". Sources without a column of their own come out nil (they degrade — no
// lookup), coherent with the find-or-create (acharPorIDExterno) of the same coverage.

// idInternoItem returns the id of the internal entity of an already imported external item
// (movie/tv), or nil when it does not exist, the source has no lookup, or the source_id is
// empty. It dispatches by the SOURCE (mirroring enrich.acharPorIDExterno):
// "tmdb"→external_id_tmdb, "tvdb"→external_id_tvdb (indexed generated columns). A real
// database error degrades to nil (it logs) — the screen read never fails because of the
// internal-first resolution, which is an optional enrichment.
func (s *Server) idInternoItem(ctx context.Context, source, sourceID string) *string {
	if sourceID == "" {
		return nil
	}
	var ent sqlcgen.Entity
	var err error
	switch source {
	case "tmdb":
		ent, err = s.leitura().ObterEntityPorTmdb(ctx, nsDe(sourceID))
	case "tvdb":
		ent, err = s.leitura().ObterEntityPorTvdb(ctx, nsDe(sourceID))
	default:
		return nil // source without a generated column: no lookup (degrades)
	}
	if err != nil {
		logSeErroReal("id interno do item", sourceID, err)
		return nil
	}
	return &ent.ID
}

// idInternoColecao returns the id of the internal COLLECTION entity (type='collection') of
// an already imported external collection, or nil when it does not exist, the source is
// not tmdb, or the source_id is empty. It uses the dedicated query ObterColecaoPorTmdb
// (restricted to type='collection') so it does not match a homonymous movie with the same
// numeric id. Same resilience as idInternoItem: a database error degrades to nil.
func (s *Server) idInternoColecao(ctx context.Context, source, sourceID string) *string {
	if source != "tmdb" || sourceID == "" {
		return nil
	}
	col, err := s.leitura().ObterColecaoPorTmdb(ctx, nsDe(sourceID))
	if err != nil {
		logSeErroReal("id interno da colecao", sourceID, err)
		return nil
	}
	return &col.ID
}

// idInternoPorMbid returns the id of the internal entity (artist/album/track) already
// cataloged with this MusicBrainz MBID, or nil when it does not exist or `sourceID` is
// empty. It generalizes idInternoItem/idInternoColecao (which dispatch by the movie/tv
// `source`) to the music vertical (ADR-0029): the universal external id of music is
// always the MBID (generated column external_id_mbid), regardless of the entity.type. Used
// by the external album screen (external_album.go) to mark internal_id both on the album
// and on each official track of the release. Same resilience as idInternoItem: a real
// database error degrades to nil (it logs).
func (s *Server) idInternoPorMbid(ctx context.Context, sourceID string) *string {
	if sourceID == "" {
		return nil
	}
	ent, err := s.leitura().ObterEntityPorMbid(ctx, nsDe(sourceID))
	if err != nil {
		logSeErroReal("id interno por mbid", sourceID, err)
		return nil
	}
	return &ent.ID
}

// logSeErroReal logs only REAL database errors — a missing row (sql.ErrNoRows) is the
// normal "not imported" case and does not deserve a log.
func logSeErroReal(contexto, sourceID string, err error) {
	if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("server: %s (source_id=%s) falhou (segue sem id interno): %v", contexto, sourceID, err)
	}
}
