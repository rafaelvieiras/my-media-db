package tvdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// This file covers the canonical DETAIL of the SERIES (the analogue of
// BuscarDetalhe/MapearDetalhe of tmdb for the "tv" type): it pulls
// /series/{id}/extended + the allowed translations (por/…, with fallback) into an
// AUGMENTED PAYLOAD (re-composable envelope, ADR-0011) and maps it -- PURE, without
// network -- to the canonical map with our field names. remoteIds become
// external_ids/external_links (crossing with TMDB/IMDB).

// respSerie wraps the response of /series/{id}/extended ({status, data}).
type respSerie struct {
	Data serieData `json:"data"`
}

// serieData mirrors the SeriesExtendedRecord (only the fields used). seasonTypes[] and
// seasons[] feed the tree (tvdb_serie.go); remoteIds the id crossing.
type serieData struct {
	ID                int64           `json:"id"`
	Name              string          `json:"name"`
	Slug              string          `json:"slug"`
	Image             string          `json:"image"`
	Overview          string          `json:"overview"`
	FirstAired        string          `json:"firstAired"`
	Year              string          `json:"year"`
	OriginalLanguage  string          `json:"originalLanguage"`
	AverageRuntime    int             `json:"averageRuntime"`
	DefaultSeasonType int             `json:"defaultSeasonType"`
	RemoteIds         []remoteID      `json:"remoteIds"`
	SeasonTypes       []seasonTypeRec `json:"seasonTypes"`
	Seasons           []seasonRec     `json:"seasons"`

	// Status/Genres of /series/{id}/extended: the FALLBACK when there is no match in
	// TMDB. status.name is the raw SITUATION (e.g. "Continuing"|"Ended"); genres[] are the
	// genres (name). TVDB has no tagline/keywords/similar -- graceful absence.
	Status struct {
		Name string `json:"name"`
	} `json:"status"`
	Genres []generoSerie `json:"genres"`
}

// generoSerie is a genre of /series/{id}/extended (only the name interests us as a tag).
type generoSerie struct {
	Name string `json:"name"`
}

// remoteID is an id of the item in another source (remoteIds[]): id + sourceName (the
// name is more stable than the numeric `type` to map tvdb/imdb/tmdb).
type remoteID struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}

// seasonTypeRec is an ordering that the series uses (SeasonType): id + type (e.g. "official").
type seasonTypeRec struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// seasonRec is a season of extended.seasons[] (SeasonBaseRecord + embedded type). number
// can be a year/0/index -- do not assume 1..N. type.type tells the ordering.
type seasonRec struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Year   string `json:"year"`
	Image  string `json:"image"`
	Name   string `json:"name"`
	Type   struct {
		Type string `json:"type"`
	} `json:"type"`
}

// traducaoSerie are the translatable fields of a series translation (name/overview).
type traducaoSerie struct {
	Name     string `json:"name"`
	Overview string `json:"overview"`
}

// envelopeSerie is the AUGMENTED PAYLOAD written to media_external: the raw detail of
// /series/{id}/extended (`data`) plus the translations per app language (derived in enrich,
// which does network -- read-time is pure). Empty Detail ⇒ LEGACY payload (mono-language).
type envelopeSerie struct {
	Detail      json.RawMessage          `json:"detail"`
	TransByLang map[string]traducaoSerie `json:"trans_by_lang"`
}

// BuscarDetalhe pulls the detail of the SERIES already MULTI-LANGUAGE:
// /series/{id}/extended plus /series/{id}/translations/{code} for each allowed language
// (with FALLBACK -- a language without a translation is skipped, it does not break). It
// returns the re-composable envelope {detail, trans_by_lang}. `tipo` must be "tv" (the
// source only covers series). Error propagates.
func (c *Client) BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("tvdb: sourceID vazio")
	}
	if tipo != "tv" {
		return nil, fmt.Errorf("tvdb: tipo invalido %q (a fonte cobre series: tv)", tipo)
	}
	detail, err := c.getBytes(ctx, "/series/"+url.PathEscape(sourceID)+"/extended", nil)
	if err != nil {
		return nil, err
	}
	var resp respSerie
	if err := json.Unmarshal(detail, &resp); err != nil {
		return nil, fmt.Errorf("tvdb: detalhe invalido: %w", err)
	}
	env := envelopeSerie{
		Detail:      json.RawMessage(detail),
		TransByLang: c.traducoesSerie(ctx, sourceID, langs),
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("tvdb: montar payload aumentado: %w", err)
	}
	return json.RawMessage(body), nil
}

// traducoesSerie fetches the series translation in each allowed language (dedup by TheTVDB
// code), skipping the absent ones (fallback). It keys by the requested app code.
func (c *Client) traducoesSerie(ctx context.Context, sourceID string, langs []string) map[string]traducaoSerie {
	out := map[string]traducaoSerie{}
	vistos := map[string]bool{}
	for _, lang := range langs {
		code := codigoTVDB(lang)
		if code == "" || vistos[code] {
			continue
		}
		vistos[code] = true
		tr, ok := c.traducaoSerie(ctx, sourceID, code)
		if ok {
			out[lang] = tr
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// traducaoSerie fetches ONE series translation; ok=false when absent (404 → fallback) or
// empty. Network errors also degrade to ok=false (the translation is best-effort).
func (c *Client) traducaoSerie(ctx context.Context, sourceID, code string) (traducaoSerie, bool) {
	body, err := c.getBytes(ctx, "/series/"+url.PathEscape(sourceID)+"/translations/"+url.PathEscape(code), nil)
	if err != nil {
		return traducaoSerie{}, false
	}
	var resp struct {
		Data traducaoSerie `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return traducaoSerie{}, false
	}
	if resp.Data.Name == "" && resp.Data.Overview == "" {
		return traducaoSerie{}, false
	}
	return resp.Data, true
}

// MapearDetalhe translates the TheTVDB payload to the CANONICAL map (our names). It is PURE
// (no network): it detects the ENVELOPE vs the LEGACY one (raw extended) and maps
// accordingly. It only includes the keys that the source brought. WITHOUT cast/crew (MD6).
func (c *Client) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	// EPISODE: payload with the entity_kind discriminator -- title/description per
	// language. It comes BEFORE the series branch (the same "tvdb" source serves both
	// shapes); a payload without the discriminator falls through below, without breaking
	// the series mapping.
	if campos, ok := provider.MapearEpisodio(raw); ok {
		return campos, nil
	}
	var env envelopeSerie
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Detail) > 0 {
		return mapearEnvelopeSerie(env)
	}
	return mapearDetailBruto(raw, nil)
}

// mapearEnvelopeSerie maps the augmented payload: title/description per language (from
// the detail + translations) and the neutral/scalar ones.
func mapearEnvelopeSerie(env envelopeSerie) (map[string]any, error) {
	return mapearDetailBruto(env.Detail, env.TransByLang)
}

// mapearDetailBruto maps the raw /series/{id}/extended (`data` or envelope-response) and
// the translations per language into the canonical map. A nil `trans` degrades to
// mono-language.
func mapearDetailBruto(detail json.RawMessage, trans map[string]traducaoSerie) (map[string]any, error) {
	var resp respSerie
	if err := json.Unmarshal(detail, &resp); err != nil {
		return nil, fmt.Errorf("tvdb: detalhe do envelope invalido: %w", err)
	}
	d := resp.Data
	out := map[string]any{}
	titulos, descricoes := traduzidosSerie(d, trans)
	if len(titulos) > 0 {
		out["title"] = titulos
	}
	if len(descricoes) > 0 {
		out["description"] = descricoes
	}
	enriquecerNeutrosSerie(out, d)
	return out, nil
}

// traduzidosSerie builds the language→title and language→description maps: it anchors the
// ORIGINAL language with the name/overview of the top (guaranteed to be in the original
// language) and overlays each allowed translation, keyed by the app code (combined +
// base).
func traduzidosSerie(d serieData, trans map[string]traducaoSerie) (titulos, descricoes map[string]any) {
	titulos = map[string]any{}
	descricoes = map[string]any{}
	if orig := codigoApp(d.OriginalLanguage); orig != "" {
		porSeAusente(titulos, orig, d.Name)
		porSeAusente(descricoes, orig, d.Overview)
	}
	for lang, tr := range trans {
		for _, chave := range codigosApp(lang) {
			porSeAusente(titulos, chave, tr.Name)
			porSeAusente(descricoes, chave, tr.Overview)
		}
	}
	return titulos, descricoes
}

// enriquecerNeutrosSerie writes the NEUTRAL/scalar fields of the detail (original title,
// original language, date, poster, duration) and the external ids
// (external_ids/links).
func enriquecerNeutrosSerie(out map[string]any, d serieData) {
	if d.Name != "" {
		out["original_title"] = d.Name
	}
	if orig := codigoApp(d.OriginalLanguage); orig != "" {
		out["original_language"] = orig
	}
	if d.FirstAired != "" {
		out["release_date"] = d.FirstAired
	}
	if d.Image != "" {
		out["poster_url"] = d.Image
	}
	if d.AverageRuntime > 0 {
		out["runtime"] = d.AverageRuntime
	}
	if ids := idsExternos(d); len(ids) > 0 {
		out["external_ids"] = ids
	}
	if links := linksExternos(d); len(links) > 0 {
		out["external_links"] = links
	}
	enriquecerFallbackRico(out, d)
}

// enriquecerFallbackRico emits the rich fields that TVDB has and that used to be discarded,
// used as a FALLBACK when there is no match in TMDB for the series node: the raw SITUATION
// (airing_status, neutral scalar) and the genres of /extended as tags anchored in the
// ORIGINAL language (TVDB does not localize them) -- it mirrors keywordsPorIdioma of TMDB,
// the same language×layer dimension that tags uses. Tagline/keywords/similar do not exist
// in TVDB (graceful absence).
func enriquecerFallbackRico(out map[string]any, d serieData) {
	if d.Status.Name != "" {
		out["airing_status"] = d.Status.Name
	}
	if orig := codigoApp(d.OriginalLanguage); orig != "" {
		if nomes := nomesGeneros(d.Genres); len(nomes) > 0 {
			out["tags"] = map[string]any{orig: nomes}
		}
	}
}

// nomesGeneros extracts the genre names of /extended as a tag list (discarding the empty
// ones). Stable order = order of the source.
func nomesGeneros(gs []generoSerie) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}

// idsExternos builds the source→id map from the remoteIds (it always includes tvdb=id of
// the series). The mapping is by sourceName (more stable than the numeric `type`).
// Exclusive series (Looney Tunes) will only have tvdb -- without tmdb/imdb (ADR-0021).
func idsExternos(d serieData) map[string]any {
	out := map[string]any{"tvdb": strconv.FormatInt(d.ID, 10)}
	for _, r := range d.RemoteIds {
		if chave := chaveFonte(r.SourceName); chave != "" && r.ID != "" {
			if _, existe := out[chave]; !existe {
				out[chave] = r.ID
			}
		}
	}
	return out
}

// chaveFonte normalizes the sourceName of the remoteId to our canonical key. Only the
// sources that interest us for crossing identity; the others are ignored.
func chaveFonte(sourceName string) string {
	switch sourceName {
	case "TheMovieDB.com":
		return "tmdb"
	case "IMDB":
		return "imdb"
	case "Wikidata":
		return "wikidata"
	case "TV Maze":
		return "tvmaze"
	default:
		return ""
	}
}

// linksExternos builds the links to external pages from the crossed ids (today imdb and
// tmdb). It reuses idsExternos to avoid reparsing the remoteIds.
func linksExternos(d serieData) map[string]any {
	ids := idsExternos(d)
	out := map[string]any{}
	if imdb, ok := ids["imdb"].(string); ok && imdb != "" {
		out["imdb"] = "https://www.imdb.com/title/" + imdb
	}
	if tmdb, ok := ids["tmdb"].(string); ok && tmdb != "" {
		out["tmdb"] = "https://www.themoviedb.org/tv/" + tmdb
	}
	return out
}

// porSeAusente writes a non-empty value under a non-empty key only if the key does not
// exist yet (the first occurrence per language wins).
func porSeAusente(m map[string]any, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}
