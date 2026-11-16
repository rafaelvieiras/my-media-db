// Package mediainfo is the ffprobe wrapper (ADR-0014): it runs the binary as a
// subprocess with JSON output and maps the technical facts of a video file
// (general/video/audio/subtitle) into the stable shape of `mediainfo_json`
// stored in media_file (doc 06).
//
// ffprobe is a RUNTIME dependency, not a build one: its presence is checked at
// boot; its absence DEGRADES the result (empty mediainfo + warning), it never
// takes the scan down. The binary path and the execution function are injectable
// to allow tests without the real ffprobe.
package mediainfo

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

// Info is the mapped shape of mediainfo, serialized into
// media_file.mediainfo_json. Empty/omitted lists when the file has no track of
// that type.
type Info struct {
	General  General    `json:"general"`
	Video    []Video    `json:"video"`
	Audio    []Audio    `json:"audio"`
	Subtitle []Subtitle `json:"subtitle,omitempty"`
	// Tags are the FORMAT tags relevant to the music vertical (ADR-0027) —
	// embedded artist/album/track/MBID etc. A NEW field (it does not replace
	// General/Video/Audio/Subtitle, which existing consumers already read): nil
	// when the container brought NO format.tags tag at all (e.g.: most video
	// files).
	Tags *AudioTags `json:"tags,omitempty"`
}

// AudioTags are the AUDIO tags read from format.tags (ffprobe -show_format),
// which used to be requested but discarded by the mapping before this change.
// ffprobe exposes the tags EXACTLY as they were written in the file, without
// normalizing name/case — different containers use different spellings for the
// same field (e.g. "ARTIST" in Vorbis Comment vs "artist" in MP4,
// "MusicBrainz Track Id" in ID3v2 vs "musicbrainz_trackid" in Vorbis vs
// "MUSICBRAINZ_TRACKID" in uppercase). extrairAudioTags normalizes the KEY on
// read (case-insensitive, ignoring spaces/underscores/punctuation); the VALUES
// are never changed.
type AudioTags struct {
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Title       string `json:"title,omitempty"`
	// TrackNumber/DiscNumber are the number already PARSED before the slash
	// (the tag may come as "3" or "3/12" — only the "3" matters); 0 when
	// absent/not numeric.
	TrackNumber int    `json:"track_number,omitempty"`
	DiscNumber  int    `json:"disc_number,omitempty"`
	Genre       string `json:"genre,omitempty"`
	// ISRC is the auxiliary track identifier (ADR-0027, item 6). The tag name
	// varies per container: "TSRC" is the ID3v2 frame name; "isrc"/"ISRC" is
	// the common spelling in Vorbis Comment/MP4.
	ISRC string `json:"isrc,omitempty"`
	// MusicBrainzTrackID is the RECORDING MBID embedded in the file — the key
	// of the scanner's matching fast-path (ADR-0027). Not every tagger writes
	// this tag.
	MusicBrainzTrackID string `json:"musicbrainz_track_id,omitempty"`
	// MusicBrainzArtistID is the embedded ARTIST MBID — rarer than the track
	// one (some taggers only write the recording MBID).
	MusicBrainzArtistID string `json:"musicbrainz_artist_id,omitempty"`
}

// General are the facts of the container (format/duration/size/bitrate).
type General struct {
	Format   string  `json:"format"`
	Duration float64 `json:"duration_seconds"`
	Size     int64   `json:"size_bytes"`
	Bitrate  int64   `json:"bitrate"`
}

// Video is a video track.
type Video struct {
	Codec  string `json:"codec"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// HDR is the detectable label ("HDR10"/"Dolby Vision"/"HLG") or "" (SDR/unknown).
	HDR string `json:"hdr,omitempty"`
}

// Audio is an audio track.
type Audio struct {
	Codec    string `json:"codec"`
	Channels int    `json:"channels"`
	Language string `json:"language,omitempty"`
}

// Subtitle is a subtitle track.
type Subtitle struct {
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"`
}

// runner executes a command and returns the stdout. Injectable for tests.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Prober runs ffprobe. The zero-value is useless: use NewProber. Binary and run
// are injectable (NewProberComRunner) to test without the real ffprobe.
type Prober struct {
	binary string
	run    runner
}

// NewProber creates a Prober that calls the "ffprobe" from PATH through os/exec.
func NewProber() *Prober {
	return NewProberComRunner("ffprobe", execRunner)
}

// NewProberComRunner creates a Prober with an explicit binary and runner (tests).
func NewProberComRunner(binary string, run runner) *Prober {
	return &Prober{binary: binary, run: run}
}

// execRunner is the production runner: it runs the command and captures the
// stdout.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Disponivel says whether the ffprobe binary is in the PATH. The scanner uses it
// at boot to log a warning and degrade (empty mediainfo) instead of failing.
func (p *Prober) Disponivel() bool {
	if _, err := exec.LookPath(p.binary); err != nil {
		return false
	}
	return true
}

// Probe runs ffprobe on the file and returns the mediainfo_json (the shape of
// Info already serialized). An error when the subprocess or the parse fails —
// the caller decides how to degrade.
func (p *Prober) Probe(ctx context.Context, path string) ([]byte, error) {
	out, err := p.run(ctx, p.binary,
		"-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path)
	if err != nil {
		return nil, fmt.Errorf("mediainfo: ffprobe falhou em %q: %w", path, err)
	}
	var raw ffprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("mediainfo: JSON do ffprobe inválido para %q: %w", path, err)
	}
	info := mapear(raw)
	b, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("mediainfo: serializar info de %q: %w", path, err)
	}
	return b, nil
}

// ── Raw ffprobe structs (only the fields we consume) ─────────────────────────

type ffprobeOutput struct {
	Format  ffFormat   `json:"format"`
	Streams []ffStream `json:"streams"`
}

type ffFormat struct {
	FormatName string            `json:"format_name"`
	Duration   string            `json:"duration"`
	Size       string            `json:"size"`
	BitRate    string            `json:"bit_rate"`
	Tags       map[string]string `json:"tags"`
}

type ffStream struct {
	CodecType      string            `json:"codec_type"`
	CodecName      string            `json:"codec_name"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	Channels       int               `json:"channels"`
	ColorTransfer  string            `json:"color_transfer"`
	ColorPrimaries string            `json:"color_primaries"`
	Tags           map[string]string `json:"tags"`
}

// mapear translates the raw ffprobe output into the stable shape of Info.
func mapear(raw ffprobeOutput) Info {
	info := Info{
		General: General{
			Format:   raw.Format.FormatName,
			Duration: parseFloat(raw.Format.Duration),
			Size:     parseInt(raw.Format.Size),
			Bitrate:  parseInt(raw.Format.BitRate),
		},
		Video:    []Video{},
		Audio:    []Audio{},
		Subtitle: []Subtitle{},
	}
	if len(raw.Format.Tags) > 0 {
		info.Tags = extrairAudioTags(raw.Format.Tags)
	}
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			info.Video = append(info.Video, Video{
				Codec: s.CodecName, Width: s.Width, Height: s.Height, HDR: detectarHDR(s),
			})
		case "audio":
			info.Audio = append(info.Audio, Audio{
				Codec: s.CodecName, Channels: s.Channels, Language: idioma(s),
			})
		case "subtitle":
			info.Subtitle = append(info.Subtitle, Subtitle{
				Codec: s.CodecName, Language: idioma(s),
			})
		}
	}
	return info
}

// detectarHDR makes a conservative HDR detection from the color characteristics
// of the video stream; "" when SDR or undetermined.
func detectarHDR(s ffStream) string {
	transfer := strings.ToLower(s.ColorTransfer)
	switch {
	case strings.Contains(transfer, "smpte2084"): // PQ
		return "HDR10"
	case strings.Contains(transfer, "arib-std-b67"): // HLG
		return "HLG"
	default:
		return ""
	}
}

// idioma reads the "language" tag (ISO 639) of the stream, when present.
func idioma(s ffStream) string {
	if s.Tags == nil {
		return ""
	}
	if l := s.Tags["language"]; l != "" && l != "und" {
		return l
	}
	return ""
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func parseInt(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v
}

// extrairAudioTags builds AudioTags from the raw map of format.tags (ADR-0027).
// It builds an index of CANONICAL keys (chaveCanonica) once and resolves each
// field through lookupTag, with the candidates already written in canonical
// form — that way "MusicBrainz Track Id", "musicbrainz_trackid" and
// "MUSICBRAINZ_TRACKID" resolve to the same candidate "musicbrainztrackid"
// without having to list every variation of separator/case by hand.
func extrairAudioTags(tags map[string]string) *AudioTags {
	idx := indexarTags(tags)
	t := &AudioTags{
		Artist:              lookupTag(idx, "artist"),
		Album:               lookupTag(idx, "album"),
		AlbumArtist:         lookupTag(idx, "albumartist"),
		Title:               lookupTag(idx, "title"),
		Genre:               lookupTag(idx, "genre"),
		ISRC:                lookupTag(idx, "isrc", "tsrc"),
		MusicBrainzTrackID:  lookupTag(idx, "musicbrainztrackid"),
		MusicBrainzArtistID: lookupTag(idx, "musicbrainzartistid"),
	}
	t.TrackNumber = parseLeadingInt(lookupTag(idx, "track"))
	t.DiscNumber = parseLeadingInt(lookupTag(idx, "disc"))
	return t
}

// indexarTags normalizes the KEYS of the raw tag map into canonical form
// (chaveCanonica), preserving the FIRST non-empty value seen for each canonical
// key (real containers do not usually repeat the same tag under different
// spellings; if they do, the map iteration order is indeterminate, but the case
// is rare enough not to justify more complexity here).
func indexarTags(tags map[string]string) map[string]string {
	idx := make(map[string]string, len(tags))
	for k, v := range tags {
		if v == "" {
			continue
		}
		ck := chaveCanonica(k)
		if _, existe := idx[ck]; !existe {
			idx[ck] = v
		}
	}
	return idx
}

// chaveCanonica reduces a tag key to lowercase with ONLY letters/digits
// (removing spaces, underscores, hyphens, punctuation) — the normalization that
// makes "MusicBrainz Track Id", "musicbrainz_trackid" and "MUSICBRAINZ_TRACKID"
// collide on the same key.
func chaveCanonica(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// lookupTag looks up, in order, each candidate (ALREADY in canonical form) in
// the index; "" if none matches.
func lookupTag(idx map[string]string, candidatos ...string) string {
	for _, c := range candidatos {
		if v, ok := idx[c]; ok {
			return v
		}
	}
	return ""
}

// parseLeadingInt parses the number before an optional slash ("3" or "3/12" →
// 3); 0 when empty or not numeric.
func parseLeadingInt(s string) int {
	if s == "" {
		return 0
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
