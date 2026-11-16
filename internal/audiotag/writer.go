package audiotag

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrFormatoNaoSuportado is returned by Escrever when the extension of the
// file is not .mp3/.flac/.ogg (only those 3 formats are supported).
var ErrFormatoNaoSuportado = errors.New("audiotag: formato de audio nao suportado (use mp3, flac ou ogg)")

// timeoutFfmpeg is the ceiling of ONE ffmpeg call — a stuck process (e.g.: a
// corrupted file) must not hold an album→tracks cascade forever (best-effort:
// a timeout failure of ONE track does not abort the others, see
// internal/server/audio_tags.go).
const timeoutFfmpeg = 60 * time.Second

// runner runs a command and returns the COMBINED output (stdout+stderr) —
// ffmpeg writes its error log to stderr; capturing both is what gives the
// caller a useful error when the subprocess fails. Injectable for tests (same
// pattern as internal/mediainfo.runner).
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Writer rewrites the audio tag through ffmpeg. Useless zero value: use
// NewWriter. Binary and run are injectable (NewWriterComRunner) to test
// without the real ffmpeg (same pattern as internal/mediainfo.Prober).
type Writer struct {
	binary string
	run    runner
}

// NewWriter creates a Writer that calls the "ffmpeg" from PATH through
// os/exec.
func NewWriter() *Writer {
	return NewWriterComRunner("ffmpeg", execRunner)
}

// NewWriterComRunner creates a Writer with an explicit binary and runner
// (tests).
func NewWriterComRunner(binary string, run runner) *Writer {
	return &Writer{binary: binary, run: run}
}

// execRunner is the production runner: it runs the command and captures
// stdout+stderr.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Disponivel tells whether the ffmpeg binary is in the PATH. The caller uses it
// to degrade with a 503 instead of trying and failing per process (same pattern
// as mediainfo.Prober.Disponivel).
func (w *Writer) Disponivel() bool {
	_, err := exec.LookPath(w.binary)
	return err == nil
}

// Escrever rewrites the tag of ONE audio file with the given Metadado,
// dispatching by FORMAT (extension of the path): ID3v2 for .mp3, Vorbis comment
// for .flac/.ogg. The write is always a TOTAL REPLACEMENT of the previous tag
// (-map_metadata -1: it discards any old tag before applying the Metadado —
// including a pre-existing cover, when Metadado.CoverPath == ""), never a merge
// — consistent with "the resolved value is the source of truth". The audio
// itself is NEVER re-encoded (-c:a copy): only the container/tag changes, with
// no quality loss and no CPU cost of a reencode. It writes to a temporary file
// in the SAME directory and swaps it with an atomic rename — the original file
// is only replaced if ffmpeg finishes successfully.
func (w *Writer) Escrever(ctx context.Context, path string, m Metadado) error {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return w.escreverID3(ctx, path, m)
	case ".flac", ".ogg":
		return w.escreverVorbis(ctx, path, m)
	default:
		return fmt.Errorf("%w: %q", ErrFormatoNaoSuportado, ext)
	}
}

// escreverID3 builds the ffmpeg args for MP3 (ID3v2): when there is a cover, a
// 2nd input (the image) is mapped as an MJPEG video stream with
// -disposition:v attached_pic — the real way for ffmpeg to produce an APIC
// frame (validated empirically: ffprobe -show_streams returns the stream back
// with disposition.attached_pic=1). -id3v2_version 3 pins ID3v2.3 (more
// compatible with old players than 2.4; both are read by the target ecosystem).
func (w *Writer) escreverID3(ctx context.Context, path string, m Metadado) error {
	temCapa := m.CoverPath != ""
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", path}
	if temCapa {
		args = append(args, "-i", m.CoverPath)
	}
	args = append(args, "-map", "0:a")
	if temCapa {
		args = append(args, "-map", "1:0", "-c:v", "mjpeg", "-disposition:v", "attached_pic")
	}
	args = append(args, "-c:a", "copy", "-map_metadata", "-1")
	args = append(args, metadataID3(m)...)
	args = append(args, "-id3v2_version", "3")
	return w.regravar(ctx, path, args)
}

// escreverVorbis builds the ffmpeg args for FLAC/OGG (Vorbis comment): the
// cover (when present) does NOT go in as a video input -- the ogg container
// refuses to mux an mjpeg stream (validated empirically: "Unsupported codec
// id in stream 1") and even FLAC, which accepts it, would gain a "real" video
// stream instead of the METADATA_BLOCK_PICTURE block that players expect.
// Instead, blocoCapaBase64 builds the binary block from the FLAC spec and
// injects it as the VALUE of an ordinary Vorbis comment
// (METADATA_BLOCK_PICTURE) -- it works identically in .flac and .ogg (validated
// with ffprobe -show_streams, which decodes the block back as a synthetic
// video stream in both formats).
func (w *Writer) escreverVorbis(ctx context.Context, path string, m Metadado) error {
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", path,
		"-map", "0:a", "-c:a", "copy", "-map_metadata", "-1"}
	metaArgs, err := metadataVorbis(m)
	if err != nil {
		return err
	}
	args = append(args, metaArgs...)
	return w.regravar(ctx, path, args)
}

// regravar completes `args` with the output path (a temporary file in the SAME
// directory as the original), runs ffmpeg, and replaces the original file with
// an atomic rename only on success — the original never ends up in a
// partial/corrupted state if ffmpeg fails halfway.
func (w *Writer) regravar(ctx context.Context, path string, args []string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "audiotag-*"+filepath.Ext(path))
	if err != nil {
		return fmt.Errorf("audiotag: criar arquivo temporario em %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }() // best-effort: the temporary file goes away if the rename does not happen

	ctxComTimeout, cancel := context.WithTimeout(ctx, timeoutFfmpeg)
	defer cancel()

	fullArgs := append(args, tmpPath)
	out, err := w.run(ctxComTimeout, w.binary, fullArgs...)
	if err != nil {
		return fmt.Errorf("audiotag: ffmpeg falhou em %q: %w (saida: %s)", path, err, truncar(out))
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("audiotag: substituir %q pelo arquivo regravado: %w", path, err)
	}
	return nil
}

// truncar limits the ffmpeg output embedded in the error message (it avoids a
// gigantic error when ffmpeg dumps a large log before failing).
func truncar(out []byte) string {
	const limite = 2000
	if len(out) <= limite {
		return string(out)
	}
	return string(out[:limite]) + "... (truncado)"
}

// metadataID3 builds the -metadata pairs for MP3 in the Picard/ID3v2 convention
// (validated empirically against the real ffmpeg: "title"/"artist"/
// "album_artist"/"album"/"date"/"genre"/"track"/"disc" map to the native
// frames TIT2/TPE1/TPE2/TALB/TYER/TCON/TRCK/TPOS; any OTHER key -- the
// MusicBrainz IDs -- becomes a TXXX frame with that key as the DESCRIPTION, the
// same convention Picard uses ("MusicBrainz Track Id" etc.) -- and "TSRC" hits
// the native 4-letter ISRC frame directly). track/disc use the "N/T" format
// when the total is known (TRCK/TPOS accept that form). There is no distinct
// RELEASE MBID in our model (ADR-0027 only persists release-group) --
// "MusicBrainz Album Id" reuses the SAME release-group value, documented here
// instead of inventing an MBID we do not have.
func metadataID3(m Metadado) []string {
	var out []string
	add := func(chave, valor string) {
		if valor != "" {
			out = append(out, "-metadata", chave+"="+valor)
		}
	}
	add("title", m.Title)
	add("artist", m.Artist)
	add("album_artist", m.AlbumArtist)
	add("album", m.Album)
	add("date", m.Date)
	add("genre", m.Genre)
	add("track", numeroETotal(m.TrackNumber, m.TrackTotal))
	add("disc", numeroETotal(m.DiscNumber, m.DiscTotal))
	add("MusicBrainz Track Id", m.MBRecordingID)
	add("MusicBrainz Album Id", m.MBReleaseGroupID)
	add("MusicBrainz Release Group Id", m.MBReleaseGroupID)
	add("MusicBrainz Artist Id", m.MBArtistID)
	add("MusicBrainz Album Artist Id", m.MBAlbumArtistID)
	add("TSRC", m.ISRC)
	if m.Compilation {
		// Best-effort: ffmpeg does not map "compilation" to the iTunes
		// binary TCMP frame -- it becomes TXXX:compilation (validated
		// empirically). Good enough for the use case (flagging it visually/
		// through a text tag); a real TCMP would be left for another round if
		// some real consumer of the owner actually needs it specifically.
		out = append(out, "-metadata", "compilation=1")
	}
	return out
}

// metadataVorbis builds the -metadata pairs for FLAC/OGG in the Vorbis
// comment/Picard convention (TITLE/ARTIST/ALBUMARTIST/ALBUM/DATE/GENRE/
// TRACKNUMBER/TRACKTOTAL/DISCNUMBER/DISCTOTAL/MUSICBRAINZ_*/ISRC/
// COMPILATION, all validated empirically against the real ffmpeg — unlike MP3,
// here TRACKNUMBER/DISCNUMBER are separate fields from TRACKTOTAL/DISCTOTAL,
// not "N/T" — and it adds METADATA_BLOCK_PICTURE when there is a cover (see
// blocoCapaBase64/escreverVorbis).
func metadataVorbis(m Metadado) ([]string, error) {
	var out []string
	add := func(chave, valor string) {
		if valor != "" {
			out = append(out, "-metadata", chave+"="+valor)
		}
	}
	addInt := func(chave string, v int) {
		if v > 0 {
			out = append(out, "-metadata", chave+"="+strconv.Itoa(v))
		}
	}
	add("TITLE", m.Title)
	add("ARTIST", m.Artist)
	add("ALBUMARTIST", m.AlbumArtist)
	add("ALBUM", m.Album)
	add("DATE", m.Date)
	add("GENRE", m.Genre)
	addInt("TRACKNUMBER", m.TrackNumber)
	addInt("TRACKTOTAL", m.TrackTotal)
	addInt("DISCNUMBER", m.DiscNumber)
	addInt("DISCTOTAL", m.DiscTotal)
	add("MUSICBRAINZ_TRACKID", m.MBRecordingID)
	add("MUSICBRAINZ_ALBUMID", m.MBReleaseGroupID)
	add("MUSICBRAINZ_RELEASEGROUPID", m.MBReleaseGroupID)
	add("MUSICBRAINZ_ARTISTID", m.MBArtistID)
	add("MUSICBRAINZ_ALBUMARTISTID", m.MBAlbumArtistID)
	add("ISRC", m.ISRC)
	if m.Compilation {
		out = append(out, "-metadata", "COMPILATION=1")
	}
	if m.CoverPath != "" {
		bloco, err := blocoCapaBase64(m.CoverPath)
		if err != nil {
			return nil, err
		}
		out = append(out, "-metadata", "METADATA_BLOCK_PICTURE="+bloco)
	}
	return out, nil
}

// numeroETotal formats "N" or "N/T" (when total > 0) for the TRCK/TPOS frames
// of ID3v2, which accept that combined format. n <= 0 returns "" (the caller
// (add) does not write the field).
func numeroETotal(n, total int) string {
	if n <= 0 {
		return ""
	}
	if total > 0 {
		return fmt.Sprintf("%d/%d", n, total)
	}
	return strconv.Itoa(n)
}

// blocoCapaBase64 reads the image at coverPath and builds the
// METADATA_BLOCK_PICTURE block (FLAC spec, reused by the Vorbis comment of any
// container -- including OGG, validated empirically) encoded in base64, ready to
// become the VALUE of a Vorbis comment. The mime is sniffed from the magic
// bytes (same mechanism as internal/server/assets.go:lerArquivoValidado, never
// the extension of the file).
func blocoCapaBase64(coverPath string) (string, error) {
	dados, err := os.ReadFile(coverPath) //nolint:gosec // path resolved/validated by the caller (server)
	if err != nil {
		return "", fmt.Errorf("audiotag: ler capa %q: %w", coverPath, err)
	}
	mime := http.DetectContentType(dados)
	return montarBlocoImagem(mime, dados), nil
}

// montarBlocoImagem serializes the METADATA_BLOCK_PICTURE binary block (FLAC
// spec, big-endian fields): type 3 = front cover (same convention as the APIC
// embedded in MP3); empty description; width/height/color depth/number of colors
// all 0 -- the spec allows 0 for "unknown/not informed" and a real reader decodes
// the image from its own bytes, not from those fields (validated empirically:
// ffprobe returns the REAL dimensions even with the fields declared as 0).
func montarBlocoImagem(mime string, dados []byte) string {
	var buf bytes.Buffer
	escreverUint32(&buf, 3) // type: front cover
	escreverString32(&buf, mime)
	escreverString32(&buf, "") // description
	escreverUint32(&buf, 0)    // width (unknown)
	escreverUint32(&buf, 0)    // height (unknown)
	escreverUint32(&buf, 0)    // color depth (unknown)
	escreverUint32(&buf, 0)    // number of colors (0 = not indexed)
	escreverUint32(&buf, uint32(len(dados)))
	buf.Write(dados)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func escreverUint32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

func escreverString32(buf *bytes.Buffer, s string) {
	escreverUint32(buf, uint32(len(s)))
	buf.WriteString(s)
}
