package audiotag

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// capturarArgs monta um runner FALSO que grava os args recebidos (sem
// chamar ffmpeg de verdade) e devolve um erro pré-configurado -- usado para
// testar a MONTAGEM dos argumentos sem depender do binário real. O runner
// ainda cria o arquivo de saída (o último arg) para regravar/os.Rename ter o
// que renomear quando queremos testar o caminho de sucesso.
func capturarArgs(t *testing.T, criarSaida bool, errar error) (*[]string, runner) {
	t.Helper()
	var capturados []string
	return &capturados, func(_ context.Context, name string, args ...string) ([]byte, error) {
		capturados = append([]string{name}, args...)
		if errar != nil {
			return []byte("ffmpeg: erro fake"), errar
		}
		if criarSaida {
			saida := args[len(args)-1]
			if err := os.WriteFile(saida, []byte("conteudo-regravado"), 0o644); err != nil {
				t.Fatalf("escrever saida fake: %v", err)
			}
		}
		return []byte{}, nil
	}
}

func contains(args []string, valor string) bool {
	for _, a := range args {
		if a == valor {
			return true
		}
	}
	return false
}

// TestEscreverMp3ArgsBasicos cobre a montagem dos argumentos ID3v2 (mp3): os
// pares -metadata na convenção Picard, id3v2_version fixo em 3, sem capa
// quando CoverPath == "".
func TestEscreverMp3ArgsBasicos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "faixa.mp3")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	capturados, run := capturarArgs(t, true, nil)
	w := NewWriterComRunner("ffmpeg-fake", run)

	m := Metadado{
		Title: "Hungry Like the Wolf", Artist: "Duran Duran", Album: "Rio",
		AlbumArtist: "Duran Duran", Date: "1982", Genre: "New Wave",
		TrackNumber: 3, TrackTotal: 9, DiscNumber: 1, DiscTotal: 1,
		ISRC: "GBAYE8200093", MBRecordingID: "rec-mbid", MBReleaseGroupID: "rg-mbid",
		MBArtistID: "art-mbid", MBAlbumArtistID: "art-mbid",
	}
	if err := w.Escrever(context.Background(), path, m); err != nil {
		t.Fatalf("Escrever: %v", err)
	}
	args := *capturados
	if !contains(args, "title=Hungry Like the Wolf") {
		t.Errorf("args nao contem title esperado: %v", args)
	}
	if !contains(args, "track=3/9") {
		t.Errorf("args nao contem track=3/9 (N/T): %v", args)
	}
	if !contains(args, "disc=1/1") {
		t.Errorf("args nao contem disc=1/1: %v", args)
	}
	if !contains(args, "TSRC=GBAYE8200093") {
		t.Errorf("args nao contem TSRC (ISRC) esperado: %v", args)
	}
	if !contains(args, "MusicBrainz Track Id=rec-mbid") {
		t.Errorf("args nao contem MusicBrainz Track Id: %v", args)
	}
	if !contains(args, "MusicBrainz Album Id=rg-mbid") {
		t.Errorf("args nao contem MusicBrainz Album Id (reuso do release-group): %v", args)
	}
	if !contains(args, "-id3v2_version") || !contains(args, "3") {
		t.Errorf("args nao fixa id3v2_version=3: %v", args)
	}
	if contains(args, "-disposition:v") {
		t.Errorf("args contem -disposition:v sem capa nenhuma: %v", args)
	}

	regravado, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ler arquivo pos-escrita: %v", err)
	}
	if string(regravado) != "conteudo-regravado" {
		t.Errorf("conteudo pos-escrita = %q, quero o arquivo temporario renomeado por cima do original", regravado)
	}
}

// TestEscreverMp3ComCapaMapeiaSegundoInput cobre a inclusão do 2º input
// (capa) com -disposition:v attached_pic quando CoverPath != "".
func TestEscreverMp3ComCapaMapeiaSegundoInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "faixa.mp3")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	capa := filepath.Join(dir, "capa.jpg")
	if err := os.WriteFile(capa, []byte{0xFF, 0xD8, 0xFF, 0xE0}, 0o644); err != nil {
		t.Fatal(err)
	}
	capturados, run := capturarArgs(t, true, nil)
	w := NewWriterComRunner("ffmpeg-fake", run)

	if err := w.Escrever(context.Background(), path, Metadado{Title: "T", CoverPath: capa}); err != nil {
		t.Fatalf("Escrever: %v", err)
	}
	args := *capturados
	if !contains(args, "-disposition:v") || !contains(args, "attached_pic") {
		t.Errorf("args nao mapeia a capa com attached_pic: %v", args)
	}
	if !contains(args, capa) {
		t.Errorf("args nao inclui o path da capa como input: %v", args)
	}
}

// TestEscreverVorbisArgsBasicos cobre a montagem dos argumentos Vorbis
// comment (flac/ogg): TRACKNUMBER/TRACKTOTAL SEPARADOS (ao contrário do
// "N/T" do mp3), chaves MUSICBRAINZ_* maiusculas.
func TestEscreverVorbisArgsBasicos(t *testing.T) {
	for _, ext := range []string{".flac", ".ogg"} {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "faixa"+ext)
			if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			capturados, run := capturarArgs(t, true, nil)
			w := NewWriterComRunner("ffmpeg-fake", run)

			m := Metadado{
				Title: "T", Artist: "A", Album: "AL", AlbumArtist: "A",
				TrackNumber: 5, TrackTotal: 12, DiscNumber: 2, DiscTotal: 2,
				MBRecordingID: "rec", Compilation: true,
			}
			if err := w.Escrever(context.Background(), path, m); err != nil {
				t.Fatalf("Escrever: %v", err)
			}
			args := *capturados
			if !contains(args, "TRACKNUMBER=5") || !contains(args, "TRACKTOTAL=12") {
				t.Errorf("args nao contem TRACKNUMBER/TRACKTOTAL separados: %v", args)
			}
			if !contains(args, "DISCNUMBER=2") || !contains(args, "DISCTOTAL=2") {
				t.Errorf("args nao contem DISCNUMBER/DISCTOTAL separados: %v", args)
			}
			if !contains(args, "MUSICBRAINZ_TRACKID=rec") {
				t.Errorf("args nao contem MUSICBRAINZ_TRACKID: %v", args)
			}
			if !contains(args, "COMPILATION=1") {
				t.Errorf("args nao contem COMPILATION=1: %v", args)
			}
		})
	}
}

// TestEscreverVorbisComCapaEmbutePicblock cobre a montagem do
// METADATA_BLOCK_PICTURE (nunca um 2º input de vídeo, ao contrário do mp3).
func TestEscreverVorbisComCapaEmbutePicblock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "faixa.flac")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	capa := filepath.Join(dir, "capa.jpg")
	if err := os.WriteFile(capa, []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	capturados, run := capturarArgs(t, true, nil)
	w := NewWriterComRunner("ffmpeg-fake", run)

	if err := w.Escrever(context.Background(), path, Metadado{Title: "T", CoverPath: capa}); err != nil {
		t.Fatalf("Escrever: %v", err)
	}
	args := *capturados
	achou := false
	for _, a := range args {
		if strings.HasPrefix(a, "METADATA_BLOCK_PICTURE=") {
			achou = true
		}
	}
	if !achou {
		t.Errorf("args nao contem METADATA_BLOCK_PICTURE: %v", args)
	}
	if contains(args, "-disposition:v") {
		t.Errorf("vorbis nao deveria mapear stream de video: %v", args)
	}
}

// TestEscreverFormatoNaoSuportado cobre o erro de extensão desconhecida.
func TestEscreverFormatoNaoSuportado(t *testing.T) {
	_, run := capturarArgs(t, false, nil)
	w := NewWriterComRunner("ffmpeg-fake", run)
	err := w.Escrever(context.Background(), "/tmp/x.wav", Metadado{Title: "T"})
	if err == nil {
		t.Fatal("Escrever(.wav) = nil, quero ErrFormatoNaoSuportado")
	}
	if !strings.Contains(err.Error(), "nao suportado") {
		t.Errorf("erro = %v, quero mencionar formato nao suportado", err)
	}
}

// TestEscreverFfmpegFalhaNaoRenomeiaOriginal cobre que uma falha do
// subprocesso NÃO troca o arquivo original (o rename só acontece em
// sucesso) -- o conteúdo original permanece intacto.
func TestEscreverFfmpegFalhaNaoRenomeiaOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "faixa.mp3")
	if err := os.WriteFile(path, []byte("original-intacto"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, run := capturarArgs(t, false, errFake)
	w := NewWriterComRunner("ffmpeg-fake", run)

	err := w.Escrever(context.Background(), path, Metadado{Title: "T"})
	if err == nil {
		t.Fatal("Escrever com runner falho = nil, quero erro")
	}
	conteudo, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("ler original: %v", rerr)
	}
	if string(conteudo) != "original-intacto" {
		t.Errorf("original foi alterado apos falha do ffmpeg: %q", conteudo)
	}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "fake: ffmpeg falhou" }

// TestDisponivel cobre a checagem de PATH (ffmpeg real, se presente no
// ambiente -- espelha mediainfo.Prober.Disponivel).
func TestDisponivel(t *testing.T) {
	w := NewWriter()
	quer := false
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		quer = true
	}
	if got := w.Disponivel(); got != quer {
		t.Errorf("Disponivel() = %v, quero %v (conforme PATH real)", got, quer)
	}
}

// TestNumeroETotal cobre o formato N/T do id3 (track/disc combinados).
func TestNumeroETotal(t *testing.T) {
	casos := []struct {
		n, total int
		quer     string
	}{
		{0, 0, ""},
		{3, 0, "3"},
		{3, 9, "3/9"},
		{-1, 5, ""},
	}
	for _, c := range casos {
		if got := numeroETotal(c.n, c.total); got != c.quer {
			t.Errorf("numeroETotal(%d,%d) = %q, quero %q", c.n, c.total, got, c.quer)
		}
	}
}
