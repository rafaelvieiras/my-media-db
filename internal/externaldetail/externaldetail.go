// Package externaldetail is the RICH EXTERNAL DETAIL service: it serves the
// provider's augmented envelope (the same re-composable cache as enrich, ADR-0011)
// with its own CACHE-FIRST, decoupled from `entity` — viewing the external does NOT
// catalog it. The first query pulls from the source and PERSISTS in external_detail;
// the next ones serve from the cache within a TTL, re-pulling when the TTL expires OR
// when the set of allowed languages changes (a newly allowed language is not in the
// stored payload).
//
// It is reused by enrich/import (which now seeds media_external from here, avoiding
// the second trip to the source when the cache is fresh) and by the GET handler of the
// external detail. The persistence is BEST-EFFORT: if the write stumbles, the read
// still answers the detail that was pulled (the next visit retries).
package externaldetail

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// TTLPadrao is how long the external detail is served from the cache before being
// re-pulled. The editorial detail changes little; one day saves the source without
// aging the content too much.
const TTLPadrao = 24 * time.Hour

// Servico resolves the external detail with cache-first. It receives the database
// pools, the multi-provider REGISTRY (the source is chosen by name) and the TTL.
// `agora` is injectable in tests (deterministic clock).
type Servico struct {
	database  *db.DB
	providers provider.Registry
	ttl       time.Duration
	agora     func() time.Time
}

// New creates the service. providers is the registry of sources (main only feeds it
// when there is a credential); ttl<=0 falls back to TTLPadrao.
func New(database *db.DB, provs provider.Registry, ttl time.Duration) *Servico {
	if ttl <= 0 {
		ttl = TTLPadrao
	}
	return &Servico{database: database, providers: provs, ttl: ttl, agora: time.Now}
}

// PayloadFresco returns the AUGMENTED ENVELOPE of the external detail (the same
// format as BuscarDetalhe, a re-composable cache): from the cache when fresh,
// otherwise pulling from the source and persisting (best-effort). `langs` are the
// ALLOWED languages (all translations in a single trip). `force` skips the READ of
// the cache (doCacheFresco) and goes straight to the provider even with a fresh cache
// within the TTL — the WRITE of the result (persistir) still happens normally, so the
// cache ends up updated with the new payload for the next reads without force. It
// propagates provider.ErrNaoEncontrado when the item does not exist in the source, and
// the raw error when the source fails (the caller translates it into 404/502).
//
// Since ADR-0024: when there is no live search provider registered for the source, OR
// the live search fails, it falls back to the "any" cache (doCacheQualquer, ignoring
// TTL/langs — it only checks for a non-empty payload) before propagating an error. An
// item visited before never loses the stored payload because of a momentary/permanent
// unavailability of the source (absent, revoked or expired key, network down,
// discontinued provider); it only really fails when there is no cache at all.
func (s *Servico) PayloadFresco(ctx context.Context, source, sourceID, tipo string, langs []string, force bool) (json.RawMessage, error) {
	if !force {
		if cached, ok := s.doCacheFresco(ctx, source, sourceID, langs); ok {
			return cached, nil
		}
	}

	prov, ok := s.providers.Get(source)
	if !ok {
		if cached, ok := s.doCacheQualquer(ctx, source, sourceID); ok {
			return cached, nil
		}
		return nil, provider.ErrNaoEncontrado
	}

	raw, err := prov.BuscarDetalhe(ctx, sourceID, tipo, langs)
	if err != nil {
		if cached, ok := s.doCacheQualquer(ctx, source, sourceID); ok {
			log.Printf("externaldetail: busca ao vivo de %s/%s falhou, servindo cache stale: %v", source, sourceID, err)
			return cached, nil
		}
		return nil, err
	}
	s.persistir(ctx, source, sourceID, tipo, raw, langs)
	return raw, nil
}

// doCacheFresco returns the cached payload when it exists and is FRESH: within the
// TTL AND with the requested set of languages contained in the stored one. Any stumble
// (absent row, unreadable timestamp/langs, empty payload) returns (nil,false) — the
// caller re-pulls. It never propagates an error: the cache is an optimization, not a
// barrier.
func (s *Servico) doCacheFresco(ctx context.Context, source, sourceID string, langs []string) (json.RawMessage, bool) {
	row, ok := s.obterLinhaCache(ctx, source, sourceID)
	if !ok {
		return nil, false
	}
	if !dentroDoTTL(row.FetchedAt, s.ttl, s.agora()) {
		return nil, false
	}
	if !idiomasContidos(row.LangsJson, langs) {
		return nil, false
	}
	return json.RawMessage(row.PayloadJson.String), true
}

// doCacheQualquer returns the cached payload when it exists, IGNORING freshness
// (TTL/langs) — used as a last resort when the live search is not possible (no
// provider) or failed, so data already saved because of a momentary/permanent
// unavailability of the source is never lost (ADR-0024).
func (s *Servico) doCacheQualquer(ctx context.Context, source, sourceID string) (json.RawMessage, bool) {
	row, ok := s.obterLinhaCache(ctx, source, sourceID)
	if !ok {
		return nil, false
	}
	return json.RawMessage(row.PayloadJson.String), true
}

// obterLinhaCache reads the external_detail row and returns (row,true) only when it
// exists and has a non-empty payload — the freshness check (TTL/langs) is left to the
// caller (doCacheFresco); doCacheQualquer uses the raw row. Any stumble (absent row,
// read error) returns (zero,false) and NEVER propagates an error: the cache is an
// optimization, not a barrier.
func (s *Servico) obterLinhaCache(ctx context.Context, source, sourceID string) (sqlcgen.ExternalDetail, bool) {
	row, err := sqlcgen.New(s.database.Read).ObterExternalDetail(ctx, sqlcgen.ObterExternalDetailParams{
		Source:   source,
		SourceID: sourceID,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("externaldetail: ler cache de %s/%s falhou: %v", source, sourceID, err)
		}
		return sqlcgen.ExternalDetail{}, false
	}
	if !row.PayloadJson.Valid || row.PayloadJson.String == "" {
		return sqlcgen.ExternalDetail{}, false
	}
	return row, true
}

// persistir writes (upsert) the payload into the external_detail cache, best-effort:
// a failure is logged and does NOT abort the read (the next visit retries).
// `langs_json` stores the languages contained, the basis of the refresh trigger by set
// of allowed ones.
func (s *Servico) persistir(ctx context.Context, source, sourceID, tipo string, raw json.RawMessage, langs []string) {
	langsJSON, err := json.Marshal(langs)
	if err != nil {
		log.Printf("externaldetail: serializar langs de %s/%s falhou (segue sem cachear): %v", source, sourceID, err)
		return
	}
	if err := sqlcgen.New(s.database.Write).UpsertExternalDetail(ctx, sqlcgen.UpsertExternalDetailParams{
		Source:      source,
		SourceID:    sourceID,
		Type:        tipo,
		PayloadJson: nsValid(string(raw)),
		LangsJson:   nsValid(string(langsJSON)),
	}); err != nil {
		log.Printf("externaldetail: cachear %s/%s falhou (segue sem cachear): %v", source, sourceID, err)
	}
}

// dentroDoTTL says whether the `fetched_at` stamp (the strftime format of the
// migrations) is less than `ttl` away from `agora`. An unreadable stamp counts as
// EXPIRED (re-pull) — the safe choice is to prefer the source over serving something
// with an unknown date.
func dentroDoTTL(fetchedAt string, ttl time.Duration, agora time.Time) bool {
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return false
	}
	return agora.Sub(t) < ttl
}

// idiomasContidos says whether ALL `pedidos` are in the set stored in `langsJSON`.
// A requested language that is absent (e.g.: newly allowed) forces the refresh
// (ADR-0011): the stored payload does not have that translation. An unreadable
// langs_json counts as "does not contain" (re-pull, safe).
func idiomasContidos(langsJSON sql.NullString, pedidos []string) bool {
	if !langsJSON.Valid || langsJSON.String == "" {
		return len(pedidos) == 0
	}
	var guardados []string
	if json.Unmarshal([]byte(langsJSON.String), &guardados) != nil {
		return false
	}
	conjunto := make(map[string]struct{}, len(guardados))
	for _, l := range guardados {
		conjunto[l] = struct{}{}
	}
	for _, p := range pedidos {
		if _, ok := conjunto[p]; !ok {
			return false
		}
	}
	return true
}

// nsValid wraps a string into a valid (non-NULL) sql.NullString.
func nsValid(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
