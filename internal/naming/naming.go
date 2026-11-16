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
	// Edition is the edition label ("Directors Cut", "Extended"...); "" if absent.
	Edition string
	// PartNo is the part in the stacking (-cd1/-part1/-disc1); 0 if absent.
	PartNo int
}

var (
	// TMDB id, Jellyfin style ("[tmdbid-N]") and Kodi style ("{tmdb-N}").
	reTMDB = regexp.MustCompile(`(?i)[\[{]tmdb(?:id)?-(\d+)[\]}]`)
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
)

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

// descartarTokensTecnicos removes, word by word, the purely technical tokens
// (resolution/codec/source/HDR) and returns the rest joined by spaces.
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
	reEpSeason = regexp.MustCompile(`(?i)\bs(\d{1,4})[ ._-]?e(\d{1,4})((?:-?e\d{1,4}|-\d{1,4})*)`)
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
	return parseEpisodeAbsolute(name)
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
