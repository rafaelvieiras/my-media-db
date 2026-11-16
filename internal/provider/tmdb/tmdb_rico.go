package tmdb

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// This file concentrates the RICH DETAIL of TMDB: the structs of the blocks coming
// from the widened append_to_response and the mapping of those blocks to the
// CANONICAL map (our field names). It is PURE (no network) -- it runs inside
// MapearDetalhe, at read time. The TRANSLATABLE fields (tagline, keywords) come out
// as `map[language]value`; the NEUTRAL ones (runtime, certification_by_region,
// backdrop_url, videos, collection, similar, vote_average, external_links) as
// scalars/lists. WITHOUT cast/crew (MD6) -- the owner's decision, out of this
// version.

// colecaoTMDB is the belongs_to_collection of the movie detail (the collection it
// belongs to, ≠ playlist). Only movies have it; series come without it.
type colecaoTMDB struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	PosterPath string `json:"poster_path"`
}

// videosTMDB wraps the list of videos (trailer/clip/teaser) of the `videos` append.
type videosTMDB struct {
	Results []videoTMDB `json:"results"`
}

// videoTMDB is a video from the source: name, key in the provider (YT/Vimeo), site,
// type (Trailer|Clip|Teaser…) and whether it is official. iso_639_1 is the language
// of the video (informative).
type videoTMDB struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Site     string `json:"site"`
	Type     string `json:"type"`
	Official bool   `json:"official"`
	Iso639   string `json:"iso_639_1"`
}

// keywordsTMDB wraps the keywords of the `keywords` append. TMDB uses `keywords` for
// a movie and `results` for a series -- both are lists {id,name}. The keywords are NOT
// localized by TMDB (they come in a single language), so the canonical anchors them
// under the original language of the item (the detail resolver falls back to them
// through the chain/last resort).
type keywordsTMDB struct {
	Keywords []generoTMDB `json:"keywords"` // movie
	Results  []generoTMDB `json:"results"`  // series
}

// itens returns the list of keywords, whether from a movie (`keywords`) or a series (`results`).
func (k keywordsTMDB) itens() []generoTMDB {
	if len(k.Keywords) > 0 {
		return k.Keywords
	}
	return k.Results
}

// listaPaisTMDB is the `release_dates` append (age rating of a MOVIE): one list per
// country, each one with the dates/certifications of that country.
type listaPaisTMDB struct {
	Results []paisReleaseDatesTMDB `json:"results"`
}

// paisReleaseDatesTMDB are the release dates of a country and their certifications.
type paisReleaseDatesTMDB struct {
	Iso3166      string                 `json:"iso_3166_1"`
	ReleaseDates []certificacaoDataTMDB `json:"release_dates"`
}

// certificacaoDataTMDB is a release_date entry with the certification (it may come
// empty in some entries -- the first non-empty one of the country is the one we use).
type certificacaoDataTMDB struct {
	Certification string `json:"certification"`
}

// contentRatingsTMDB is the `content_ratings` append (age rating of a SERIES): one
// rating per country.
type contentRatingsTMDB struct {
	Results []paisContentRatingTMDB `json:"results"`
}

// paisContentRatingTMDB is the series rating of a country.
type paisContentRatingTMDB struct {
	Iso3166 string `json:"iso_3166_1"`
	Rating  string `json:"rating"`
}

// externalIdsTMDB is the `external_ids` append: imdb_id (link to the external page)
// and tvdb_id (the cross-id that fanart.tv requires for TV; TMDB already returns it for
// free for /tv/{id}, empty/harmless for /movie/{id}).
type externalIdsTMDB struct {
	ImdbID string         `json:"imdb_id"`
	TvdbID idFlexivelTMDB `json:"tvdb_id"`
}

// idFlexivelTMDB accepts `tvdb_id` coming as a STRING or a JSON NUMBER -- TMDB is
// inconsistent between titles/endpoints on this specific field. Before this tolerance,
// a single title with a numeric tvdb_id made the Unmarshal of the WHOLE detail fail,
// bringing down title/overview/cast/tagline along with it (mapearEnvelope aborts
// everything on any subfield error).
type idFlexivelTMDB string

func (id *idFlexivelTMDB) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*id = idFlexivelTMDB(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*id = idFlexivelTMDB(n.String())
	return nil
}

// recommendationsTMDB wraps the recommended items of the `recommendations` append
// (switched from `similar`, see tmdb.go).
type recommendationsTMDB struct {
	Results []itemRecommendationTMDB `json:"results"`
}

// itemRecommendationTMDB is a recommended item (same media_type as the parent).
// title/release_date are movie ones; name/first_air_date series ones -- the parent
// defines the type (the item inherits it). The QUALITY fields
// (genre_ids/vote_average/vote_count/adult) were added so that the payload of
// `recommendations`, which already carried them, would stop being discarded in the
// unmarshal -- they only serve the filter/sorting of similaresFiltrados, they are never
// exposed in the canonical (provider.Similar).
type itemRecommendationTMDB struct {
	ID           int64   `json:"id"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	GenreIds     []int64 `json:"genre_ids"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	Adult        bool    `json:"adult"`
}

// enriquecerCanonicoRico adds the fields of the RICH DETAIL to the canonical map, in
// the convention of ADR-0018 (translatable ones as a map by language, neutral ones as
// scalars/lists). It only writes keys with a value -- the consumer distinguishes
// "absent field" from "empty field". It does not touch the keys already set by
// mapearEnvelope.
func enriquecerCanonicoRico(out map[string]any, d detalheTMDB) {
	enriquecerTraduziveisRico(out, d)
	enriquecerNeutrosRico(out, d)
	enriquecerCreditos(out, d)
}

// enriquecerTraduziveisRico writes the TRANSLATABLE rich fields (map by language):
// tagline and keywords.
func enriquecerTraduziveisRico(out map[string]any, d detalheTMDB) {
	if taglines := taglinesPorIdioma(d); len(taglines) > 0 {
		out["tagline"] = taglines
	}
	if kws := keywordsPorIdioma(d); len(kws) > 0 {
		out["keywords"] = kws
	}
}

// enriquecerNeutrosRico writes the NEUTRAL rich fields (scalars/lists): duration, rating
// by region, backdrop, score, videos, collection and links. The SIMILAR ones are no
// longer filled in here -- that responsibility migrated entirely to the Similares method
// (provider.SimilarProvider), called explicitly by the server
// (entities.go/external_detail.go) outside MapearDetalhe, so that the filter/sorting by
// affinity is a real extension point (see provider.go).
func enriquecerNeutrosRico(out map[string]any, d detalheTMDB) {
	if r := runtimeDe(d); r > 0 {
		out["runtime"] = r
	}
	if cert := certificacoesPorRegiao(d); len(cert) > 0 {
		out["certification_by_region"] = cert
	}
	if d.BackdropPath != "" {
		out["backdrop_url"] = backdropBaseURL + d.BackdropPath
	}
	if d.VoteAverage > 0 {
		out["vote_average"] = d.VoteAverage
	}
	if vids := videosCanonicos(d.Videos); len(vids) > 0 {
		out["videos"] = vids
	}
	if col := colecaoCanonica(d.BelongsToCollection); col != nil {
		out["collection"] = col
	}
	if links := linksExternos(d); len(links) > 0 {
		out["external_links"] = links
	}
}

// taglinesPorIdioma builds the language→tagline map from the translations of the
// detail, keying each language by the COMBINED code (pt-BR) and by the BASE one (pt) --
// the same convention as the titles (see traduzidosPorIdioma). The tagline of the top of
// the detail comes in the language of the request and also appears in the translations,
// so it needs no anchor.
func taglinesPorIdioma(d detalheTMDB) map[string]any {
	out := map[string]any{}
	for _, tr := range d.Translations.Translations {
		combinado := combinarIdioma(tr.Iso639, tr.Iso3166)
		if combinado == "" || tr.Data.Tagline == "" {
			continue
		}
		porSeAusenteAny(out, combinado, tr.Data.Tagline)
		porSeAusenteAny(out, tr.Iso639, tr.Data.Tagline)
	}
	return out
}

// keywordsPorIdioma returns the keywords under the ORIGINAL language of the item (TMDB
// does not localize them). It stays in the language×layer dimension (a translatable
// field, a review decision), but populated in a single language; the detail resolver
// falls back to it through the chain or the last resort. Without an original language
// there is no way to anchor it -- it returns empty.
func keywordsPorIdioma(d detalheTMDB) map[string]any {
	nomes := nomesDosGeneros(d.Keywords.itens())
	if len(nomes) == 0 || d.OriginalLanguage == "" {
		return nil
	}
	return map[string]any{d.OriginalLanguage: nomes}
}

// runtimeDe returns the duration in minutes: `runtime` for a movie; the first
// `episode_run_time` for a series. 0 when the source did not provide one.
func runtimeDe(d detalheTMDB) int {
	if d.Runtime > 0 {
		return d.Runtime
	}
	for _, r := range d.EpisodeRunTime {
		if r > 0 {
			return r
		}
	}
	return 0
}

// certificacoesPorRegiao builds the REGION→rating map (neutral; the handler chooses the
// region of the active language with a US fallback). It comes from release_dates (movie)
// or content_ratings (series). For a movie, the first non-empty certification of the
// country wins.
func certificacoesPorRegiao(d detalheTMDB) map[string]any {
	out := map[string]any{}
	for _, pais := range d.ReleaseDates.Results {
		if pais.Iso3166 == "" {
			continue
		}
		for _, rd := range pais.ReleaseDates {
			if rd.Certification != "" {
				out[pais.Iso3166] = rd.Certification
				break
			}
		}
	}
	for _, pais := range d.ContentRatings.Results {
		if pais.Iso3166 != "" && pais.Rating != "" {
			if _, existe := out[pais.Iso3166]; !existe {
				out[pais.Iso3166] = pais.Rating
			}
		}
	}
	return out
}

// videosCanonicos converts the videos of the source into the canonical list (maps),
// building the provider URL when known (YouTube/Vimeo). It discards videos without a
// key. The "local ones featured" ordering does NOT apply to the external ones (there is
// no local file) -- the order is the source's; the UI decides the presentation.
func videosCanonicos(v videosTMDB) []any {
	out := make([]any, 0, len(v.Results))
	for _, vid := range v.Results {
		if vid.Key == "" {
			continue
		}
		item := map[string]any{
			"name":     vid.Name,
			"key":      vid.Key,
			"site":     vid.Site,
			"type":     vid.Type,
			"official": vid.Official,
		}
		if u := urlDoVideo(vid.Site, vid.Key); u != "" {
			item["url"] = u
		}
		out = append(out, item)
	}
	return out
}

// urlDoVideo builds the public URL of the video from the provider site; "" when the site
// is unknown (the UI may then only display the name/thumb).
func urlDoVideo(site, key string) string {
	switch site {
	case "YouTube":
		return "https://www.youtube.com/watch?v=" + key
	case "Vimeo":
		return "https://vimeo.com/" + key
	default:
		return ""
	}
}

// colecaoCanonica converts belongs_to_collection into the canonical collection (id as
// source_id, for navigating to the external detail, name and poster). nil when the item
// does not belong to a collection (or is a series).
func colecaoCanonica(c *colecaoTMDB) map[string]any {
	if c == nil || c.ID == 0 {
		return nil
	}
	out := map[string]any{
		"source_id": itoa(c.ID),
		"name":      c.Name,
	}
	if c.PosterPath != "" {
		out["poster_url"] = imagemBaseURL + c.PosterPath
	}
	return out
}

// votoMinimoSimilar is the vote_count floor below which a recommended item is discarded:
// vote volume is a better proxy of "recommendation of a real, known title, not noise"
// than the average score alone -- a title with 2 votes and a score of 8 is not "better"
// than one with 500 votes and a score of 6.5. There is no score floor: even a
// median-score title is a valid recommendation if it is of the right genre and widely
// watched.
const votoMinimoSimilar = 20

// similaresFiltrados filters and sorts the items of `d.Recommendations.Results` by
// quality and by affinity with the ORIGIN item:
//  1. it discards adult items (`Adult == true`) -- never adult content in the list;
//  2. it discards items with `VoteCount` below votoMinimoSimilar (statistical noise);
//  3. it sorts what is left by (a) genre overlap with the origin item, DESC -- using
//     the numeric IDs of `d.Genres` (the origin item, the SAME payload), never
//     translated/resolved tag names (it avoids a language/taxonomy mismatch between
//     sources); (b) `VoteAverage` DESC as a tiebreaker.
//
// It does not truncate the list beyond the filter -- what is left is returned whole
// (SliceStable preserves the original source order on a full tie).
func similaresFiltrados(d detalheTMDB) []itemRecommendationTMDB {
	generosOrigem := make(map[int64]struct{}, len(d.Genres))
	for _, g := range d.Genres {
		generosOrigem[g.ID] = struct{}{}
	}
	overlapDe := func(it itemRecommendationTMDB) int {
		n := 0
		for _, gid := range it.GenreIds {
			if _, ok := generosOrigem[gid]; ok {
				n++
			}
		}
		return n
	}

	itens := make([]itemRecommendationTMDB, 0, len(d.Recommendations.Results))
	for _, it := range d.Recommendations.Results {
		if it.Adult {
			continue
		}
		if it.VoteCount < votoMinimoSimilar {
			continue
		}
		itens = append(itens, it)
	}

	sort.SliceStable(itens, func(i, j int) bool {
		oi, oj := overlapDe(itens[i]), overlapDe(itens[j])
		if oi != oj {
			return oi > oj
		}
		return itens[i].VoteAverage > itens[j].VoteAverage
	})
	return itens
}

// similaresCanonicos converts the ALREADY FILTERED/SORTED items (similaresFiltrados)
// into the canonical list (provider.Similar), inheriting the TYPE of the parent
// (movie/series) -- used to navigate to the external detail of the similar. It discards
// items without a title (the usual rule).
func similaresCanonicos(d detalheTMDB) []provider.Similar {
	tipo := tipoInferido(d)
	filtrados := similaresFiltrados(d)
	out := make([]provider.Similar, 0, len(filtrados))
	for _, it := range filtrados {
		titulo := primeiroNaoVazio(it.Title, it.Name)
		if titulo == "" {
			continue
		}
		item := provider.Similar{
			SourceID: itoa(it.ID),
			Type:     tipo,
			Title:    titulo,
		}
		if ano := anoDe(primeiroNaoVazio(it.ReleaseDate, it.FirstAirDate)); ano != nil {
			item.Year = ano
		}
		if it.PosterPath != "" {
			u := imagemBaseURL + it.PosterPath
			item.PosterURL = &u
		}
		out = append(out, item)
	}
	return out
}

// Similares implements provider.SimilarProvider: it extracts the similar/recommended
// items from a detail payload ALREADY fetched by BuscarDetalhe -- PURE, without an extra
// round trip to the network (the same pattern as TvdbIDDoDetalheSerie). It accepts both
// the AUGMENTED envelope {detail,tags_by_lang} and the LEGACY payload without an
// envelope, via detalheBrutoDoRaw (the same detection as MapearDetalhe, extracted in
// tmdb.go so the two methods reuse it without duplicating the parsing).
func (c *Client) Similares(raw json.RawMessage) ([]provider.Similar, error) {
	var d detalheTMDB
	if err := json.Unmarshal(detalheBrutoDoRaw(raw), &d); err != nil {
		return nil, fmt.Errorf("tmdb: detalhe invalido para similares: %w", err)
	}
	return similaresCanonicos(d), nil
}

// linksExternos builds the map of links to external pages. Today only imdb (from
// imdb_id); the link of TMDB itself is built by the handler (it needs the type+id of
// the route).
func linksExternos(d detalheTMDB) map[string]any {
	out := map[string]any{}
	if d.ExternalIds.ImdbID != "" {
		out["imdb"] = "https://www.imdb.com/title/" + d.ExternalIds.ImdbID
	}
	return out
}

// tipoInferido infers the media_type of the detail: a movie has `title`; a series has
// `name` (the series detail does not bring `title`). Default movie (the most common
// case).
func tipoInferido(d detalheTMDB) string {
	if d.Title == "" && d.Name != "" {
		return "tv"
	}
	return "movie"
}

// porSeAusenteAny writes a non-empty value under a non-empty key only if the key does
// not exist yet (the first translation per language wins -- like porSeAusente, for
// map[string]any).
func porSeAusenteAny(m map[string]any, chave, valor string) {
	if chave == "" || valor == "" {
		return
	}
	if _, existe := m[chave]; existe {
		return
	}
	m[chave] = valor
}

// itoa formats an integer TMDB id as a string (canonical source_id).
func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

// TvdbIDDoDetalheSerie extracts the tvdb_id from the external_ids append of a series
// detail ALREADY FETCHED (the same raw returned by BuscarDetalhe/PayloadFresco) --
// WITHOUT an extra round trip to the network. It satisfies provider.TvdbCruzadoProvider:
// it closes the gap that left every series whose tree comes only from TMDB (without TVDB
// configured) without a usable id for fanart.tv, which for TV requires tvdb_id
// exclusively (rotaETabela, case "tv"). It accepts both the AUGMENTED envelope
// {detail,tags_by_lang} and the raw detail without an envelope (legacy payload) -- if
// there is no `detail` in the envelope, it tries the raw directly. ok=false without a
// tvdb_id or with an unreadable payload.
func (c *Client) TvdbIDDoDetalheSerie(raw json.RawMessage) (string, bool) {
	var d struct {
		ExternalIds externalIdsTMDB `json:"external_ids"`
	}
	if err := json.Unmarshal(detalheBrutoDoRaw(raw), &d); err != nil {
		return "", false
	}
	return string(d.ExternalIds.TvdbID), d.ExternalIds.TvdbID != ""
}
