// Package richfields concentrates the TYPES and the RESOLUTION of the rich
// fields of an item (videos, collection, similar items, certification by region,
// tagline, keywords, rating, runtime, backdrop, links) from the CANONICAL map
// produced by the provider (MapearDetalhe). It is consumed by TWO places that
// need the SAME shape:
//
//   - the GET /external/{source}/{type}/{id} handler (detail of the external
//     item BEFORE importing), and
//   - the resolver of the IMPORTED entity (GET /entities/{id}), which exposes the
//     same rich fields so that the catalog detail screen has visual parity.
//
// Centralizing here keeps two sources of truth from drifting apart: the JSONs of
// the external item and of the entity carry the rich fields in the SAME format.
// All the functions are PURE over the canonical `map[string]any` — no I/O, no
// SQL, no dependency on the concrete provider.
package richfields

import "sort"

// RegiaoFallback is the region used when the active language carries no region
// (e.g.: `en`) or the source does not bring the certification of the requested
// region (Phase 3 review decision: it falls back to US).
const RegiaoFallback = "US"

// Video is a video (trailer/clip) of the item. URL is nil when the provider site
// is unknown (the UI shows only name/type in that case).
type Video struct {
	Name     string  `json:"name"`
	Key      string  `json:"key"`
	Site     string  `json:"site"`
	Type     string  `json:"type"`
	Official bool    `json:"official"`
	URL      *string `json:"url"`
}

// Collection is the collection (≠ playlist) the item belongs to. SourceID allows
// NAVIGATING to the collection; PosterURL is optional. InternalID is the id of the
// internal collection entity when it has ALREADY been imported — the UI then
// links to the internal destination instead of the external page. ColecaoDe stays
// PURE and leaves it nil; whoever has database access (the handler / the
// resolverEntity in the server) fills it in.
type Collection struct {
	SourceID   string  `json:"source_id"`
	Name       string  `json:"name"`
	PosterURL  *string `json:"poster_url"`
	InternalID *string `json:"internal_id"`
}

// Similar is a similar item, with the Type to NAVIGATE to its external detail —
// the same GET, recursive, without cataloging.
type Similar struct {
	SourceID  string  `json:"source_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Year      *string `json:"year"`
	PosterURL *string `json:"poster_url"`
	// InternalID is the id of the internal entity when the similar item has already
	// been imported (extends the internal-first behavior). SimilaresDe is PURE and
	// leaves it nil; whoever has a DB (the external detail handler and the entity
	// resolver, through the server) fills it in.
	InternalID *string `json:"internal_id"`
}

// ── Resolution of translatable fields ───────────────────────────────────────

// ResolverEscalarTraduzivel resolves a translatable scalar field (title/
// description/tagline) by walking the chain; it falls back to "any language
// present" (stable order) if the chain does not match. It returns (value,
// effective language). It tolerates the field as a plain scalar (legacy payload)
// — in that case provenance is "".
func ResolverEscalarTraduzivel(campos map[string]any, campo string, cadeia []string) (*string, string) {
	v, ok := campos[campo]
	if !ok || v == nil {
		return nil, ""
	}
	switch m := v.(type) {
	case string:
		if m == "" {
			return nil, ""
		}
		return &m, ""
	case map[string]any:
		return escalarDoMapaIdioma(m, cadeia)
	}
	return nil, ""
}

// escalarDoMapaIdioma picks the text from the language→value map: the first
// language of the chain with text, otherwise any language present (stable order).
// It returns the effective language as provenance.
func escalarDoMapaIdioma(m map[string]any, cadeia []string) (*string, string) {
	for _, l := range cadeia {
		if s, ok := m[l].(string); ok && s != "" {
			return &s, l
		}
	}
	for _, l := range chavesOrdenadas(m) {
		if s, ok := m[l].(string); ok && s != "" {
			return &s, l
		}
	}
	return nil, ""
}

// ResolverListaTraduzivel resolves a translatable list field (tags/keywords) by
// the chain, with the same "any language present" fallback (stable order). It
// returns (list, effective language). It tolerates a plain list (legacy) —
// provenance "".
func ResolverListaTraduzivel(campos map[string]any, campo string, cadeia []string) ([]string, string) {
	v, ok := campos[campo]
	if !ok || v == nil {
		return nil, ""
	}
	m, ehMap := v.(map[string]any)
	if !ehMap {
		return comoLista(v), ""
	}
	for _, l := range cadeia {
		if lst := comoLista(m[l]); len(lst) > 0 {
			return lst, l
		}
	}
	for _, l := range chavesOrdenadas(m) {
		if lst := comoLista(m[l]); len(lst) > 0 {
			return lst, l
		}
	}
	return nil, ""
}

// ── Rich neutral fields ──────────────────────────────────────────────────────

// CertificacaoDaRegiao picks the certification of the REGION of the active
// language (pt-BR→BR), falling back to US when the language has no region or the
// source does not bring that region.
func CertificacaoDaRegiao(campos map[string]any, lang string) *string {
	m, ok := campos["certification_by_region"].(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	regiao := RegiaoDoIdioma(lang)
	if s, ok := m[regiao].(string); ok && s != "" {
		return &s
	}
	if s, ok := m[RegiaoFallback].(string); ok && s != "" {
		return &s
	}
	return nil
}

// RegiaoDoIdioma extracts the region (iso_3166_1) from a language code: the
// suffix after the "-" (pt-BR→BR); without a suffix, the US fallback (review
// decision).
func RegiaoDoIdioma(lang string) string {
	if i := lastIndexByte(lang, '-'); i >= 0 && i < len(lang)-1 {
		return toUpper(lang[i+1:])
	}
	return RegiaoFallback
}

// VideosDe converts the canonical list of videos.
func VideosDe(campos map[string]any) []Video {
	itens := comoListaAny(campos["videos"])
	out := make([]Video, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Video{
			Name:     ComoStringMapa(m, "name"),
			Key:      ComoStringMapa(m, "key"),
			Site:     ComoStringMapa(m, "site"),
			Type:     ComoStringMapa(m, "type"),
			Official: comoBool(m["official"]),
			URL:      PtrSeNaoVazio(ComoStringMapa(m, "url")),
		})
	}
	return out
}

// PrimeiroTrailerYoutube finds the first video of type "Trailer" (TMDB/TVDB
// convention) hosted on YouTube with a `key` present — the SAME criterion as
// `primeiroTrailer` in the frontend (web/src/components/rich-detail-view.tsx),
// mirrored here so that the backend can materialize it on disk (ADR-0025)
// without duplicating the rule in two places with no documented common source.
// No fallback to Teaser/Clip; ok=false when no video matches.
func PrimeiroTrailerYoutube(videos []Video) (Video, bool) {
	for _, v := range videos {
		if v.Type == "Trailer" && v.Site == "YouTube" && v.Key != "" {
			return v, true
		}
	}
	return Video{}, false
}

// ColecaoDe converts the canonical collection, or nil when absent.
func ColecaoDe(campos map[string]any) *Collection {
	m, ok := campos["collection"].(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	return &Collection{
		SourceID:  ComoStringMapa(m, "source_id"),
		Name:      ComoStringMapa(m, "name"),
		PosterURL: PtrSeNaoVazio(ComoStringMapa(m, "poster_url")),
	}
}

// SimilaresDe converts the canonical list of similar items (with the type to
// navigate).
func SimilaresDe(campos map[string]any) []Similar {
	itens := comoListaAny(campos["similar"])
	out := make([]Similar, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Similar{
			SourceID:  ComoStringMapa(m, "source_id"),
			Type:      ComoStringMapa(m, "type"),
			Title:     ComoStringMapa(m, "title"),
			Year:      PtrSeNaoVazio(ComoStringMapa(m, "year")),
			PosterURL: PtrSeNaoVazio(ComoStringMapa(m, "poster_url")),
		})
	}
	return out
}

// LinksExternos builds the map of links from the canonical one (e.g.: imdb from
// the provider), adding the TMDB/IMDb/TVDB links built from the RAW IDS when the
// caller has them (cross-source `externalIDs` already resolved in the resolver) —
// the provider sometimes maps only a subset into `external_links`. Precedence:
// whatever the SOURCE already sent in `external_links` ALWAYS wins (it may be a
// more specific URL built by the provider); the raw ids only fill in what is
// still missing from the map. Empty `tmdbID`/`imdbID`/`tvdbID` simply omit the
// respective link.
func LinksExternos(campos map[string]any, tmdbTipo, tmdbID, imdbID, tvdbID string) map[string]string {
	out := map[string]string{}
	if m, ok := campos["external_links"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok && s != "" {
				out[k] = s
			}
		}
	}
	if tmdbID != "" && tmdbTipo != "" {
		out["tmdb"] = "https://www.themoviedb.org/" + tmdbTipo + "/" + tmdbID
	}
	if imdbID != "" {
		if _, ok := out["imdb"]; !ok {
			out["imdb"] = "https://www.imdb.com/title/" + imdbID + "/"
		}
	}
	if tvdbID != "" {
		if _, ok := out["tvdb"]; !ok {
			out["tvdb"] = "https://www.thetvdb.com/dereferrer/series/" + tvdbID
		}
	}
	return out
}

// ── Credits: cast, crew and production companies (MD6 phase B, ADR-0013) ────

// creditoImagemBaseURL is the base of the TMDB person photo/company logo. The
// canonical credits map (keys `profile_path`/`logo_path`, MD6 phase A, ADR-0013)
// stores the RAW fragment of the source — unlike `poster_url`/`backdrop_url`,
// which already arrive ready from the provider — because today only TMDB feeds
// credits. Building the URL here is the only coupling point; if another credits
// source comes in, this helper becomes one function per source. Size w185 covers
// a cast thumbnail/logo well.
const creditoImagemBaseURL = "https://image.tmdb.org/t/p/w185"

// Cast is a CAST credit (MD6): Name is neutral (a person's name is not
// translated); Character is the current display (editing per language is left for
// phase C). EpisodeCount only exists on series (role aggregated over episodes);
// nil on movies. ProfileURL is the DIRECT (non-proxied) URL of the photo — the
// caller with access to the proxy (server) decides whether to rewrite it to the
// local route, mirroring poster/backdrop (StrPtrDe + urlImagemServida). PersonID
// is the id of the person IN OUR catalog (internal-first, MD6 phase D) when it has
// already been persisted by some import — CastDe leaves it nil (pure); whoever has
// database access (the server) fills it in, mirroring
// Collection.InternalID/Similar.InternalID.
type Cast struct {
	SourceID     string  `json:"source_id"`
	Name         string  `json:"name"`
	Character    string  `json:"character"`
	Order        int     `json:"order"`
	ProfileURL   *string `json:"profile_url"`
	EpisodeCount *int    `json:"episode_count"`
	PersonID     *string `json:"person_id"`
}

// Crew is a CREW credit (MD6): Department/Job come from the canonical map;
// EpisodeCount only on series (role aggregated). See Cast for ProfileURL and
// PersonID.
type Crew struct {
	SourceID     string  `json:"source_id"`
	Name         string  `json:"name"`
	Department   string  `json:"department"`
	Job          string  `json:"job"`
	ProfileURL   *string `json:"profile_url"`
	EpisodeCount *int    `json:"episode_count"`
	PersonID     *string `json:"person_id"`
}

// Company is a production company/studio (MD6). See Cast for LogoURL.
type Company struct {
	SourceID string  `json:"source_id"`
	Name     string  `json:"name"`
	LogoURL  *string `json:"logo_url"`
}

// CastDe converts the canonical cast list (top ~50 by billing, already trimmed by
// the provider). It always returns a non-nil slice (`[]` when absent).
func CastDe(campos map[string]any) []Cast {
	itens := comoListaAny(campos["cast"])
	out := make([]Cast, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Cast{
			SourceID:     ComoStringMapa(m, "source_id"),
			Name:         ComoStringMapa(m, "name"),
			Character:    ComoStringMapa(m, "character"),
			Order:        intDeMapa(m, "order"),
			ProfileURL:   urlCreditoDe(m, "profile_path"),
			EpisodeCount: IntPtrDe(m, "episode_count"),
		})
	}
	return out
}

// CrewDe converts the canonical crew list (no cap — deduplication already done by
// the provider). It always returns a non-nil slice (`[]` when absent).
func CrewDe(campos map[string]any) []Crew {
	itens := comoListaAny(campos["crew"])
	out := make([]Crew, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Crew{
			SourceID:     ComoStringMapa(m, "source_id"),
			Name:         ComoStringMapa(m, "name"),
			Department:   ComoStringMapa(m, "department"),
			Job:          ComoStringMapa(m, "job"),
			ProfileURL:   urlCreditoDe(m, "profile_path"),
			EpisodeCount: IntPtrDe(m, "episode_count"),
		})
	}
	return out
}

// CompaniesDe converts the canonical list of production companies/studios. It
// always returns a non-nil slice (`[]` when absent).
func CompaniesDe(campos map[string]any) []Company {
	itens := comoListaAny(campos["companies"])
	out := make([]Company, 0, len(itens))
	for _, it := range itens {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Company{
			SourceID: ComoStringMapa(m, "source_id"),
			Name:     ComoStringMapa(m, "name"),
			LogoURL:  urlCreditoDe(m, "logo_path"),
		})
	}
	return out
}

// urlCreditoDe builds the DIRECT (non-proxied) URL of a credit photo/logo from
// the raw fragment of the canonical map (key `profile_path`/`logo_path`), or nil
// when absent/empty.
func urlCreditoDe(m map[string]any, chave string) *string {
	frag := ComoStringMapa(m, chave)
	if frag == "" {
		return nil
	}
	u := creditoImagemBaseURL + frag
	return &u
}

// intDeMapa reads an integer key from the map (0 when absent/wrong type) — for
// `order`, always present and non-pointer in the shape (unlike `episode_count`,
// optional through IntPtrDe).
func intDeMapa(m map[string]any, chave string) int {
	switch n := m[chave].(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// ── Conversion helpers (pure over the canonical map) ─────────────────────────

// ComoStringMapa reads a string key from the map ("" when absent/wrong type).
func ComoStringMapa(m map[string]any, chave string) string {
	if s, ok := m[chave].(string); ok {
		return s
	}
	return ""
}

// StrPtrDe reads a string key from the map as *string (nil when absent/empty).
func StrPtrDe(m map[string]any, chave string) *string {
	return PtrSeNaoVazio(ComoStringMapa(m, chave))
}

// IntPtrDe reads a numeric key from the map as *int (nil when absent). The
// canonical map stores runtime values as int; we tolerate float64 (JSON
// round-trip).
func IntPtrDe(m map[string]any, chave string) *int {
	switch n := m[chave].(type) {
	case int:
		v := n
		return &v
	case float64:
		v := int(n)
		return &v
	default:
		return nil
	}
}

// FloatPtrDe reads a numeric key from the map as *float64 (nil when absent).
func FloatPtrDe(m map[string]any, chave string) *float64 {
	switch n := m[chave].(type) {
	case float64:
		v := n
		return &v
	case int:
		v := float64(n)
		return &v
	default:
		return nil
	}
}

// PtrSeNaoVazio returns a *string for a string, or nil when it is empty.
func PtrSeNaoVazio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// DerefOuVazio dereferences a *string ("" when nil) — for non-nullable fields
// (title).
func DerefOuVazio(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// OuListaVazia swaps nil for an empty slice (JSON comes out as `[]`, never null).
func OuListaVazia(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// AnoDePtr extracts the first 4 digits (year) of a pointed ISO date, or nil.
func AnoDePtr(data *string) *string {
	if data == nil || len(*data) < 4 {
		return nil
	}
	ano := (*data)[:4]
	return &ano
}

// comoBool reads a dynamic bool (false when absent/wrong type).
func comoBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// comoLista converts a dynamic value into a non-nil slice of strings (accepts
// []string and []any of strings).
func comoLista(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

// comoListaAny returns a []any slice (nil when the value is not a list).
func comoListaAny(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// chavesOrdenadas returns the keys of the map in a stable order (determinism in
// the "any language present" fallback).
func chavesOrdenadas(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// lastIndexByte and toUpper avoid importing strings for just two trivial
// operations (keeping the package lean and with no dependencies beyond sort).
func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func toUpper(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'a' && out[i] <= 'z' {
			out[i] -= 'a' - 'A'
		}
	}
	return string(out)
}
