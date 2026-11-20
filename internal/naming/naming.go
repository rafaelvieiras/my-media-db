// Package naming reads folder and file names through the Kodi/Jellyfin
// convention (ADR-0008) and extracts the identity facts: title, year, embedded
// external id (`[tmdbid-N]`/`{tmdb-N}`/`tt...`), edition label and part number
// (stacking). It is a PURE READER, without I/O and without state: it never
// renames anything, it never touches the disk. It is the heart of matching and,
// being pure, the most testable part.
//
// MVP scope (ADR-0008): MOVIES (folder = movie = one Entity; the files in the
// folder are versions/editions of the SAME work). The TV hierarchy
// (series↔season↔episode) is an explicit follow-up — the design does not prevent
// it, but this parser does not handle it yet.
//
// The thresholds and regexes are conservative on purpose (tuning, not model): it
// is better to miss a label than to invent one. Extending them is safe.
package naming

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Info is the result of reading a name. Empty/zero fields mean "absent".
type Info struct {
	// Title is the clean title (normalized separators, technical tokens
	// removed). Empty when nothing useful is left (e.g.: a file whose name is
	// only "2160p") — the scanner treats that as unidentified (draft).
	Title string
	// Year is the release year in parentheses ("(2017)"); 0 if absent.
	Year int
	// TMDBID is the embedded TMDB id ("[tmdbid-335984]" or "{tmdb-335984}"); "" if absent.
	TMDBID string
	// IMDBID is the embedded IMDb id ("tt0133093", with or without brackets); "" if absent.
	IMDBID string
	// TVDBID is the embedded TVDB id ("[tvdbid-248741]" or "{tvdb-248741}"); "" if absent.
	TVDBID string
	// Edition is the edition label ("Directors Cut", "Extended"...); "" if absent.
	Edition string
	// PartNo is the part in the stacking (-cd1/-part1/-disc1); 0 if absent.
	PartNo int
}

var (
	// TMDB id, Jellyfin style ("[tmdbid-N]") and Kodi style ("{tmdb-N}").
	reTMDB = regexp.MustCompile(`(?i)[\[{]tmdb(?:id)?-(\d+)[\]}]`)
	// TVDB id, same two styles ("[tvdbid-N]"/"{tvdb-N}") -- TheTVDB is the source that
	// models the series tree, so a folder carrying it is the strongest hint there is.
	reTVDB = regexp.MustCompile(`(?i)[\[{]tvdb(?:id)?-(\d+)[\]}]`)
	// IMDb id between brackets/braces ("[imdbid-tt...]"/"{imdb-tt...}").
	reIMDBBracket = regexp.MustCompile(`(?i)[\[{]imdb(?:id)?-(tt\d+)[\]}]`)
	// "Bare" IMDb id in the name ("tt0133093").
	reIMDBBare = regexp.MustCompile(`(?i)\b(tt\d{6,9})\b`)
	// Year ONLY in parentheses (conservative: a loose "2049" in the title of
	// "Blade Runner 2049" is NOT a year). The group captures the 4 digits.
	reYearParen = regexp.MustCompile(`\(((?:19|20)\d{2})\)`)
	// Kodi-style edition: "{edition-Directors Cut}".
	reEditionKodi = regexp.MustCompile(`(?i)\{edition-([^}]+)\}`)
	// Edition by known keyword (a conservative set).
	reEditionKw = regexp.MustCompile(`(?i)\b(director'?s cut|extended(?: cut| edition)?|unrated|theatrical(?: cut)?|remastered|special edition|ultimate edition|final cut|imax)\b`)
	// Part in the stacking: -cd1 / -part 1 / -disc1 / -pt1 / -disk1.
	rePart = regexp.MustCompile(`(?i)(?:^|[-_. ])(?:cd|part|pt|disc|disk)[-_ ]?(\d{1,2})(?:$|[-_. \]])`)
	// Groups between brackets/braces/parentheses removed when cleaning the title.
	reBrackets = regexp.MustCompile(`[\[({][^\])}]*[\])}]`)
	// Collapse of whitespace.
	reSpaces = regexp.MustCompile(`\s+`)
	// Resolution glued into a longer token: the release name joins the source and the
	// resolution ("BD1080p", "TrialBD1080p", "HDTV720p"), so a plain token lookup
	// misses it.
	reResolucaoColada = regexp.MustCompile(`(?i)(?:^|[^0-9])(?:480|576|720|1080|1440|2160)p`)
	// Codec glued the same way ("H264-ACHEIOFILME").
	reCodecColado = regexp.MustCompile(`(?i)(x26[45]|h26[45]|hevc)`)
)

// anoMaximoPlausivel is the ceiling of a release year read from the NAME. It is a
// fixed number on purpose (a clock would make the parser depend on when it runs):
// anything above it is a title that looks like a year ("Blade Runner 2049").
const anoMaximoPlausivel = 2029

// tokensTecnicosAmbiguos are the technical tokens that are also ordinary TITLE words.
// Dropping them from a name that is not release-style breaks the title
// ("Charlotte's Web"), so they only go when the name is unmistakably a release name.
var tokensTecnicosAmbiguos = map[string]bool{
	"web": true, "dv": true, "dvd": true, "divx": true, "xvid": true,
}

// tokensTecnicos are technical words (resolution, source, codec, audio, HDR) that
// are not part of the title and are discarded during the cleanup. Compared in
// lowercase, token by token.
var tokensTecnicos = map[string]bool{
	// resolution
	"480p": true, "576p": true, "720p": true, "1080p": true, "1440p": true,
	"2160p": true, "4k": true, "8k": true,
	// source
	"bluray": true, "blu-ray": true, "remux": true, "webdl": true, "web-dl": true,
	"webrip": true, "web": true, "hdtv": true, "dvdrip": true, "dvd": true,
	"bdrip": true, "brrip": true, "hdrip": true,
	// video codec
	"x264": true, "x265": true, "h264": true, "h265": true, "hevc": true,
	"avc": true, "xvid": true, "divx": true,
	// audio
	"aac": true, "ac3": true, "eac3": true, "dts": true, "truehd": true,
	"flac": true, "mp3": true, "atmos": true,
	// HDR / depth
	"hdr": true, "hdr10": true, "dv": true, "dovi": true, "hlg": true,
	"10bit": true, "8bit": true,
}

// Parse reads a name (of a folder OR of a file already WITHOUT extension) and
// extracts the Info. It is idempotent and free of side effects.
func Parse(name string) Info {
	var info Info

	if m := reTMDB.FindStringSubmatch(name); m != nil {
		info.TMDBID = m[1]
	}
	if m := reTVDB.FindStringSubmatch(name); m != nil {
		info.TVDBID = m[1]
	}
	info.IMDBID = extrairIMDB(name)
	if m := reYearParen.FindStringSubmatch(name); m != nil {
		info.Year, _ = strconv.Atoi(m[1])
	}
	info.Edition = extrairEdition(name)
	if m := rePart.FindStringSubmatch(name); m != nil {
		info.PartNo, _ = strconv.Atoi(m[1])
	}
	info.Title, info.Year = extrairTitulo(name, info.Year)
	return info
}

// extrairIMDB returns the IMDb id (brackets take priority over the bare form);
// "" if absent.
func extrairIMDB(name string) string {
	if m := reIMDBBracket.FindStringSubmatch(name); m != nil {
		return strings.ToLower(m[1])
	}
	if m := reIMDBBare.FindStringSubmatch(name); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// extrairEdition returns the edition label; "" if absent. The Kodi block
// "{edition-...}" takes priority over the keyword search.
func extrairEdition(name string) string {
	if m := reEditionKodi.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := reEditionKw.FindStringSubmatch(name); m != nil {
		return normalizarEspacos(m[1])
	}
	return ""
}

// extrairTitulo isolates and cleans the title: it cuts at the "(year)" when there
// is one, removes the groups between brackets/braces/parentheses, normalizes the
// separators and discards the remaining technical tokens. It returns "" if
// nothing useful is left.
//
// RELEASE-STYLE NAMES (`The.Matrix.1999.1080p.x264`, how most of a library is named
// on disk) get the extra treatment: the title ENDS at the first release token, and
// the token glued right before it is the year. Without it the title kept the whole
// tail -- "101.Dálmatas.1996.WEB-DL.MAIOR.1080p..." became "101 Dálmatas 1996
// MAIOR DualÁudio H264-ACHEIOFILME COM-trailer", a string no source answers, and the
// year stayed 0 so the item could not even use the year to break a tie. MEASURED on a
// production library: 46 of 67 unmatched movies derived a string like that from the
// release name, all of them with year 0.
//
// The cut only happens when the name really has a release section (a technical token
// after the title) or is written release-style (`.`/`_` separators). A plain folder
// title has neither to cut: "Death Race 2000" is the TITLE, and its dots ("Dr.
// Strangelove") must survive.
func extrairTitulo(name string, anoDoParen int) (titulo string, ano int) {
	base := name
	if loc := reYearParen.FindStringIndex(name); loc != nil {
		base = name[:loc[0]]
	}
	base = reBrackets.ReplaceAllString(base, " ")
	estiloRelease := strings.ContainsAny(base, "._")
	base = strings.NewReplacer(".", " ", "_", " ").Replace(base)
	campos := strings.Fields(normalizarEspacos(base))
	ano = anoDoParen
	fronteira := cortarNaFronteiraDeRelease(campos)
	if len(fronteira) < len(campos) || estiloRelease {
		campos = fronteira
		if ano == 0 {
			campos, ano = tirarAnoDoFim(campos)
		}
	}
	return descartarTokensTecnicos(campos, estiloRelease), ano
}

// cortarNaFronteiraDeRelease truncates the fields at the FIRST release token -- the
// point where the title ends and the release metadata begins. A name with nothing
// technical keeps every field ("Dr. Strangelove" loses its dot, nothing else).
func cortarNaFronteiraDeRelease(campos []string) []string {
	for i, c := range campos {
		if i > 0 && tokenDeRelease(c) {
			return campos[:i]
		}
	}
	return campos
}

// tirarAnoDoFim takes the YEAR out of the tail of the release name -- the place the
// convention puts it (`101.Dálmatas.1996.WEB-DL`, `Housekeeping.87.BD1080p`,
// `Quinto.Elemento.1997.Trial.Bluray1080p`). The year does not have to be the VERY last
// field: noise often follows it (`…1997 Trial`, `…1993 rmz`), which is why the scan walks
// the last few fields backwards and drops everything from the year onward.
//
// It never consumes the whole title (`2012.1080p` is the movie "2012", not a year with an
// empty title) and it refuses an implausible year, which is what keeps
// "Blade.Runner.2049.2160p" intact: 2049 is the TITLE, not a release year.
func tirarAnoDoFim(campos []string) ([]string, int) {
	const janela = 3
	inicio := len(campos) - janela
	if inicio < 1 {
		inicio = 1 // sempre sobra pelo menos um campo: o título nunca fica vazio
	}
	for i := len(campos) - 1; i >= inicio; i-- {
		if ano := anoDoCampo(campos[i]); ano > 0 {
			return campos[:i], ano
		}
	}
	return campos, 0
}

// anoDoCampo reads one field as a release year: 4 digits inside the plausible range, or
// the 2-digit form older releases use (`Predador.86`). One digit is never a year
// (`Toy.Story.2`).
func anoDoCampo(campo string) int {
	limpo := strings.Trim(strings.TrimSpace(campo), "-_.[](){}")
	switch len(limpo) {
	case 4:
		if n, err := strconv.Atoi(limpo); err == nil && n >= 1900 && n <= anoMaximoPlausivel {
			return n
		}
	case 2:
		if n, err := strconv.Atoi(limpo); err == nil && n >= 30 {
			return 1900 + n
		}
	}
	return 0
}

// tokenDeRelease says whether a field marks the beginning of the release section:
// a pure technical token, a resolution or codec glued into a longer token
// ("BD1080p", "TrialBD1080p", "H264-ACHEIOFILME") or an audio marker
// ("DualÁudio", "Dublado", "Legendado").
func tokenDeRelease(campo string) bool {
	limpo := strings.ToLower(strings.Trim(campo, "-_."))
	if limpo == "" {
		return false
	}
	if tokensTecnicos[limpo] {
		// The ambiguous ones ("web", "dvd") are ordinary TITLE words far more often than
		// technical markers, so they never open a release section;
		// "Charlotte's Web (2006)" keeps its "Web".
		return !tokensTecnicosAmbiguos[limpo]
	}
	switch limpo {
	case "dualaudio", "dualáudio", "dual", "dublado", "dublada", "legendado", "legendada":
		return true
	}
	return reResolucaoColada.MatchString(limpo) || reCodecColado.MatchString(limpo)
}

// descartarTokensTecnicos removes, word by word, the purely technical tokens
// (resolution/codec/source/HDR) and returns the rest joined by spaces. For a name
// that is NOT release-style the AMBIGUOUS tokens survive, because there they are
// title words: "Charlotte's Web" would become "Charlotte's".
func descartarTokensTecnicos(campos []string, estiloRelease bool) string {
	mantidos := make([]string, 0, len(campos))
	for _, c := range campos {
		limpo := strings.Trim(c, "-_.[](){}")
		if limpo == "" {
			continue
		}
		chave := strings.ToLower(limpo)
		if tokensTecnicos[chave] && (estiloRelease || !tokensTecnicosAmbiguos[chave]) {
			continue
		}
		mantidos = append(mantidos, limpo)
	}
	return strings.TrimSpace(strings.Join(mantidos, " "))
}

// reSeparadorDeSubtitulo matches the dash that separates the TITLE from a subtitle or an
// edition tag inside a folder name: "101 Dálmatas - O Filme (1996)" -> the title is
// "101 Dálmatas". Accepts the hyphen, the en dash and the em dash, with the spaces the
// convention puts around them.
var reSeparadorDeSubtitulo = regexp.MustCompile(`\s+[-–—]\s+`)

// TituloSemSubtitulo keeps only the part of a folder name BEFORE the subtitle separator.
//
// The Kodi convention (the one every media player follows) puts the WORK in the folder name
// and there is no other place it lives -- so the folder is the authority, more than the file
// name inside it. Its shape is "<Title> - <Subtitle or edition> (year)", and sources index
// the title alone: TMDb answers zero for "101 Dálmatas O Filme" and the right movie for
// "101 Dálmatas".
//
// The input is the RAW folder name (with the year and the brackets still in it): the groups
// are dropped first, so the separator search never lands inside "(2011-2013)". It returns ""
// when the cut would leave nothing (" - O Filme" alone).
func TituloSemSubtitulo(pasta string) string {
	semGrupos := reBrackets.ReplaceAllString(pasta, " ")
	partes := reSeparadorDeSubtitulo.Split(semGrupos, 2)
	if len(partes) < 2 {
		return ""
	}
	return normalizarEspacos(partes[0])
}

// enfeitesDeTitulo are the trailing phrases a folder or release title carries and the
// source title usually does NOT ("Mortal Kombat O Filme", "Aladdin A Série", "Space Jam O
// Jogo"). Compared as a SUFFIX, so a title that merely starts with them is untouched.
var enfeitesDeTitulo = [][]string{
	{"o", "filme"}, {"the", "movie"}, {"the", "film"},
	{"a", "série"}, {"a", "serie"}, {"the", "series"},
	{"o", "especial"}, {"the", "special"},
}

// SemEnfeite drops the trailing filler from a title, giving the variant a source is
// likely to answer. It is the fix for the folder naming style that adds a genre tag after
// the title: "101 Dálmatas - O Filme" is not a title any catalogue has, while
// "101 Dálmatas" is (MEASURED: 0 results for the first, the right movie is the first result
// for the second).
//
// It returns the input unchanged when there is nothing to drop or when dropping would
// leave the title empty ("O Filme" alone is not a title with a filler).
func SemEnfeite(titulo string) string {
	campos := strings.Fields(titulo)
	if len(campos) < 2 {
		return titulo
	}
	norm := make([]string, len(campos))
	for i, c := range campos {
		norm[i] = dobrarAcento(c)
	}
	for _, enfeite := range enfeitesDeTitulo {
		if len(campos) <= len(enfeite) {
			continue
		}
		if temSufixo(norm, enfeite) {
			return strings.Join(campos[:len(campos)-len(enfeite)], " ")
		}
	}
	return titulo
}

// temSufixo tells whether the last normalized fields equal the filler phrase.
func temSufixo(norm []string, enfeite []string) bool {
	deslocamento := len(norm) - len(enfeite)
	for i, palavra := range enfeite {
		if norm[deslocamento+i] != palavra {
			return false
		}
	}
	return true
}

// dobrarAcento lowercases a word and folds the Portuguese accents onto ASCII, so the
// comparison does not depend on how the release was typed ("Série"/"Serie").
func dobrarAcento(palavra string) string {
	minuscula := strings.ToLower(strings.Trim(palavra, "-_.:!?"))
	var b strings.Builder
	b.Grow(len(minuscula))
	for _, r := range minuscula {
		if dobrada, ok := acentosDobrados[r]; ok {
			b.WriteRune(dobrada)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// acentosDobrados maps the accented letters of Portuguese onto their plain form.
var acentosDobrados = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

// normalizarEspacos collapses whitespace and trims the edges.
func normalizarEspacos(s string) string {
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// ─── TV parser (episodes) ────────────────────────────────────────────────────
//
// ParseEpisode recognizes the EPISODE name patterns of the
// Kodi/Jellyfin/Plex/Sonarr conventions and of anime fansub (absolute). It is a
// pure reader, independent from the movie Parse (which stays intact): the series
// scanner decides which one to call based on the content_type of the library.
// Conservative on purpose — when in doubt it returns (zero, false) and the item
// goes to the review queue, instead of inventing a wrong number (ADR-0021,
// §2.4).

// maxEpisodiosNoRange is the ceiling of episodes that a range/combined set in a
// single file may expand into. Ranges larger than this (e.g.: "S01E01-E99") are
// almost always a parse error (a "99" that was something else) — better to send
// it to review than to create dozens of spurious links.
const maxEpisodiosNoRange = 24

// maxEpisodioCruzado is the ceiling of a single episode number in the "1x05" and
// "<number> - title" forms. It exists to reject a resolution's second half
// ("1080") and other four-digit noise that survived the shape of the match.
const maxEpisodioCruzado = 999

// EpisodeInfo is the result of reading an episode name. Zero-value + false means
// "not recognized".
type EpisodeInfo struct {
	// Season is the season number. It may be 0 (specials) and is NOT
	// necessarily 1..N (box-set, year). It stays 0 on the absolute path.
	Season int
	// Episodes are the episodes of that season, already expanded (a range
	// "E01-E03" becomes [1,2,3]; "E01E02" becomes [1,2]). Empty on the absolute
	// path.
	Episodes []int
	// Absolute are the absolute numbers (anime), already expanded. Empty outside
	// the absolute path.
	Absolute []int
	// IsAbsolute marks that the recognition came from the absolute pattern
	// (without SxxEyy): the scanner matches against absolute_number, not against
	// (season, number).
	IsAbsolute bool
}

var (
	// SxxEyy with an explicit season, capturing the rest of the string from the
	// first Eyy onward for the extensions (hyphen ranges and glued episodes).
	// E.g.: "S01E05", "s1e5", "S01E01-E03", "S01E01E02". Group 1 is the season,
	// group 2 the first episode, group 3 the "tail" (extensions).
	//
	// The tail only accepts episode tokens TIGHTLY glued to the Eyy: "Edd"
	// (glued, Kodi "E01E02"), "-Edd" or "-dd" (hyphen range). It does NOT accept
	// a space/dot/underscore separator — otherwise an "S01E05 1080p" would drag
	// "1080" along as if it were another episode (a classic bug). Quality tokens
	// come separated by a space/dot and therefore stay out.
	// The leading class is `(?:^|[^0-9a-z])` and NOT `\b`: in RE2 the underscore IS
	// a word character, so `\bs` never matches "Show_S01E05" -- and that is how much
	// of the real-world library is named ("Aaahh.Monstros_S01E01.mkv",
	// "ALF_Ridler182_GOm_S03E01.avi"). The class keeps every case the old `\b`
	// accepted (space, dot, hyphen, brackets) and adds the underscore, while still
	// refusing a glued "ShowS01E01" and "1080p".
	reEpSeason = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])s(\d{1,4})[ ._-]?e(\d{1,4})((?:-?e\d{1,4}|-\d{1,4})*)`)
	// Tail of a range/combined set: each occurrence is an episode number (with or
	// without "E", with or without a hyphen). Used to expand "-E03"/"E02"/"-03".
	reEpTailNum = regexp.MustCompile(`(?i)e?(\d{1,4})`)
	// Anime absolute: separator " - " (space-hyphen-space) followed by 1..4
	// digits and, optionally, a range "-NNN". Conservative: it REQUIRES the
	// " - " separator so as not to be confused with the title; the
	// reAbsoluteVeto filter rejects year, resolution, codec etc. right after.
	// E.g.: "Show - 013", "Show - 013-015".
	reAbsolute = regexp.MustCompile(`(?i)(?:^|\s)-\s+(\d{1,4})(?:-(\d{1,4}))?(?:\s|$|[._\[(])`)
	// Absolute veto: if right after the number comes a token that disqualifies it
	// (the p of "1080p", "x264" is already filtered out for not matching pure
	// digits), we treat it as a non-episode. Here we catch the glued "p"/"i"
	// suffix (resolution) and "x" (codec) that would survive the \d{1,4}. Simple
	// and conservative.
	reAbsoluteVetoSuffix = regexp.MustCompile(`(?i)^\s*-\s+\d{1,4}[pikx]`)
	// "1x05" / "01X05": the season x episode form that Kodi and Jellyfin both accept
	// (the old Kodi style). The guard against a resolution is the SEASON RANGE: every
	// resolution pair — "1920x1080", "1280x720", "720x480", "3840x2160" — has its
	// first number far above 99, so a two-digit season ceiling rejects all of them
	// without a blacklist. "1920x1080" cannot even start the match: the leading class
	// forbids a digit before the season, so the engine never anchors inside a number.
	reEpCruzado = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])(\d{1,2})x(\d{1,4})(?:$|[^0-9a-z])`)
	// Labeled episode: "Episódio 16 - …", "Ep 01", "Episode 4".
	reEpRotulo = regexp.MustCompile(`(?i)^(?:epis[oó]dio|episode|ep)\s*[._-]?\s*(\d{1,4})\b`)
	// Episode number at the START of the name followed by a separator and any
	// non-digit: "01 - O Doutor Fanático", "07. Amor em Erupção", "12: The End".
	// Kodi and Jellyfin both read the number; the SEASON comes from the folder.
	reEpInicial = regexp.MustCompile(`^(\d{1,4})\s*[-–—:._)]\s*(?:\D|$)`)
	// Bare number as the whole name: "14".
	reEpSoNumero = regexp.MustCompile(`^(\d{1,4})$`)
)

// ParseEpisode reads a file name (WITHOUT extension) and returns the episodes it
// expresses. It returns (EpisodeInfo, true) when it recognizes an episode
// pattern; (zero, false) otherwise. It is idempotent and free of side effects.
//
// Attempt order: first the pattern with a season (SxxEyy and its extensions,
// which is the most specific and least ambiguous one); only if it does not match
// do we try the absolute (anime) one, which is riskier and therefore more
// restricted.
//
// Known limits (documented on purpose — doubtful cases → false, the scanner sends
// them to review instead of getting them wrong):
//   - It does not handle the "1x05" pattern (old Kodi) — it can collide with
//     resolution/aspect ratio; it stays out until there is demand.
//   - Absolute REQUIRES the " - " separator (space-hyphen-space). "Show 013"
//     (without a hyphen) is NOT recognized: a loose "013" is too ambiguous (part,
//     track, short year). We prefer review over a guess.
//   - Ranges are expanded only when increasing and within maxEpisodiosNoRange; an
//     inverted range (E05-E03) or a giant one → false.
func ParseEpisode(name string) (EpisodeInfo, bool) {
	if info, ok := parseEpisodeSeason(name); ok {
		return info, true
	}
	if info, ok := parseEpisodeCruzado(name); ok {
		return info, true
	}
	return parseEpisodeAbsolute(name)
}

// reEpSolto reads an EPISODE-only number: "E05", "EP05", "S1EP05", "T01E05". It is the shape
// of Kodi's second enumeration pattern (AdvancedSettings.cpp, "[_. -]?()e(?:p[ ._-]?)?([0-9]+)"):
// the season group is empty and the season comes from the folder.
//
// Two deliberate differences from Kodi's pattern, both to avoid the false positives that a
// loose rule produces on a library that mixes movies and episodes:
//   - the digits follow "e" directly ("e" and "ep" glued, and "ep" may carry ONE separator:
//     "EP01", "EP_01", "EP.01"), so "The 12 Days of Beavis" is not read as episode 12;
//   - the character before the "e" may not be a LETTER, so "Dome1996" is not read as episode
//     1996, while "S1EP01" (digit before the E) and "Show.E05" still are.
var reEpSolto = regexp.MustCompile(`(?i)(?:^|[^a-z])(?:ep[ ._-]?|e)(\d{1,4})(?:$|[^0-9a-z])`)

// ParseEpisodeNoCaminho resolves the numbering of a video FILE the way Kodi does (and Jellyfin
// copies: its expression list is headed "Kodi Standard Naming"): first the name alone, which is
// what states a season AND an episode; if the name states no episode, an EPISODE-only number
// ("E05", "EP05", "S1EP05", "T01E05") combined with the SEASON of the folder the file lives in.
//
// The folder half is what makes it safe: a loose episode number is only worth something when
// there is a season folder to anchor it, exactly as NumeroEpisodioInicial documents. A file
// outside a season folder returns false and goes to review instead of getting a guess.
//
// It is a SUPERSET of ParseEpisode: every name ParseEpisode understands keeps the same result
// (it is tried first), so no existing numbering changes.
func ParseEpisodeNoCaminho(caminho string) (EpisodeInfo, bool) {
	base := filepath.Base(caminho)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if ep, ok := ParseEpisode(base); ok {
		return ep, true
	}
	temporada, ok := NumeroDaPastaTemporada(filepath.Base(filepath.Dir(caminho)))
	if !ok {
		return EpisodeInfo{}, false
	}
	n, ok := NumeroEpisodioInicial(base)
	if !ok {
		return EpisodeInfo{}, false
	}
	return EpisodeInfo{Season: temporada, Episodes: []int{n}}, true
}

// parseEpisodeCruzado covers the "1x05" form (Kodi/Jellyfin). It runs after the
// SxxEyy form — which is unambiguous — and before the absolute one.
func parseEpisodeCruzado(name string) (EpisodeInfo, bool) {
	m := reEpCruzado.FindStringSubmatch(name)
	if m == nil {
		return EpisodeInfo{}, false
	}
	season, err := strconv.Atoi(m[1])
	if err != nil {
		return EpisodeInfo{}, false
	}
	episodio, err := strconv.Atoi(m[2])
	if err != nil || episodio == 0 || episodio > maxEpisodioCruzado {
		return EpisodeInfo{}, false
	}
	return EpisodeInfo{Season: season, Episodes: []int{episodio}}, true
}

// NumeroEpisodioInicial returns the episode number that a file name states WITHOUT
// the season — the form Kodi and Jellyfin accept when the file sits inside a season
// folder: "01 - O Doutor Fanático.mkv", "Episódio 16 - O Trem do Terror.mkv",
// "14.mkv".
//
// It is deliberately context-free and conservative: it does NOT read the folder (the
// caller must, and without a season folder the number is discarded, because a loose
// number has nothing to anchor it). A four-digit year and a glued "1080p" are
// rejected.
func NumeroEpisodioInicial(name string) (int, bool) {
	nome := strings.TrimSpace(name)
	expressoes := []*regexp.Regexp{reEpRotulo, reEpInicial, reEpSoNumero}
	// The glued "E05"/"EP05" form is the LAST resort and only when the name does not already
	// state a season: "Show S01E01" keeps being read by ParseEpisode ("S01E01" carries the
	// season, and "E01" inside it is the same number), so reading it here would be noise.
	if _, temTemporada := ParseEpisode(nome); !temTemporada {
		expressoes = append(expressoes, reEpSolto)
	}
	for _, re := range expressoes {
		m := re.FindStringSubmatch(nome)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n == 0 || n > maxEpisodioCruzado {
			return 0, false
		}
		// A bare year is a year, not an episode ("1931.mkv" inside a year season).
		if n >= 1900 && n <= 2099 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// parseEpisodeSeason covers SxxEyy and the multi-episode-in-one-file extensions:
// hyphen range ("S01E01-E03", "S01E01-03") and Kodi-style glued episodes
// ("S01E01E02E03").
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

	// The tail (m[3]) brings the additional episodes (range or glued ones). We
	// collect every number — the first of them, when it exists, defines whether
	// it is a range (two numbers = extremes) or a glued list (>=2 already
	// explicit).
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

// montarEpisodios decides, from the shape of the tail, whether it is a RANGE (a
// single number after a hyphen → expand from the first one up to it) or a GLUED
// LIST (already explicit episodes, "E01E02E03" style). It returns the final list,
// expanded and ordered by occurrence, or false if anything is incoherent
// (inverted range, too large, duplicate out of order).
func montarEpisodios(caudaRaw string, primeiro int, cauda []int) ([]int, bool) {
	if len(cauda) == 0 {
		return []int{primeiro}, true
	}

	// Hyphen range with a single number in the tail: "E01-E03" / "E01-03".
	if strings.Contains(caudaRaw, "-") && len(cauda) == 1 {
		fim := cauda[0]
		if fim <= primeiro || fim-primeiro+1 > maxEpisodiosNoRange {
			return nil, false // inverted, degenerate or too large → review.
		}
		out := make([]int, 0, fim-primeiro+1)
		for n := primeiro; n <= fim; n++ {
			out = append(out, n)
		}
		return out, true
	}

	// Kodi-style glued list: "E01E02E03" (or with hyphens between pairs). Each
	// number is an explicit episode; we strictly require it to be increasing so
	// as not to accept absurd sequences, and we respect the ceiling.
	out := make([]int, 0, len(cauda)+1)
	out = append(out, primeiro)
	anterior := primeiro
	for _, n := range cauda {
		if n <= anterior {
			return nil, false // non-increasing/duplicate → review.
		}
		out = append(out, n)
		anterior = n
	}
	if len(out) > maxEpisodiosNoRange {
		return nil, false
	}
	return out, true
}

// parseEpisodeAbsolute covers the anime absolute pattern ("Show - 013",
// "[Group] Show - 013", "Show - 013-015"). It is the riskiest path: it requires
// the " - " separator and vetoes technical suffixes (resolution/codec) that would
// survive the digit matching.
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

// absolutoInicial recognizes the first absolute number and applies the
// conservative vetoes (technical suffix, number zero, loose year). It returns the
// initial number and the full match (m[2] holds the optional end of the range);
// ok=false when the excerpt is not a legitimate absolute episode.
func absolutoInicial(name string) (int, []string, bool) {
	loc := reAbsolute.FindStringSubmatchIndex(name)
	if loc == nil {
		return 0, nil, false
	}
	// Veto: does the excerpt starting at the hyphen look like "- 1080p"/"- 720p"/
	// "- 264x"? Then it is not an absolute episode, it is quality/codec.
	if reAbsoluteVetoSuffix.MatchString(name[loc[0]:]) {
		return 0, nil, false
	}
	m := reAbsolute.FindStringSubmatch(name)
	ini, err := strconv.Atoi(m[1])
	if err != nil || ini == 0 {
		return 0, nil, false
	}
	// A loose year between 1900 and 2099 after " - " is almost always a year, not
	// an absolute episode — conservative, we send it to review.
	if ini >= 1900 && ini <= 2099 {
		return 0, nil, false
	}
	return ini, m, true
}

// expandirAbsoluto expands the absolute range [ini..fim] (inclusive); (nil,false)
// if the range is inverted/degenerate or goes past the maxEpisodiosNoRange
// ceiling.
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
