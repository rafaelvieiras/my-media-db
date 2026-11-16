package mediainfo

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
)

// saidaFake mimics the ffprobe JSON for a clip with 1 video + 1 audio.
const saidaFake = `{
  "streams": [
    {"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"color_transfer":"smpte2084"},
    {"codec_type":"audio","codec_name":"aac","channels":6,"tags":{"language":"eng"}},
    {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"por"}}
  ],
  "format": {"format_name":"matroska,webm","duration":"1.5","size":"12503","bit_rate":"97775"}
}`

// eq fails the test if got != want. It concentrates the branching in a helper to
// keep the test functions with low cyclomatic complexity.
func eq[T comparable](t *testing.T, nome string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, quero %v", nome, got, want)
	}
}

// TestProbeMapeiaComRunnerInjetado exercises the mapping without depending on
// the real ffprobe: it injects a runner that returns a fixed JSON.
func TestProbeMapeiaComRunnerInjetado(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(saidaFake), nil
	}
	p := NewProberComRunner("ffprobe-fake", run)

	raw, err := p.Probe(context.Background(), "/x.mkv")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	var got Info
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	eq(t, "general.format", got.General.Format, "matroska,webm")
	eq(t, "general.duration", got.General.Duration, 1.5)
	eq(t, "len(video)", len(got.Video), 1)
	eq(t, "video.codec", got.Video[0].Codec, "h264")
	eq(t, "video.width", got.Video[0].Width, 1920)
	eq(t, "video.hdr", got.Video[0].HDR, "HDR10")
	eq(t, "len(audio)", len(got.Audio), 1)
	eq(t, "audio.channels", got.Audio[0].Channels, 6)
	eq(t, "audio.language", got.Audio[0].Language, "eng")
	eq(t, "len(subtitle)", len(got.Subtitle), 1)
	eq(t, "subtitle.language", got.Subtitle[0].Language, "por")
}

// saidaFakeAudio mimics the ffprobe JSON for an AUDIO file (ADR-0027):
// format.tags with DELIBERATELY varied spellings (title/artist in the usual
// lowercase, MusicBrainz Track/Artist Id in different separator/case forms, ISRC
// through the uppercase TSRC, track/disc with the "N/M" slash) to prove that
// extrairAudioTags normalizes the key and not only the obvious case.
const saidaFakeAudio = `{
  "streams": [
    {"codec_type":"audio","codec_name":"flac","channels":2}
  ],
  "format": {
    "format_name":"flac",
    "duration":"210.5",
    "size":"5000000",
    "bit_rate":"900000",
    "tags": {
      "artist":"Fake Band",
      "album":"Fake Album",
      "ALBUM_ARTIST":"Fake Band",
      "title":"Fake Track",
      "track":"3/12",
      "disc":"1/2",
      "genre":"Rock",
      "TSRC":"US1234500001",
      "MusicBrainz Track Id":"cccccccc-0000-0000-0000-000000000001",
      "musicbrainz_artistid":"aaaaaaaa-0000-0000-0000-000000000001"
    }
  }
}`

// TestProbeMapeiaTagsDeAudioComRunnerInjetado covers the NEW Info.Tags field
// (ADR-0027, mediainfo): artist/album/album_artist/title/genre/track/disc (with
// the "N/M" slash) and the two MBIDs (track/artist) with tag spellings different
// from the others — the proof that extrairAudioTags normalizes the KEY and does
// not depend on the file using exactly "musicbrainz_trackid".
func TestProbeMapeiaTagsDeAudioComRunnerInjetado(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(saidaFakeAudio), nil
	}
	p := NewProberComRunner("ffprobe-fake", run)

	raw, err := p.Probe(context.Background(), "/x.flac")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	var got Info
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tags == nil {
		t.Fatal("Tags = nil, quero AudioTags preenchido")
	}
	eq(t, "tags.artist", got.Tags.Artist, "Fake Band")
	eq(t, "tags.album", got.Tags.Album, "Fake Album")
	eq(t, "tags.album_artist", got.Tags.AlbumArtist, "Fake Band")
	eq(t, "tags.title", got.Tags.Title, "Fake Track")
	eq(t, "tags.track_number", got.Tags.TrackNumber, 3)
	eq(t, "tags.disc_number", got.Tags.DiscNumber, 1)
	eq(t, "tags.genre", got.Tags.Genre, "Rock")
	eq(t, "tags.isrc", got.Tags.ISRC, "US1234500001")
	eq(t, "tags.musicbrainz_track_id", got.Tags.MusicBrainzTrackID, "cccccccc-0000-0000-0000-000000000001")
	eq(t, "tags.musicbrainz_artist_id", got.Tags.MusicBrainzArtistID, "aaaaaaaa-0000-0000-0000-000000000001")
	// Pre-existing consumer of General/Video/Audio: nothing broke.
	eq(t, "general.duration", got.General.Duration, 210.5)
	eq(t, "len(audio)", len(got.Audio), 1)
}

// TestProbeSemTagsDeFormatoDevolveTagsNil covers backward compatibility: a
// regular VIDEO file, without format.tags, must not gain an empty Info.Tags{}
// (pollution of the persisted JSON) — it must stay nil.
func TestProbeSemTagsDeFormatoDevolveTagsNil(t *testing.T) {
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(saidaFake), nil
	}
	p := NewProberComRunner("ffprobe-fake", run)
	raw, err := p.Probe(context.Background(), "/x.mkv")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	var got Info
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tags != nil {
		t.Errorf("Tags = %+v, quero nil (sem format.tags na fonte)", got.Tags)
	}
}

// TestProbeRealFfprobeAudioTags runs the REAL ffprobe against a synthetic .mp3
// generated by ffmpeg with -metadata (the same pattern as TestProbeRealFfprobe,
// but for audio tags). Skip if ffmpeg/ffprobe are missing from the environment.
func TestProbeRealFfprobeAudioTags(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg ausente; pulando o teste de integração de tags de áudio")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe ausente; pulando o teste de integração de tags de áudio")
	}
	dir := t.TempDir()
	clipe := dir + "/clipe.mp3"
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=1",
		"-metadata", "artist=Fake Band",
		"-metadata", "album=Fake Album",
		"-metadata", "album_artist=Fake Band",
		"-metadata", "title=Fake Track",
		"-metadata", "track=3/12",
		"-metadata", "genre=Rock",
		"-c:a", "libmp3lame", "-b:a", "64k", clipe)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg não gerou o clipe de áudio: %v", err)
	}

	p := NewProber()
	if !p.Disponivel() {
		t.Skip("ffprobe indisponível")
	}
	raw, err := p.Probe(context.Background(), clipe)
	if err != nil {
		t.Fatalf("Probe real: %v", err)
	}
	var got Info
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Tags == nil {
		t.Fatal("Tags = nil, quero AudioTags preenchido a partir do .mp3 real")
	}
	eq(t, "tags.artist real", got.Tags.Artist, "Fake Band")
	eq(t, "tags.album real", got.Tags.Album, "Fake Album")
	eq(t, "tags.title real", got.Tags.Title, "Fake Track")
	eq(t, "tags.track_number real", got.Tags.TrackNumber, 3)
	eq(t, "tags.genre real", got.Tags.Genre, "Rock")
}

// TestProbeRealFfprobe runs the REAL ffprobe against a synthetic clip generated
// by ffmpeg. If either of the binaries is missing from the environment, it Skips
// (it does not fail).
func TestProbeRealFfprobe(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg ausente; pulando o teste de integração do ffprobe")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe ausente; pulando o teste de integração do ffprobe")
	}
	dir := t.TempDir()
	clipe := dir + "/clipe.mkv"
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-f", "lavfi",
		"-i", "testsrc=d=1:s=64x64", "-c:v", "libx264", "-t", "1", clipe)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg não gerou o clipe: %v", err)
	}

	p := NewProber()
	if !p.Disponivel() {
		t.Skip("ffprobe indisponível")
	}
	raw, err := p.Probe(context.Background(), clipe)
	if err != nil {
		t.Fatalf("Probe real: %v", err)
	}
	var got Info
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	eq(t, "len(video) real", len(got.Video), 1)
	eq(t, "video.width real", got.Video[0].Width, 64)
	eq(t, "video.height real", got.Video[0].Height, 64)
	if got.General.Duration <= 0 {
		t.Errorf("duração real = %v, quero > 0", got.General.Duration)
	}
}
