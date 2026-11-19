// Package resolver composes the RESOLVED view of an entity from the available
// layers. The precedence is `override ⟵ externo ⟵ arquivo`: the owner's override
// wins over everything, the external (enrichment) layer wins over the file, and
// the file/core is the always-present base. The composition is field by field,
// guided by a declarative REGISTRY of origin (map `campo → {base, kind}`) that
// the engine ITERATES — no `switch` on the field name (ADR-0001). Adding a
// vertical/field means changing the registry (data), not the code.
//
// The shape of the response (Entidade) is the DEFINITIVE one (docs 04/03) and the
// public signature `Resolver(Camadas) Entidade` does NOT change when layers
// arrive — only the content does. That is why the UI does not feel Phase 3.
package resolver

import (
	"bytes"
	"database/sql"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// CamadaExterna is ONE external source already in CANONICAL form: `Campos` uses
// OUR field names (e.g.: "title", "description", "tags", "extension_data"),
// not the provider's raw shape. The raw→canonical mapping belongs to the
// provider; the resolver does NOT know TMDB. The layers arrive already ORDERED
// by the priority list (R2): the first one with a value for a field forms its
// external base.
type CamadaExterna struct {
	// Fonte identifies the provider (e.g.: "tmdb"); today only informative.
	Fonte string
	// Campos are the canonical values of that source (names of our fields).
	Campos map[string]any
}

// Camadas aggregates the LAYERS that the resolver composes: the file/core (base,
// always present), the external sources and the owner's override patch. Callers
// pass whatever they have; the Resolver signature does not change.
type Camadas struct {
	// Entity is the core row (FILE layer, always present).
	Entity sqlcgen.Entity
	// Files are the entity's media_file rows (derived ownership and count come
	// from here).
	Files []sqlcgen.MediaFile
	// PosterURL is the (relative) reference of the endpoint that serves the local
	// poster, or nil when the entity has no poster. The caller builds it (it is an
	// API link, not domain data); the resolver only places it in the final shape.
	PosterURL *string
	// BackdropURL is the reference of the endpoint that serves the local
	// BACKGROUND (fanart/backdrop), when cached. When nil, the resolver
	// falls back to the source's direct URL (backdrop_url of the external
	// canonical) — the same field, served from disk when available.
	BackdropURL *string

	// Externo are the canonical EXTERNAL layers, ordered by priority (R2).
	// Empty = no enrichment (pre-enrichment behavior preserved).
	Externo []CamadaExterna
	// Override is the RAW sparse patch of media_override.override_json: presence of
	// the key = active override, even with a `null` value (R6/ADR-0006). Nil = no
	// override.
	Override map[string]json.RawMessage

	// LangPedido is the resolved language of the request (default = the main one
	// from the settings). Echoed in the resolved shape; empty when there is no
	// language context (the engine then preserves the pre-multilanguage
	// behavior).
	LangPedido string
	// Cadeia is the fallback order already derived from the settings (ADR-0019):
	// `[pedido, fallback, original_language?]` deduplicated, ending IMPLICITLY at
	// the neutral base (file) — the engine falls back to the file when no language
	// in the chain has a value. EMPTY = no language context: translatable fields
	// compose as in the pre-multilanguage model (without consulting the chain).
	Cadeia []string
	// IdiomaPrincipal is the MAIN language of the settings. It is used to
	// interpret the LEGACY external/override (legacy format, a scalar/list value
	// with no language key): that value is treated as belonging to the main one
	// (ADR-0018, tolerance note of ADR-0019). Empty ⇒ uses the first language of
	// the chain.
	IdiomaPrincipal string
}

// RatingFonte is the rating (vote_average) of ONE specific source — unlike
// VoteAverage (merged by priority, a single value), here there is 1 entry PER
// source that models the field, so the UI can show them side by side.
type RatingFonte struct {
	Source string  `json:"source"`
	Value  float64 `json:"value"`
}

// Entidade is the RESOLVED view — the final shape served at
// GET /api/v1/entities/{id}. The pointers come out as `null` in JSON when absent;
// `tags`/`external_ids`/`extension_data` always come out as a list/object (never
// null) to simplify consumption in the front end.
type Entidade struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Title         string            `json:"title"`
	OriginalTitle *string           `json:"original_title"`
	Description   *string           `json:"description"`
	ReleaseDate   *string           `json:"release_date"`
	Status        string            `json:"status"`
	Tags          []string          `json:"tags"`
	ExternalIDs   map[string]string `json:"external_ids"`
	Owned         bool              `json:"owned"`
	FileCount     int               `json:"file_count"`
	PosterURL     *string           `json:"poster_url"`
	ExtensionData map[string]any    `json:"extension_data"`

	// ── RICH fields (parity with the external detail) ──
	// They are READ-ONLY: they come only from the EXTERNAL layer (the owner does
	// not edit them by override in this phase). The translatable ones
	// (Tagline/Keywords) compose per language like title/description; the
	// neutral/structured ones are extracted straight from the external canonical
	// via internal/richfields (the SAME shape as the external detail).
	// Genres have NO field of their own: they are the `tags` (which already carry
	// the genres).
	Tagline  *string  `json:"tagline"`
	Keywords []string `json:"keywords"`
	// AiringStatus is the AIRING situation of a SERIES: the RAW string
	// from the source, exposed VERBATIM (TMDB: "Returning Series"|"Ended"|
	// "Canceled"|"In Production"|"Planned"; TVDB: status.name) — the translation
	// to pt-BR happens in the UI. It is NEUTRAL (not translatable) and READ-ONLY
	// (the owner does not edit it). Do NOT confuse it with the CATALOG `Status`
	// above (owned/cataloged, the owner's intent): they are distinct fields.
	// Absent (omitempty) when the source does not report it (movie, or TVDB-only
	// series with no match).
	AiringStatus  *string  `json:"airing_status,omitempty"`
	Runtime       *int     `json:"runtime"`
	Certification *string  `json:"certification"`
	VoteAverage   *float64 `json:"vote_average"`
	// LocalRating is the owner's LOCAL RATING (0–10, ADR-0012): the OWN evaluation,
	// stored in the OVERRIDE layer, which COMPLEMENTS/OVERRIDES the source rating
	// (VoteAverage). null when the owner has not rated it — the UI then shows the
	// source rating (clearing the local rating is NON-DESTRUCTIVE). Unlike the
	// other rich fields (read-only), this one is EDITABLE by override: it enters
	// the registry as kindNumero.
	LocalRating   *float64               `json:"local_rating"`
	BackdropURL   *string                `json:"backdrop_url"`
	Videos        []richfields.Video     `json:"videos"`
	Collection    *richfields.Collection `json:"collection"`
	Similar       []richfields.Similar   `json:"similar"`
	ExternalLinks map[string]string      `json:"external_links"`

	// Cast/Crew/Companies are the CREDITS (cast, crew, production companies —
	// ADR-0013): parity with the external detail, same shape (via richfields,
	// single source). READ-ONLY in this round (no editor — Phase C). Always `[]`
	// (never null) when absent. ProfileURL/LogoURL come out with the provider's
	// DIRECT URL (richfields is pure); the server rewrites them to the local proxy
	// after resolution (same boundary as Ancestors/Ownership).
	Cast      []richfields.Cast    `json:"cast"`
	Crew      []richfields.Crew    `json:"crew"`
	Companies []richfields.Company `json:"companies"`

	// Ratings are the ratings PER SOURCE (vote_average of each external layer that
	// models the field), next to VoteAverage (the single value already merged by
	// priority) — it lets the UI compare ratings from different sources side by
	// side. Always `[]` (never null); a source that does not model the field does
	// not appear.
	Ratings []RatingFonte `json:"ratings"`

	// Lang is the echo of the requested language (ADR-0019). Empty when there is no
	// language context (ADR-0019 behavior).
	Lang string `json:"lang"`
	// OriginalLanguage is the NEUTRAL code of the work's original language (anchor
	// of the fallback), or null when the source did not report it.
	OriginalLanguage *string `json:"original_language"`
	// ResolvedLanguages is the LANGUAGE PROVENANCE (ADR-0019): translatable field →
	// effective language that served the value (`""` = neutral/file base). A side
	// map that does NOT change the fields (the shape stays flat). Always non-nil
	// (empty when there is no language context).
	ResolvedLanguages map[string]string `json:"resolved_languages"`

	// Ancestors is the ROOT→PARENT trail (breadcrumb) of the entity in the
	// series tree: episode → [series, season]; season → [series]; top-level work
	// (movie/tv/collection) → []. Always non-nil (`[]`, never null). It does NOT
	// come from the pure resolver — it is annotated in the server after resolution
	// (walking up through ListarRelacoesPais), the same boundary as the
	// internal_id.
	Ancestors []AncestorRef `json:"ancestors"`

	// Ownership is the AGGREGATED ownership of a parent entity of the series tree:
	// how many episodes are owned out of the total. Present (non-null)
	// only for `tv` (sum across seasons) and `season` (direct episodes); null for
	// movie/episode/collection (simple ownership via `owned`). It does NOT come
	// from the pure resolver — it is annotated in the server after resolution (same
	// boundary as Ancestors), because the aggregation walks entity_relation.
	Ownership *OwnershipInfo `json:"ownership"`

	// MatchSuggestion is the "did you mean this?" hint the SCANNER persisted for an item
	// that is still unidentified: the name on disk found nothing, but a cheaper variant of
	// it (one token less) did. It is a HINT, never an identity -- the owner decides, and the
	// action behind it is the existing POST /entities/{id}/confirm-candidate. Absent
	// (omitempty) when there is no hint, which is the normal case. Annotated in the server
	// after resolution (same boundary as Ancestors/Ownership): it lives in its own table
	// (match_suggestion), not in the entity.
	MatchSuggestion *MatchSuggestion `json:"match_suggestion,omitempty"`

	// Orderings are the available episode display orders of a SERIES:
	// the `default` (the default_season_type recorded on import) and the `available`
	// list (union of the `numbering` keys of the episodes + `absolute` when there is
	// an absolute_number), with the default first. Present (non-null) only for `tv`
	// with at least one order; `omitempty` (absent) for the other types and for a
	// series with no numbering — the UI then hides the selector. Annotated in the
	// server after resolution (same boundary as Ancestors/Ownership), because it
	// scans the episode tree.
	Orderings *OrderingsInfo `json:"orderings,omitempty"`

	// StillURL is the RAW URL of the EPISODE's still (16:9 frame):
	// language-NEUTRAL artwork, read from extension_data core.tv@1 (still_url).
	// Present (non-null) only for `episode` with a still; `omitempty` (absent) for
	// the other types and for an episode without a still — the UI falls back to the
	// placeholder. Annotated in the server after resolution (the resolver is pure
	// and does not know core.tv), and served by the image proxy (fetch+cache on
	// demand) in the front end. It is not downloaded on import: only the URL is
	// persisted.
	StillURL *string `json:"still_url,omitempty"`

	// CatalogCompleteness is the item's CATALOG completeness against the scale
	// configured in /settings, computed LIVE on every request by the server
	// (anotarCompletudeCatalogacao, entities.go) — it reuses the data that this very
	// resolverEntity already builds, with no extra query. It does NOT come from the
	// pure resolver (same boundary as Ancestors/Ownership/StillURL). `null` only
	// when the item is a draft (never matched) — there is no point in evaluating
	// metadata that does not exist yet; for a non-draft item it is never nil here
	// (unlike EntityCard.catalog_completeness in the listing, which reads from a
	// batch cache recalculated by the job and may stay nil until the first run).
	// Distinct from OWNERSHIP completeness (web/src/lib/entity-badges.ts).
	CatalogCompleteness *CatalogCompleteness `json:"catalog_completeness"`
}

// CatalogCompleteness is the result of evaluating the CATALOG completeness of an
// item against the per-type scale configured in /settings
// (internal/server/catalog_completeness.go). It lives in the resolver package (not
// in internal/server) so that it can be a FIELD of the final shape (Entidade),
// serialized directly by GET /entities/{id} — the server package uses this SAME
// type (through the alias `resultadoCompletude`,
// internal/server/completeness.go) so as not to duplicate the definition nor
// require a conversion when filling the field.
type CatalogCompleteness struct {
	// Complete is true when the item meets the whole scale, or when the type has
	// no entry in the config (a type with no scale is never incomplete).
	Complete bool `json:"complete"`
	// Missing are the STABLE keys of what is missing, in a fixed order
	// (description, runtime, certification, keywords, cast, media_file,
	// artwork:<kind>...). `[]` (never nil) when Complete is true.
	Missing []string `json:"missing"`
}

// OrderingsInfo describes the episode display orders of a series:
// `Default` is the initial order (the series' default_season_type) and `Available`
// the list of orders offered to the selector (the default first, `absolute` last
// when there is an absolute number). The UI only shows the selector when
// `Available` has more than one item.
type OrderingsInfo struct {
	Default   string   `json:"default"`
	Available []string `json:"available"`
}

// OwnershipInfo is the aggregated ownership of a season/series: owned episodes
// (`owned_count`) over the total number of episodes (`total_count`). The UI
// renders the badge "possui 12/13". Counts derived from the media_file_entity
// links (kind='tv_episode') + the entity_relation tree.
type OwnershipInfo struct {
	OwnedCount int `json:"owned_count"`
	TotalCount int `json:"total_count"`
}

// AncestorRef is an ancestor node in the breadcrumb trail: the minimum to render
// and NAVIGATE to a parent level (id + title + type). Built in the server.
type AncestorRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// baseOrigem says where the BASE of a field comes from before the override:
// `externo` (enrichment overrides the file), `arquivo` (the file is authoritative;
// the external is not consulted) or `nenhuma` (only the override speaks). The
// engine reads the label, not the field name.
type baseOrigem int

const (
	baseArquivo baseOrigem = iota
	baseExterno
	baseNenhuma
)

// kindCampo classifies the composition form of the field (scalar, list by
// operations, or extension by namespace/key). The engine dispatches by kind — a
// behavior class —, never by field name.
type kindCampo int

const (
	kindEscalar kindCampo = iota
	kindLista
	kindExtensao
	// kindNumero is a NUMERIC scalar (*float64) — e.g.: the owner's local rating. It
	// composes like the scalar (override wins; `null` erases), but the value is a
	// number, not a string. It is a class of its own so the engine dispatches without
	// a `switch` on the name.
	kindNumero
	// kindCreditos is the credit (cast/crew/production companies, ADR-0013):
	// base ALWAYS external (merged by priority, no file-base), the override is a
	// PATCH segregated by sub-kind (cast/crew/companies) with
	// reorder/remove/edit/add operations — not a common
	// scalar/list/extension. See comporCreditos.
	kindCreditos
)

// origemCampo is the declarative label of an editorial field. `traduzivel` marks
// the fields that compose BY LANGUAGE (ADR-0018): when there is a language context
// (non-empty chain), the engine resolves them in the language × layer dimension;
// the NEUTRAL ones (traduzivel=false) never consult the chain. It is a label in the
// registry, not a `switch` on the field name (R1/ADR-0001).
type origemCampo struct {
	base       baseOrigem
	kind       kindCampo
	traduzivel bool
	// soLeitura marks fields that the engine RESOLVES (composes from the external
	// layer) but that are NOT editable by the owner through an override: they stay
	// out of CamposEditaveis. They are the RICH fields of parity with the external
	// (tagline, keywords) — display, not edition. The composition treats them like
	// any field; the label only governs the EDITING surface.
	soLeitura bool
}

// registroOrigem is the origin METADATA per field (ADR-0001): the engine iterates
// this map to compose. Only EDITORIAL fields enter here. They stay OUT (they do not
// go through the layer composition): id/type/created_at/updated_at (system), status
// (the owner's intent), owned/file_count (derived), poster_url (API link) and
// external_ids (authoritative identity, MD4 — comes from the core). Changing this
// map (data) adds/changes fields without touching the engine.
var registroOrigem = map[string]origemCampo{
	"title":          {base: baseExterno, kind: kindEscalar, traduzivel: true},
	"original_title": {base: baseExterno, kind: kindEscalar},
	"description":    {base: baseExterno, kind: kindEscalar, traduzivel: true},
	"release_date":   {base: baseExterno, kind: kindEscalar},
	"tags":           {base: baseExterno, kind: kindLista, traduzivel: true},
	"extension_data": {base: baseExterno, kind: kindExtensao},
	// Translatable RICH fields, READ-ONLY (parity with the external): the engine
	// composes them per language (like title/tags), but they stay OUT of the
	// override editing — soLeitura excludes them from CamposEditaveis. The rich
	// neutral ones (runtime, vote_average, backdrop_url, certification, videos,
	// collection, similar) do not enter the registry: they are extracted straight
	// from the external in Resolver (via richfields).
	"tagline":  {base: baseExterno, kind: kindEscalar, traduzivel: true, soLeitura: true},
	"keywords": {base: baseExterno, kind: kindLista, traduzivel: true, soLeitura: true},
	// SERIES airing situation: NEUTRAL scalar (verbatim, not translatable) and
	// READ-ONLY. External base (it comes from the provider's canonical); soLeitura
	// excludes it from CamposEditaveis (display, not edition), like
	// tagline/keywords.
	"airing_status": {base: baseExterno, kind: kindEscalar, soLeitura: true},
	// Owner's LOCAL rating (ADR-0012): NEUTRAL number, NO base (only the override
	// speaks — the file and the external do not have it; the source rating is the
	// vote_average, a distinct field). EDITABLE (not soLeitura): it enters
	// CamposEditaveis for the override editor.
	"local_rating": {base: baseNenhuma, kind: kindNumero},
	// Credits (cast/crew/production companies, ADR-0013): external base
	// (merged by priority, like the rich fields), NEUTRAL (not translatable —
	// character/job/department do not compose per language in this phase, a decision
	// settled in §6.5) and EDITABLE (the owner reorders/removes/edits/adds credits
	// without destroying the provider's base).
	"credits": {base: baseExterno, kind: kindCreditos},
}

// ── Public surface of the registry (R1) ─────────────────────────────────────
//
// The override editor needs to know which fields are EDITABLE and which compose
// BY LANGUAGE in order to build/validate the patch — WITHOUT retyping the list
// (which would become a second source of truth). These helpers only READ
// registroOrigem; they change neither the composition nor the Resolver
// signature.

// Kind is the COMPOSITION CLASS of an editorial field, exposed for the override
// editor to validate the SHAPE of the patch (scalar accepts string|null;
// list accepts array|{add,remove}|null; extension accepts object|null).
type Kind string

const (
	// KindEscalar is a single-value field (title/original_title/description/
	// release_date).
	KindEscalar Kind = "escalar"
	// KindLista is a list field by operations (tags).
	KindLista Kind = "lista"
	// KindExtensao is the map of verticals by namespace/key (extension_data).
	KindExtensao Kind = "extensao"
	// KindNumero is a NUMERIC scalar field (local rating): accepts number|null.
	KindNumero Kind = "numero"
	// KindCreditos is the credit (cast/crew/production companies): accepts `null`
	// (erases everything) or an object `{cast?,crew?,companies?}` with the
	// reorder/remove/edit/add operations per sub-kind (ADR-0013).
	KindCreditos Kind = "creditos"
)

// CampoInfo is the PUBLIC label of an editorial field, derived from
// registroOrigem: whether it composes by language (Traduzivel) and its composition
// class (Kind).
type CampoInfo struct {
	Traduzivel bool
	Kind       Kind
}

// kindPublico translates the internal kind label into the public one.
func kindPublico(k kindCampo) Kind {
	switch k {
	case kindLista:
		return KindLista
	case kindExtensao:
		return KindExtensao
	case kindNumero:
		return KindNumero
	case kindCreditos:
		return KindCreditos
	default:
		return KindEscalar
	}
}

// CamposEditaveis returns the field→CampoInfo map of the EDITORIAL fields (the ones
// that go through the layer composition), derived from registroOrigem. A fresh copy
// on every call — the internal registry remains immutable from the outside. It is
// the single source that the server consults to know which override keys are valid
// (R1/ADR-0001).
func CamposEditaveis() map[string]CampoInfo {
	out := make(map[string]CampoInfo, len(registroOrigem))
	for campo, orig := range registroOrigem {
		if orig.soLeitura {
			continue // rich read-only field: it resolves, but is not editable.
		}
		out[campo] = CampoInfo{Traduzivel: orig.traduzivel, Kind: kindPublico(orig.kind)}
	}
	return out
}

// MapaIdiomaDoOverride normalizes the RAW override of a TRANSLATABLE field into a
// language→value map ready for the editor's 2-LEVEL MERGE. If the value is already
// in the BY LANGUAGE format, it returns a copy; if it is in the LEGACY format (no
// language key), it wraps it under `principal` (the same tolerance as the read
// path). It uses the SAME classification as the composition (parseIdiomaMap),
// preserving the single source. For a non-translatable or nonexistent field it
// returns nil.
func MapaIdiomaDoOverride(campo string, raw json.RawMessage, principal string) map[string]json.RawMessage {
	orig, ok := registroOrigem[campo]
	if !ok || !orig.traduzivel {
		return nil
	}
	m, ehMap := parseIdiomaMap(raw, true, orig.kind)
	out := make(map[string]json.RawMessage, len(m)+1)
	if ehMap {
		for idioma, v := range m {
			out[idioma] = v
		}
		return out
	}
	out[principal] = raw
	return out
}

// Resolver composes the resolved Entidade from the layers. It is the SINGLE POINT
// where the precedence `override ⟵ externo ⟵ arquivo` is applied. The engine builds
// the FILE base as canonical values, iterates the registry composing field by
// field, and assembles the final shape. Fields outside the registry come straight
// from the core.
func Resolver(c Camadas) Entidade {
	// FILE base in canonical values (same names/types that the composition uses).
	baseArquivoVals := map[string]any{
		"title":          strPtr(c.Entity.Title),
		"original_title": nsPtr(c.Entity.OriginalTitle),
		"description":    nsPtr(c.Entity.Description),
		"release_date":   nsPtr(c.Entity.ReleaseDate),
		"tags":           parseTags(c.Entity.TagsJson),
		"extension_data": parseExtensionData(c.Entity.ExtensionDataJson),
		// The local rating has no file base (only the override defines it): the base is
		// nil.
		"local_rating": (*float64)(nil),
		// Credits have no file base (only external+override speak) — the same as the
		// extension_data pattern, whose file base is always an empty map.
		"credits": map[string]any{},
	}

	// LANGUAGE mode: only when there is a chain derived from the settings. Without a
	// chain, the engine fully preserves the pre-multilanguage behavior (composition
	// with no language).
	langMode := len(c.Cadeia) > 0
	principal := c.IdiomaPrincipal
	if principal == "" && langMode {
		principal = c.Cadeia[0]
	}

	// Generic composition: iterates the registry, dispatches by kind, composes. For
	// TRANSLATABLE fields in language mode, it composes in the language × layer
	// dimension and records the language provenance; the others follow the neutral
	// composition.
	resolvido := make(map[string]any, len(registroOrigem))
	resolvedLangs := map[string]string{}
	for campo, orig := range registroOrigem {
		if langMode && orig.traduzivel {
			val, prov := comporTraduzivel(campo, orig, baseArquivoVals[campo], c.Cadeia, principal, c.Externo, c.Override)
			resolvido[campo] = val
			resolvedLangs[campo] = prov
			continue
		}
		resolvido[campo] = comporCampo(campo, orig, baseArquivoVals[campo], c.Externo, c.Override)
	}

	// RICH neutral/structured fields: they do NOT go through the layer composition
	// (they have neither file base nor override) — they come straight from the
	// external canonical, in the R2 priority (first source with a value). It reuses
	// richfields (shared with /external), guaranteeing the SAME shape as the external
	// detail.
	rico := camposExternosMesclados(c.Externo)
	externalIDs := parseExternalIDs(c.Entity.ExternalIdsJson)

	// Credits (ADR-0013): the INPUT of richfields.CastDe/CrewDe/CompaniesDe is no
	// longer the raw `rico` and becomes the result of the COMPOSITION
	// (resolvido["credits"], already with the override applied) — the extractors
	// themselves do not change (100% reuse of the existing parsing).
	creditos := asObjMap(resolvido["credits"])

	// Backdrop: prefers the locally cached BACKGROUND (the caller's route); without a
	// cache, it falls back to the source's direct URL (external canonical). Same
	// field, different origin.
	backdrop := c.BackdropURL
	if backdrop == nil {
		backdrop = richfields.StrPtrDe(rico, "backdrop_url")
	}

	return Entidade{
		ID:                c.Entity.ID,
		Type:              c.Entity.Type,
		Title:             derefStr(resolvido["title"]),
		OriginalTitle:     asStrPtr(resolvido["original_title"]),
		Description:       asStrPtr(resolvido["description"]),
		ReleaseDate:       asStrPtr(resolvido["release_date"]),
		Status:            c.Entity.Status,
		Tags:              asStrSlice(resolvido["tags"]),
		ExternalIDs:       externalIDs,
		Owned:             temPosse(c.Files),
		FileCount:         len(c.Files),
		PosterURL:         c.PosterURL,
		ExtensionData:     asObjMap(resolvido["extension_data"]),
		Tagline:           asStrPtr(resolvido["tagline"]),
		Keywords:          asStrSlice(resolvido["keywords"]),
		AiringStatus:      asStrPtr(resolvido["airing_status"]),
		Runtime:           richfields.IntPtrDe(rico, "runtime"),
		Certification:     richfields.CertificacaoDaRegiao(rico, c.LangPedido),
		VoteAverage:       richfields.FloatPtrDe(rico, "vote_average"),
		LocalRating:       asFloatPtr(resolvido["local_rating"]),
		BackdropURL:       backdrop,
		Videos:            richfields.VideosDe(rico),
		Collection:        richfields.ColecaoDe(rico),
		Similar:           richfields.SimilaresDe(rico),
		Cast:              richfields.CastDe(creditos),
		Crew:              richfields.CrewDe(creditos),
		Companies:         richfields.CompaniesDe(creditos),
		Ratings:           ratingsPorFonte(c.Externo),
		ExternalLinks:     richfields.LinksExternos(rico, c.Entity.Type, externalIDs["tmdb"], externalIDs["imdb"], externalIDs["tvdb"]),
		Lang:              c.LangPedido,
		OriginalLanguage:  nsPtr(c.Entity.OriginalLanguage),
		ResolvedLanguages: resolvedLangs,
	}
}

// camposExternosMesclados flattens the external layers into a single canonical
// view for the NEUTRAL rich fields: for each key, the value of the HIGHEST
// priority source that has it (index 0 = highest, R2). It iterates from the end to
// the beginning so the index-0 one prevails. Only non-nil values enter.
func camposExternosMesclados(externo []CamadaExterna) map[string]any {
	out := map[string]any{}
	for i := len(externo) - 1; i >= 0; i-- {
		for k, v := range externo[i].Campos {
			if v != nil {
				out[k] = v
			}
		}
	}
	return out
}

// ratingsPorFonte collects the rating of EACH external layer that models
// vote_average, instead of the single value merged by priority — it lets the UI
// show ratings from different sources side by side. A source that does not model
// the field simply does not appear (silent degradation, AGENTS.md §6).
func ratingsPorFonte(externo []CamadaExterna) []RatingFonte {
	out := []RatingFonte{}
	for _, cam := range externo {
		if v := richfields.FloatPtrDe(cam.Campos, "vote_average"); v != nil {
			out = append(out, RatingFonte{Source: cam.Fonte, Value: *v})
		}
	}
	return out
}

// comporCampo dispatches the composition of a field by its CLASS (kind) — never by
// the name. This is where the engine stays generic: new fields only need an entry
// in the registry with one of the already supported kinds.
func comporCampo(campo string, orig origemCampo, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) any {
	switch orig.kind {
	case kindLista:
		return comporLista(campo, orig, arquivo, externo, override)
	case kindExtensao:
		return comporExtensao(campo, arquivo, externo, override)
	case kindNumero:
		return comporNumero(campo, orig, arquivo, externo, override)
	case kindCreditos:
		return comporCreditos(campo, externo, override)
	default: // kindEscalar
		return comporEscalar(campo, orig, arquivo, externo, override)
	}
}

// comporNumero applies the precedence to a NUMERIC scalar field (local rating):
// the override wins (key present, even `null` → erases with nil); otherwise, if the
// base is external, the first numeric value of the sources; otherwise the file
// base. It always returns a *float64. It mirrors comporEscalar, swapping string for
// number.
func comporNumero(campo string, orig origemCampo, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) *float64 {
	if raw, ok := override[campo]; ok {
		// Presence of the key decides the override; `null` → nil (erases); number → &n;
		// any other form degrades to nil.
		var v *float64
		_ = json.Unmarshal(raw, &v)
		return v
	}
	if orig.base == baseExterno {
		for _, cam := range externo {
			if v := richfields.FloatPtrDe(cam.Campos, campo); v != nil {
				return v
			}
		}
	}
	return asFloatPtr(arquivo)
}

// ── Credits (cast/crew/production companies, ADR-0013) ──────────────────────

// creditosKinds are the 3 credit sub-kinds, in the order in which the canonical
// form of the patch declares them (openapi.yaml OverridePatch).
var creditosKinds = [...]string{"cast", "crew", "companies"}

// comporCreditos composes the `credits` field: the BASE is always the external
// layer merged by priority (the same 3 keys that the read-only rich fields
// already used), with NO file base. Without an override, it returns the base as
// is (keys absent when the source did not bring them). `override["credits"] ==
// null` erases EVERYTHING (empty map). A present patch is decoded per sub-kind
// (cast/crew/companies) and applies remove→edit→add→reorder — a sub-key absent
// from the patch inherits that sub-kind's base intact. It always returns a non-nil
// map.
func comporCreditos(campo string, externo []CamadaExterna, override map[string]json.RawMessage) map[string]any {
	rico := camposExternosMesclados(externo)
	base := map[string]any{}
	for _, kind := range creditosKinds {
		if v, ok := rico[kind]; ok {
			base[kind] = v
		}
	}

	raw, temOverride := override[campo]
	if !temOverride {
		return base
	}
	if ehNull(raw) {
		return map[string]any{}
	}

	var patch map[string]json.RawMessage
	if json.Unmarshal(raw, &patch) != nil {
		// Invalid form (already blocked on write by the server): degrades to the
		// intact base instead of propagating an error from a resolver that is pure.
		return base
	}

	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	for _, kind := range creditosKinds {
		rawKind, ok := patch[kind]
		if !ok {
			continue // sub-key absent: inherits that sub-kind's base intact.
		}
		if ehNull(rawKind) {
			out[kind] = []any{}
			continue
		}
		itens := clonarListaCreditos(base[kind])
		itens = aplicarPatchCredito(kind, itens, rawKind)
		out[kind] = creditosParaAny(itens)
	}
	return out
}

// patchCredito is the canonical form of ONE sub-block of the credits patch: the 4
// operations, all optional. Edit is key→field patch
// (map[string]any tolerates any value; the shape validation already ran in the
// server).
type patchCredito struct {
	Reorder []string                  `json:"reorder"`
	Remove  []string                  `json:"remove"`
	Edit    map[string]map[string]any `json:"edit"`
	Add     []map[string]any          `json:"add"`
}

// aplicarPatchCredito applies the 4 operations of ONE sub-kind IN the settled
// ORDER: remove → edit → add → reorder. For `cast`, it rewrites the `order` field
// of each item at the end (0-based index), keeping richfields.Cast.Order coherent
// with the resulting list (crew/companies have no `order` field).
func aplicarPatchCredito(kind string, itens []map[string]any, raw json.RawMessage) []map[string]any {
	var ops patchCredito
	if json.Unmarshal(raw, &ops) != nil {
		return itens // invalid form: degrades to the intact items.
	}

	if len(ops.Remove) > 0 {
		removerSet := make(map[string]struct{}, len(ops.Remove))
		for _, k := range ops.Remove {
			removerSet[k] = struct{}{}
		}
		filtrados := make([]map[string]any, 0, len(itens))
		for _, it := range itens {
			if _, off := removerSet[chaveCredito(kind, it)]; !off {
				filtrados = append(filtrados, it)
			}
		}
		itens = filtrados
	}

	// Keys BEFORE the edit: for crew, the key includes `job` — a field that `edit`
	// itself can change. If the `reorder` (built by the front end with the OLD key)
	// matched the POST-edit key, the edited item would "vanish" from the reorder and
	// be pushed to the end of the list just for having been edited. Fixing the keys
	// here, before the mutation, keeps `edit` and `reorder` on the SAME key.
	chaves := make([]string, len(itens))
	for i, it := range itens {
		chaves[i] = chaveCredito(kind, it)
	}

	for i, it := range itens {
		if patchCampos, ok := ops.Edit[chaves[i]]; ok {
			for k, v := range patchCampos {
				it[k] = v
			}
		}
	}

	for _, novo := range ops.Add {
		itens = append(itens, novo)
		chaves = append(chaves, chaveCredito(kind, novo))
	}

	if len(ops.Reorder) > 0 {
		itens = reordenarCreditos(itens, chaves, ops.Reorder)
	}

	if kind == "cast" {
		for i := range itens {
			itens[i]["order"] = i
		}
	}

	return itens
}

// reordenarCreditos moves the items whose keys (pre-edit, `chaves[i]` matches
// `itens[i]`) appear in `reorder` to the corresponding position, IN the given
// ORDER; the remaining items (not mentioned) come afterwards, in the original
// relative order. Keys in `reorder` that do not exist (anymore) are silently
// ignored.
func reordenarCreditos(itens []map[string]any, chaves []string, reorder []string) []map[string]any {
	idx := make(map[string]int, len(itens))
	for i, k := range chaves {
		idx[k] = i
	}
	usado := make(map[string]bool, len(itens))
	out := make([]map[string]any, 0, len(itens))
	for _, k := range reorder {
		if i, ok := idx[k]; ok && !usado[k] {
			out = append(out, itens[i])
			usado[k] = true
		}
	}
	for i, it := range itens {
		k := chaves[i]
		if !usado[k] {
			out = append(out, it)
			usado[k] = true
		}
	}
	return out
}

// chaveCredito is the KEY of the credit for reorder/remove/edit: `source_id` for
// cast/companies; `source_id + "|" + job` for crew — the SAME key that
// equipeSimples/equipeAgregada (tmdb_creditos.go) already use for dedup, because a
// crew member can hold two jobs with the same source_id. An absent `job` (e.g.:
// incomplete custom credit) is treated as "".
func chaveCredito(kind string, item map[string]any) string {
	sourceID, _ := item["source_id"].(string)
	if kind != "crew" {
		return sourceID
	}
	job, _ := item["job"].(string)
	return sourceID + "|" + job
}

// clonarListaCreditos converts the base of a sub-kind (an []any of map[string]any,
// the same shape that the items of rico["cast"] etc. already have) into a
// []map[string]any of SHALLOW COPIES — so the patch (edit/reorder of `order`) does
// not mutate the maps of the original external layer.
func clonarListaCreditos(v any) []map[string]any {
	itensAny, _ := v.([]any)
	out := make([]map[string]any, 0, len(itensAny))
	for _, it := range itensAny {
		if m, ok := it.(map[string]any); ok {
			out = append(out, clonarCredito(m))
		}
	}
	return out
}

// clonarCredito makes a shallow copy of a credit item.
func clonarCredito(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// creditosParaAny converts the []map[string]any back into []any — the shape that
// richfields.CastDe/CrewDe/CompaniesDe (through comoListaAny) expect.
func creditosParaAny(itens []map[string]any) []any {
	out := make([]any, len(itens))
	for i, it := range itens {
		out[i] = it
	}
	return out
}

// ── Per-language composition (translatables) ────────────────────────────────

// comporTraduzivel composes a TRANSLATABLE field in the language × layer
// dimension (ADR-0019). It walks the `cadeia` (`pedido → fallback → original → …`);
// for each language it tries `override[campo][idioma]` (presence decides; `null`
// erases AND INTERRUPTS the fallback — R6 per language) and otherwise
// `externo[campo][idioma]` (first source with a value); the FIRST step with
// presence defines the value and the PROVENANCE (effective language). No language
// of the chain with a value falls back to the FILE base (neutral), provenance
// `""`. Returns (value, effective language).
func comporTraduzivel(campo string, orig origemCampo, arquivo any, cadeia []string, principal string, externo []CamadaExterna, override map[string]json.RawMessage) (any, string) {
	if orig.kind == kindLista {
		return comporListaIdioma(campo, arquivo, cadeia, principal, externo, override)
	}
	return comporEscalarIdioma(campo, arquivo, cadeia, principal, externo, override)
}

// comporEscalarIdioma resolves title/description per language. See
// comporTraduzivel.
func comporEscalarIdioma(campo string, arquivo any, cadeia []string, principal string, externo []CamadaExterna, override map[string]json.RawMessage) (*string, string) {
	rawOv, temOv := override[campo]
	idiomaMap, ehMap := parseIdiomaMap(rawOv, temOv, kindEscalar)
	for _, idioma := range cadeia {
		// OVERRIDE of the language: presence decides (null erases and interrupts).
		if temOv {
			if ehMap {
				if r, ok := idiomaMap[idioma]; ok {
					return escalarDeRaw(r), idioma
				}
			} else if idioma == principal {
				// Legacy: an override with no language key applies under the main one.
				return escalarDeRaw(rawOv), idioma
			}
		}
		// EXTERNAL of the language: first source with a value.
		if v, ok := externoEscalarIdioma(externo, campo, idioma, principal); ok {
			return v, idioma
		}
	}
	// FILE base (neutral): last resort of the chain.
	return asStrPtr(arquivo), ""
}

// comporListaIdioma resolves tags per language: inside the chosen language, the
// `{add,remove}`/array/null override does the MERGE over the external base of that
// language (or the file base when the source has no value for it) — R3 per
// language.
func comporListaIdioma(campo string, arquivo any, cadeia []string, principal string, externo []CamadaExterna, override map[string]json.RawMessage) ([]string, string) {
	arquivoTags := asStrSlice(arquivo)
	rawOv, temOv := override[campo]
	idiomaMap, ehMap := parseIdiomaMap(rawOv, temOv, kindLista)
	for _, idioma := range cadeia {
		base := arquivoTags
		extList, extOk := externoListaIdioma(externo, campo, idioma, principal)
		if extOk {
			base = extList
		}
		if temOv {
			if ehMap {
				if r, ok := idiomaMap[idioma]; ok {
					return aplicarOverrideLista(base, r), idioma
				}
			} else if idioma == principal {
				return aplicarOverrideLista(base, rawOv), idioma
			}
		}
		if extOk {
			return dedup(base), idioma
		}
	}
	return dedup(arquivoTags), ""
}

// parseIdiomaMap decides whether the RAW override of a translatable field is in the
// BY LANGUAGE format (`{"pt-BR": …, "en": …}`) or in the LEGACY format
// (scalar/array/operations object, with no language key). Only a JSON OBJECT can be
// a per-language map; for lists, an operations object `{add,remove}` is legacy, not
// language. Returns (language→raw map, ehMap). When ehMap=false, the caller treats
// the whole override as the value of the main language.
func parseIdiomaMap(raw json.RawMessage, temOv bool, kind kindCampo) (map[string]json.RawMessage, bool) {
	if !temOv {
		return nil, false
	}
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '{' {
		return nil, false // scalar, array or null → legacy
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	if kind == kindLista && apenasChavesDeOperacao(m) {
		return nil, false // {add,remove} → legacy operation, not a per-language map
	}
	return m, true
}

// apenasChavesDeOperacao says whether all keys of the object are list OPERATION
// (`add`/`remove`) keys — which characterizes the legacy list override (not a
// per-language map). An empty object counts as an operation (empty merge over the
// base).
func apenasChavesDeOperacao(m map[string]json.RawMessage) bool {
	for k := range m {
		if k != "add" && k != "remove" {
			return false
		}
	}
	return true
}

// escalarDeRaw decodes a raw override scalar value into a *string: `null` → nil
// (erases), `"s"` → &s, another form degrades to nil. It mirrors comporEscalar.
func escalarDeRaw(raw json.RawMessage) *string {
	var v *string
	_ = json.Unmarshal(raw, &v)
	return v
}

// externoEscalarIdioma searches, in the source priority order, the first scalar
// value of the field for the requested language. The field's external may come as
// `map[idioma]valor` (the per-language format) OR as a plain scalar (the LEGACY
// format) — the legacy one only matches when the requested language is the main one
// (ADR-0018).
func externoEscalarIdioma(externo []CamadaExterna, campo, idioma, principal string) (*string, bool) {
	for _, cam := range externo {
		v, ok := cam.Campos[campo]
		if !ok || v == nil {
			continue
		}
		if m, isMap := v.(map[string]any); isMap {
			if val, has := m[idioma]; has {
				if s, ok := val.(string); ok {
					return &s, true
				}
			}
			continue
		}
		if idioma == principal {
			if s, ok := v.(string); ok {
				return &s, true
			}
		}
	}
	return nil, false
}

// externoListaIdioma searches the field's list for the requested language, with the
// same tolerance to the legacy format as externoEscalarIdioma (plain list = main
// language).
func externoListaIdioma(externo []CamadaExterna, campo, idioma, principal string) ([]string, bool) {
	for _, cam := range externo {
		v, ok := cam.Campos[campo]
		if !ok || v == nil {
			continue
		}
		if m, isMap := v.(map[string]any); isMap {
			if val, has := m[idioma]; has && val != nil {
				return asStrSlice(val), true
			}
			continue
		}
		if idioma == principal {
			return asStrSlice(v), true
		}
	}
	return nil, false
}

// comporEscalar applies the precedence to a scalar field: override (key present,
// even `null` → wins, erasing with nil) ⟶ otherwise external (first source with a
// value, if the base is external) ⟶ otherwise file. It always returns a *string.
func comporEscalar(campo string, orig origemCampo, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) *string {
	if raw, ok := override[campo]; ok {
		// Presence of the key decides the override; the value decides the content:
		// `null` → nil (erases); "s" → &s; any other form degrades to nil.
		var v *string
		_ = json.Unmarshal(raw, &v)
		return v
	}
	if orig.base == baseExterno {
		if v, ok := primeiroExternoEscalar(externo, campo); ok {
			return v
		}
	}
	return asStrPtr(arquivo)
}

// primeiroExternoEscalar returns the value of the FIRST source (in priority order)
// whose Campos[campo] exists and is a string — the external base of the field.
func primeiroExternoEscalar(externo []CamadaExterna, campo string) (*string, bool) {
	for _, cam := range externo {
		if v, ok := cam.Campos[campo]; ok && v != nil {
			if s, ok := v.(string); ok {
				return &s, true
			}
		}
	}
	return nil, false
}

// comporLista composes a list field by MERGE OF OPERATIONS (R3/ADR-0003). The base
// is the external list (first source with a value) or the file one; the
// `{add, remove}` override rewrites over it. Order: base, removals by value,
// additions at the end, dedup. A plain array override is a total replacement;
// `null` erases the base.
func comporLista(campo string, orig origemCampo, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) []string {
	base := asStrSlice(arquivo)
	if orig.base == baseExterno {
		if v, ok := primeiroExternoLista(externo, campo); ok {
			base = v
		}
	}
	if raw, ok := override[campo]; ok {
		return aplicarOverrideLista(base, raw)
	}
	return dedup(base)
}

// primeiroExternoLista returns the list of the FIRST source with a value for the
// field.
func primeiroExternoLista(externo []CamadaExterna, campo string) ([]string, bool) {
	for _, cam := range externo {
		if v, ok := cam.Campos[campo]; ok && v != nil {
			return asStrSlice(v), true
		}
	}
	return nil, false
}

// aplicarOverrideLista interprets the override of a list field: `null` erases the
// base; a plain array replaces the base; an `{add, remove}` object does the merge of
// operations. Canonical form = `{add, remove}` (ADR-0003); an array is the shortcut
// for total replacement. Any other form degrades to the intact base.
func aplicarOverrideLista(base []string, raw json.RawMessage) []string {
	if ehNull(raw) {
		return []string{}
	}
	// Total replacement (plain array).
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return dedup(arr)
	}
	// Merge by operations (canonical form).
	var ops struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := json.Unmarshal(raw, &ops); err == nil {
		return mesclarLista(base, ops.Add, ops.Remove)
	}
	return dedup(base)
}

// mesclarLista applies the operations over the base: removal by value, the
// additions appended at the end, and dedup preserving the first occurrence (stable
// order).
func mesclarLista(base, add, remove []string) []string {
	remover := make(map[string]struct{}, len(remove))
	for _, r := range remove {
		remover[r] = struct{}{}
	}
	out := make([]string, 0, len(base)+len(add))
	for _, v := range base {
		if _, off := remover[v]; !off {
			out = append(out, v)
		}
	}
	out = append(out, add...)
	return dedup(out)
}

// comporExtensao composes extension_data by NAMESPACE and by inner KEY with the same
// precedence (R5/ADR-0005): the file is the base, each external source overrides it
// (the highest priority one wins), and the override rewrites on top — with `null`
// erasing the whole namespace or a key. Shallow recursive composition
// (namespace → object of fields). It always returns a non-nil map.
func comporExtensao(campo string, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) map[string]any {
	merged := clonarExt(asObjMap(arquivo))

	// Externo: index 0 is the HIGHEST priority. It applies from the end to the
	// beginning so the highest priority source prevails per namespace/key.
	for i := len(externo) - 1; i >= 0; i-- {
		mesclarExt(merged, asObjMap(externo[i].Campos[campo]))
	}

	if raw, ok := override[campo]; ok {
		aplicarOverrideExt(merged, raw)
	}
	return merged
}

// mesclarExt overlays `src` onto `dst` per namespace/key: each key of src wins over
// the one in dst; new namespaces enter whole.
func mesclarExt(dst, src map[string]any) {
	for ns, obj := range src {
		alvo := asObjMap(dst[ns])
		for k, v := range asObjMap(obj) {
			alvo[k] = v
		}
		dst[ns] = alvo
	}
}

// aplicarOverrideExt rewrites the composed extension_data with the owner's patch. A
// top-level `null` erases everything; `null` in a namespace erases the namespace;
// `null` in a key erases the key; any other value overrides. An absent
// key/namespace inherits the base.
func aplicarOverrideExt(merged map[string]any, raw json.RawMessage) {
	if ehNull(raw) {
		for ns := range merged {
			delete(merged, ns)
		}
		return
	}
	var namespaces map[string]json.RawMessage
	if err := json.Unmarshal(raw, &namespaces); err != nil {
		return
	}
	for ns, rawNs := range namespaces {
		if ehNull(rawNs) {
			delete(merged, ns)
			continue
		}
		var chaves map[string]json.RawMessage
		if err := json.Unmarshal(rawNs, &chaves); err != nil {
			continue
		}
		alvo := asObjMap(merged[ns])
		for k, rawV := range chaves {
			if ehNull(rawV) {
				delete(alvo, k)
				continue
			}
			var val any
			if err := json.Unmarshal(rawV, &val); err == nil {
				alvo[k] = val
			}
		}
		merged[ns] = alvo
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// temPosse applies the DERIVED OWNERSHIP (ADR-0009) over the already loaded files:
// true if, and only if, there is at least one media_file with state='present'.
func temPosse(files []sqlcgen.MediaFile) bool {
	for _, f := range files {
		if f.State == "present" {
			return true
		}
	}
	return false
}

// parseTags decodes tags_json (a JSON array of strings) into a slice. It always
// returns a non-nil slice (empty when absent/invalid) so the JSON comes out as
// `[]` instead of `null`.
func parseTags(raw sql.NullString) []string {
	out := []string{}
	if !raw.Valid || raw.String == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw.String), &out)
	if out == nil {
		return []string{}
	}
	return out
}

// parseExtensionData decodes extension_data_json (a JSON object namespace→object)
// into a map. It always returns a non-nil map (empty when absent/invalid) so the
// JSON comes out as `{}` instead of `null`.
func parseExtensionData(raw sql.NullString) map[string]any {
	out := map[string]any{}
	if !raw.Valid || raw.String == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw.String), &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

// parseExternalIDs decodes external_ids_json (a JSON object string->string).
// Non-string values are ignored. It always returns a non-nil map.
func parseExternalIDs(raw sql.NullString) map[string]string {
	out := map[string]string{}
	if !raw.Valid || raw.String == "" {
		return out
	}
	bruto := map[string]any{}
	if err := json.Unmarshal([]byte(raw.String), &bruto); err != nil {
		return out
	}
	for k, v := range bruto {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// nsPtr converts a sql.NullString into a *string (nil when NULL) so the field comes
// out as `null` in the JSON.
func nsPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// strPtr wraps a string in a *string (the file base of the scalars is always a
// pointer, to uniformize the composition).
func strPtr(s string) *string { return &s }

// asStrPtr extracts a *string from a dynamic value of the composition (nil when the
// value is not a *string).
func asStrPtr(v any) *string {
	if p, ok := v.(*string); ok {
		return p
	}
	return nil
}

// asFloatPtr extracts a *float64 from a dynamic value of the composition (nil when
// the value is not a *float64) — used by the numeric field (local rating).
func asFloatPtr(v any) *float64 {
	if p, ok := v.(*float64); ok {
		return p
	}
	return nil
}

// derefStr dereferences a dynamic *string into a string ("" when nil) — used in the
// title field, which is non-nullable in the shape.
func derefStr(v any) string {
	if p, ok := v.(*string); ok && p != nil {
		return *p
	}
	return ""
}

// asStrSlice converts a dynamic value into a non-nil slice of strings. It accepts
// []string and []any (with string elements), ignoring whatever is not a string.
func asStrSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		if s == nil {
			return []string{}
		}
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
		return []string{}
	}
}

// asObjMap converts a dynamic value into a non-nil object (map[string]any).
func asObjMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}

// clonarExt makes a shallow-per-namespace copy of extension_data (namespace level
// and key level), so the composition does not mutate the original file base.
func clonarExt(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for ns, obj := range src {
		copia := make(map[string]any)
		for k, v := range asObjMap(obj) {
			copia[k] = v
		}
		out[ns] = copia
	}
	return out
}

// dedup removes duplicates preserving the first occurrence; it returns a non-nil
// slice.
func dedup(in []string) []string {
	visto := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := visto[v]; ok {
			continue
		}
		visto[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// ehNull says whether the raw JSON is the literal `null` (after trimming spaces) —
// the ERASE signal in the presence-of-the-key rule (R6).
func ehNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// MatchSuggestion is a candidate the scanner found for a name it could not resolve, kept
// for the owner to accept or ignore. Source/SourceID identify the CANDIDATE (the entity
// itself has no identity yet) -- with one nuance: for the 'tree_mismatch' reason the
// candidate IS the current identity, because the suspicion is about it (see Reason and
// CanApply).
type MatchSuggestion struct {
	Source    string  `json:"source"`
	SourceID  string  `json:"source_id"`
	Title     string  `json:"title"`
	Year      *string `json:"year"`
	PosterURL *string `json:"poster_url"`
	// Score is the similarity [0,100] between the name ON DISK and the candidate: it says
	// how plausible the hint is, and it is what the UI uses to sort/justify it.
	Score int `json:"score"`
	// Query is the variant that found it (e.g. "2 Broke"), so the hint can explain itself.
	Query string `json:"query"`
	// Reason is WHY the owner is being asked: "name" (a cheaper variant of the name on disk
	// found this candidate), "empty_tree" (the source has no episodes for the current match)
	// or "tree_mismatch" (the tree exists but does not fit the files). It selects the
	// sentence the screen shows, nothing else.
	Reason string `json:"reason"`
	// CanApply is false when the candidate IS the current identity: offering to "apply" what
	// is already there would be a button that does nothing (or worse, re-applies the
	// suspicion). The screen hides the action in that case.
	CanApply bool `json:"can_apply"`
	// FilesUnmatched/FilesTotal are the evidence of the 'tree_mismatch' alert: how many
	// episodes of the series found no place in the tree, out of how many. Zero for the other
	// reasons (a name hint has no file census).
	FilesUnmatched int `json:"files_unmatched"`
	FilesTotal     int `json:"files_total"`
}
