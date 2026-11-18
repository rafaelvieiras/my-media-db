package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// This file implements GET /api/v1/search: LOCAL-FIRST search.
// The catalog is ALWAYS queried (FTS5 over entity_fts), regardless of the external
// source; the external source (when there is a provider) runs in passthrough only to
// enrich, with results marked "not saved" (saved:false) — the search never
// creates an entity. A failure/absence of the external one degrades (external:[],
// external_available:false), never 5xx.

// ttlCacheExterno is the short TTL of the in-memory cache of the external result per
// (query,type). The volume is low (self-hosted): it spares the source without an
// elaborate limiter.
const ttlCacheExterno = 60 * time.Second

// buscarCardsFTSSQL is the LOCAL search query. Hand-written (not via sqlc) for
// two reasons: (1) sqlc v1.31 does not parse the whole-table MATCH of FTS5
// (`entity_fts MATCH ?`), only the single-column one (`title MATCH ?`), which would lose
// original_title/tags; (2) kept as an ASCII Go const with EXPLICIT columns,
// it avoids the sqlc corruption bug with query .sql files altogether. The SHAPE of the
// columns is IDENTICAL to ListarEntityCards (same order), so the result is scanned
// into sqlcgen.ListarEntityCardsRow and reuses the list's cardDTO.
//
// The `?1` placeholder (match) arrives SANITIZED (see sanitizarMatchFTS): each user term
// becomes a quoted phrase with a prefix, so that quotes, parentheses
// and FTS5 operators in the input do not break the query syntax.
//
// By DEFAULT the search only returns TOP-LEVEL WORKS: the sub-nodes of tiposSubNo
// (ADR-0030, entities.go — season/episode/album/faixa) stay
// out, otherwise a search for "Simpsons" would drown the series in a flood of
// episodes/seasons from several series (SUPERSEDES the hard exclusion of the old
// approach). The clause `e.type NOT IN (/*SLICE:sub_nos*/?) OR e.type IN
// (/*SLICE:incluir*/?)` uses the SAME mechanism sqlc generates for
// `sqlc.slice` (see ListarTiposComContagem in entity.sql.go), replicated by hand
// here because this query is written manually (reason (1)/(2) above) — the
// markers are expanded in Go by expandirSlice before execution:
// sub_nos ALWAYS receives the whole tiposSubNo (fixed exclusion), incluir receives the
// sub-nodes switched back on via `?include=` (parseInclude), empty by default (the empty
// list expands to `NOT IN (NULL)`/`IN (NULL)`, which never matches, preserving
// the top-only behavior). The positional parameters follow (match, sub_nos..., incluir...,
// tipo, tipo, limit).
//
// owned/file_count ALSO count the media_file_entity join: with `?include=
// episode` the search brings episodes, whose file has entity_id=SERIES and links to the
// episode only through the join -- without this the episode card would come as
// owned=false/file_count=0. Same extension (and same UNION shapes that preserve
// bool/int64 in the scan) as ListarEntityCards. owned also adds any DESCENDANT in the
// entity_relation tree that has a 'present' media_file -- this covers a music
// artist/album, whose only link to the file is on the TRACK, two levels below (see the
// long note in ObterEntityCard, internal/db/queries/entity.sql).
const buscarCardsFTSSQL = `WITH RECURSIVE arvore(ancestor_id, descendant_id) AS (
    SELECT parent_id, child_id FROM entity_relation
    UNION
    SELECT a.ancestor_id, er.child_id FROM arvore a JOIN entity_relation er ON er.parent_id = a.descendant_id
)
SELECT
    e.id, e.type, e.title, e.status,
    EXISTS (
        SELECT 1 FROM media_file m WHERE m.entity_id = e.id AND m.state = 'present'
        UNION ALL
        SELECT 1 FROM media_file_entity mfe JOIN media_file mf ON mf.id = mfe.media_file_id
            WHERE mfe.entity_id = e.id AND mf.state = 'present'
        UNION ALL
        SELECT 1 FROM arvore t JOIN media_file m ON m.entity_id = t.descendant_id AND m.state = 'present'
            WHERE t.ancestor_id = e.id
    ) AS owned,
    (SELECT COUNT(*) FROM (
        SELECT m.id FROM media_file m WHERE m.entity_id = e.id
        UNION ALL
        SELECT DISTINCT mfe.media_file_id FROM media_file_entity mfe WHERE mfe.entity_id = e.id
    )) AS file_count,
    (SELECT MAX(m.match_confidence) FROM media_file m WHERE m.entity_id = e.id) AS match_confidence,
    EXISTS (SELECT 1 FROM asset a WHERE a.entity_id = e.id AND a.kind = 'poster') AS has_poster
FROM entity_fts
JOIN entity e ON e.id = entity_fts.entity_id
WHERE entity_fts MATCH ?
  AND (e.type NOT IN (/*SLICE:sub_nos*/?) OR e.type IN (/*SLICE:incluir*/?))
  AND (? IS NULL OR e.type = ?)
ORDER BY entity_fts.rank
LIMIT ?;`

// expandirSlice replaces the `/*SLICE:<marcador>*/?` marker of buscarCardsFTSSQL with
// a `?,?,...` list the size of valores (and appends each value to args, in
// the same order) or with `NULL` when valores is empty (same mechanism that sqlc
// generates for `sqlc.slice` in a generated query — replicated by hand here because this
// query is written manually). `x NOT IN (NULL)`/`x IN (NULL)` never matches any row,
// so an empty slice is always "no exception"/"nothing included", never an SQL syntax
// error.
func expandirSlice(query, marcador string, valores []string, args []any) (string, []any) {
	m := "/*SLICE:" + marcador + "*/?"
	if len(valores) == 0 {
		return strings.Replace(query, m, "NULL", 1), args
	}
	for _, v := range valores {
		args = append(args, v)
	}
	return strings.Replace(query, m, strings.Repeat(",?", len(valores))[1:], 1), args
}

// externalResultDTO is a result of the EXTERNAL source. saved is
// always false: an external result is never a catalog entity until the import.
// InternalID is the id of the internal entity when the external item HAS already been
// imported — nil otherwise: the UI shows "in the library" and links to the internal one
// instead of re-importing.
type externalResultDTO struct {
	Source     string  `json:"source"`
	SourceID   string  `json:"source_id"`
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Year       *string `json:"year"`
	Overview   *string `json:"overview"`
	PosterURL  *string `json:"poster_url"`
	Saved      bool    `json:"saved"`
	InternalID *string `json:"internal_id"`
}

// searchResponseDTO is the response of GET /api/v1/search. local are catalog
// cards (same shape as the list); external are "not saved" results.
// external_available separates "source unavailable" from "zero results".
type searchResponseDTO struct {
	Query             string              `json:"query"`
	Local             []entityCardDTO     `json:"local"`
	External          []externalResultDTO `json:"external"`
	ExternalAvailable bool                `json:"external_available"`
}

// search handles GET /api/v1/search?q=&type=. q is required (empty => 400). The
// local search always runs; the external one only when there is a provider and degrades
// silently.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	q := r.URL.Query()
	termo := strings.TrimSpace(q.Get("q"))
	if termo == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "o parâmetro q é obrigatório")
		return
	}
	tipo := parseTipoFiltro(q) // string | nil
	incluir := parseInclude(q) // sub-nodes of tiposSubNo to switch back on in the local search

	ctx := r.Context()
	// ACTIVE read language: it validates the ?lang= against the allowed ones (400 if
	// outside) and resolves the effective one (default = primary). The EXTERNAL search
	// passes it on to the source; the local search serves the neutral card (same contract
	// as /entities).
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	lang, ok := s.parseLang(w, cfg, q.Get("lang"))
	if !ok {
		return
	}

	locais, err := s.buscarLocal(ctx, termo, tipo, incluir)
	if err != nil {
		respondErroInterno(w)
		return
	}

	tipoStr, _ := tipo.(string)
	externos, disponivel := s.buscarExternoOuReferencia(ctx, termo, tipoStr, lang)

	writeJSON(w, http.StatusOK, searchResponseDTO{
		Query:             termo,
		Local:             locais,
		External:          s.mapearExternos(ctx, externos),
		ExternalAvailable: disponivel,
	})
}

// parseInclude reads the `include` parameter (CSV, e.g.: "season,episode" or
// "album,faixa") and returns the sub-nodes of tiposSubNo to
// SWITCH BACK ON in the local search. Generalized: it accepts ANY value of
// tiposSubNo, no longer only season/episode — a new schema that becomes a sub-node (e.g.
// a future vertical) is switched back on through this same parameter, without touching
// anything here. It accepts repetition (idempotent, dedup) and silently ignores any
// value outside tiposSubNo. By default (no include) it returns empty — the
// sub-nodes stay out.
func parseInclude(q url.Values) []string {
	permitido := make(map[string]bool, len(tiposSubNo))
	for _, t := range tiposSubNo {
		permitido[t] = true
	}
	vistos := make(map[string]bool)
	incluir := make([]string, 0, len(tiposSubNo))
	for _, raw := range q["include"] {
		for _, v := range strings.Split(raw, ",") {
			v = strings.TrimSpace(v)
			if v == "" || !permitido[v] || vistos[v] {
				continue
			}
			vistos[v] = true
			incluir = append(incluir, v)
		}
	}
	return incluir
}

// buscarLocal runs the sanitized FTS5 MATCH on the READ pool and returns the cards.
// incluir are the sub-nodes of tiposSubNo to SWITCH BACK ON in the search (coming from
// parseInclude); by default (nil/empty) the sub-nodes stay out. Input without
// any searchable term (only punctuation/operators) returns an empty list without touching
// the database — never an error.
func (s *Server) buscarLocal(ctx context.Context, termo string, tipo any, incluir []string) ([]entityCardDTO, error) {
	match, ok := sanitizarMatchFTS(termo)
	if !ok {
		return []entityCardDTO{}, nil
	}
	query := buscarCardsFTSSQL
	args := []any{match}
	query, args = expandirSlice(query, "sub_nos", tiposSubNo, args)
	query, args = expandirSlice(query, "incluir", incluir, args)
	args = append(args, tipo, tipo, int64(limitPadrao))
	rows, err := s.db.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]entityCardDTO, 0)
	for rows.Next() {
		var row sqlcgen.ListarEntityCardsRow
		if err := rows.Scan(
			&row.ID, &row.Type, &row.Title, &row.Status,
			&row.Owned, &row.FileCount, &row.MatchConfidence, &row.HasPoster,
		); err != nil {
			return nil, err
		}
		card := cardDTO(row)
		items = append(items, card)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// SHALLOW ownership on the tv/season cards of the search — same annotation as the
	// listing, made after closing the cursor (it avoids nested queries on the same
	// Read). It degrades to null on failure; it never takes the search down.
	for i := range items {
		s.anotarPosseCard(ctx, &items[i])
	}
	return items, nil
}

// buscarExternoOuReferencia is the external search with the ID DOOR in front: when the
// term names an item directly (`TMDB:39340`, `TVDB:248741`, a bare `tt...`, or the item
// URL) the sources are not asked to match a title at all -- the id resolves the item, and
// a typo in the folder name stops mattering (internal/provider/idref.go explains why this
// is the door Jellyfin and tinyMediaManager use).
//
// Everything else keeps the literal search. A reference that no registered source can
// resolve falls THROUGH to the literal search instead of erroring: the user typed
// something odd, and the old behavior is still the best answer.
func (s *Server) buscarExternoOuReferencia(ctx context.Context, termo, tipo, lang string) ([]provider.ResultadoExterno, bool) {
	if ref, ok := provider.InterpretarReferencia(termo); ok {
		if res, disponivel, tratado := s.resolverReferencia(ctx, ref, tipo, lang); tratado {
			return res, disponivel
		}
	}
	return s.buscarExterno(ctx, termo, tipo, lang)
}

// resolverReferencia asks the provider that owns the id to resolve it.
//
// `tratado` says whether the reference was OURS to handle: false only when no registered
// source can do it (no provider for that source, or none implements
// provider.ResolvedorDeID) -- the caller then falls back to the literal search. Once a
// provider takes it, its failure is a DEGRADATION (available:false), not a reason to run a
// title search for the literal text "TMDB:39340", which could only add noise.
//
// The type hint from the input (?type=) is only applied when the reference does not carry
// one of its own (a themoviedb.org URL is more specific than the filter).
func (s *Server) resolverReferencia(ctx context.Context, ref provider.ReferenciaDeID, tipo, lang string) ([]provider.ResultadoExterno, bool, bool) {
	if ref.Tipo == "" {
		ref.Tipo = tipo // may stay empty: the provider then decides
	}
	p, ok := s.providers.Get(ref.Fonte)
	if !ok {
		return nil, false, false
	}
	resolvedor, ok := p.(provider.ResolvedorDeID)
	if !ok {
		return nil, false, false
	}
	chave := ref.Fonte + ":" + ref.ID + "\x00" + ref.Tipo + "\x00" + lang
	if res, acerto := s.cacheExterno.obter(chave); acerto {
		return res, true, true
	}
	res, err := resolvedor.ResolverID(ctx, ref, lang)
	if err != nil {
		// The source failed: same degradation as a failed search (external:[],
		// available:false), never a 5xx.
		return nil, false, true
	}
	s.cacheExterno.guardar(chave, res)
	return res, true, true
}

// buscarExterno queries ALL the sources of the registry with a short TTL cache and MERGES
// the results (dedup by (source, source_id); deterministic order by prioridadeFontes).
// It returns (results, available). Empty registry => (nil, false).
// available is true when AT LEAST ONE source responded (error/timeout of all =>
// false): the handler then degrades to external:[], external_available:false. Only the
// successful merged result enters the cache.
func (s *Server) buscarExterno(ctx context.Context, termo, tipo, lang string) ([]provider.ResultadoExterno, bool) {
	if s.providers.Vazio() {
		return nil, false
	}
	// The key includes the language: the same search in pt-BR and es has distinct
	// results and cannot share a cache entry.
	chave := termo + "\x00" + tipo + "\x00" + lang
	if res, ok := s.cacheExterno.obter(chave); ok {
		return res, true
	}
	res, algum := s.mesclarBuscas(ctx, termo, tipo, lang)
	if !algum {
		return nil, false
	}
	s.cacheExterno.guardar(chave, res)
	return res, true
}

// mesclarBuscas queries each provider (in priority order) and merges the results,
// deduplicating by (source, source_id) — the first to bring a pair wins. `algum` says
// whether ANY source responded (success), separating "all failed" from "zero results".
// A failure of ONE source degrades that source (it skips it), not the whole search.
func (s *Server) mesclarBuscas(ctx context.Context, termo, tipo, lang string) ([]provider.ResultadoExterno, bool) {
	var out []provider.ResultadoExterno
	vistos := make(map[string]bool)
	algum := false
	for _, p := range s.providersOrdenados() {
		res, err := p.Buscar(ctx, termo, tipo, lang)
		if err != nil {
			continue // this source degrades
		}
		algum = true
		for _, r := range res {
			chave := r.Source + "\x00" + r.SourceID
			if vistos[chave] {
				continue
			}
			vistos[chave] = true
			out = append(out, r)
		}
	}
	return out, algum
}

// providersOrdenados returns the providers of the registry in DETERMINISTIC order: first
// the ones from prioridadeFontes (R2), in the order of the list; then any sources registered
// outside the list (in registration order), so a configured source never disappears.
func (s *Server) providersOrdenados() []provider.Provider {
	out := make([]provider.Provider, 0, len(s.providers.Nomes()))
	vistos := make(map[string]bool)
	for _, nome := range prioridadeFontes {
		if p, ok := s.providers.Get(nome); ok {
			out = append(out, p)
			vistos[nome] = true
		}
	}
	for _, nome := range s.providers.Nomes() {
		if vistos[nome] {
			continue
		}
		if p, ok := s.providers.Get(nome); ok {
			out = append(out, p)
		}
	}
	return out
}

// mapearExternos converts the provider's canonical results into the external DTO,
// forcing saved:false (it is never an entity until the import) and annotating the
// interno-first internal_id: each external result that has already been imported gets the
// id of the corresponding entity (via idInternoItem), so the UI shows "in the library" and
// links to the internal one. The lookup matches by (r.Source, r.SourceID) against the
// indexed identity — ANY source with a generated column resolves (tmdb AND tvdb), not only
// tmdb.
func (s *Server) mapearExternos(ctx context.Context, res []provider.ResultadoExterno) []externalResultDTO {
	out := make([]externalResultDTO, 0, len(res))
	for _, r := range res {
		out = append(out, externalResultDTO{
			Source:     r.Source,
			SourceID:   r.SourceID,
			Type:       r.Type,
			Title:      r.Title,
			Year:       r.Year,
			Overview:   r.Overview,
			PosterURL:  r.PosterURL,
			Saved:      false,
			InternalID: s.idInternoItem(ctx, r.Source, r.SourceID),
		})
	}
	return out
}

// sanitizarMatchFTS turns the user text into a SAFE MATCH expression of
// FTS5. Robust strategy: it removes quotes (they become separators), splits into terms by
// spaces, discards terms without any letter/digit and wraps each remaining term
// in a quoted PHRASE with a prefix (`"term"*`). Thus:
//   - quotes, parentheses, `:`, `{}`, `*`, `AND`/`OR`/`NEAR` etc. in the input become
//     literal phrase content and are NOT interpreted as syntax/operators;
//   - the `*` prefix gives incremental search (it matches the beginning of a word).
//
// It returns ok=false when no searchable term is left — the caller then does not even
// query the database (empty local result, no error).
func sanitizarMatchFTS(termo string) (string, bool) {
	limpo := strings.ReplaceAll(termo, `"`, " ")
	campos := strings.Fields(limpo)
	frases := make([]string, 0, len(campos))
	for _, c := range campos {
		if !temAlfanumerico(c) {
			continue
		}
		frases = append(frases, `"`+c+`"*`)
	}
	if len(frases) == 0 {
		return "", false
	}
	return strings.Join(frases, " "), true
}

// temAlfanumerico says whether the string has at least one unicode letter or digit —
// it guarantees the generated FTS5 phrase is not empty (it avoids a syntax error).
func temAlfanumerico(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// ── external cache ─────────────────────────────────────────────────────────────

// cacheExterno is an in-memory cache, protected by a mutex, of the external result
// per key (query+type), with TTL expiration. Simple on purpose (low volume,
// self-hosted).
type cacheExterno struct {
	mu    sync.Mutex
	ttl   time.Duration
	itens map[string]entradaCacheExterno
	agora func() time.Time
}

// entradaCacheExterno is a cache entry: the results and the expiration
// instant.
type entradaCacheExterno struct {
	resultados []provider.ResultadoExterno
	expira     time.Time
}

// novoCacheExterno creates a cache with the given TTL.
func novoCacheExterno(ttl time.Duration) *cacheExterno {
	return &cacheExterno{ttl: ttl, itens: make(map[string]entradaCacheExterno), agora: time.Now}
}

// obter returns the cached results and true when the key exists and has not
// expired; otherwise (nil, false).
func (c *cacheExterno) obter(chave string) ([]provider.ResultadoExterno, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.itens[chave]
	if !ok || c.agora().After(e.expira) {
		return nil, false
	}
	return e.resultados, true
}

// guardar writes (or overwrites) the results of the key with a new expiration.
func (c *cacheExterno) guardar(chave string, res []provider.ResultadoExterno) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.itens[chave] = entradaCacheExterno{resultados: res, expira: c.agora().Add(c.ttl)}
}
