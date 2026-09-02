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

// ErrFormatoNaoSuportado é devolvido por Escrever quando a extensão do
// arquivo não é .mp3/.flac/.ogg (briefing 0125 §3: só esses 3 formatos).
var ErrFormatoNaoSuportado = errors.New("audiotag: formato de audio nao suportado (use mp3, flac ou ogg)")

// timeoutFfmpeg é o teto de UMA chamada de ffmpeg — um processo travado (ex.:
// arquivo corrompido) não deve prender para sempre uma cascata álbum→faixas
// (best-effort: a falha por timeout de UMA faixa não aborta as demais, ver
// internal/server/audio_tags.go).
const timeoutFfmpeg = 60 * time.Second

// runner executa um comando e devolve a saída COMBINADA (stdout+stderr) — o
// ffmpeg escreve o log de erro no stderr; capturar os dois é o que dá um
// erro útil ao chamador quando o subprocesso falha. Injetável para testes
// (mesmo padrão de internal/mediainfo.runner).
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Writer regrava a tag de áudio via ffmpeg. Zero-value inútil: use NewWriter.
// Binary e run são injetáveis (NewWriterComRunner) para testar sem o ffmpeg
// real (mesmo padrão de internal/mediainfo.Prober).
type Writer struct {
	binary string
	run    runner
}

// NewWriter cria um Writer que chama o "ffmpeg" do PATH via os/exec.
func NewWriter() *Writer {
	return NewWriterComRunner("ffmpeg", execRunner)
}

// NewWriterComRunner cria um Writer com binário e runner explícitos (testes).
func NewWriterComRunner(binary string, run runner) *Writer {
	return &Writer{binary: binary, run: run}
}

// execRunner é o runner de produção: roda o comando e captura stdout+stderr.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Disponivel diz se o binário do ffmpeg está no PATH. O chamador usa isso
// para degradar com 503 em vez de tentar e falhar por processo (mesmo padrão
// de mediainfo.Prober.Disponivel).
func (w *Writer) Disponivel() bool {
	_, err := exec.LookPath(w.binary)
	return err == nil
}

// Escrever regrava a tag de UM arquivo de áudio com o Metadado dado,
// despachando pelo FORMATO (extensão do path): ID3v2 para .mp3, Vorbis
// comment para .flac/.ogg. A escrita é sempre uma SUBSTITUIÇÃO TOTAL da tag
// anterior (-map_metadata -1: descarta qualquer tag antiga antes de aplicar
// o Metadado — inclusive uma capa preexistente, quando Metadado.CoverPath ==
// ""), nunca um merge — coerente com "o valor resolvido é a fonte de
// verdade" (briefing 0125 §6). O áudio em si NUNCA é recodificado (-c:a
// copy): só o container/tag muda, sem perda de qualidade nem custo de CPU de
// um reencode. Grava num arquivo temporário no MESMO diretório e troca por
// rename atômico — o arquivo original só é substituído se o ffmpeg terminar
// com sucesso.
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

// escreverID3 monta os args do ffmpeg para MP3 (ID3v2): quando há capa, um
// 2º input (a imagem) é mapeado como stream de vídeo MJPEG com
// -disposition:v attached_pic — o jeito real do ffmpeg gerar um frame APIC
// (validado empiricamente: ffprobe -show_streams devolve o stream de volta
// com disposition.attached_pic=1). -id3v2_version 3 fixa ID3v2.3 (mais
// compatível com players antigos que 2.4; ambos são lidos pelo ecossistema
// alvo do briefing 0125).
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

// escreverVorbis monta os args do ffmpeg para FLAC/OGG (Vorbis comment): a
// capa (quando houver) NÃO entra como input de vídeo -- o container ogg
// recusa mux de um stream mjpeg (validado empiricamente: "Unsupported codec
// id in stream 1") e mesmo o FLAC, que aceita, ganharia um stream de vídeo
// "de verdade" em vez do bloco METADATA_BLOCK_PICTURE que players esperam.
// Em vez disso, blocoCapaBase64 monta o bloco binário da spec do FLAC e o
// injeta como o VALOR de um comentário Vorbis comum (METADATA_BLOCK_PICTURE)
// -- funciona identicamente em .flac e .ogg (validado com ffprobe
// -show_streams, que decodifica o bloco de volta como um stream de vídeo
// sintético nos dois formatos).
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

// regravar completa `args` com o path de saída (um temporário no MESMO
// diretório do original), roda o ffmpeg, e substitui o arquivo original por
// rename atômico só em caso de sucesso — o original nunca fica num estado
// parcial/corrompido se o ffmpeg falhar no meio.
func (w *Writer) regravar(ctx context.Context, path string, args []string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "audiotag-*"+filepath.Ext(path))
	if err != nil {
		return fmt.Errorf("audiotag: criar arquivo temporario em %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }() // best-effort: some o temporario se o rename nao rolar

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

// truncar limita a saída do ffmpeg embutida na mensagem de erro (evita um
// erro gigante quando o ffmpeg despeja um log grande antes de falhar).
func truncar(out []byte) string {
	const limite = 2000
	if len(out) <= limite {
		return string(out)
	}
	return string(out[:limite]) + "... (truncado)"
}

// metadataID3 monta os pares -metadata para MP3 na convenção Picard/ID3v2
// (validada empiricamente contra o ffmpeg real: "title"/"artist"/
// "album_artist"/"album"/"date"/"genre"/"track"/"disc" mapeiam para os
// frames nativos TIT2/TPE1/TPE2/TALB/TYER/TCON/TRCK/TPOS; qualquer OUTRA
// chave -- os MusicBrainz IDs -- vira um frame TXXX com essa chave como
// DESCRIÇÃO, a mesma convenção que o Picard usa ("MusicBrainz Track Id" etc.)
// -- e "TSRC" bate direto no frame nativo de 4 letras do ISRC). track/disc
// usam o formato "N/T" quando o total é conhecido (TRCK/TPOS aceitam essa
// forma). Não há MBID de RELEASE distinto no nosso modelo (ADR-0027 só
// persiste release-group) -- "MusicBrainz Album Id" reusa o MESMO valor de
// release-group, documentado aqui em vez de inventar um MBID que não temos.
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
		// Best-effort: o ffmpeg não mapeia "compilation" para o frame
		// binário TCMP do iTunes -- vira TXXX:compilation (validado
		// empiricamente). Suficiente pro caso de uso (marcar visualmente/
		// via tag de texto); TCMP de verdade ficaria para outra rodada se
		// algum consumidor real do dono precisar dele especificamente.
		out = append(out, "-metadata", "compilation=1")
	}
	return out
}

// metadataVorbis monta os pares -metadata para FLAC/OGG na convenção
// Vorbis comment/Picard (TITLE/ARTIST/ALBUMARTIST/ALBUM/DATE/GENRE/
// TRACKNUMBER/TRACKTOTAL/DISCNUMBER/DISCTOTAL/MUSICBRAINZ_*/ISRC/
// COMPILATION, todas validadas empiricamente contra o ffmpeg real —
// diferente do MP3, aqui TRACKNUMBER/DISCNUMBER são campos separados de
// TRACKTOTAL/DISCTOTAL, não "N/T" — e adiciona METADATA_BLOCK_PICTURE
// quando há capa (ver blocoCapaBase64/escreverVorbis).
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

// numeroETotal formata "N" ou "N/T" (quando total > 0) para os frames
// TRCK/TPOS do ID3v2, que aceitam esse formato combinado. n <= 0 devolve ""
// (o chamador (add) não escreve o campo).
func numeroETotal(n, total int) string {
	if n <= 0 {
		return ""
	}
	if total > 0 {
		return fmt.Sprintf("%d/%d", n, total)
	}
	return strconv.Itoa(n)
}

// blocoCapaBase64 lê a imagem em coverPath e monta o bloco
// METADATA_BLOCK_PICTURE (spec do FLAC, reusada pelo Vorbis comment de
// qualquer container -- inclusive OGG, validado empiricamente) codificado em
// base64, pronto para virar o VALOR de um comentário Vorbis. O mime é
// farejado pelos magic bytes (mesmo mecanismo de
// internal/server/assets.go:lerArquivoValidado, nunca a extensão do
// arquivo).
func blocoCapaBase64(coverPath string) (string, error) {
	dados, err := os.ReadFile(coverPath) //nolint:gosec // path resolvido/validado pelo chamador (server)
	if err != nil {
		return "", fmt.Errorf("audiotag: ler capa %q: %w", coverPath, err)
	}
	mime := http.DetectContentType(dados)
	return montarBlocoImagem(mime, dados), nil
}

// montarBlocoImagem serializa o bloco binário METADATA_BLOCK_PICTURE (spec
// do FLAC, campos big-endian): tipo 3 = capa frontal (front cover, mesma
// convenção do APIC embutido no MP3); descrição vazia; largura/altura/
// profundidade de cor/nº de cores todos 0 -- a spec permite 0 para
// "desconhecido/não informado" e um leitor real decodifica a imagem pelos
// bytes dela mesma, não por esses campos (validado empiricamente: ffprobe
// devolve as dimensões REAIS mesmo com os campos declarados em 0).
func montarBlocoImagem(mime string, dados []byte) string {
	var buf bytes.Buffer
	escreverUint32(&buf, 3) // tipo: capa frontal
	escreverString32(&buf, mime)
	escreverString32(&buf, "") // descricao
	escreverUint32(&buf, 0)    // largura (desconhecida)
	escreverUint32(&buf, 0)    // altura (desconhecida)
	escreverUint32(&buf, 0)    // profundidade de cor (desconhecida)
	escreverUint32(&buf, 0)    // numero de cores (0 = nao indexado)
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
