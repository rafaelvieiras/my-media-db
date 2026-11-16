// Package musicext concentrates the READING of the extension_data of the music
// vertical (namespace core.music@1, ADR-0027): extract the namespace object and
// coerce the JSON numbers — the same mold as internal/tvext (series). It also
// keeps the entity_relation `kind`s and the canonical MBID of "Various Artists"
// used to tie the artist→album→track hierarchy.
package musicext

import (
	"database/sql"
	"encoding/json"
)

// Namespace of the extension_data of this vertical.
const Namespace = "core.music@1"

// entity_relation kinds that tie the hierarchy (ADR-0027 §2/§4).
const (
	// KindAlbum connects album (parent) → track (child); sort_index = track_number.
	KindAlbum = "music_album"
	// KindArtistaPrincipal connects artist (parent) → album or track (child);
	// sort_index = billing order.
	KindArtistaPrincipal = "music_artista_principal"
	// KindArtistaFeat connects artist (parent) → track (child): one-off
	// collaboration, when the artist of the track diverges from the main artist
	// of the album.
	KindArtistaFeat = "music_artista_feat"
)

// Keys of the core.music@1 extension_data that hold the RAW name/MBID of the
// collaboration (feat.) of a track, when its artist-credit diverges from the
// MAIN artist of the album (ADR-0027 item 4, ADR-0029 item 4). They are
// READ-ONLY metadata -- written by the SCOPED import (local-first, one owned
// track at a time, internal/enrich/musica.go ImportarFaixaPossuida) WITHOUT
// creating entity/entity_relation(kind=music_artista_feat) automatically; the
// import of the complete TREE (importarArtista, an explicit pull by the owner)
// keeps creating the entity+relation as before (ADR-0029 item 5) and also writes
// these same keys (redundant with the relation, but with no cost and no
// contradiction).
const (
	// FeatArtistNameKey is the display name of the collaborating artist, when the
	// source brought it. Absent from the payload when there is no artist-credit
	// divergence.
	FeatArtistNameKey = "feat_artist_name"
	// FeatArtistMbidKey is the MBID of the collaborating artist at the source.
	// Always present together with FeatArtistNameKey (the divergence is only
	// detectable with the MBID).
	FeatArtistMbidKey = "feat_artist_mbid"
)

// MBIDVariousArtists is the canonical MBID that MusicBrainz itself uses for the
// grouping artist of compilations/soundtracks. Use this value when linking a
// compilation album to "Various Artists" — it is not a code special case, it is
// the right id (ADR-0027 §3/MU4).
const MBIDVariousArtists = "89ad4ac3-39f7-470e-963a-56509c546377"

// FonteMusicBrainz/ChaveExternalIDMbid document the ONLY exception to the
// "external_ids key == Provider.Nome()" convention that tmdb/imdb/tvdb follow in
// the rest of the system: the MusicBrainz id always lives under the "mbid" key
// (ADR-0027 item 6, generated column external_id_mbid + index), never under the
// literal source name "musicbrainz" — Nome() only identifies the SOURCE
// (media_external.source, credit tracking), not the identity key.
// acharOuCriarArtista/Album/Faixa (musica.go) already write straight under
// "mbid"; the gap this mapping closes is in the GENERIC enrichment path
// (mesclarExternalIds, acharColisaoExterna) which, without it, would
// write/query under "musicbrainz" — a key that idsExternosDe (enrich/artes.go)
// and the generated column never read.
const (
	FonteMusicBrainz    = "musicbrainz"
	ChaveExternalIDMbid = "mbid"
)

// ExtractNamespace extracts the namespace object (e.g. "core.music@1") from an
// extension_data_json (namespace→object envelope). It returns nil when the raw
// is NULL/empty, unreadable, or does not contain the namespace — the caller
// degrades (the entity without that namespace simply does not have the data).
func ExtractNamespace(raw sql.NullString, ns string) map[string]any {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw.String), &env); err != nil {
		return nil
	}
	bruto, ok := env[ns]
	if !ok {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(bruto, &m); err != nil {
		return nil
	}
	return m
}

// AsInt coerces a JSON-decoded value to int (JSON numbers become float64 in the
// generic unmarshal). (0,false) when it is not a number.
func AsInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

// AsString coerces a JSON-decoded value to string. ("",false) when it is not a
// string (e.g.: isrc).
func AsString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
