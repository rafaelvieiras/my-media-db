// Package historico implements the retroactive BACKFILL of orphan events
// (ADR-0022): when an entity gains an external id (tmdb/imdb/tvdb) -- through
// PUT /entities/{id}/external-ids or through enrichment (internal/enrich) --
// this package links the `historico_evento` events that were left without an
// entity (entity_id NULL, written by the Jellyfin webhook when no entity
// matched, see internal/server/webhook_jellyfin.go) whose raw IDs match the new
// id.
//
// A small package with no HTTP (same spirit as internal/webhook): only logic
// over sqlcgen.Queries, importable both by internal/server and by
// internal/enrich without creating a cycle (neither of them imports this
// package back).
package historico

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// fontesReconhecidas are the only sources that the webhook matching uses today
// (internal/server/webhook_jellyfin.go, casarEntityJellyfin) and, therefore,
// the only ones an orphan event can have written to raw_*_id -- the backfill
// covers exactly the same 3. A source outside this map in `externalIDs` (e.g.:
// an artwork/trailer source that does not take part in the matching) is
// ignored, it is not an error.
var fontesReconhecidas = map[string]bool{"tmdb": true, "imdb": true, "tvdb": true}

// ns wraps a string into a valid sql.NullString when non-empty (NULL when
// ""). Local copy of the equivalent helper in internal/server (nsDe) -- this
// package cannot import internal/server (dependency in the wrong direction:
// server would import historico, not the other way around).
func ns(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// VincularOrfaos links to entityID every orphan event (historico_evento with
// entity_id IS NULL) whose raw id of some source present in externalIDs matches
// (tmdb/imdb/tvdb -- the same 3 sources that the webhook matching uses; keys
// outside that set are ignored). It runs one backfill query per source that is
// present and has a non-empty value; it adds up the affected rows from all of
// them.
//
// An error from ONE source does not abort the others -- it is accumulated via
// errors.Join and returned at the end (the caller typically only logs, the
// backfill is a best-effort bonus on top of an identity write that already
// succeeded).
func VincularOrfaos(ctx context.Context, q *sqlcgen.Queries, entityID string, externalIDs map[string]string) (int64, error) {
	var total int64
	var erros error

	for fonte, valor := range externalIDs {
		if valor == "" || !fontesReconhecidas[fonte] {
			continue
		}
		var linhas int64
		var err error
		switch fonte {
		case "tmdb":
			linhas, err = q.VincularHistoricoEventoOrfaoPorTmdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorTmdbParams{
				EntityID:  ns(entityID),
				RawTmdbID: ns(valor),
			})
		case "imdb":
			linhas, err = q.VincularHistoricoEventoOrfaoPorImdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorImdbParams{
				EntityID:  ns(entityID),
				RawImdbID: ns(valor),
			})
		case "tvdb":
			linhas, err = q.VincularHistoricoEventoOrfaoPorTvdb(ctx, sqlcgen.VincularHistoricoEventoOrfaoPorTvdbParams{
				EntityID:  ns(entityID),
				RawTvdbID: ns(valor),
			})
		}
		if err != nil {
			erros = errors.Join(erros, err)
			continue
		}
		total += linhas
	}
	return total, erros
}
