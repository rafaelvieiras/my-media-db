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

// reDigitosPasta extracts the number from the end of a season folder name.
var reDigitosPasta = regexp.MustCompile(`\d{1,4}`)

// EhPastaTemporada says whether the folder name is a season folder (to go up to
// the series folder). It includes the specials (season 0).
func EhPastaTemporada(nome string) bool {
	l := strings.ToLower(strings.TrimSpace(nome))
	if l == "specials" || l == "especiais" {
		return true
	}
	return reSeasonFolder.MatchString(nome)
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
