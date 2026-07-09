// Package naming lê nomes de pasta e de arquivo pela convenção Kodi/Jellyfin
// (ADR-0008) e extrai os fatos de identidade: título, ano, id externo embutido
// (`[tmdbid-N]`/`{tmdb-N}`/`tt...`), rótulo de edição e número de parte
// (stacking). É um LEITOR puro, sem I/O e sem estado: nunca renomeia nada, nunca
// toca o disco. É o coração do matching e, por ser puro, o mais testável.
//
// Escopo do MVP (briefing 0009): FILMES (pasta = filme = uma Entity; os arquivos
// da pasta são versões/edições da MESMA obra). A hierarquia de TV
// (série↔temporada↔episódio) é follow-up explícito — o design não a impede, mas
// este parser ainda não a trata.
//
// Os limiares e regex são conservadores de propósito (tuning, não modelo): é
// melhor deixar de reconhecer um rótulo do que inventar um. Ampliar é seguro.
package naming

import (
	"regexp"
	"strconv"
	"strings"
)

// Info é o resultado de ler um nome. Campos vazios/zero significam "ausente".
type Info struct {
	// Title é o título limpo (separadores normalizados, tokens técnicos
	// removidos). Vazio quando não sobra nada de útil (ex.: um arquivo cujo
	// nome é só "2160p") — o scanner trata isso como não-identificado (draft).
	Title string
	// Year é o ano de lançamento entre parênteses ("(2017)"); 0 se ausente.
	Year int
	// TMDBID é o id TMDB embutido ("[tmdbid-335984]" ou "{tmdb-335984}"); "" se ausente.
	TMDBID string
	// IMDBID é o id IMDb embutido ("tt0133093", com ou sem colchetes); "" se ausente.
	IMDBID string
	// Edition é o rótulo de edição ("Directors Cut", "Extended"...); "" se ausente.
	Edition string
	// PartNo é a parte no stacking (-cd1/-part1/-disc1); 0 se ausente.
	PartNo int
}

var (
	// Id TMDB estilo Jellyfin ("[tmdbid-N]") e Kodi ("{tmdb-N}").
	reTMDB = regexp.MustCompile(`(?i)[\[{]tmdb(?:id)?-(\d+)[\]}]`)
	// Id IMDb entre colchetes/chaves ("[imdbid-tt...]"/"{imdb-tt...}").
	reIMDBBracket = regexp.MustCompile(`(?i)[\[{]imdb(?:id)?-(tt\d+)[\]}]`)
	// Id IMDb "pelado" no nome ("tt0133093").
	reIMDBBare = regexp.MustCompile(`(?i)\b(tt\d{6,9})\b`)
	// Ano SÓ entre parênteses (conservador: um "2049" solto no título de
	// "Blade Runner 2049" NÃO é ano). O grupo captura os 4 dígitos.
	reYearParen = regexp.MustCompile(`\(((?:19|20)\d{2})\)`)
	// Edição estilo Kodi: "{edition-Directors Cut}".
	reEditionKodi = regexp.MustCompile(`(?i)\{edition-([^}]+)\}`)
	// Edição por palavra-chave conhecida (conjunto conservador).
	reEditionKw = regexp.MustCompile(`(?i)\b(director'?s cut|extended(?: cut| edition)?|unrated|theatrical(?: cut)?|remastered|special edition|ultimate edition|final cut|imax)\b`)
	// Parte no stacking: -cd1 / -part 1 / -disc1 / -pt1 / -disk1.
	rePart = regexp.MustCompile(`(?i)(?:^|[-_. ])(?:cd|part|pt|disc|disk)[-_ ]?(\d{1,2})(?:$|[-_. \]])`)
	// Grupos entre colchetes/chaves/parênteses removidos ao limpar o título.
	reBrackets = regexp.MustCompile(`[\[({][^\])}]*[\])}]`)
	// Colapso de espaços em branco.
	reSpaces = regexp.MustCompile(`\s+`)
)

// tokensTecnicos são palavras técnicas (resolução, fonte, codec, áudio, HDR) que
// não fazem parte do título e são descartadas na limpeza. Comparadas em
// minúsculas, token a token.
var tokensTecnicos = map[string]bool{
	// resolução
	"480p": true, "576p": true, "720p": true, "1080p": true, "1440p": true,
	"2160p": true, "4k": true, "8k": true,
	// fonte
	"bluray": true, "blu-ray": true, "remux": true, "webdl": true, "web-dl": true,
	"webrip": true, "web": true, "hdtv": true, "dvdrip": true, "dvd": true,
	"bdrip": true, "brrip": true, "hdrip": true,
	// codec de vídeo
	"x264": true, "x265": true, "h264": true, "h265": true, "hevc": true,
	"avc": true, "xvid": true, "divx": true,
	// áudio
	"aac": true, "ac3": true, "eac3": true, "dts": true, "truehd": true,
	"flac": true, "mp3": true, "atmos": true,
	// HDR / profundidade
	"hdr": true, "hdr10": true, "dv": true, "dovi": true, "hlg": true,
	"10bit": true, "8bit": true,
}

// Parse lê um nome (de pasta OU de arquivo já SEM extensão) e extrai a Info. É
// idempotente e livre de efeitos colaterais.
func Parse(name string) Info {
	var info Info

	if m := reTMDB.FindStringSubmatch(name); m != nil {
		info.TMDBID = m[1]
	}
	info.IMDBID = extrairIMDB(name)
	if m := reYearParen.FindStringSubmatch(name); m != nil {
		info.Year, _ = strconv.Atoi(m[1])
	}
	info.Edition = extrairEdition(name)
	if m := rePart.FindStringSubmatch(name); m != nil {
		info.PartNo, _ = strconv.Atoi(m[1])
	}
	info.Title = extrairTitulo(name)
	return info
}

// extrairIMDB devolve o id IMDb (colchetes têm prioridade sobre a forma pelada);
// "" se ausente.
func extrairIMDB(name string) string {
	if m := reIMDBBracket.FindStringSubmatch(name); m != nil {
		return strings.ToLower(m[1])
	}
	if m := reIMDBBare.FindStringSubmatch(name); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// extrairEdition devolve o rótulo de edição; "" se ausente. O bloco Kodi
// "{edition-...}" tem prioridade sobre a busca por palavra-chave.
func extrairEdition(name string) string {
	if m := reEditionKodi.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := reEditionKw.FindStringSubmatch(name); m != nil {
		return normalizarEspacos(m[1])
	}
	return ""
}

// extrairTitulo isola e limpa o título: corta no "(ano)" quando existe, remove
// os grupos entre colchetes/chaves/parênteses, normaliza separadores e descarta
// os tokens técnicos remanescentes. Devolve "" se nada útil sobrar.
func extrairTitulo(name string) string {
	base := name
	if loc := reYearParen.FindStringIndex(name); loc != nil {
		base = name[:loc[0]]
	}
	base = reBrackets.ReplaceAllString(base, " ")
	base = strings.NewReplacer(".", " ", "_", " ").Replace(base)
	base = normalizarEspacos(base)
	return descartarTokensTecnicos(base)
}

// descartarTokensTecnicos remove, palavra a palavra, os tokens puramente
// técnicos (resolução/codec/fonte/HDR) e devolve o restante colado por espaços.
func descartarTokensTecnicos(s string) string {
	campos := strings.Fields(s)
	mantidos := make([]string, 0, len(campos))
	for _, c := range campos {
		limpo := strings.Trim(c, "-_.")
		if limpo == "" || tokensTecnicos[strings.ToLower(limpo)] {
			continue
		}
		mantidos = append(mantidos, limpo)
	}
	return strings.TrimSpace(strings.Join(mantidos, " "))
}

// normalizarEspacos colapsa espaços e apara as pontas.
func normalizarEspacos(s string) string {
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}
