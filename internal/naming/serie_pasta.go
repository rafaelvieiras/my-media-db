// Series folder: the "go up one level when the folder is a season folder" rule
// used to live only in internal/scanner (derivarSerie) and became necessary in
// internal/server as well, which needs the SERIES NAME to build the auto-match
// query (ADR-0021). It lives here, in the leaf package of name parsing, to have a
// single owner — the scanner and the server use the SAME rule, never two copies
// that can diverge.
package naming

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// reSeasonFolder recognizes the season folder (Kodi/Jellyfin/Plex):
// "Season 01", "Temporada 1", "S01". Used only to GO UP to the series folder; the
// real season of the episode comes from the file name (ParseEpisode), not from
// here.
var reSeasonFolder = regexp.MustCompile(`(?i)^(season|temporada|s)[ ._-]*\d{1,4}$`)

// reSeasonOrdinal is the ordinal form that the plain pattern above misses:
// "1ª Temporada", "2a Temporada", "3º Temporada", "1o Season". Real libraries use it, and
// without it the SEASON folder is taken for the SERIES folder — the show then
// disappears and a phantom series named "1ª Temporada" is created instead.
var reSeasonOrdinal = regexp.MustCompile(`(?i)^\d{1,3}\s*[ªº°ao]?\s*(season|temporada)$`)

// reSeasonAno is a bare year used as the season number. TVDB numbers some shows
// by year — "common with older shows, daytime soaps and talk shows" (Kodi wiki,
// Naming video files/Seasons) — and the file name carries it in SxxEyy form
// ("Looney Tunes - S1931E09.mkv"). Only 4 digits between 1900 and 2099.
var reSeasonAno = regexp.MustCompile(`^(?:19|20)\d{2}$`)

// reSeasonEspeciais is the specials folder with the WORD in front of it -- "Temporada
// Specials", "Season Extras" -- and the bare "Extras". The bare "Specials"/"Especiais"
// are checked by hand below. Without it the folder is taken for the SERIES folder and a
// phantom series named "Temporada Specials" is born (2 files measured in production).
var reSeasonEspeciais = regexp.MustCompile(`(?i)^(?:(season|temporada|s)[ ._-]*)?(specials?|especiais|extras?)$`)

// reDigitosPasta extracts the number from the end of a season folder name.
var reDigitosPasta = regexp.MustCompile(`\d{1,4}`)

// EhPastaTemporada says whether the folder name is a season folder (to go up to
// the series folder). It includes the specials (season 0).
func EhPastaTemporada(nome string) bool {
	l := strings.ToLower(strings.TrimSpace(nome))
	if l == "specials" || l == "especiais" {
		return true
	}
	if reSeasonEspeciais.MatchString(strings.TrimSpace(nome)) {
		return true
	}
	return reSeasonFolder.MatchString(nome) || reSeasonOrdinal.MatchString(strings.TrimSpace(nome)) ||
		reSeasonAno.MatchString(strings.TrimSpace(nome))
}

// PastaDaSerie returns the series folder of a file: the immediate folder or, if
// that one is a season folder, the folder ABOVE it. It has no notion of the
// source root (the caller decides what to do when the result is the root itself);
// it is used by the auto-match to derive the series title/year (ADR-0021).
func PastaDaSerie(path string) string {
	dir := filepath.Dir(path)
	if !EhPastaTemporada(filepath.Base(dir)) {
		return dir
	}
	if parent := filepath.Dir(dir); parent != "." && parent != string(filepath.Separator) {
		return parent
	}
	return dir
}

// NumeroDaPastaTemporada returns the season number that the folder name declares
// ("Season 03" -> 3, "Temporada 1" -> 1, "S02" -> 2) and, for the specials
// folders ("Specials", "Especiais"), 0. ok=false when the name is not a season
// folder. Necessary because a file without SxxEyy in the name still knows which
// season it is in, from the folder it lives in (ADR-0021).
func NumeroDaPastaTemporada(nome string) (int, bool) {
	if !EhPastaTemporada(nome) {
		return 0, false
	}
	l := strings.ToLower(strings.TrimSpace(nome))
	if l == "specials" || l == "especiais" {
		return 0, true
	}
	// The specials folder has no number of its own: it IS season 0 (the Kodi/TMDB
	// convention the whole codebase already uses for "Specials").
	if reSeasonEspeciais.MatchString(strings.TrimSpace(nome)) {
		return 0, true
	}
	digitos := reDigitosPasta.FindString(l)
	if digitos == "" {
		return 0, false
	}
	n, err := strconv.Atoi(digitos)
	if err != nil {
		return 0, false
	}
	return n, true
}
