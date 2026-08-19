// Package mediainfo é o wrapper do ffprobe (ADR-0014): roda o binário como
// subprocesso com saída JSON e mapeia os fatos técnicos de um arquivo de vídeo
// (general/video/audio/subtitle) para o shape estável de `mediainfo_json`
// gravado em media_file (doc 06).
//
// ffprobe é dependência de RUNTIME, não de build: a presença é checada no boot;
// a ausência DEGRADA (mediainfo vazio + aviso), nunca derruba o scan. O caminho
// do binário e a função de execução são injetáveis para permitir testes sem o
// ffprobe real.
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

// Info é o shape mapeado de mediainfo, serializado em media_file.mediainfo_json.
// Listas vazias/omitidas quando o arquivo não tem aquele tipo de faixa.
type Info struct {
	General  General    `json:"general"`
	Video    []Video    `json:"video"`
	Audio    []Audio    `json:"audio"`
	Subtitle []Subtitle `json:"subtitle,omitempty"`
	// Tags são as tags de FORMATO relevantes à vertical de música (briefing
	// 0107, ADR-0027) — artista/álbum/faixa/MBID embutidos etc. Campo NOVO
	// (não troca General/Video/Audio/Subtitle, que consumidores existentes já
	// leem): nil quando o container não trouxe NENHUMA tag de format.tags
	// (ex.: a maioria dos arquivos de vídeo).
	Tags *AudioTags `json:"tags,omitempty"`
}

// AudioTags são as tags de ÁUDIO lidas de format.tags (ffprobe -show_format),
// hoje pedidas mas descartadas pelo mapeamento anterior a este briefing. O
// ffprobe expõe as tags EXATAMENTE como gravadas no arquivo, sem normalizar
// nome/case — containers diferentes usam grafias diferentes para o mesmo
// campo (ex. "ARTIST" no Vorbis Comment vs "artist" no MP4, "MusicBrainz
// Track Id" no ID3v2 vs "musicbrainz_trackid" no Vorbis vs
// "MUSICBRAINZ_TRACKID" em maiúsculas). extrairAudioTags normaliza a CHAVE na
// leitura (case-insensitive, ignorando espaços/underscores/pontuação); os
// VALORES nunca são alterados.
type AudioTags struct {
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Title       string `json:"title,omitempty"`
	// TrackNumber/DiscNumber são o número já PARSEADO antes da barra (a tag
	// pode vir "3" ou "3/12" — só o "3" importa); 0 quando ausente/não-numérico.
	TrackNumber int    `json:"track_number,omitempty"`
	DiscNumber  int    `json:"disc_number,omitempty"`
	Genre       string `json:"genre,omitempty"`
	// ISRC é o identificador auxiliar de faixa (ADR-0027 item 6). Nome de tag
	// varia por container: "TSRC" é o nome do frame ID3v2; "isrc"/"ISRC" é a
	// grafia comum em Vorbis Comment/MP4.
	ISRC string `json:"isrc,omitempty"`
	// MusicBrainzTrackID é o MBID de RECORDING embutido no arquivo — a chave
	// do fast-path de matching do scanner (briefing 0107). Nem todo tagueador
	// grava esta tag.
	MusicBrainzTrackID string `json:"musicbrainz_track_id,omitempty"`
	// MusicBrainzArtistID é o MBID do ARTISTA embutido — mais raro que o de
	// faixa (alguns tagueadores só gravam o recording MBID).
	MusicBrainzArtistID string `json:"musicbrainz_artist_id,omitempty"`
}

// General são os fatos do container (format/duração/tamanho/bitrate).
type General struct {
	Format   string  `json:"format"`
	Duration float64 `json:"duration_seconds"`
	Size     int64   `json:"size_bytes"`
	Bitrate  int64   `json:"bitrate"`
}

// Video é uma faixa de vídeo.
type Video struct {
	Codec  string `json:"codec"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// HDR é o rótulo detectável ("HDR10"/"Dolby Vision"/"HLG") ou "" (SDR/desconhecido).
	HDR string `json:"hdr,omitempty"`
}

// Audio é uma faixa de áudio.
type Audio struct {
	Codec    string `json:"codec"`
	Channels int    `json:"channels"`
	Language string `json:"language,omitempty"`
}

// Subtitle é uma faixa de legenda.
type Subtitle struct {
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"`
}

// runner executa um comando e devolve o stdout. Injetável para testes.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Prober roda o ffprobe. Zero-value inútil: use NewProber. Binary e run são
// injetáveis (NewProberComRunner) para testar sem o ffprobe real.
type Prober struct {
	binary string
	run    runner
}

// NewProber cria um Prober que chama o "ffprobe" do PATH via os/exec.
func NewProber() *Prober {
	return NewProberComRunner("ffprobe", execRunner)
}

// NewProberComRunner cria um Prober com binário e runner explícitos (testes).
func NewProberComRunner(binary string, run runner) *Prober {
	return &Prober{binary: binary, run: run}
}

// execRunner é o runner de produção: roda o comando e captura o stdout.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Disponivel diz se o binário do ffprobe está no PATH. O scanner usa isso no
// boot para logar um aviso e degradar (mediainfo vazio) em vez de falhar.
func (p *Prober) Disponivel() bool {
	if _, err := exec.LookPath(p.binary); err != nil {
		return false
	}
	return true
}

// Probe roda o ffprobe no arquivo e devolve o mediainfo_json (o shape de Info já
// serializado). Erro se o subprocesso ou o parse falhar — o chamador decide
// degradar.
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

// ── Structs cruas do ffprobe (só os campos que consumimos) ───────────────────

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

// mapear traduz a saída crua do ffprobe para o shape estável de Info.
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

// detectarHDR faz uma detecção conservadora de HDR pelas características de cor
// do stream de vídeo; "" quando SDR ou indeterminado.
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

// idioma lê a tag "language" (ISO 639) do stream, quando presente.
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

// extrairAudioTags monta AudioTags a partir do mapa cru de format.tags (briefing
// 0107). Constrói um índice de chave CANÔNICA (chaveCanonica) uma vez e resolve
// cada campo por lookupTag, com os candidatos já escritos na forma canônica —
// assim "MusicBrainz Track Id", "musicbrainz_trackid" e "MUSICBRAINZ_TRACKID"
// resolvem para o mesmo candidato "musicbrainztrackid" sem precisar listar cada
// variação de separador/case à mão.
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

// indexarTags normaliza as CHAVES do mapa cru de tags para a forma canônica
// (chaveCanonica), preservando o PRIMEIRO valor não-vazio visto para cada
// chave canônica (containers reais não costumam repetir a mesma tag sob
// grafias diferentes; se repetirem, a ordem de iteração do map é
// indeterminada, mas o caso é raro o bastante para não justificar mais
// complexidade aqui).
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

// chaveCanonica reduz uma chave de tag a minúsculas SÓ com letras/dígitos
// (remove espaço, underscore, hífen, pontuação) — a normalização que faz
// "MusicBrainz Track Id", "musicbrainz_trackid" e "MUSICBRAINZ_TRACKID"
// colidirem na mesma chave.
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

// lookupTag procura, na ordem, cada candidato (JÁ em forma canônica) no
// índice; "" se nenhum bater.
func lookupTag(idx map[string]string, candidatos ...string) string {
	for _, c := range candidatos {
		if v, ok := idx[c]; ok {
			return v
		}
	}
	return ""
}

// parseLeadingInt parseia o número antes de uma barra opcional ("3" ou
// "3/12" → 3); 0 quando vazio ou não-numérico.
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
