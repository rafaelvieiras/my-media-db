// Package provider defines the abstraction of EXTERNAL metadata SOURCE. A Provider knows
// how to search for titles in a source (TMDB, TVDB, MusicBrainz…) and return results in
// the canonical type ResultadoExterno -- without EVER persisting anything (the search is
// local-first; only the import writes).
//
// The interface is the multi-vertical pluggability point: replacing or adding a provider
// does not touch the search handler, which talks only to this abstraction.
package provider

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNaoEncontrado is the SENTINEL error that a provider returns when the item does NOT
// EXIST in the source (e.g. a 404 from TMDB), distinct from a communication failure. The
// external detail handler translates it into 404 (nonexistent id), reserving the 502 for
// real failures of the source (timeout/5xx). Compare with
// errors.Is(err, provider.ErrNaoEncontrado).
var ErrNaoEncontrado = errors.New("provider: item nao encontrado na fonte")

// Provider is an external metadata source. Implementations are injected into the server
// through an option; nil means "external search off" (it degrades to local results only,
// without an error).
type Provider interface {
	// Nome identifies the source (e.g. "tmdb"). Used in logs/diagnostics.
	Nome() string
	// Buscar queries the source by `query`, optionally filtering by `tipo`
	// (""|"movie"|"tv"). `lang` is the ACTIVE reading language: the source returns
	// titles/overviews in it when available (without `lang`, it falls back to the default
	// of the source). It is a single language (the search shows direct results, without
	// the fallback chain of the resolver). Error/timeout is the caller's responsibility to
	// degrade -- the provider only propagates it.
	Buscar(ctx context.Context, query, tipo, lang string) ([]ResultadoExterno, error)
	// BuscarDetalhe pulls the DETAIL (payload of the source) of an item by its `sourceID`
	// and `tipo` ("movie"|"tv"), for the import/enrich. The `langs` are the ALLOWED
	// languages of the settings: the multi-language source pulls ALL the allowed
	// translations in a single round trip and returns an AUGMENTED PAYLOAD
	// ({detail, tags_by_lang}) -- still a re-composable cache (ADR-0011). Without `langs`
	// it degrades to the mono-language behavior. The canonical mapping is MapearDetalhe's
	// (PURE, without network). Error/timeout propagates.
	BuscarDetalhe(ctx context.Context, sourceID, tipo string, langs []string) (json.RawMessage, error)
	// MapearDetalhe translates the payload of BuscarDetalhe into the CANONICAL map with
	// OUR field names, consumed by the resolver (raw→canonical belongs to the provider; the
	// resolver does not know TMDB). It is PURE (it runs at read time, without network). The
	// TRANSLATABLE fields (title, description, tags) come out as `map[language]value`; the
	// NEUTRAL ones (original_title, release_date, poster_url) as scalars. It also exposes
	// `original_language` (enrich writes it to entity.original_language). A LEGACY payload
	// (raw detail, without envelope) degrades to scalars under the main language. It only
	// includes the keys that the source brought. WITHOUT cast/crew (MD6).
	MapearDetalhe(raw json.RawMessage) (map[string]any, error)
}

// ColecaoProvider is an OPTIONAL CAPABILITY of a Provider: searching for a collection
// (belongs_to_collection of TMDB -- a set of movies, ≠ playlist) with its parts, for the
// collection PAGE. It is optional because not every source has collections; the handler
// type-asserts the Provider to this interface and degrades with 503 when the source does
// not implement it. Unlike the detail, there is NO re-composable multi-language cache
// here: the collection is fetched in the ACTIVE language (pragmatic mono-language); the
// full multi-language stays a follow-up.
type ColecaoProvider interface {
	// BuscarColecao pulls the collection by its `colecaoID` from the source, localized in
	// `lang` (active reading language; empty falls back to the default of the source).
	// ErrNaoEncontrado when the collection does not exist (→ 404); the other errors
	// propagate (→ 502).
	BuscarColecao(ctx context.Context, colecaoID, lang string) (Colecao, error)
}

// Colecao is a canonical collection: the metadata of the set (name/overview/art) and its
// PARTS, each one a ResultadoExterno (the same shape of a search item -- deliberate
// reuse, the part is a movie navigable to the external detail). The pointers are
// optional (nil when the source did not bring the field).
type Colecao struct {
	// Source is the name of the source (e.g. "tmdb").
	Source string
	// SourceID is the id of the collection IN THE SOURCE (as a string).
	SourceID string
	// Name is the displayable name of the collection.
	Name string
	// Overview is the overview of the collection, when available.
	Overview *string
	// PosterURL/BackdropURL are the DIRECT art URLs of the collection in the source, when
	// available (the handler may rewrite them to the local proxy).
	PosterURL   *string
	BackdropURL *string
	// Parts are the items of the collection, in the order of the source.
	Parts []ResultadoExterno
}

// TituloAlternativoProvider is an OPTIONAL CAPABILITY of a Provider (analogous to
// ColecaoProvider): bringing the ALTERNATIVE/TRANSLATED TITLES that the source knows for
// an item, so that the candidate score (matching.PontuarComAlternativos) compares against
// the RANGE of titles the source already has, not only the title in the queried language.
// It is optional because not every source exposes title translations cheaply; the
// candidate finder (melhorCandidatoExterno, internal/server/automatch.go) type-asserts
// the Provider to this interface and degrades to the single-title score when absent -- it
// NEVER blocks the scoring.
type TituloAlternativoProvider interface {
	// BuscarTitulosAlternativos lists the alternative/translated titles that the source
	// knows for the item `sourceID` (tipo "movie"|"tv"). The list may contain duplicates or
	// empty strings -- the caller does not require perfect dedupe. Error/timeout propagates;
	// the caller degrades (best-effort, it logs and goes on without alternatives for that
	// candidate).
	BuscarTitulosAlternativos(ctx context.Context, tipo, sourceID string) ([]string, error)
}

// SerieProvider is an OPTIONAL CAPABILITY of a Provider (analogous to ColecaoProvider,
// ADR-0021): bringing the HIERARCHY of a series -- seasons + episodes + the orderings
// (season types) it uses. It is optional because not every source models series with
// orders (TMDB does not expose season types); the handler type-asserts the Provider to
// this interface and DEGRADES (503) when the source does not implement it. Like the
// detail, the languages are resolved with FALLBACK and the translatable fields come out
// per language; the neutral ones as scalars.
type SerieProvider interface {
	// BuscarSerieArvore pulls the tree of the series `sourceID` from the source, localized
	// in the ALLOWED languages `langs` (empty falls back to the language of the source). It
	// returns ONLY the seasons of the DEFAULT ordering (lean); the other orders exist only
	// as numbering in the episodes (Numbering). ErrNaoEncontrado when the series does not
	// exist (→ 404); the other errors propagate (→ 502).
	BuscarSerieArvore(ctx context.Context, sourceID string, langs []string) (SerieArvore, error)
}

// SerieRicoProvider is an OPTIONAL CAPABILITY of a Provider (route C): ENRICHING THE
// SERIES NODE with the rich fields that the source of the tree (TVDB) does not provide --
// tagline, genres, rating, keywords, similar and SITUATION. It is separated from
// SerieProvider (the tree) on purpose: whoever has the tree (TVDB) does not have the rich
// fields, and whoever has the rich fields (TMDB) does not model the tree with orders. The
// enrich type-asserts the "tmdb" provider to this interface and DEGRADES silently when
// absent (a TVDB-only series stays with empty fields, never an error).
type SerieRicoProvider interface {
	// BuscarSerieDetalheRico crosses the `tvdbID` of the series to the id of the rich
	// source and returns the RICH canonical already mapped (ONLY the rich keys -- never
	// title/overview/original_language, so as not to clobber the identity of the TVDB tree
	// in the layer composition). ok=false without a match (gracefully, without error); a
	// real error (network) propagates so enrich can log it and go on TVDB-only. `langs` are
	// the allowed languages: tagline and genres come out per language.
	BuscarSerieDetalheRico(ctx context.Context, tvdbID string, langs []string) (map[string]any, bool, error)
}

// ChaveEnvelopeSerieRico is the key of the media_external envelope that carries the RICH
// canonical of a series ALREADY MAPPED. The enrich writes the payload
// {ChaveEnvelopeSerieRico: <rich canonical>} under the "tmdb" source of the series node,
// and the MapearDetalhe of the TMDB provider recognizes it and returns the map as it is --
// that way the rich fields enter the layer composition (montarExterno) reusing the movie
// machinery, WITHOUT clobbering title/language of the TVDB tree (the envelope does not
// carry them).
const ChaveEnvelopeSerieRico = "tmdb_serie_rico"

// TvdbCruzadoProvider is an OPTIONAL CAPABILITY of a Provider: extracting the TVDB
// cross-id from a series detail payload already fetched (without an extra round trip to
// the network) -- it closes the gap that left every series whose tree comes only from TMDB
// (without TVDB configured, the provider-independence scenario) without a usable id for
// the fanart.tv ArteProvider, which for TV requires tvdb_id exclusively
// (fanarttv.rotaETabela, case "tv" -- without a fallback to tmdb/imdb, unlike the "movie"
// case). Only the TMDB client implements it today (the `external_ids` append that TMDB
// already fetches in the rich detail brings `tvdb_id` for free, but the canonical mapping
// only extracted the `imdb_id` until now). The enrich type-asserts the provider of the
// BASE SOURCE of the tree to this interface and degrades silently when absent (base source
// != TMDB) or without a match in the payload.
type TvdbCruzadoProvider interface {
	// TvdbIDDoDetalheSerie extracts the tvdb_id from a series detail payload ALREADY
	// fetched (the same raw returned by BuscarDetalhe/PayloadFresco) -- PURE, without
	// network. ok=false when the payload does not bring a usable tvdb_id.
	TvdbIDDoDetalheSerie(raw json.RawMessage) (string, bool)
}

// Similar is a canonical similar/recommended item, already filtered (no adult, no
// irrelevant vote volume) and SORTED by affinity with the origin item -- the consumer does
// not reprocess score/genre/adult, it only displays.
type Similar struct {
	// SourceID is the id of the item IN THE SOURCE (e.g. the numeric TMDB id, as a string).
	SourceID string
	// Type is our canonical type ("movie"|"tv"), inherited from the origin item.
	Type string
	// Title is the displayable title of the item (in the queried language, when the
	// source translates it).
	Title string
	// TituloOriginal is the title the item has in its ORIGINAL language, when the source
	// reports it (TMDB: original_title for movies, original_name for series).
	//
	// It exists because the LOCAL NAME being matched usually is the original title, not
	// the translation: a folder "White Collar" compared only against the pt-BR
	// "Crimes do Colarinho Branco" scores ~0, falls out of the top-N rescue and loses to
	// "White Collar Blue" (measured, 94.1 vs 0). Empty when the source does not report it.
	TituloOriginal string
	// Year are the first 4 digits of the release/first-air date, when available.
	Year *string
	// PosterURL is the DIRECT poster URL of the item in the source, when available.
	PosterURL *string
}

// SimilarProvider is an OPTIONAL CAPABILITY of a Provider: it extracts the
// SIMILAR/recommended items from the detail payload ALREADY fetched by BuscarDetalhe
// (without an extra round trip to the network -- the same pattern as TvdbCruzadoProvider),
// already filtered by quality (no adult, no irrelevant vote volume) and SORTED by real
// affinity with the origin item. It is optional because not every source models
// recommendations (TVDB does not); the caller (entities.go/external_detail.go)
// type-asserts the Provider to this interface and degrades to an empty list (without
// error, without 500) when absent.
type SimilarProvider interface {
	// Similares extracts the similar/recommended items from the `raw` payload (the same
	// one returned by BuscarDetalhe) -- PURE, without network. A real error (unreadable
	// payload) propagates; the caller logs it and goes on without similars from that layer.
	Similares(raw json.RawMessage) ([]Similar, error)
}

// ArteProvider is an OPTIONAL CAPABILITY of a Provider: searching the INVENTORY of arts
// of an item by its external id -- poster, fanart, logo, clearart, disc, banner, thumb,
// characterart and season arts. It is the capability of fanart.tv (a source DEDICATED to
// art that TMDB/TVDB do not cover well). It is optional because not every source has extra
// art; the enrich type-asserts the Provider to this interface and DEGRADES silently when
// absent (without the configured key, zero regression). The result is an INVENTORY (list
// of URLs + metadata), NEVER downloaded binaries -- the real download is lazy (only the
// selected art, via cachearArte).
type ArteProvider interface {
	// BuscarArtes lists the arts of the item by the external ids it has (`ids`), filtering
	// by `tipo` ("movie"|"tv"; since the music vertical, also "album" for the cover through
	// Cover Art Archive/MusicBrainz). `langs` are the allowed languages, for choosing the art
	// served by default; the inventory PRESERVES the `lang` of all of them. Without a usable id
	// for the source, or without a match, it returns (nil, nil) -- gracefully. A real error
	// (network/status) propagates so enrich can log it and go on.
	BuscarArtes(ctx context.Context, ids ExternalIDs, tipo string, langs []string) ([]Arte, error)
}

// TemaMusicalProvider is an OPTIONAL CAPABILITY: it searches a musical-theme candidate
// (video link, never a binary) by the external ids that the item has. It is optional because
// not every source models a musical theme (only ThemerrDB today); the caller type-asserts it
// and degrades silently (without a candidate) when absent or without a match.
type TemaMusicalProvider interface {
	// BuscarTemaMusical looks up a theme by the external ids (`ids`), filtering by `tipo`
	// ("movie"|"tv"). Without a usable id or without a match: (TemaMusical{}, false, nil) --
	// gracefully, not an error. A real error (network/status) propagates.
	BuscarTemaMusical(ctx context.Context, ids ExternalIDs, tipo string) (TemaMusical, bool, error)
}

// TemaMusical is a musical theme candidate (ThemerrDB): the URL of the video in the source
// (YouTube) -- NEVER the binary; the real download is lazy through yt-dlp.
type TemaMusical struct {
	URL string
}

// ChaveEnvelopeTemaMusical is the key of the media_external envelope that carries the
// CANDIDATE of musical theme from a source (ThemerrDB) -- the same technique of
// ChaveEnvelopeInventarioArtes, but simpler (a single candidate, not a map per kind). The
// enrich writes {ChaveEnvelopeTemaMusical: <url>} under the "themerrdb" source when there is
// a match; without a match, it writes an empty envelope (the query was already made, without
// hitting the API again on every GET). It is a PURE candidate layer: it NEVER enters the
// layer composition of the resolver.
const ChaveEnvelopeTemaMusical = "themerrdb_tema"

// PessoaProvider is an OPTIONAL CAPABILITY of a Provider (second section): bringing the
// FILMOGRAPHY of a PERSON in the source -- their works (cast/crew) that are NOT YET in the
// catalog, so the person page can show "not yet imported" next to the already imported
// filmography (which comes only from the database, without depending on a provider). It is
// optional because not every source models a person; the handler type-asserts the Provider
// to this interface and DEGRADES to `[]` (without error) when the source does not implement
// it.
type PessoaProvider interface {
	// BuscarFilmografia pulls the works of the person `sourceID` from the source, localized
	// in `lang` (active reading language; empty falls back to the default of the source). The
	// implementation decides the ceiling/sorting/dedup (each source has its own notion of "too
	// large a filmography"). ErrNaoEncontrado when the person does not exist in the source (the
	// handler degrades the same way as a real error: it logs and returns `[]`, the page does not
	// go down). (nil, nil) is graceful (the person exists, without credits).
	BuscarFilmografia(ctx context.Context, sourceID, lang string) ([]CreditoFilmografia, error)
}

// CreditoFilmografia is a canonical work of the EXTERNAL filmography of a person (not yet
// imported): the identity of the item (source/source_id/type) plus the role of the person in
// it (kind/character/job -- the pair that does not apply to the kind stays nil). It mirrors
// ResultadoExterno on the identity of the item, but also carries the role (which
// ResultadoExterno does not have, as it is not about a person).
type CreditoFilmografia struct {
	// Source is the name of the source (e.g. "tmdb").
	Source string
	// SourceID is the id of the item (work) IN THE SOURCE.
	SourceID string
	// Type is our canonical type of the item ("movie"|"tv").
	Type string
	// Title is the displayable title of the item (in the queried language, when the
	// source translates it).
	Title string
	// TituloOriginal is the title the item has in its ORIGINAL language, when the source
	// reports it (TMDB: original_title for movies, original_name for series).
	//
	// It exists because the LOCAL NAME being matched usually is the original title, not
	// the translation: a folder "White Collar" compared only against the pt-BR
	// "Crimes do Colarinho Branco" scores ~0, falls out of the top-N rescue and loses to
	// "White Collar Blue" (measured, 94.1 vs 0). Empty when the source does not report it.
	TituloOriginal string
	// Year are the first 4 digits of the release/first-air date, when available.
	Year *string
	// PosterURL is the DIRECT poster URL of the item in the source, when available (the
	// handler proxies it, like the other external image fields).
	PosterURL *string
	// Kind is the role of the person IN THIS item: "cast" (acting) or "crew" (crew).
	Kind string
	// Character is the character, only when Kind="cast" (nil in Kind="crew").
	Character *string
	// Job is the crew job(s), only when Kind="crew" (nil in Kind="cast"); it may join
	// several distinct jobs of the SAME work (the source may credit more than one).
	Job *string
}

// ExternalIDs carries the external ids that an item has (whatever the identity has).
// fanart.tv uses Tmdb/Imdb for a movie and Tvdb for a series; each ArteProvider chooses what
// it knows how to consume and degrades when the id it needs is empty. Mbid is the
// MusicBrainz MBID of the music item -- for an album, it is the MBID of the RELEASE-GROUP
// (the same one stored in external_id_mbid of the `album` entity, ADR-0027 item 2), not the
// one of a specific release/pressing; the Cover Art Archive accepts that mbid directly in the
// /release-group/{mbid} endpoint.
type ExternalIDs struct {
	Tmdb string
	Imdb string
	Tvdb string
	Mbid string
}

// Arte is a canonical art of the inventory of an ArteProvider. Kind is our normalized kind
// (poster/fanart/logo/clearart/disc/banner/thumb/characterart); URL is the DIRECT art in the
// source (served through the image proxy). Lang is the language (2 letters or ""=textless).
// Likes is the popularity (for sorting). DiscType/Disc only apply to disc; Season (number,
// "all" or "0") only to season art -- it routes the art to the entity of the corresponding
// season.
type Arte struct {
	Kind     string
	URL      string
	Lang     string
	Likes    int
	DiscType string
	Disc     int
	Season   string
}

// ChaveEnvelopeInventarioArtes is the key of the media_external envelope that carries the
// INVENTORY of arts of an art source (fanart.tv). The enrich writes
// {ChaveEnvelopeInventarioArtes: {kind: [ArteInventario...]}} under the art source
// (source="fanarttv"), WITHOUT downloading binaries; the /artwork endpoint reads it to group
// the available arts by kind. It is a PURE inventory layer: the MapearDetalhe of the art
// source is a no-op (it does not pollute the layer composition of the resolver).
const ChaveEnvelopeInventarioArtes = "fanarttv_inventory"

// ArteInventario is an art serialized in the inventory envelope (media_external). It is the
// persistable mirror of Arte WITHOUT the Kind (which becomes the key of the per-kind map in
// the envelope).
type ArteInventario struct {
	URL      string `json:"url"`
	Lang     string `json:"lang,omitempty"`
	Likes    int    `json:"likes,omitempty"`
	DiscType string `json:"disc_type,omitempty"`
	Disc     int    `json:"disc,omitempty"`
	Season   string `json:"season,omitempty"`
}

// SerieArvore is the canonical hierarchy of a series: the default ordering, the list of
// orderings the series uses, and the seasons ONLY of the default ordering. Each episode
// (fine-grained, anchored on the id of the source) carries the numbering of ALL orders.
type SerieArvore struct {
	// DefaultSeasonType is the default ordering of the series (e.g. "official"); it is the
	// only one materialized in Temporadas.
	DefaultSeasonType string
	// SeasonTypes are the orderings that the series uses (e.g. ["official","dvd","absolute"]).
	SeasonTypes []string
	// Temporadas are the seasons ONLY of the default ordering (ADR-0021: the alternatives do
	// not become a season -- only numbering in the episodes).
	Temporadas []TemporadaExterna
}

// TemporadaExterna is a canonical season of the default ordering, with its episodes.
type TemporadaExterna struct {
	// Ordering is the ordering the season belongs to (always the default one here).
	Ordering string
	// SeasonNumber is the number of the season IN THAT order (it may be a calendar year, 0
	// for specials, a box-set index -- do not assume 1..N; ADR-0021).
	SeasonNumber int
	// Name is the displayable name per language (it may be empty: seasons rarely have a name).
	Name map[string]string
	// Year is the year of the season, when the source provided it.
	Year string
	// PosterURL is the DIRECT art of the season in the source, when available.
	PosterURL string
	// Episodios are the episodes of this season (in the default ordering), in order.
	Episodios []EpisodioExterno
}

// EpisodioExterno is a canonical fine-grained episode, anchored on the id of the source.
// The numbering of ALL orders lives in Numbering; the identity is NEVER the (season, number),
// which changes per order (ADR-0021).
type EpisodioExterno struct {
	// SourceID is the episode.id in the source -- the canonical ANCHOR (never the number).
	SourceID string
	// ExternalIDs are the ids of the episode in other sources ({tvdb, imdb, tmdb} when
	// available). From the tree only {tvdb} comes (the crossing per episode requires the
	// extended, N calls -- deferred); the strong crossing is at the SERIES level.
	ExternalIDs map[string]string
	// Title is the title per language, WITH FALLBACK (the language without a translation
	// simply does not appear in the map -- the resolver falls back through the chain). It may
	// contain only the original language.
	Title map[string]string
	// Overview is the overview per language (same semantics as Title).
	Overview map[string]string
	// AbsoluteNumber is the canonical linear numbering (stable between orders), when available.
	AbsoluteNumber *int
	// Numbering brings the numbering of the SAME episode in ALL the orders that the series uses
	// (e.g.: {"official":{1933,16}, "dvd":{23,5}, "absolute":{1,373}}).
	Numbering map[string]Numero
	// AirDate is the air date (YYYY-MM-DD), when available.
	AirDate string
	// Runtime is the duration in minutes, when the source provided it.
	Runtime *int
	// StillURL is the frame/screencap of the episode, when available.
	StillURL string
}

// Numero is the (season, number) pair of an episode IN an ordering. It is not a key: the
// same episode has a different Numero per order (ADR-0021).
type Numero struct {
	Season int
	Number int
}

// MusicaProvider is an OPTIONAL CAPABILITY of a Provider (the mould of SerieProvider):
// bringing the HIERARCHY of an ARTIST -- discography (albums) and tracks, with the REAL
// artist of each track (artist-credit of the recording), which may diverge from the artist of
// the album (feat./collaboration -- ADR-0027 item 4). It is optional because not every source
// models music (only MusicBrainz today); the caller (enrich) type-asserts the Provider to this
// interface and degrades when absent.
type MusicaProvider interface {
	// BuscarArtistaArvore pulls the tree of the artist `sourceID` (MBID) from the source, with
	// all the albums (release-groups) and tracks (recordings). `langs` is kept in the signature
	// for parity with SerieProvider/BuscarSerieArvore; MusicBrainz is not multi-language like
	// TMDB/TVDB (the textual fields come out in the native language registered in the source),
	// so the current implementation ignores it -- reserved for a future source that uses it.
	// ErrNaoEncontrado when the artist does not exist in the source (→ 404); the other errors
	// propagate (→ 502).
	BuscarArtistaArvore(ctx context.Context, sourceID string, langs []string) (ArtistaArvore, error)
}

// ArtistaArvore is the canonical hierarchy of an artist: identity + bio (when the source has
// one) + discography (albums, each one already with its tracks).
type ArtistaArvore struct {
	// Name is the displayable name of the artist.
	Name string
	// MBID is the id of the artist in the source (MusicBrainz) -- the canonical anchor.
	MBID string
	// Bio is the biography/annotation of the artist, when the source has one (MusicBrainz
	// rarely does; empty is the common case, not an error).
	Bio string
	// Albuns are the albums (release-groups) of the artist, in the order the source lists them.
	Albuns []AlbumExterno
}

// AlbumExterno is a canonical album (release-group of MusicBrainz) with its tracks. The
// artist of the album is implicitly the root of ArtistaArvore (including when it is "Various
// Artists" -- ADR-0027 item 5, without a special case).
type AlbumExterno struct {
	// Name is the title of the album.
	Name string
	// MBID is the id of the release-group in the source -- the canonical anchor of the album
	// (the same one stored in external_id_mbid of the `album` entity; also used by
	// ExternalIDs.Mbid for the art lookup through the Cover Art Archive,
	// /release-group/{mbid} endpoint).
	MBID string
	// Year are the first 4 digits of the release date of the release-group, when available.
	Year string
	// Genre is the genre with the highest count (vote) that the source associates with the
	// album, when available (ADR-0027 item 7: the genre lives at the album/artist level, not per
	// track).
	Genre string
	// Faixas are the tracks of the album (of the "official" release chosen -- see the comment
	// in escolherRelease of the musicbrainz client), in disc+position order.
	Faixas []FaixaExterna
}

// FaixaExterna is a canonical track (recording of MusicBrainz), anchored on the MBID of the
// RECORDING -- never on a number/position (ADR-0027 item 1, it mirrors EpisodioExterno).
type FaixaExterna struct {
	// Title is the title of the track.
	Title string
	// MBID is the id of the recording in the source -- the canonical anchor of the track.
	MBID string
	// TrackNumber is the position of the track inside the disc (1-based).
	TrackNumber int
	// DiscNumber is the number of the disc/medium inside the release (1-based).
	DiscNumber int
	// DiscTotal is the total of discs/media of the release.
	DiscTotal int
	// ISRC is the auxiliary identifier of the track, when the source has one; there may be
	// more than one ISRC per recording -- we carry only the first one.
	ISRC string
	// ArtistaCreditoNome is the name of the REAL artist credited on this track (artist-credit
	// of the recording) -- it may diverge from the artist of the album (feat./collaboration,
	// ADR-0027 item 4).
	ArtistaCreditoNome string
	// ArtistaCreditoMBID is the MBID of the REAL artist credited on this track.
	ArtistaCreditoMBID string
	// DuracaoMs is the duration of the track in milliseconds, when the source provides it;
	// 0 = unknown.
	DuracaoMs int
}

// AlvoFaixaAlbum optionally identifies ONE specific track that the caller needs to find
// inside the resolved album -- it allows the provider to try alternative releases of the same
// release-group when the one chosen by default (e.g. "Official") does not contain that track
// (a real finding: Bomfunk MC's "Freestyler" exists only in a complete album of the
// release-group, not in the "Official" edition -- a maxi-single with remixed tracks). The zero
// value (all fields empty) preserves the usual behavior of choosing only 1 release, without
// any additional attempt. Priority when more than one field is filled in: RecordingMBID >
// FaixaTitulo > FaixaNumero (the same order as enrich.FaixaAlvo/escolherFaixaAlvo -- minimal
// and deliberate duplication: the provider cannot depend on enrich).
type AlvoFaixaAlbum struct {
	RecordingMBID string
	FaixaTitulo   string
	FaixaNumero   int
	// FaixaDisco is the disc number of the track in the FILE (tag disc_number), when known; 0 =
	// unknown. It enters ONLY as a tiebreaker, when two or more tracks of the album tie on the
	// title (a reissue that repeats the same track on different discs -- the real case "The
	// Hurting (Deluxe Edition)").
	FaixaDisco int
	// ArtistaNome is the artist of the ALBUM (album_artist of the tag, with a fallback to
	// artist). It scopes the album search by TITLE -- without it, a common title ("Love Songs",
	// "Lockjaw") returns homonyms of other artists and the right album stays out of the top (a
	// finding in production). A compilation (album_artist "Various Artists") is not scoped:
	// there the title is the only signal.
	ArtistaNome string
	// DuracaoMs is the known duration of the local file, in milliseconds, when available; 0 =
	// not provided. It is used as a signal of additional CONFIRMATION to the title, never of
	// search.
	DuracaoMs int
}

// MusicaAlbumProvider is an OPTIONAL CAPABILITY of a Provider (ADR-0029 item 6): searching/
// assembling ONE specific release (album) of the artist, WITHOUT enumerating the whole
// discography -- the SELECTIVE counterpart of MusicaProvider (the complete tree), used by the
// LOCAL-FIRST import of the scanner (an owned track brings only the artist + the ALBUM OF THAT
// track, never the other releases -- see enrich.ImportarFaixaPossuida). It is optional because
// not every music source models this selective search (only MusicBrainz today); the enrich
// type-asserts the Provider to this interface and returns an error (the scanner degrades, logs
// and falls to the next step of the pipeline/draft) when the source does not implement it.
type MusicaAlbumProvider interface {
	// BuscarAlbumDaFaixa resolves, from the MBID of the RECORDING `recordingMBID`, the release
	// (album) that contains it, and assembles THAT single album (name/year/genre/tracks) -- the
	// PRECISE path, used when the file has the track MBID embedded. It never enumerates the whole
	// discography of the artist. ok=false when the recording has NO associated release
	// (incomplete data in the source, extremely rare) -- gracefully, not an error; a real error
	// (network, nonexistent MBID) propagates.
	BuscarAlbumDaFaixa(ctx context.Context, recordingMBID string, langs []string) (AlbumExterno, bool, error)

	// BuscarAlbumPorNome locates, among the release-groups of the artist `artistaMBID`, the one
	// whose title is closest to `nomeAlbum` (the same similarity engine/threshold that the scanner
	// already uses in the textual match -- internal/matching.LimiarAutoMatch) and assembles THAT
	// single album -- the TEXTUAL path, used when the file has no embedded track MBID (fallback).
	// The implementation may need 1 cheap call (ids/titles only) to list the release-groups of the
	// artist and choose the candidate, but it assembles -- tracks included -- only the chosen one,
	// never the others. `alvoFaixa`, when filled in, allows the implementation to try alternative
	// releases of the SAME chosen release-group when the default release does not contain that
	// track (the zero value preserves the usual behavior of always a single release). ok=false
	// without a plausible candidate (a tie at the top or below the threshold) -- gracefully; a
	// real error propagates.
	BuscarAlbumPorNome(ctx context.Context, artistaMBID, nomeAlbum string, alvoFaixa AlvoFaixaAlbum, langs []string) (AlbumExterno, bool, error)

	// BuscarAlbumPorMbid assembles the album whose RELEASE-GROUP MBID is already known -- the
	// DIRECT path, used when the caller already has the mbid stored (e.g. the internal `album`
	// entity, via external_ids["mbid"]), without needing a track nor a starting name. Unlike
	// BuscarAlbumDaFaixa/BuscarAlbumPorNome (which resolve the release-group from another datum
	// before assembling), here there is no previous resolution step: the EXISTENCE of the
	// release-group is validated in this very call, and ErrNaoEncontrado propagates (errors.Is)
	// when `albumMBID` does not exist in the source -- other network/parsing errors also
	// propagate. It never enumerates the discography of the artist.
	BuscarAlbumPorMbid(ctx context.Context, albumMBID string, langs []string) (AlbumExterno, error)
}

// FaixaResolverProvider is an OPTIONAL CAPABILITY of a Provider: it resolves the MBID of the
// ARTIST from the MBID of an already known track (recording) -- it covers the case of a file
// with the track MBID embedded by the tagger but WITHOUT the artist MBID (some taggers only
// write the recording MBID, not the artist one). Without this capability, the scanner cannot
// call Importar(artistaMBID, "artista", ...) from only a track MBID. It is optional because not
// every source models this reverse resolution (only MusicBrainz today); the scanner
// (internal/scanner/musica.go) type-asserts the Provider to this interface and degrades (it
// falls back to the textual search, or to draft) when absent or without a match.
type FaixaResolverProvider interface {
	// ResolverArtistaDaFaixa resolves the MBID of the artist from the MBID of the recording
	// `recordingMBID`, using the FIRST artist-credit of the track in the source (the same "main
	// artist" criterion used in BuscarArtistaArvore/FaixaExterna.ArtistaCreditoMBID).
	// ErrNaoEncontrado when the track does not exist in the source (→ the caller degrades); the
	// other errors (network, a track without any artist-credit) propagate.
	ResolverArtistaDaFaixa(ctx context.Context, recordingMBID string) (artistaMBID string, err error)
}

// FingerprintProvider is an OPTIONAL CAPABILITY of a Provider: identifying a track from the
// ACOUSTIC FINGERPRINT of the audio (not from metadata/name) -- the last fallback of the music
// matching pipeline, used only when the embedded MBID and the textual search have already
// failed. Not every source has this capability (today: a dedicated AcoustID client, never the
// MusicaProvider/MusicaAlbumProvider of MusicBrainz -- the lookup by fingerprint is a
// service/endpoint DISTINCT from the metadata API); the scanner type-asserts the Provider to
// this interface and degrades silently (it falls back to the draft step) when absent.
type FingerprintProvider interface {
	IdentificarPorFingerprint(ctx context.Context, fingerprint string, duracaoSegundos float64) (RecordingCandidato, bool, error)
}

// RecordingCandidato is the result of ONE lookup by fingerprint: the most probable recording
// MBID and the confidence score returned by AcoustID (0-1), used as the minimum acceptance
// threshold (see matching.go).
type RecordingCandidato struct {
	RecordingMBID string
	Confianca     float64
}

// ResultadoExterno is the canonical result of an external search, independent of the source.
// The pointers are optional (nil when the source did not bring the field). Saved is ALWAYS false
// in the search: an external result is never an entity of the catalog until the explicit
// import.
type ResultadoExterno struct {
	// Source is the name of the source (e.g. "tmdb").
	Source string
	// SourceID is the id of the item IN THE SOURCE (e.g. the numeric TMDB id, as a string).
	SourceID string
	// Type is our canonical type ("movie"|"tv").
	Type string
	// Title is the displayable title of the item (in the queried language, when the
	// source translates it).
	Title string
	// TituloOriginal is the title the item has in its ORIGINAL language, when the source
	// reports it (TMDB: original_title for movies, original_name for series).
	//
	// It exists because the LOCAL NAME being matched usually is the original title, not
	// the translation: a folder "White Collar" compared only against the pt-BR
	// "Crimes do Colarinho Branco" scores ~0, falls out of the top-N rescue and loses to
	// "White Collar Blue" (measured, 94.1 vs 0). Empty when the source does not report it.
	TituloOriginal string
	// Year are the first 4 digits of the release date, when available.
	Year *string
	// Overview is the overview, when available.
	Overview *string
	// PosterURL is the DIRECT art URL in the source, when available (the image proxy/cache is
	// the import's job).
	PosterURL *string
	// Saved indicates whether the item already exists in the catalog. In the search it is always false.
	Saved bool
}

// AlbumComArtista is the result of an album search by TITLE (capability
// MusicaAlbumPorTituloProvider): the album assembled from the source PLUS the identity of the
// main artist that the source itself attributes to it -- it is from that identity that the
// artist of a compilation/soundtrack comes, where there is no reliable local `album_artist`
// ("Various Artists").
type AlbumComArtista struct {
	Album AlbumExterno
	// ArtistaMBID is the id of the artist in the artist-credit of the album. Empty when the
	// source did not bring the credit (the caller degrades: no artist, no import).
	ArtistaMBID string
	// ArtistaNome is the name of the artist in the same credit.
	ArtistaNome string
}

// ArtistaExterno is the minimum identity of an artist coming from a credit of the source (MBID
// + display name).
type ArtistaExterno struct {
	MBID string
	Nome string
}

// MusicaAlbumPorTituloProvider is an OPTIONAL CAPABILITY of a Provider: resolving ONE album by
// its TITLE, without starting from an artist. It exists because the normal textual pipeline
// starts with the artist search -- and real compilations/soundtracks have
// `album_artist = "Various Artists"`, which does not return a unique identity. Optional for the
// same reason as MusicaAlbumProvider: not every source models this search; the ones that do not
// implement it degrade silently (the track stays as a draft).
type MusicaAlbumPorTituloProvider interface {
	// BuscarAlbumPorTitulo assembles the album whose tracklist CONTAINS the track described by
	// `alvo` (title/number/MBID), among the release-groups most similar to `nomeAlbum`
	// (matching.LimiarAutoMatch threshold). ok=false when no candidate passes the threshold or
	// none contains the track -- never a "similar one".
	BuscarAlbumPorTitulo(ctx context.Context, nomeAlbum string, alvo AlvoFaixaAlbum, langs []string) (AlbumComArtista, bool, error)
}
