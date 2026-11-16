package audiotag

// writer_real_test.go runs the REAL ffmpeg (same pattern as
// internal/mediainfo/mediainfo_test.go: TestProbeRealFfprobe*, self-skip if
// the binaries are missing from the environment): it generates a synthetic
// audio clip, rewrites the tag with Escrever, and reads it back with ffprobe to
// confirm that the frames/comments match what the package documentation
// promises (EMPIRICAL validation, not just "the args were built").

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ffprobeTags runs `ffprobe -show_format` on the path and returns the tag map
// (JSON format, the same -print_format used by internal/mediainfo).
func ffprobeTags(t *testing.T, path string) map[string]string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path).Output()
	if err != nil {
		t.Fatalf("ffprobe -show_format %q: %v", path, err)
	}
	var parsed struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal ffprobe: %v", err)
	}
	return parsed.Format.Tags
}

// ffprobeStreams returns the streams (codec_type/codec_name/disposition) of the
// file -- used to confirm the embedded cover (mjpeg video stream with
// attached_pic=1).
type streamInfo struct {
	CodecType   string            `json:"codec_type"`
	CodecName   string            `json:"codec_name"`
	Disposition map[string]int    `json:"disposition"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	StreamTags  map[string]string `json:"tags"`
}

func ffprobeStreams(t *testing.T, path string) []streamInfo {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_streams", path).Output()
	if err != nil {
		t.Fatalf("ffprobe -show_streams %q: %v", path, err)
	}
	var parsed struct {
		Streams []streamInfo `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal ffprobe streams: %v", err)
	}
	return parsed.Streams
}

// pularSemFfmpeg skips the test if ffmpeg OR ffprobe are missing from the PATH
// (same pattern as mediainfo_test.go).
func pularSemFfmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg ausente; pulando teste de integracao real")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe ausente; pulando teste de integracao real")
	}
}

// gerarClipe creates a synthetic 1s audio clip at the given path (the extension
// determines the codec: mp3→libmp3lame, flac→flac, ogg→vorbis).
func gerarClipe(t *testing.T, path string) {
	t.Helper()
	var codec []string
	switch filepath.Ext(path) {
	case ".mp3":
		codec = []string{"-c:a", "libmp3lame"}
	case ".flac":
		codec = []string{"-c:a", "flac"}
	case ".ogg":
		codec = []string{"-ac", "2", "-c:a", "vorbis", "-strict", "-2"}
	}
	args := append([]string{"-v", "quiet", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=44100"}, codec...)
	args = append(args, path)
	if err := exec.Command("ffmpeg", args...).Run(); err != nil {
		t.Skipf("ffmpeg nao gerou o clipe %q: %v", path, err)
	}
}

// gerarCapa creates a minimal valid JPEG image at the given path.
func gerarCapa(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-y", "-f", "lavfi",
		"-i", "color=c=red:s=32x32:d=1", "-frames:v", "1", path)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg nao gerou a capa: %v", err)
	}
}

// TestRealMp3EscreveTagsECapa runs the Writer FOR REAL against a synthetic .mp3
// and confirms, through ffprobe, title/artist/track/disc/ISRC/MBIDs and the
// embedded cover as an mjpeg attached_pic stream (acceptance criterion:
// "Supports MP3... with title/artist/album/year/track-disc/genre/MBIDs/ISRC/
// cover resolved").
func TestRealMp3EscreveTagsECapa(t *testing.T) {
	pularSemFfmpeg(t)
	dir := t.TempDir()
	clipe := filepath.Join(dir, "faixa.mp3")
	gerarClipe(t, clipe)
	capa := filepath.Join(dir, "capa.jpg")
	gerarCapa(t, capa)

	w := NewWriter()
	if !w.Disponivel() {
		t.Skip("ffmpeg indisponivel")
	}
	m := Metadado{
		Title: "Real Track", Artist: "Real Artist", Album: "Real Album",
		AlbumArtist: "Real Artist", Date: "1999", Genre: "Rock",
		TrackNumber: 4, TrackTotal: 10, DiscNumber: 1, DiscTotal: 2,
		ISRC: "USRC17607839", MBRecordingID: "rec-mbid", MBReleaseGroupID: "rg-mbid",
		MBArtistID: "art-mbid", MBAlbumArtistID: "art-mbid", CoverPath: capa,
	}
	if err := w.Escrever(context.Background(), clipe, m); err != nil {
		t.Fatalf("Escrever real: %v", err)
	}

	tags := ffprobeTags(t, clipe)
	eqTag(t, tags, "title", "Real Track")
	eqTag(t, tags, "artist", "Real Artist")
	eqTag(t, tags, "album_artist", "Real Artist")
	eqTag(t, tags, "album", "Real Album")
	eqTag(t, tags, "genre", "Rock")
	eqTag(t, tags, "track", "4/10")
	eqTag(t, tags, "disc", "1/2")
	eqTag(t, tags, "TSRC", "USRC17607839")
	eqTag(t, tags, "MusicBrainz Track Id", "rec-mbid")
	eqTag(t, tags, "MusicBrainz Album Id", "rg-mbid")
	eqTag(t, tags, "MusicBrainz Release Group Id", "rg-mbid")
	eqTag(t, tags, "MusicBrainz Artist Id", "art-mbid")

	streams := ffprobeStreams(t, clipe)
	achouCapa := false
	for _, s := range streams {
		if s.CodecType == "video" && s.CodecName == "mjpeg" && s.Disposition["attached_pic"] == 1 {
			achouCapa = true
		}
	}
	if !achouCapa {
		t.Errorf("nao achei stream de video mjpeg attached_pic=1 (capa embutida) em %+v", streams)
	}
}

// TestRealFlacEOggEscrevemTagsECapa runs the Writer FOR REAL against synthetic
// .flac and .ogg files and confirms through ffprobe the Vorbis comments
// (uppercase keys, TRACKNUMBER/TRACKTOTAL separate) and the
// METADATA_BLOCK_PICTURE decoded back as an image stream.
func TestRealFlacEOggEscrevemTagsECapa(t *testing.T) {
	pularSemFfmpeg(t)
	for _, ext := range []string{".flac", ".ogg"} {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			clipe := filepath.Join(dir, "faixa"+ext)
			gerarClipe(t, clipe)
			capa := filepath.Join(dir, "capa.jpg")
			gerarCapa(t, capa)

			w := NewWriter()
			if !w.Disponivel() {
				t.Skip("ffmpeg indisponivel")
			}
			m := Metadado{
				Title: "Real Track", Artist: "Real Artist", Album: "Real Album",
				AlbumArtist: "Real Artist", TrackNumber: 5, TrackTotal: 11,
				DiscNumber: 1, DiscTotal: 1, MBRecordingID: "rec-mbid",
				Compilation: true, CoverPath: capa,
			}
			if err := w.Escrever(context.Background(), clipe, m); err != nil {
				t.Fatalf("Escrever real (%s): %v", ext, err)
			}

			// For .flac the tags come out at format level; for .ogg they come
			// out on the first stream (validated empirically in the manual
			// exploration) -- join both universes into a single check.
			// ffprobe (reading) normalizes TRACKNUMBER/DISCNUMBER/ALBUMARTIST
			// to its generic keys "track"/"disc"/"album_artist" -- the same
			// normalization that internal/mediainfo (our OWN reader) already
			// does through lookupTag; the remaining fields (TRACKTOTAL,
			// MUSICBRAINZ_*, COMPILATION) have no generic mapping and come
			// out verbatim, as written.
			tags := ffprobeTags(t, clipe)
			if tags == nil {
				tags = map[string]string{}
			}
			streams := ffprobeStreams(t, clipe)
			for _, s := range streams {
				for k, v := range s.StreamTags {
					if _, existe := tags[k]; !existe {
						tags[k] = v
					}
				}
			}

			eqTag(t, tags, "TITLE", "Real Track")
			eqTag(t, tags, "ARTIST", "Real Artist")
			eqTag(t, tags, "track", "5")
			eqTag(t, tags, "TRACKTOTAL", "11")
			eqTag(t, tags, "MUSICBRAINZ_TRACKID", "rec-mbid")
			eqTag(t, tags, "COMPILATION", "1")

			achouCapa := false
			for _, s := range streams {
				if s.CodecType == "video" {
					achouCapa = true
				}
			}
			if !achouCapa {
				t.Errorf("(%s) nao achei stream de imagem decodificado do METADATA_BLOCK_PICTURE: %+v", ext, streams)
			}
		})
	}
}

func eqTag(t *testing.T, tags map[string]string, chave, quer string) {
	t.Helper()
	if got := tags[chave]; got != quer {
		t.Errorf("tag %q = %q, quero %q (tags=%v)", chave, got, quer, tags)
	}
}

// TestRealEscritaNaoQuebraArquivoQuandoOriginalValido guarantees that the
// atomic rename mechanism leaves the REWRITTEN file playable by ffprobe (not
// corrupted) -- a general sanity smoke test of the real pipeline.
func TestRealEscritaNaoQuebraArquivoQuandoOriginalValido(t *testing.T) {
	pularSemFfmpeg(t)
	dir := t.TempDir()
	clipe := filepath.Join(dir, "faixa.mp3")
	gerarClipe(t, clipe)

	w := NewWriter()
	if !w.Disponivel() {
		t.Skip("ffmpeg indisponivel")
	}
	if err := w.Escrever(context.Background(), clipe, Metadado{Title: "T"}); err != nil {
		t.Fatalf("Escrever: %v", err)
	}
	if _, err := os.Stat(clipe); err != nil {
		t.Fatalf("arquivo regravado sumiu: %v", err)
	}
	if _, err := exec.Command("ffprobe", "-v", "error", clipe).CombinedOutput(); err != nil {
		t.Errorf("ffprobe rejeitou o arquivo regravado (corrompido?): %v", err)
	}
}
