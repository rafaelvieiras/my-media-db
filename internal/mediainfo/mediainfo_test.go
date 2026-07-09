package mediainfo

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
)

// saidaFake imita o JSON do ffprobe para um clipe com 1 vídeo + 1 áudio.
const saidaFake = `{
  "streams": [
    {"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"color_transfer":"smpte2084"},
    {"codec_type":"audio","codec_name":"aac","channels":6,"tags":{"language":"eng"}},
    {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"por"}}
  ],
  "format": {"format_name":"matroska,webm","duration":"1.5","size":"12503","bit_rate":"97775"}
}`

// eq falha o teste se got != want. Concentra a ramificação num helper para
// manter as funções de teste com baixa complexidade ciclomática.
func eq[T comparable](t *testing.T, nome string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, quero %v", nome, got, want)
	}
}

// TestProbeMapeiaComRunnerInjetado exercita o mapeamento sem depender do ffprobe
// real: injeta um runner que devolve um JSON fixo.
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

// TestProbeRealFfprobe roda o ffprobe DE VERDADE contra um clipe sintético
// gerado pelo ffmpeg. Se qualquer um dos binários faltar no ambiente, faz Skip
// (não falha).
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
