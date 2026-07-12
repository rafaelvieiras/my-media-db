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

// ─── Parser de TV (episódios) ────────────────────────────────────────────────
//
// ParseEpisode reconhece os padrões de nome de EPISÓDIO das convenções
// Kodi/Jellyfin/Plex/Sonarr e do fansub de anime (absoluto). É um leitor puro,
// independente do Parse de filme (que fica intacto): o scanner de séries decide
// qual chamar pelo content_type da biblioteca. Conservador de propósito — na
// dúvida devolve (zero, false) e o item vai para a fila de revisão, em vez de
// inventar um número errado (briefing 0034 §2.4).

// maxEpisodiosNoRange é o teto de episódios que um range/combinado num único
// arquivo pode expandir. Ranges maiores que isto (ex.: "S01E01-E99") são quase
// sempre erro de parse (um "99" que era outra coisa) — melhor mandar para
// revisão do que criar dezenas de vínculos espúrios.
const maxEpisodiosNoRange = 24

// EpisodeInfo é o resultado de ler um nome de episódio. Zero-value + false
// significa "não reconhecido".
type EpisodeInfo struct {
	// Season é o número da temporada. Pode ser 0 (specials) e NÃO é
	// necessariamente 1..N (box-set, ano). Fica 0 no caminho absoluto.
	Season int
	// Episodes são os episódios daquela temporada, já expandidos (um range
	// "E01-E03" vira [1,2,3]; "E01E02" vira [1,2]). Vazio no caminho absoluto.
	Episodes []int
	// Absolute são os números absolutos (anime), já expandidos. Vazio fora do
	// caminho absoluto.
	Absolute []int
	// IsAbsolute marca que o reconhecimento veio do padrão absoluto (sem SxxEyy):
	// o scanner casa contra o absolute_number, não contra (season, number).
	IsAbsolute bool
}

var (
	// SxxEyy com temporada explícita, capturando o restante da string a partir
	// do primeiro Eyy para as extensões (ranges com hífen e episódios colados).
	// Ex.: "S01E05", "s1e5", "S01E01-E03", "S01E01E02". O grupo 1 é a temporada,
	// o grupo 2 o primeiro episódio, o grupo 3 a "cauda" (extensões).
	//
	// A cauda só aceita tokens de episódio ESTREITAMENTE colados ao Eyy: "Edd"
	// (colado, Kodi "E01E02"), "-Edd" ou "-dd" (range com hífen). NÃO aceita
	// separador de espaço/ponto/underscore — senão um "S01E05 1080p" arrastaria
	// "1080" como se fosse outro episódio (bug clássico). Tokens de qualidade
	// vêm separados por espaço/ponto e por isso ficam de fora.
	reEpSeason = regexp.MustCompile(`(?i)\bs(\d{1,4})[ ._-]?e(\d{1,4})((?:-?e\d{1,4}|-\d{1,4})*)`)
	// Cauda de range/combinado: cada ocorrência é um número de episódio (com ou
	// sem "E", com ou sem hífen). Usada para expandir "-E03"/"E02"/"-03".
	reEpTailNum = regexp.MustCompile(`(?i)e?(\d{1,4})`)
	// Absoluto de anime: separador " - " (espaço-hífen-espaço) seguido de 1..4
	// dígitos e, opcionalmente, um range "-NNN". Conservador: EXIGE o separador
	// " - " para não confundir com título; o filtro reAbsoluteVeto rejeita ano,
	// resolução, codec etc. logo depois. Ex.: "Show - 013", "Show - 013-015".
	reAbsolute = regexp.MustCompile(`(?i)(?:^|\s)-\s+(\d{1,4})(?:-(\d{1,4}))?(?:\s|$|[._\[(])`)
	// Veto do absoluto: se logo após o número vier um token que o desqualifica
	// (p de "1080p", "x264" já é filtrado por não casar dígitos puros), tratamos
	// como não-episódio. Aqui pegamos o sufixo "p"/"i" colado (resolução) e "x"
	// (codec) que sobreviveriam ao \d{1,4}. Simples e conservador.
	reAbsoluteVetoSuffix = regexp.MustCompile(`(?i)^\s*-\s+\d{1,4}[pikx]`)
)

// ParseEpisode lê um nome de arquivo (SEM extensão) e devolve os episódios que
// ele expressa. Retorna (EpisodeInfo, true) quando reconhece um padrão de
// episódio; (zero, false) caso contrário. É idempotente e sem efeitos colaterais.
//
// Ordem de tentativa: primeiro o padrão com temporada (SxxEyy e suas extensões,
// que é o mais específico e menos ambíguo); só se ele não casar tentamos o
// absoluto (anime), que é mais arriscado e por isso mais restrito.
//
// Limites conhecidos (documentados de propósito — casos duvidosos → false, o
// scanner manda para revisão em vez de errar):
//   - Não trata o padrão "1x05" (Kodi antigo) — pode colidir com resolução/relação
//     de aspecto; fica de fora até haver demanda.
//   - Absoluto EXIGE o separador " - " (espaço-hífen-espaço). "Show 013" (sem
//     hífen) NÃO é reconhecido: "013" solto é ambíguo demais (parte, faixa, ano
//     curto). Preferimos revisão a um palpite.
//   - Ranges são expandidos só quando crescentes e dentro de maxEpisodiosNoRange;
//     range invertido (E05-E03) ou gigante → false.
func ParseEpisode(name string) (EpisodeInfo, bool) {
	if info, ok := parseEpisodeSeason(name); ok {
		return info, true
	}
	return parseEpisodeAbsolute(name)
}

// parseEpisodeSeason cobre SxxEyy e as extensões de múltiplos episódios num
// arquivo: range com hífen ("S01E01-E03", "S01E01-03") e episódios colados
// estilo Kodi ("S01E01E02E03").
func parseEpisodeSeason(name string) (EpisodeInfo, bool) {
	m := reEpSeason.FindStringSubmatch(name)
	if m == nil {
		return EpisodeInfo{}, false
	}
	season, err := strconv.Atoi(m[1])
	if err != nil {
		return EpisodeInfo{}, false
	}
	primeiro, err := strconv.Atoi(m[2])
	if err != nil {
		return EpisodeInfo{}, false
	}

	// A cauda (m[3]) traz os episódios adicionais (range ou colados). Coletamos
	// todos os números — o primeiro deles, quando existe, define se é range
	// (dois números = extremos) ou lista colada (>=2 já explícitos).
	var cauda []int
	for _, t := range reEpTailNum.FindAllStringSubmatch(m[3], -1) {
		n, err := strconv.Atoi(t[1])
		if err != nil {
			return EpisodeInfo{}, false
		}
		cauda = append(cauda, n)
	}

	episodios, ok := montarEpisodios(m[3], primeiro, cauda)
	if !ok {
		return EpisodeInfo{}, false
	}
	return EpisodeInfo{Season: season, Episodes: episodios}, true
}

// montarEpisodios decide, a partir da forma da cauda, se ela é um RANGE (um
// único número após um hífen → expandir do primeiro até ele) ou uma LISTA colada
// (episódios já explícitos, estilo "E01E02E03"). Devolve a lista final expandida
// e ordenada por ocorrência, ou false se algo for incoerente (range invertido,
// grande demais, duplicado fora de ordem).
func montarEpisodios(caudaRaw string, primeiro int, cauda []int) ([]int, bool) {
	if len(cauda) == 0 {
		return []int{primeiro}, true
	}

	// Range com hífen e um único número na cauda: "E01-E03" / "E01-03".
	if strings.Contains(caudaRaw, "-") && len(cauda) == 1 {
		fim := cauda[0]
		if fim <= primeiro || fim-primeiro+1 > maxEpisodiosNoRange {
			return nil, false // invertido, degenerado ou grande demais → revisão.
		}
		out := make([]int, 0, fim-primeiro+1)
		for n := primeiro; n <= fim; n++ {
			out = append(out, n)
		}
		return out, true
	}

	// Lista colada estilo Kodi: "E01E02E03" (ou com hífens entre pares). Cada
	// número é um episódio explícito; exigimos estritamente crescente para não
	// aceitar sequências absurdas, e respeitamos o teto.
	out := make([]int, 0, len(cauda)+1)
	out = append(out, primeiro)
	anterior := primeiro
	for _, n := range cauda {
		if n <= anterior {
			return nil, false // não-crescente/duplicado → revisão.
		}
		out = append(out, n)
		anterior = n
	}
	if len(out) > maxEpisodiosNoRange {
		return nil, false
	}
	return out, true
}

// parseEpisodeAbsolute cobre o padrão absoluto de anime ("Show - 013",
// "[Group] Show - 013", "Show - 013-015"). É o caminho mais arriscado: exige o
// separador " - " e veta sufixos técnicos (resolução/codec) que sobreviveriam ao
// casamento de dígitos.
func parseEpisodeAbsolute(name string) (EpisodeInfo, bool) {
	ini, m, ok := absolutoInicial(name)
	if !ok {
		return EpisodeInfo{}, false
	}
	if m[2] == "" {
		return EpisodeInfo{Absolute: []int{ini}, IsAbsolute: true}, true
	}
	fim, err := strconv.Atoi(m[2])
	if err != nil {
		return EpisodeInfo{}, false
	}
	out, ok := expandirAbsoluto(ini, fim)
	if !ok {
		return EpisodeInfo{}, false
	}
	return EpisodeInfo{Absolute: out, IsAbsolute: true}, true
}

// absolutoInicial reconhece o primeiro número absoluto e aplica os vetos
// conservadores (sufixo técnico, número zero, ano solto). Devolve o número
// inicial e o match completo (m[2] guarda o fim opcional do range); ok=false
// quando o trecho não é um episódio absoluto legítimo.
func absolutoInicial(name string) (int, []string, bool) {
	loc := reAbsolute.FindStringSubmatchIndex(name)
	if loc == nil {
		return 0, nil, false
	}
	// Veto: o trecho a partir do hífen parece "- 1080p"/"- 720p"/"- 264x"? Então
	// não é episódio absoluto, é qualidade/codec.
	if reAbsoluteVetoSuffix.MatchString(name[loc[0]:]) {
		return 0, nil, false
	}
	m := reAbsolute.FindStringSubmatch(name)
	ini, err := strconv.Atoi(m[1])
	if err != nil || ini == 0 {
		return 0, nil, false
	}
	// Ano entre 1900 e 2099 solto após " - " é quase sempre ano, não episódio
	// absoluto — conservador, mandamos para revisão.
	if ini >= 1900 && ini <= 2099 {
		return 0, nil, false
	}
	return ini, m, true
}

// expandirAbsoluto expande o range absoluto [ini..fim] (inclusive); (nil,false)
// se o range for invertido/degenerado ou passar do teto maxEpisodiosNoRange.
func expandirAbsoluto(ini, fim int) ([]int, bool) {
	if fim <= ini || fim-ini+1 > maxEpisodiosNoRange {
		return nil, false
	}
	out := make([]int, 0, fim-ini+1)
	for n := ini; n <= fim; n++ {
		out = append(out, n)
	}
	return out, true
}
