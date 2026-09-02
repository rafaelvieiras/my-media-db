package audiotag

// writer_real_test.go roda o ffmpeg DE VERDADE (mesmo padrão de
// internal/mediainfo/mediainfo_test.go: TestProbeRealFfprobe*, self-skip se
// os binários faltarem no ambiente): gera um clipe de áudio sintético,
// regrava a tag com Escrever, e lê de volta com ffprobe para confirmar que
// os frames/comentários batem com o que a documentação do pacote promete
// (validação EMPÍRICA, não só "os args foram montados").

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ffprobeTags roda `ffprobe -show_format` no path e devolve o mapa de tags
// (formato JSON, mesmo -print_format usado pelo internal/mediainfo).
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

// ffprobeStreams devolve os streams (codec_type/codec_name/disposition) do
// arquivo -- usado para confirmar a capa embutida (stream de vídeo mjpeg com
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

// pularSemFfmpeg pula o teste se ffmpeg OU ffprobe faltarem no PATH (mesmo
// padrão de mediainfo_test.go).
func pularSemFfmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg ausente; pulando teste de integracao real")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe ausente; pulando teste de integracao real")
	}
}

// gerarClipe cria um clipe de áudio sintético de 1s no path dado (extensão
// determina o codec: mp3→libmp3lame, flac→flac, ogg→vorbis).
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

// gerarCapa cria uma imagem JPEG mínima válida no path dado.
func gerarCapa(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-y", "-f", "lavfi",
		"-i", "color=c=red:s=32x32:d=1", "-frames:v", "1", path)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg nao gerou a capa: %v", err)
	}
}

// TestRealMp3EscreveTagsECapa roda o Writer DE VERDADE contra um .mp3
// sintético e confirma, via ffprobe, título/artista/track/disc/ISRC/MBIDs e
// a capa embutida como stream mjpeg attached_pic (briefing 0125, critério de
// aceite: "Suporta MP3... com título/artista/álbum/ano/faixa-disco/gênero/
// MBIDs/ISRC/capa resolvidos").
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

// TestRealFlacEOggEscrevemTagsECapa roda o Writer DE VERDADE contra .flac e
// .ogg sintéticos e confirma via ffprobe os comentários Vorbis (chaves
// maiúsculas, TRACKNUMBER/TRACKTOTAL separados) e o METADATA_BLOCK_PICTURE
// decodificado de volta como um stream de imagem.
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

			// Para .flac as tags saem no format-level; para .ogg saem no
			// primeiro stream (validado empiricamente na exploração
			// manual) -- junta os dois universos numa única checagem.
			// ffprobe (leitura) normaliza TRACKNUMBER/DISCNUMBER/ALBUMARTIST
			// para suas chaves genéricas "track"/"disc"/"album_artist" --
			// mesma normalização que internal/mediainfo (nosso PRÓPRIO
			// leitor) já faz via lookupTag; os demais campos (TRACKTOTAL,
			// MUSICBRAINZ_*, COMPILATION) não têm mapeamento genérico e
			// saem verbatim, como escritos.
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

// TestRealEscritaNaoQuebraArquivoQuandoOriginalValido garante que o
// mecanismo de rename atômico deixa o arquivo REGRAVADO tocável pelo
// ffprobe (não corrompido) -- um smoke test de sanidade geral do pipeline
// real.
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
