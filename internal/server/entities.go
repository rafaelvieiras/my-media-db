package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
)

// This file implements the READ surface of /api/v1/entities (ADR-0010):
// paginated list with derived ownership, resolved detail, files with
// mediainfo and the service that serves the poster binary. ALL queries go
// through the READ pool — nothing here writes. The /{id} responses already
// carry the final resolver shape; later phases only fill in more layers.

// Pagination limits: a generous default for the UI and a ceiling so a client
// cannot ask for the whole library at once.
const (
	limitPadrao = 50
	limitMaximo = 200
)

// entityCardDTO is one item of the list (GET /api/v1/entities): the card
// fields plus derived ownership, the file count, the match confidence (review
// badge) and the poster link (only when there is local artwork).
type entityCardDTO struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	Owned           bool     `json:"owned"`
	FileCount       int64    `json:"file_count"`
	MatchConfidence *float64 `json:"match_confidence"`
	PosterURL       *string  `json:"poster_url"`
	// Ownership is the AGGREGATED ownership of the card when the type has
	// aggregation in the series tree: `season` (direct episodes) and optionally
	// `tv` (sum across seasons). Annotated on the server after listing in
	// getEntityMembers — it does NOT come from the card's SQL query (same
	// boundary as Ancestors). `omitempty`: absent (null in the UI) for
	// movie/episode/collection, which use the simple ownership of `owned`.
	Ownership *resolver.OwnershipInfo `json:"ownership,omitempty"`
	// Numbering is the REAL numbering of the episode: `season`/`number`
	// projected onto the ordering of the parent season (read from the episode's
	// extension_data core.tv@1) plus the `absolute_number` when present.
	// Annotated on the server in montarMembros — like Ownership/Ancestors,
	// outside the card query. `omitempty`: absent for every type that is not
	// `episode` and for an episode without numbering (draft) — the UI falls back
	// to the neutral label, never inventing a position. Replaces the old ordinal
	// position of the card.
	Numbering *episodeNumberingDTO `json:"numbering,omitempty"`
	// ReviewGroup is the GROUP of the review queue: kind+key+label derived from
	// the entity's `media_file.path`. Annotated on the server ONLY in
	// GET /api/v1/review (like Ownership/Numbering, outside the card query).
	// `omitempty`: absent in every other card listing.
	ReviewGroup *reviewGroupDTO `json:"review_group,omitempty"`
	// StillURL is the RAW URL of the episode still (16:9 frame), read from
	// extension_data core.tv@1 (still_url) — language-NEUTRAL artwork, written at
	// import without downloading the image. The front end serves it through the
	// image proxy (fetch+cache on demand). Annotated on the server together with
	// Numbering (same extension_data read, no extra queries). `omitempty`: absent
	// for non-`episode` and for an episode without a still — the UI falls back to
	// the placeholder.
	StillURL *string `json:"still_url,omitempty"`
	// CatalogCompleteness is the card's CATALOG completeness against the
	// /settings ruler, read from the persisted cache entity_catalog_completeness
	// (LEFT JOIN in the card queries) -- recalculated in batch by the
	// saude_biblioteca job, NOT live (see the long note in
	// completeness.go/completeness_recalc.go about why). `null` when the item has
	// not been evaluated yet (new item or draft, with no row in the cache).
	CatalogCompleteness *resultadoCompletude `json:"catalog_completeness"`
}

// episodeNumberingDTO is the real numbering of an episode in the card:
// `season` and `number` in the parent season's ordering; `absolute_number`
// (optional) when the source reported the absolute number (useful for
// anime/continuous ordering). Distinct from the POSITION in the list — it
// reflects the cataloged number, even with gaps in the library.
type episodeNumberingDTO struct {
	Season         int  `json:"season"`
	Number         int  `json:"number"`
	AbsoluteNumber *int `json:"absolute_number,omitempty"`
}

// entityListDTO is the paginated response of GET /api/v1/entities.
type entityListDTO struct {
	Items  []entityCardDTO `json:"items"`
	Total  int64           `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// mediaFileDTO is one file in GET /api/v1/entities/{id}/files. Mediainfo is
// the JSON already extracted by the scanner (ffprobe), passed through raw —
// `null` when absent.
type mediaFileDTO struct {
	ID        string          `json:"id"`
	Path      string          `json:"path"`
	Size      *int64          `json:"size"`
	Edition   *string         `json:"edition"`
	PartNo    *int64          `json:"part_no"`
	State     string          `json:"state"`
	Mediainfo json.RawMessage `json:"mediainfo"`
}

// filesListDTO is the response of GET /api/v1/entities/{id}/files.
type filesListDTO struct {
	Items []mediaFileDTO `json:"items"`
}

// entitiesUnavailable responds 503 when no database is injected (route test
// scenarios). Mirrors libraryUnavailable — protects against a nil deref.
func (s *Server) entitiesUnavailable(w http.ResponseWriter) bool {
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "db_unavailable",
			"persistência indisponível")
		return true
	}
	return false
}

// leitura returns the Queries bound to the READ POOL (ADR-0010). Every route
// in this file uses only this connection — no writes.
func (s *Server) leitura() *sqlcgen.Queries { return sqlcgen.New(s.db.Read) }

// posterPath builds the (relative) reference of the endpoint that serves the
// entity's local poster. Single source of the template — used by the list and
// the detail.
func posterPath(id string) string {
	return "/api/v1/entities/" + id + "/assets/poster"
}

// fanartPath builds the reference of the endpoint that serves the entity's
// local BACKGROUND (fanart/backdrop) — the poster's analogue for the rich
// detail hero.
func fanartPath(id string) string {
	return "/api/v1/entities/" + id + "/assets/fanart"
}

// stillPath builds the reference of the endpoint that serves the custom STILL
// (16:9 frame) of the EPISODE — the poster/fanart analogue for the owner's
// `user` still.
func stillPath(id string) string {
	return "/api/v1/entities/" + id + "/assets/still"
}

// artesLocais returns the local routes of the poster and the BACKGROUND when
// there is a servable asset of that kind (precedence preferred→user→external);
// nil when there is none. Single source so the resolver knows the entity has
// local artwork to serve.
func artesLocais(assets []sqlcgen.Asset, id string) (poster, backdrop *string) {
	if _, ok := escolherAssetServido(assets, kindPoster); ok {
		u := posterPath(id)
		poster = &u
	}
	if _, ok := escolherAssetServido(assets, kindFanart); ok {
		u := fanartPath(id)
		backdrop = &u
	}
	return poster, backdrop
}

// listEntities handles GET /api/v1/entities: paginated list with optional
// filters (type, owned), ordering by title (sort=title|-title) and the derived
// ownership/count/confidence in a SINGLE query (no N+1).
func (s *Server) listEntities(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	q := r.URL.Query()

	limit, offset, ok := parsePaginacao(w, q)
	if !ok {
		return
	}
	tipoFiltro := parseTipoFiltro(q)
	ownedFiltro, ok := parseOwnedFiltro(w, q)
	if !ok {
		return
	}
	modo, ok := parseSort(w, q)
	if !ok {
		return
	}

	ctx := r.Context()
	// Validates the ?lang= against the allowed ones (400 when outside). The list
	// cards serve the NEUTRAL (file) title — per-language resolution belongs to
	// the detail (/entities/{id}); here lang is only validated for a coherent
	// contract.
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if _, ok := s.parseLang(w, cfg, q.Get("lang")); !ok {
		return
	}
	total, err := s.leitura().ContarEntityCards(ctx, sqlcgen.ContarEntityCardsParams{
		Type:  tipoFiltro,
		Owned: ownedFiltro,
	})
	if err != nil {
		respondErroInterno(w)
		return
	}

	rows, err := s.listarCards(ctx, tipoFiltro, ownedFiltro, int64(limit), int64(offset), modo)
	if err != nil {
		respondErroInterno(w)
		return
	}

	items := make([]entityCardDTO, 0, len(rows))
	for _, row := range rows {
		card := cardDTO(row)
		// SHALLOW ownership by type: season→episodes, tv→seasons. Annotated after
		// listing (the card's SQL query does not change); degrades to null on
		// failure.
		s.anotarPosseCard(ctx, &card)
		items = append(items, card)
	}
	writeJSON(w, http.StatusOK, entityListDTO{Items: items, Total: total, Limit: limit, Offset: offset})
}

// listarCards chooses the query by sorting mode (sqlc cannot bind a parameter
// inside ORDER BY, so there is one query per mode — title asc, title desc, or
// created_at desc) and normalizes the result to the same row type (the three
// queries have exactly the same columns, in the same order — a direct struct
// conversion).
func (s *Server) listarCards(ctx context.Context, tipo, owned any, limit, offset int64, modo sortModo) ([]sqlcgen.ListarEntityCardsRow, error) {
	switch modo {
	case sortTituloDesc:
		rows, err := s.leitura().ListarEntityCardsDesc(ctx, sqlcgen.ListarEntityCardsDescParams{
			Type: tipo, Owned: owned, Limit: limit, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]sqlcgen.ListarEntityCardsRow, len(rows))
		for i, r := range rows {
			out[i] = sqlcgen.ListarEntityCardsRow(r)
		}
		return out, nil
	case sortCriacaoDesc:
		rows, err := s.leitura().ListarEntityCardsRecentes(ctx, sqlcgen.ListarEntityCardsRecentesParams{
			Type: tipo, Owned: owned, Limit: limit, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]sqlcgen.ListarEntityCardsRow, len(rows))
		for i, r := range rows {
			out[i] = sqlcgen.ListarEntityCardsRow(r)
		}
		return out, nil
	default:
		return s.leitura().ListarEntityCards(ctx, sqlcgen.ListarEntityCardsParams{
			Type: tipo, Owned: owned, Limit: limit, Offset: offset,
		})
	}
}

// cardDTO converts a card query row into the API DTO, building the poster_url
// only when there is local artwork.
func cardDTO(row sqlcgen.ListarEntityCardsRow) entityCardDTO {
	dto := entityCardDTO{
		ID:                  row.ID,
		Type:                row.Type,
		Title:               row.Title,
		Status:              row.Status,
		Owned:               row.Owned,
		FileCount:           row.FileCount,
		MatchConfidence:     floatPtr(row.MatchConfidence),
		CatalogCompleteness: completudeDoCache(row.CompletenessComplete, row.CompletenessMissingJson),
	}
	if row.HasPoster {
		u := posterPath(row.ID)
		dto.PosterURL = &u
	}
	return dto
}

// entityTypeDTO is one item of GET /api/v1/entities/types: an entity `type`
// with at least 1 cataloged item and the corresponding count.
type entityTypeDTO struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// tiposSubNo is the SINGLE registry (ADR-0030) of the entity `type` values
// that are SUB-NODES: reachable only through the parent's drill-down
// (season/episode of the series, track of the album, album of the artist) —
// NEVER top-level (home/navbar/`/library`/default search). It is an EXCLUSION
// list, not an inclusion one: a new schema (an unprecedented `entity.type`) is
// born "top-level" by default without needing an entry here — only explicit
// sub-nodes go into this list. If the catalog grows to the point where
// sub-nodes become the most common case, flipping to an inclusion list is a
// localized change right here.
//
// Single source consumed by two places that used to reimplement the same
// exclusion separately and drifted apart (neither covered album/faixa, found in
// production): ListarTiposComContagem (GET /entities/types, below) and
// buscarCardsFTSSQL (GET /search, search.go).
var tiposSubNo = []string{tipoEntTemporada, tipoEntEpisodio, tipoEntAlbum, tipoEntFaixa}

// listEntityTypes handles GET /api/v1/entities/types: the distinct `type`
// values with at least 1 CATALOGED entity (status != 'draft' — see the long
// comment of ListarTiposComContagem in entity.sql about why this criterion, and
// not "owned"/physical file ownership, was chosen). It feeds the home
// (carousels of "recently added" per schema) and the navbar dropdown without
// hardcoding a fixed list of types in the frontend — it excludes the sub-nodes
// of tiposSubNo (season/episode/album/faixa) together with movie/tv, without
// requiring a change here for every new vertical. STATIC route registered in
// server.go before /entities/{id} (chi's radix tree resolves this even without
// the order, but the position makes the intent explicit — same pattern as
// /library/browse).
func (s *Server) listEntityTypes(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	rows, err := s.leitura().ListarTiposComContagem(r.Context(), tiposSubNo)
	if err != nil {
		respondErroInterno(w)
		return
	}
	out := make([]entityTypeDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, entityTypeDTO{Type: row.Type, Count: row.Count})
	}
	writeJSON(w, http.StatusOK, out)
}

// kindColecao is the `kind` of the entity_relation that links a collection to
// its movies. Mirrors relacaoColecao of the enrich service (distinct sources,
// same value — the link is born there and read here).
const kindColecao = "collection"

// Entity-parent types and the relation kinds of their children in the series
// tree (ADR-0021). The member read picks the kind and the ORDERING from the
// parent type: a collection orders by YEAR; series/season follow the sort_index
// (season/episode number), the order ListarRelacoesFilhas already returns.
const (
	tipoEntSerie     = "tv"
	tipoEntTemporada = "season"
	kindTemporada    = "tv_season"
	kindEpisodio     = "tv_episode"
	// tipoEntAlbum/tipoEntArtista: music vertical (ADR-0027). album → track via
	// musicext.KindAlbum (sort_index = track_number); artist → album/track via
	// musicext.KindArtistaPrincipal (main billing — it includes tracks with a
	// diverging credit, filtered in the front end so the discography grid shows
	// only albums, see discografia-artista.tsx).
	tipoEntAlbum   = "album"
	tipoEntArtista = "artista"
	// tipoEntFaixa is the `type` of the track (music vertical, ADR-0027):
	// sub-node of the album, reachable only through drill-down (never top-level —
	// see tiposSubNo).
	tipoEntFaixa = "faixa"
	// nsTVCore is the extension_data namespace of the series vertical
	// (core.tv@1): the season stores `ordering`/`season_number`, the episode
	// stores `numbering` (order→{season,number}) + `absolute_number`. Read here
	// for the card numbering; the WRITE is born in enrich (same value, distinct
	// sources).
	nsTVCore = "core.tv@1"
	// orderingAbsoluto is the VIRTUAL order of the selector that numbers
	// episodes by `absolute_number` (continuous across seasons, anime), in a
	// single group — it is not a `numbering` key, it is derived from the presence
	// of absolute_number.
	orderingAbsoluto = "absolute"
)

// membroCard carries the already-built card + the member's release_date, only
// to sort by year without leaking the field into the DTO (the member card has
// the SAME shape as the list card).
type membroCard struct {
	dto entityCardDTO
	ano sql.NullString
}

// getEntityMembers handles GET /api/v1/entities/{id}/members: the MEMBERS of a
// collection as cards (the SAME shape as GET /entities), ordered by YEAR
// ascending (items without a year last). `[]` when the entity is not a
// collection or has no members; 404 when the entity does not exist. `?lang=` is
// accepted for parity, but the card uses the BASE title (like the list) —
// per-language localization of the card is a follow-up.
func (s *Server) getEntityMembers(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}

	out, err := s.montarMembros(ctx, id, ent.Type)
	if err != nil {
		respondErroInterno(w)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// montarMembros loads the CHILDREN of an entity-parent as cards (the SAME
// shape as GET /entities). The relation kind and the ORDERING depend on the
// parent type (kindEOrdemMembros): collection → members by YEAR; series →
// seasons in sort_index order; season → episodes numbered in sort_index order
// (the order ListarRelacoesFilhas already returns). `[]` when the entity has no
// children for that relation. It is the single path reused by the read
// (getEntityMembers) and by the Phase B mutations (add/removeMember). It skips
// a child purged in the middle (NoRows).
func (s *Server) montarMembros(ctx context.Context, id, tipoPai string) ([]entityCardDTO, error) {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, id)
	if err != nil {
		return nil, err
	}
	kind, ordenarPorAno := kindEOrdemMembros(tipoPai)
	// When the parent is a season, episodes get the REAL numbering projected
	// onto the ordering of the season ITSELF — read once from its extension_data.
	ordem, seasonNumber := s.ordenacaoDaTemporada(ctx, id, tipoPai)
	membros := make([]membroCard, 0, len(rels))
	for _, rel := range rels {
		if rel.Kind != kind {
			continue
		}
		row, err := s.leitura().ObterEntityCard(ctx, rel.ChildID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // child purged in the middle: skip it
			}
			return nil, err
		}
		card := cardDTODeMembro(row)
		s.anotarPosseCard(ctx, &card)
		s.anotarNumbering(ctx, &card, ordem, seasonNumber)
		membros = append(membros, membroCard{dto: card, ano: row.ReleaseDate})
	}
	// Series/season preserve the sort_index order (number); only a collection is
	// reordered by year — episodes must stay 1,2,3 and not follow air date.
	if ordenarPorAno {
		sort.SliceStable(membros, func(i, j int) bool { return anoAntes(membros[i].ano, membros[j].ano) })
	}

	out := make([]entityCardDTO, len(membros))
	for i, m := range membros {
		out[i] = m.dto
	}
	return out, nil
}

// ordenacaoDaTemporada returns the default ordering (`ordering`) and the
// `season_number` of the parent season, read from its extension_data
// (core.tv@1, written at import by extensaoTemporada). It only makes sense when
// the parent is `season`; for the other types it returns ("",0) and the
// numbering is not annotated. It degrades silently ("",0) if the season does not
// exist or has no namespace — the card falls back to the neutral label.
func (s *Server) ordenacaoDaTemporada(ctx context.Context, seasonID, tipoPai string) (ordering string, seasonNumber int) {
	if tipoPai != tipoEntTemporada {
		return "", 0
	}
	ent, err := s.leitura().ObterEntity(ctx, seasonID)
	if err != nil {
		return "", 0
	}
	ns := tvext.Namespace(ent.ExtensionDataJson, nsTVCore)
	if ns == nil {
		return "", 0
	}
	if o, ok := ns["ordering"].(string); ok {
		ordering = o
	}
	if n, ok := tvext.AsInt(ns["season_number"]); ok {
		seasonNumber = n
	}
	return ordering, seasonNumber
}

// anotarNumbering fills card.Numbering (REAL numbering of the episode) and
// card.StillURL (16:9 frame) from the SAME extension_data of the episode: a
// single read serves both (the still rode along on the numbering walk, without
// multiplying queries). It only acts on `episode` cards (an extra read per
// episode, a scale of dozens per season, acceptable; TODO: a single aggregated
// query if it becomes heavy). It degrades on any failure (the UI omits
// number/still, never invents).
func (s *Server) anotarNumbering(ctx context.Context, card *entityCardDTO, ordering string, seasonNumber int) {
	if card.Type != tipoEntEpisodio {
		return
	}
	ent, err := s.leitura().ObterEntity(ctx, card.ID)
	if err != nil {
		return
	}
	ns := tvext.Namespace(ent.ExtensionDataJson, nsTVCore)
	card.Numbering = projetarNumbering(ns, ordering, seasonNumber)
	// Still precedence: the custom still (asset `user`) beats the provider's raw
	// one. An extra read per episode (acceptable, same scale as the numbering).
	assets, _ := s.leitura().ListarAssetsPorEntity(ctx, card.ID)
	card.StillURL = stillDoEpisodio(assets, card.ID, ns)
}

// stillDoNS extracts the episode's RAW still URL from the core.tv@1 namespace:
// `still_url` when it is a non-empty string, otherwise nil (episode without a
// still → placeholder in the UI). The SINGLE point reused by the card
// (/members and /episodes) and by the episode detail.
func stillDoNS(ns map[string]any) *string {
	if ns == nil {
		return nil
	}
	if u, ok := ns["still_url"].(string); ok && u != "" {
		return &u
	}
	return nil
}

// projetarNumbering is the PURE projection of the episode numbering onto the
// ordering of the parent season. Preference: the STRICT entry
// `numbering[ordering]` (via entradaNumbering). Fallback: with no entry for the
// ordering but with an `absolute_number`, it uses the absolute value as the
// number (Season = the parent season's). With neither (draft) → nil (neutral
// label in the UI). The fallback to the absolute is what distinguishes this from
// the selector projection, which groups by STRICT order.
func projetarNumbering(ns map[string]any, ordering string, seasonNumber int) *episodeNumberingDTO {
	if dto := entradaNumbering(ns, ordering); dto != nil {
		return dto
	}
	if ns == nil {
		return nil
	}
	if a, ok := tvext.AsInt(ns["absolute_number"]); ok {
		return &episodeNumberingDTO{Season: seasonNumber, Number: a}
	}
	return nil
}

// entradaNumbering extracts the STRICT numbering of the episode for an
// ordering: the entry `numbering[ordering]` → {season,number} (+ the absolute
// when present). nil when there is NO entry for that order — no fallback to the
// absolute (the caller decides what to do with an episode "without numbering in
// that order"). It is the core reused by the projection (with fallback) and by
// the selector (strict).
func entradaNumbering(ns map[string]any, ordering string) *episodeNumberingDTO {
	if ns == nil {
		return nil
	}
	var abs *int
	if a, ok := tvext.AsInt(ns["absolute_number"]); ok {
		abs = &a
	}
	numbering, ok := ns["numbering"].(map[string]any)
	if !ok {
		return nil
	}
	par, ok := numbering[ordering].(map[string]any)
	if !ok {
		return nil
	}
	season, sOK := tvext.AsInt(par["season"])
	number, nOK := tvext.AsInt(par["number"])
	if !sOK || !nOK {
		return nil
	}
	return &episodeNumberingDTO{Season: season, Number: number, AbsoluteNumber: abs}
}

// numeracaoAbsoluta projects the episode onto the ABSOLUTE order: number =
// absolute_number, with no season (Season 0 → a single continuous group). nil
// when the episode has no absolute_number (it falls into the "No numbering"
// group).
func numeracaoAbsoluta(ns map[string]any) *episodeNumberingDTO {
	if ns == nil {
		return nil
	}
	a, ok := tvext.AsInt(ns["absolute_number"])
	if !ok {
		return nil
	}
	return &episodeNumberingDTO{Season: 0, Number: a, AbsoluteNumber: &a}
}

// projetarParaOrdem chooses the episode projection according to the requested
// order: the ABSOLUTE one uses numeracaoAbsoluta (single group); any other uses
// the STRICT entry `numbering[ordering]`. In both cases nil = episode "without
// numbering in this order".
func projetarParaOrdem(ns map[string]any, ordering string) *episodeNumberingDTO {
	if ordering == orderingAbsoluto {
		return numeracaoAbsoluta(ns)
	}
	return entradaNumbering(ns, ordering)
}

// kindEOrdemMembros chooses the relation kind of the children and whether they
// are reordered by YEAR, according to the parent type: series → seasons
// (sort_index); season → episodes (sort_index); album → tracks (sort_index =
// track_number); artist → discography by year (same logic as a collection); any
// other (collection) → members by year. The default preserves the collection
// behavior for the callers that are not part of the series/music tree.
func kindEOrdemMembros(tipoPai string) (kind string, ordenarPorAno bool) {
	switch tipoPai {
	case tipoEntSerie:
		return kindTemporada, false
	case tipoEntTemporada:
		return kindEpisodio, false
	case tipoEntAlbum:
		return musicext.KindAlbum, false
	case tipoEntArtista:
		return musicext.KindArtistaPrincipal, true
	default:
		return kindColecao, true
	}
}

// cardDTODeMembro reuses the list's cardDTO from the ObterEntityCard row (same
// derived columns + release_date), discarding release_date (used only to
// sort).
func cardDTODeMembro(row sqlcgen.ObterEntityCardRow) entityCardDTO {
	return cardDTO(sqlcgen.ListarEntityCardsRow{
		ID:                      row.ID,
		Type:                    row.Type,
		Title:                   row.Title,
		Status:                  row.Status,
		Owned:                   row.Owned,
		FileCount:               row.FileCount,
		MatchConfidence:         row.MatchConfidence,
		HasPoster:               row.HasPoster,
		CompletenessComplete:    row.CompletenessComplete,
		CompletenessMissingJson: row.CompletenessMissingJson,
	})
}

// anotarPosseCard fills the card with the SHALLOW ownership (1 level) — "how
// much of the level below do I own" — when the type has aggregation in the
// series tree:
//   - `season`: direct episodes with a file / total episodes.
//   - `tv`: seasons WITH content / total seasons — NOT the deep sum of
//     episodes. That keeps the card always shallow and consistent (glanceable),
//     while the DETAIL (ResolvedEntity/montarPosseAgregada) remains DEEP
//     (episodes, e.g. 2/875) and does not change.
//
// Every read degrades to nil on any failure: the UI just omits the badge, it
// NEVER takes the list down. The remaining types (movie/episode/collection)
// stay without ownership.
func (s *Server) anotarPosseCard(ctx context.Context, card *entityCardDTO) {
	switch card.Type {
	case tipoEntTemporada:
		if info, err := s.posseDaTemporada(ctx, card.ID); err == nil {
			card.Ownership = info
		}
	case tipoEntSerie:
		card.Ownership = s.posseRasaDaSerie(ctx, card.ID)
	}
}

// posseRasaDaSerie computes the SHALLOW ownership of the SERIES card: total =
// number of tv_season children; owned = how many of those seasons have at least
// one episode in the library (posseDaTemporada().owned > 0). It is the
// GLANCEABLE count of the card ("X/Y seasons"), distinct from the DEEP
// ownership of the detail (episodes), which stays in montarPosseAgregada and
// does NOT change. Cost ~1 + number of seasons reads (self-hosted; follow-up: a
// single aggregated query if the library grows). Degrades to nil on any
// failure.
func (s *Server) posseRasaDaSerie(ctx context.Context, serieID string) *resolver.OwnershipInfo {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		return nil
	}
	total, owned := 0, 0
	for _, rel := range rels {
		if rel.Kind != kindTemporada {
			continue
		}
		total++
		info, err := s.posseDaTemporada(ctx, rel.ChildID)
		if err != nil {
			return nil
		}
		if info.OwnedCount > 0 {
			owned++
		}
	}
	return &resolver.OwnershipInfo{OwnedCount: owned, TotalCount: total}
}

// anoAntes sorts by ISO release_date ascending (the ISO string sorts
// chronologically); missing/empty dates go to the END.
func anoAntes(a, b sql.NullString) bool {
	av := a.Valid && a.String != ""
	bv := b.Valid && b.String != ""
	if av != bv {
		return av
	}
	return a.String < b.String
}

// getEntity handles GET /api/v1/entities/{id}: it returns the RESOLVED entity
// (all layers: file ⟵ external ⟵ override) or 404.
func (s *Server) getEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}

	// Default DISPLAY language: without an explicit `?lang=` in the querystring,
	// the default becomes the preferred language of the item's origin library
	// (which already falls back to the global `primary` when there is no
	// preference, or in case of ambiguity -- s.idiomaDeBusca). A real error while
	// resolving this degrades to the global `primary` (best-effort, it is only the
	// DEFAULT -- it should never break the response because of this). An explicit
	// `?lang=` ALWAYS wins, no exception (unchanged behavior: validated against
	// the allowed ones via parseLang).
	rawLang := r.URL.Query().Get("lang")
	var lang string
	if rawLang == "" {
		lang, err = s.idiomaDeBusca(ctx, id)
		if err != nil {
			lang = cfg.Primary
		}
	} else {
		var ok bool
		lang, ok = s.parseLang(w, cfg, rawLang)
		if !ok {
			return
		}
	}

	resolvida, encontrada, err := s.resolverEntity(ctx, id, lang, true)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, resolvida)
}

// resolverEntity builds the RESOLVED response of an entity from ALL layers
// (file ⟵ external ⟵ override) — the single path reused by getEntity, enrich
// and import. It returns found=false (without an error) when the entity does not
// exist. This is WHERE the external layer becomes live end to end (the wiring
// deferred in ADR-0013). proxiarImagens controls whether s.proxyarCreditos
// rewrites the image URLs of credits (cast/crew/companies) to the internal proxy
// route (/api/v1/images?src=...) — meant for the web frontend to decouple from
// the source's CDN. When false (writing .nfo), credits keep the provider's
// DIRECT URL (e.g. TMDB), which is what a Kodi/Jellyfin reading the .nfo from
// disk needs.
func (s *Server) resolverEntity(ctx context.Context, id, lang string, proxiarImagens bool) (resolver.Entidade, bool, error) {
	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resolver.Entidade{}, false, nil
		}
		return resolver.Entidade{}, false, err
	}

	// LANGUAGE context: resolves the effective lang (default = primary) and
	// derives the fallback chain from the settings + the entity's
	// original_language anchor.
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		return resolver.Entidade{}, false, err
	}
	if lang == "" {
		lang = cfg.Primary
	}
	cadeia := cfg.cadeiaDeIdiomas(lang, ent.OriginalLanguage)

	files, err := s.listarFilesDaEntity(ctx, ent)
	if err != nil {
		return resolver.Entidade{}, false, err
	}

	// Local artwork: poster and BACKGROUND (fanart/backdrop) are served by local
	// routes when an asset exists (the same preferred→user→external precedence as
	// the service).
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		return resolver.Entidade{}, false, err
	}
	posterURL, backdropURL := artesLocais(assets, id)

	// OVERRIDE layer: the owner's raw patch (media_override). A missing row is
	// not an error (no override, inherits the base).
	override, err := s.carregarOverride(ctx, id)
	if err != nil {
		return resolver.Entidade{}, false, err
	}

	// EXTERNAL layer: maps each raw media_external row to the canonical layer
	// via the provider, ordered by the priority of the sources.
	externo, err := s.montarExterno(ctx, id)
	if err != nil {
		return resolver.Entidade{}, false, err
	}

	resolvida := resolver.Resolver(resolver.Camadas{
		Entity:          ent,
		Files:           files,
		PosterURL:       posterURL,
		BackdropURL:     backdropURL,
		Override:        override,
		Externo:         externo,
		LangPedido:      lang,
		Cadeia:          cadeia,
		IdiomaPrincipal: cfg.Primary,
	})
	s.anotarInternos(ctx, &resolvida)
	// Interno-first for credits: the source that fed cast/crew is not
	// necessarily the same (each field wins in the fusion by priority — R2), so
	// the source is discovered per field instead of assuming the entity's
	// highest-priority source.
	s.anotarCreditosInternos(ctx, fonteDoCampoExterno(externo, "cast"), resolvida.Cast, nil)
	s.anotarCreditosInternos(ctx, fonteDoCampoExterno(externo, "crew"), nil, resolvida.Crew)
	if proxiarImagens {
		s.proxyarCreditos(resolvida.Cast, resolvida.Crew, resolvida.Companies)
	}
	s.completarOwnedViaArvore(ctx, &resolvida, id)
	resolvida.Ancestors = s.montarAncestrais(ctx, id)
	resolvida.Ownership = s.montarPosseAgregada(ctx, ent)
	resolvida.Orderings = s.montarOrderings(ctx, ent)
	resolvida.StillURL = stillDaEntity(assets, ent)
	s.herdarFanartTemporada(ctx, &resolvida, assets, ent)
	s.anotarCompletudeCatalogacao(ctx, &resolvida, ent, assets)
	return resolvida, true, nil
}

// completarOwnedViaArvore completes the Owned of the single detail when the
// resolver (via listarFilesDaEntity/temPosse) gave it as false: listarFilesDaEntity
// only looks at media_file.entity_id DIRECTLY (or the media_file_entity join,
// for the episode) — it never walks down the entity_relation tree. For music
// artist/album (ADR-0027: only the TRACK, two levels below, links media_file),
// that made Owned ALWAYS false in the single detail, even though it was already
// fixed in the listings (entity.sql, recursive CTE). It only queries the
// database again when the resolver found no direct ownership (extra cost
// restricted to the case that needs it); a read error degrades silently (keeps
// the Owned the resolver already gave).
func (s *Server) completarOwnedViaArvore(ctx context.Context, e *resolver.Entidade, id string) {
	if e.Owned {
		return
	}
	if arv, err := s.leitura().EntityTemPosseNaArvore(ctx, id); err == nil && arv {
		e.Owned = true
	}
}

// anotarCompletudeCatalogacao fills resolvida.CatalogCompleteness LIVE, on every
// detail request: zero new queries, just a loop over booleans and the list of
// assets already in memory (resolverEntity already pays the cost of assembling
// everything else, with or without completeness). The field stays `nil` for a
// draft item (ehDraft, completeness.go) -- there is no point in evaluating
// metadata that does not exist yet. `assets` is the SAME list resolverEntity
// already loaded (ListarAssetsPorEntity), reused here with no new query. A
// failure to read the completeness config degrades to `nil` (same spirit as the
// other post-resolution annotations: never take the detail down for an auxiliary
// step).
func (s *Server) anotarCompletudeCatalogacao(ctx context.Context, e *resolver.Entidade, ent sqlcgen.Entity, assets []sqlcgen.Asset) {
	if ehDraft(ent.Status) {
		return
	}
	cfg, err := s.obterCatalogCompletenessSettings(ctx)
	if err != nil {
		return
	}
	tipoCfg, achou := cfg[ent.Type]
	dados := dadosCompletudeDeEntidade(*e, assets)
	resultado := avaliarCompletude(tipoCfg, achou, dados)
	e.CatalogCompleteness = &resultado
}

// herdarFanartTemporada fills the BackdropURL of a SEASON without its own fanart
// with the fanart of the PARENT SERIES (DISPLAY inheritance, like
// tinyMediaManager): it invents no data and downloads nothing — it reuses the
// series asset already downloaded. No-op for types != season and for a season
// that already has its own servable `fanart` asset (in that case artesLocais
// already filled BackdropURL with it). Annotated on the server, outside the pure
// resolver (same boundary as Ancestors/Ownership/still).
func (s *Server) herdarFanartTemporada(ctx context.Context, e *resolver.Entidade, assets []sqlcgen.Asset, ent sqlcgen.Entity) {
	if ent.Type != tipoEntTemporada {
		return
	}
	if _, ok := escolherAssetServido(assets, kindFanart); ok {
		return // it has fanart of its own: nothing to inherit
	}
	if serieID, ok := s.fanartDaSeriePai(ctx, ent.ID); ok {
		u := fanartPath(serieID)
		e.BackdropURL = &u
	}
}

// fanartDaSeriePai walks up from the season to the PARENT SERIES (via
// ListarRelacoesPais — the season is a child of the series) and returns the
// series id when it has a SERVABLE `fanart` asset. false when there is no
// parent, the parent has no servable fanart, or on a read failure (degrades to
// "no inheritance", never invents). It downloads nothing.
func (s *Server) fanartDaSeriePai(ctx context.Context, seasonID string) (string, bool) {
	pais, err := s.leitura().ListarRelacoesPais(ctx, seasonID)
	if err != nil || len(pais) == 0 {
		return "", false
	}
	serieID := pais[0].ParentID
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, serieID)
	if err != nil {
		return "", false
	}
	if _, ok := escolherAssetServido(assets, kindFanart); !ok {
		return "", false
	}
	return serieID, true
}

// stillDaEntity returns the episode still URL for the detail, with the still
// PRECEDENCE: a `user` still (local /assets/still route) BEATS the provider's
// raw still_url (extension_data core.tv@1). `episode` only, nil for the other
// types. The resolver is PURE (it does not know core.tv); the annotation lives
// here, on the same boundary as Ancestors/Ownership/Orderings. Without a custom
// still it keeps the raw one (the front end proxies it).
func stillDaEntity(assets []sqlcgen.Asset, ent sqlcgen.Entity) *string {
	if ent.Type != tipoEntEpisodio {
		return nil
	}
	return stillDoEpisodio(assets, ent.ID, tvext.Namespace(ent.ExtensionDataJson, nsTVCore))
}

// stillDoEpisodio applies the still precedence: if there is a SERVABLE `still`
// asset for the episode, it returns the local route (/assets/still); otherwise
// it falls back to the provider's raw still_url (ns). The SINGLE point reused by
// the detail (stillDaEntity) and by the card (anotarNumbering) — the same
// custom→provider rule on both sides.
func stillDoEpisodio(assets []sqlcgen.Asset, id string, ns map[string]any) *string {
	if _, ok := escolherAssetServido(assets, kindStill); ok {
		u := stillPath(id)
		return &u
	}
	return stillDoNS(ns)
}

// tipoEntEpisodio is the `type` of the episode entity (fine grain of the series
// tree, ADR-0021). A series file has `media_file.entity_id` = series, and the
// per-episode links live in media_file_entity (kind='tv_episode'); that is why
// the file area of the EPISODE comes from the join, not from
// media_file.entity_id.
const tipoEntEpisodio = "episode"

// listarFilesDaEntity loads the media_file rows that make up the OWNERSHIP and
// the file list of an entity. For an EPISODE, the files come from the N:M links
// (media_file_entity, kind='tv_episode') via ListarArquivosDaEntidade — because
// the series file points `entity_id` at the SERIES, not at the episode. For the
// other types (movie etc.), the classic media_file.entity_id path.
func (s *Server) listarFilesDaEntity(ctx context.Context, ent sqlcgen.Entity) ([]sqlcgen.MediaFile, error) {
	if ent.Type == tipoEntEpisodio {
		rows, err := s.leitura().ListarArquivosDaEntidade(ctx, sqlcgen.ListarArquivosDaEntidadeParams{
			EntityID: ent.ID,
			Kind:     kindEpisodio,
		})
		if err != nil {
			return nil, err
		}
		files := make([]sqlcgen.MediaFile, len(rows))
		for i, r := range rows {
			files[i] = arquivoDaEntidadeParaMediaFile(r)
		}
		return files, nil
	}
	return s.leitura().ListarMediaFilesPorEntity(ctx, ent.ID)
}

// arquivoDaEntidadeParaMediaFile reprojects the JOIN row
// (ListarArquivosDaEntidade) into the sqlcgen.MediaFile shape, so the SAME
// fileDTO/ownership of the movie can be reused. The `ordinal` (order of the
// episode inside the file, in ranges) does not go into the file DTO.
func arquivoDaEntidadeParaMediaFile(r sqlcgen.ListarArquivosDaEntidadeRow) sqlcgen.MediaFile {
	return sqlcgen.MediaFile{
		ID:              r.ID,
		EntityID:        r.EntityID,
		Path:            r.Path,
		ContentHash:     r.ContentHash,
		Size:            r.Size,
		Mtime:           r.Mtime,
		Edition:         r.Edition,
		PartNo:          r.PartNo,
		MatchConfidence: r.MatchConfidence,
		MediainfoJson:   r.MediainfoJson,
		State:           r.State,
		LastSeen:        r.LastSeen,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
		LibrarySourceID: r.LibrarySourceID,
	}
}

// montarPosseAgregada computes the aggregated ownership of an entity-parent of
// the series tree: it returns nil for types without aggregation (movie, episode,
// collection).
//   - SEASON: total = number of child episodes (kind='tv_episode'); owned = how
//     many of those have at least one linked 'present' file
//     (ContarEntidadesComPosse).
//   - SERIES: sum across seasons — the direct query only counts DIRECT children
//     (which for a series are seasons, with no link of their own), so we iterate
//     the seasons and add up the total/owned of each one. Small volume
//     (self-hosted); N+1 tolerable.
//
// Defensive: any read failure degrades to nil (the UI just omits the badge),
// without taking the detail down — consistent with the post-resolution assembly
// of Ancestors.
func (s *Server) montarPosseAgregada(ctx context.Context, ent sqlcgen.Entity) *resolver.OwnershipInfo {
	switch ent.Type {
	case tipoEntTemporada:
		info, err := s.posseDaTemporada(ctx, ent.ID)
		if err != nil {
			return nil
		}
		return info
	case tipoEntSerie:
		total, owned := 0, 0
		rels, err := s.leitura().ListarRelacoesFilhas(ctx, ent.ID)
		if err != nil {
			return nil
		}
		for _, rel := range rels {
			if rel.Kind != kindTemporada {
				continue
			}
			info, err := s.posseDaTemporada(ctx, rel.ChildID)
			if err != nil {
				return nil
			}
			total += info.TotalCount
			owned += info.OwnedCount
		}
		return &resolver.OwnershipInfo{OwnedCount: owned, TotalCount: total}
	default:
		return nil
	}
}

// posseDaTemporada returns (owned episodes / total) of ONE season: total is the
// number of kind='tv_episode' children; owned comes from ContarEntidadesComPosse
// (children with a linked 'present' file). Reused by the series (sum per
// season).
func (s *Server) posseDaTemporada(ctx context.Context, seasonID string) (*resolver.OwnershipInfo, error) {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, seasonID)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, rel := range rels {
		if rel.Kind == kindEpisodio {
			total++
		}
	}
	owned, err := s.leitura().ContarEntidadesComPosse(ctx, sqlcgen.ContarEntidadesComPosseParams{
		ParentID: seasonID,
		Kind:     kindEpisodio,
	})
	if err != nil {
		return nil, err
	}
	return &resolver.OwnershipInfo{OwnedCount: int(owned), TotalCount: total}, nil
}

// maxProfundidadeAncestrais limits the breadcrumb walk-up so it NEVER loops on a
// defective entity_relation cycle. 8 is roomy: the real tree has at most 2
// levels (episode→season→series).
const maxProfundidadeAncestrais = 8

// montarAncestrais walks up the series tree from `id`, taking the FIRST parent
// of each level (ListarRelacoesPais) up to the root, and returns the trail
// inverted to ROOT→PARENT — the breadcrumb. Each node carries {id, title, type}
// with the BASE title of the row (cheap; resolving each ancestor's
// language-specific title is a follow-up). Always non-nil (`[]` for
// movie/tv/collection, which have no parent). Defensive: it stops at the first
// failure/missing parent and limits the depth (maxProfundidadeAncestrais) so it
// never loops. Built here (server), not in the pure resolver (boundary of
// ADR-0021).
func (s *Server) montarAncestrais(ctx context.Context, id string) []resolver.AncestorRef {
	trilha := make([]resolver.AncestorRef, 0)
	atual := id
	for i := 0; i < maxProfundidadeAncestrais; i++ {
		pais, err := s.leitura().ListarRelacoesPais(ctx, atual)
		if err != nil || len(pais) == 0 {
			break
		}
		pai, err := s.leitura().ObterEntity(ctx, pais[0].ParentID)
		if err != nil {
			break // parent purged in the middle: stop the trail here
		}
		trilha = append(trilha, resolver.AncestorRef{ID: pai.ID, Title: pai.Title, Type: pai.Type})
		atual = pai.ID
	}
	// We collect bottom-up (parent, grandparent, …); invert to ROOT→PARENT.
	for i, j := 0, len(trilha)-1; i < j; i, j = i+1, j-1 {
		trilha[i], trilha[j] = trilha[j], trilha[i]
	}
	return trilha
}

// anotarInternos fills the `internal_id` of the collection and of the similar
// items: the resolver is PURE (it does not touch the database), so the
// external→internal resolution is annotated here, after resolution. The entity's
// collection and similar items come from TMDB (the external link already assumes
// that), so the resolution is by tmdb id. Isolated so as not to bloat
// resolverEntity (gocyclo).
func (s *Server) anotarInternos(ctx context.Context, e *resolver.Entidade) {
	if e.Collection != nil {
		e.Collection.InternalID = s.idInternoColecao(ctx, "tmdb", e.Collection.SourceID)
	}
	for i := range e.Similar {
		e.Similar[i].InternalID = s.idInternoItem(ctx, "tmdb", e.Similar[i].SourceID)
	}
}

// prioridadeFontes is the global precedence order of the external sources
// (R2 / ADR-0002): per field, the FIRST source in the list with a value forms its
// external base; it also dictates the deterministic order of the merge search
// (search.go). It is the SINGLE point of the policy — making it configurable
// (via settings) is a follow-up; for now it is the default.
var prioridadeFontes = []string{"tmdb", "tvdb"}

// codecPara resolves the provider used ONLY to interpret an already cached
// payload (MapearDetalhe/Similares) of a source: it first tries the parse
// registry (codecs, always populated); it falls back to the live search registry
// (providers) when absent -- this covers the MYMEDIADB_FAKE_PROVIDER of the E2E
// tests, which only registers in providers. ADR-0024.
func (s *Server) codecPara(fonte string) (provider.Provider, bool) {
	if p, ok := s.codecs.Get(fonte); ok {
		return p, ok
	}
	return s.providers.Get(fonte)
}

// montarExterno loads the media_external rows of the entity and converts them
// into canonical layers via the provider (raw→canonical belongs to the provider;
// the resolver does not know TMDB). With no provider configured it returns nil
// (original behavior preserved). A row whose mapping fails is SKIPPED (that
// source degrades), it does not take the read down. It orders by the priority of
// the sources.
func (s *Server) montarExterno(ctx context.Context, id string) ([]resolver.CamadaExterna, error) {
	// It only skips the query when NEITHER registry (live search AND parse) has
	// anything -- an optimization preserved only for an installation that never had
	// any provider. With `codecs` always populated by main (ADR-0024), this guard
	// only actually fires in tests that build the Server without any provider
	// option.
	if s.providers.Vazio() && s.codecs.Vazio() {
		return nil, nil
	}
	rows, err := s.leitura().ListarMediaExternalPorEntity(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	var camadas []resolver.CamadaExterna
	for _, fonte := range prioridadeFontes {
		camadas = append(camadas, s.camadasDaFonte(rows, fonte)...)
	}
	return camadas, nil
}

// similaresParaCampo converts the provider's canonical list
// (provider.Similar) to the map format that campos["similar"] has used for a
// long time — []any of map[string]any with the keys richfields.SimilaresDe
// expects (source_id/type/title/year/poster_url). Nil pointers simply omit the
// key (same convention as similaresCanonicos in the TMDB provider).
func similaresParaCampo(sims []provider.Similar) []any {
	out := make([]any, 0, len(sims))
	for _, s := range sims {
		item := map[string]any{
			"source_id": s.SourceID,
			"type":      s.Type,
			"title":     s.Title,
		}
		if s.Year != nil {
			item["year"] = *s.Year
		}
		if s.PosterURL != nil {
			item["poster_url"] = *s.PosterURL
		}
		out = append(out, item)
	}
	return out
}

// preencherSimilares calls the OPTIONAL SimilarProvider capability when the
// provider implements it and writes the result into campos["similar"] in the
// format richfields.SimilaresDe expects. Without the capability it does nothing
// (the "similar" key stays absent, as it is today when the source does not model
// it). A real error from Similares (unreadable payload) is only logged and
// carries on without similar items from that layer — it never takes the whole
// row down.
func preencherSimilares(prov provider.Provider, raw json.RawMessage, fonte, entityID string, campos map[string]any) {
	sp, ok := prov.(provider.SimilarProvider)
	if !ok {
		return
	}
	sims, err := sp.Similares(raw)
	if err != nil {
		log.Printf("entities: similares da fonte %q da entity %q falharam (degrada só os similares): %v", fonte, entityID, err)
		return
	}
	if len(sims) == 0 {
		return
	}
	campos["similar"] = similaresParaCampo(sims)
}

// camadasDaFonte converts the rows of ONE source into canonical layers via THAT
// source's provider (chosen in the registry by name). It returns nil when the
// source has no registered provider (degrades: empty layer). A row with a
// missing/invalid payload is skipped (that row degrades).
func (s *Server) camadasDaFonte(rows []sqlcgen.MediaExternal, fonte string) []resolver.CamadaExterna {
	prov, ok := s.codecPara(fonte)
	if !ok {
		return nil
	}
	var out []resolver.CamadaExterna
	for _, row := range rows {
		if row.Source != fonte || !row.PayloadJson.Valid || row.PayloadJson.String == "" {
			continue
		}
		raw := json.RawMessage(row.PayloadJson.String)
		campos, err := prov.MapearDetalhe(raw)
		if err != nil {
			log.Printf("entities: mapear detalhe da fonte %q da entity %q falhou (degrada esta fonte): %v", row.Source, row.EntityID, err)
			continue // invalid raw payload: this source degrades
		}
		preencherSimilares(prov, raw, row.Source, row.EntityID, campos)
		out = append(out, resolver.CamadaExterna{Fonte: row.Source, Campos: campos})
	}
	return out
}

// carregarOverride reads the entity's override patch (media_override) and
// decodes it into the raw sparse map the resolver expects. With no row
// (sql.ErrNoRows) or with an empty/invalid override_json it returns nil (no
// override, inherits the base) — only a real database error is propagated.
func (s *Server) carregarOverride(ctx context.Context, id string) (map[string]json.RawMessage, error) {
	ov, err := s.leitura().ObterMediaOverride(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !ov.OverrideJson.Valid || ov.OverrideJson.String == "" {
		return nil, nil
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal([]byte(ov.OverrideJson.String), &patch); err != nil {
		// A malformed override degrades to "no override" instead of taking the read
		// down (the editorial/file base remains servable).
		return nil, nil
	}
	return patch, nil
}

// getEntityFiles handles GET /api/v1/entities/{id}/files: it lists the
// media_file rows with the technical facts of the mediainfo. 404 if the entity
// does not exist.
func (s *Server) getEntityFiles(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}

	// Episode: the files come from the N:M links (the file points entity_id at
	// the series); other types: the classic media_file.entity_id path (same helper
	// used in resolution, single source).
	files, err := s.listarFilesDaEntity(ctx, ent)
	if err != nil {
		respondErroInterno(w)
		return
	}
	items := make([]mediaFileDTO, 0, len(files))
	for _, f := range files {
		items = append(items, fileDTO(f))
	}
	writeJSON(w, http.StatusOK, filesListDTO{Items: items})
}

// fileDTO converts a media_file into the file DTO, passing the raw
// mediainfo_json through (or `null`).
func fileDTO(f sqlcgen.MediaFile) mediaFileDTO {
	dto := mediaFileDTO{
		ID:      f.ID,
		Path:    f.Path,
		Size:    ni64Ptr(f.Size),
		Edition: nsPtr(f.Edition),
		PartNo:  ni64Ptr(f.PartNo),
		State:   f.State,
	}
	if f.MediainfoJson.Valid && f.MediainfoJson.String != "" {
		dto.Mediainfo = json.RawMessage(f.MediainfoJson.String)
	}
	return dto
}

// getEntityPoster handles GET /api/v1/entities/{id}/assets/poster: it serves
// the local poster binary with the correct content-type, applying the serving
// PRECEDENCE (preferred → user → external → 404) over the list of assets of kind
// 'poster'. 404 when there is no poster (the UI falls back to the placeholder);
// 403 when the stored path is NOT under a known datasource (PATH-TRAVERSAL
// guard: we never serve an arbitrary path). The owner's choice wins — it also
// applies to what Jellyfin consumes from this endpoint.
func (s *Server) getEntityPoster(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	asset, ok := escolherAssetServido(assets, kindPoster)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "pôster não encontrado")
		return
	}
	s.servirBinarioAsset(w, r, ctx, asset)
}

// getEntityFanart handles GET /api/v1/entities/{id}/assets/fanart: it serves
// the local BACKGROUND (backdrop) binary — the analogue of getEntityPoster for
// the 'fanart' kind, with the SAME serving precedence (preferred → user →
// external → 404) and the same path-traversal guard. 404 when there is no
// background (the UI falls back to the direct URL/placeholder).
func (s *Server) getEntityFanart(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	asset, ok := escolherAssetServido(assets, kindFanart)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "fundo não encontrado")
		return
	}
	s.servirBinarioAsset(w, r, ctx, asset)
}

// getEntityStill handles GET /api/v1/entities/{id}/assets/still: it serves the
// custom STILL (16:9 frame) binary of the episode — the analogue of
// getEntityPoster/Fanart for the 'still' kind, with the SAME serving precedence
// (preferred → user → external → 404) and the same path-traversal guard. 404
// when there is no custom still (the detail falls back to the provider's raw
// still_url).
func (s *Server) getEntityStill(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	assets, err := s.leitura().ListarAssetsPorEntity(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}
	asset, ok := escolherAssetServido(assets, kindStill)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "still não encontrado")
		return
	}
	s.servirBinarioAsset(w, r, ctx, asset)
}

// raizesConfiaveis is the list of directories under which a poster MAY be
// served: the roots of the datasources (local artwork from the scanner) plus the
// assets directory (external image cache), when configured. It is the basis of
// the path-traversal guard of the poster endpoint.
func (s *Server) raizesConfiaveis(ctx context.Context) ([]string, error) {
	fontes, err := s.leitura().ListarLibrarySources(ctx)
	if err != nil {
		return nil, err
	}
	raizes := make([]string, 0, len(fontes)+1)
	for _, f := range fontes {
		raizes = append(raizes, f.Path)
	}
	if s.assetsDir != "" {
		raizes = append(raizes, s.assetsDir)
	}
	return raizes, nil
}

// contentTypeImagem determines the poster content-type: first by extension
// (cheap and accurate for the common formats); if that does not resolve to
// image/*, it sniffs the first bytes. It rewinds the file pointer to the start so
// ServeContent resends from scratch.
func contentTypeImagem(path string, f *os.File) string {
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	_, _ = f.Seek(0, 0)
	return http.DetectContentType(buf[:n])
}

// caminhoSobRaizes says whether `alvo` is contained in ANY of the roots,
// comparing CLEAN and ABSOLUTE paths via filepath.Rel — the relative path cannot
// start with ".." nor be absolute for it to be contained. It is the central
// check of the path-traversal guard.
func caminhoSobRaizes(alvo string, raizes []string) bool {
	ap, err := filepath.Abs(filepath.Clean(alvo))
	if err != nil {
		return false
	}
	for _, raiz := range raizes {
		rp, err := filepath.Abs(filepath.Clean(raiz))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rp, ap)
		if err != nil {
			continue
		}
		if rel == "." {
			return true
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

// ── Query parameter parsing ──────────────────────────────────────────────────

// parsePaginacao reads limit/offset with defaults and a ceiling. Non-numeric or
// negative values respond 400 invalid_query (and ok=false).
func parsePaginacao(w http.ResponseWriter, q url.Values) (limit, offset int, ok bool) {
	limit = limitPadrao
	offset = 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "limit inválido")
			return 0, 0, false
		}
		limit = n
	}
	if limit > limitMaximo {
		limit = limitMaximo
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "offset inválido")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

// parseTipoFiltro returns the type filter as a value for sqlc (string) or nil
// when absent (no filter).
func parseTipoFiltro(q url.Values) any {
	if v := q.Get("type"); v != "" {
		return v
	}
	return nil
}

// parseOwnedFiltro translates owned=true|false|1|0 into the integer 1/0 that
// the query compares to EXISTS. Absent => nil (no filter). Invalid value =>
// 400.
func parseOwnedFiltro(w http.ResponseWriter, q url.Values) (any, bool) {
	v := q.Get("owned")
	if v == "" {
		return nil, true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", "owned inválido (use true ou false)")
		return nil, false
	}
	if b {
		return int64(1), true
	}
	return int64(0), true
}

// sortModo enumerates the accepted values of ?sort= in GET /api/v1/entities:
// the two historical orderings by title plus the `-created_at` ("recently
// added"). Each mode maps to ONE sqlc query in listarCards (sqlc cannot bind a
// parameter inside ORDER BY).
type sortModo int

const (
	sortTituloAsc   sortModo = iota // default: sort absent, "title" or "title_asc"
	sortTituloDesc                  // "-title" or "title_desc"
	sortCriacaoDesc                 // "-created_at": only the DESC direction is requested
)

// parseSort reads sort=title|-title|-created_at. The default (absent/"title")
// is title ascending; "-title"/"title_desc" is title descending; "-created_at"
// is creation descending — there is no ascending variant ("oldest first" was not
// requested). Any other value => 400.
func parseSort(w http.ResponseWriter, q url.Values) (sortModo, bool) {
	switch q.Get("sort") {
	case "", "title", "title_asc":
		return sortTituloAsc, true
	case "-title", "title_desc":
		return sortTituloDesc, true
	case "-created_at":
		return sortCriacaoDesc, true
	default:
		writeError(w, http.StatusBadRequest, "invalid_query", "sort inválido (use title, -title ou -created_at)")
		return sortTituloAsc, false
	}
}

// ── Conversion helpers ───────────────────────────────────────────────────────

// respondErroInterno responds 500 in the standard shape without leaking the
// internal detail.
func respondErroInterno(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "internal_error",
		"erro interno ao processar a requisição")
}

// floatPtr extracts a *float64 from the dynamic value of MAX(match_confidence)
// (REAL or NULL). No match => nil.
func floatPtr(v any) *float64 {
	switch x := v.(type) {
	case float64:
		return &x
	case int64:
		f := float64(x)
		return &f
	default:
		return nil
	}
}

// nsPtr converts sql.NullString into *string (nil when NULL).
func nsPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// ni64Ptr converts sql.NullInt64 into *int64 (nil when NULL).
func ni64Ptr(ni sql.NullInt64) *int64 {
	if !ni.Valid {
		return nil
	}
	v := ni.Int64
	return &v
}
