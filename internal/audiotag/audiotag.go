// Package audiotag REWRITES the audio tag of a file (.mp3/.flac/.ogg) with the
// already RESOLVED metadata of a track — title, artist, album, year,
// track/disc, genre, MBIDs, ISRC and embedded cover — so that ANY
// player/server that reads audio tags (LMS, Navidrome, gonic, Supysonic,
// Subsonic/OpenSubsonic clients, Kodi in tag-scan mode, Jellyfin) sees the
// MyMediaDB metadata without depending on NFO.
//
// # Mechanism decision
//
// So far the project only READS tags through ffprobe (a subprocess,
// internal/mediainfo); there was no WRITE library in use. Two options were on
// the table: (a) dedicated Go libraries for ID3v2 + Vorbis comment; (b) the
// ffmpeg subprocess. Option (b) prevailed: `apk add ffmpeg` in the FINAL
// Dockerfile already installs the `ffmpeg` binary (together with the `ffprobe`
// that mediainfo already uses) — WRITING through ffmpeg introduces NO new
// binary dependency, unlike (a), which would require two separate Go libraries
// (ID3v2 and Vorbis comment/FLAC) to cover MP3 and FLAC/OGG. `ffmpeg
// -map_metadata -1 -c:a copy -metadata k=v ...` rewrites the container WITHOUT
// re-encoding the audio (lossless, fast) and solves the 3 formats with the SAME
// mechanism — only the field NAME convention changes per format (see
// escreverID3/escreverVorbis).
//
// Embedded cover: for MP3, the cover goes in as a second input mapped as an
// MJPEG video stream with `-disposition:v attached_pic` (it becomes the APIC
// frame of ID3v2 — validated empirically: `ffprobe` decodes back a video/mjpeg
// stream with disposition attached_pic=1). For FLAC/OGG the container does not
// accept a real video stream (ffmpeg refuses to mux mjpeg into ogg); the cover
// goes in as the value of ONE Vorbis comment, `METADATA_BLOCK_PICTURE`,
// holding the binary block from the FLAC spec (type+mime+description+dimensions
// [zeroed, the spec allows "unknown"]+image bytes) encoded in base64 — built by
// hand (montarBlocoImagem) because ffmpeg does not generate that block on its
// own from an ordinary image input; validated by reading it back with `ffprobe
// -show_streams`, which decodes the block correctly both in `.flac` and in
// `.ogg`.
//
// Pure with respect to the resolver: this package only receives the metadata
// ALREADY resolved (Metadado) — it does not decide override/external/file
// precedence, it does not know entity/resolver. Whoever builds the Metadado is
// the caller (internal/server).
package audiotag

// Metadado is the ALREADY RESOLVED value (override ⟵ external ⟵ file,
// AGENTS.md §1) to write into the tag of ONE track. Every field is optional: an
// empty string or a number <= 0 is simply not written into that frame/comment
// (it never produces a value "0"/"" in the file). An empty CoverPath = no cover
// is embedded (and a pre-existing cover in the file is removed on the rewrite —
// the write is always a TOTAL replacement of the old tag, never a merge, see
// Writer.Escrever).
type Metadado struct {
	// Title/Artist/Album are the title of the TRACK, the artist CREDITED on
	// this track (it may diverge from AlbumArtist on feat./collaboration
	// tracks, ADR-0027 item 4 — the CALLER resolves that divergence) and the
	// title of the ALBUM.
	Title  string
	Artist string
	Album  string
	// AlbumArtist is the MAIN artist of the album (main billing), always the
	// same across every track of the album — distinct from Artist when the
	// artist-credit diverges.
	AlbumArtist string
	// Date is the release year/date of the ALBUM (not of the track — the
	// track has no date of its own in the model, ADR-0027).
	Date string
	// Genre is the genre of the ALBUM (ADR-0027 item 7: genre is not per
	// track).
	Genre string
	// TrackNumber/TrackTotal/DiscNumber/DiscTotal are the numbering of the
	// track inside the album. TrackTotal is the total number of tracks of the
	// album (derived by the caller, it is not part of the track's
	// extension_data); DiscTotal comes from the extension_data of the track
	// itself (ADR-0027 item 1). <= 0 = that field is not written.
	TrackNumber int
	TrackTotal  int
	DiscNumber  int
	DiscTotal   int
	// Compilation sets the compilation flag (the album is tied to the
	// "Various Artists" artist, musicext.MBIDVariousArtists) — written as a
	// best-effort text comment/frame (see the comment in metadataID3; ffmpeg
	// does not map it to the iTunes binary TCMP frame).
	Compilation bool
	// ISRC is the auxiliary identifier of the TRACK (ADR-0027 item 6).
	ISRC string
	// MBRecordingID/MBReleaseGroupID/MBArtistID/MBAlbumArtistID are the
	// MusicBrainz IDs known to MyMediaDB (ADR-0027: we only track
	// recording/release-group/artist — there is no distinct RELEASE MBID
	// persisted; see the note in metadataID3/metadataVorbis about reusing the
	// release-group one as "Album Id" too). MBArtistID is the MBID of the
	// artist CREDITED on this track (the same feat. divergence as Artist);
	// MBAlbumArtistID is always the one of the album's main artist.
	MBRecordingID    string
	MBReleaseGroupID string
	MBArtistID       string
	MBAlbumArtistID  string
	// CoverPath is the on-disk path (already resolved by the caller — the same
	// served-art precedence of the album, escolherAssetServido in
	// internal/server/assets.go) of a JPEG/PNG image to embed as the cover.
	// "" = no cover embedded.
	CoverPath string
}
