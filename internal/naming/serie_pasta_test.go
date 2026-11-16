package naming

import (
	"path/filepath"
	"testing"
)

// TestEhPastaTemporada covers the season folder recognitions (including the
// specials/season 0) and the rejection of names that are the series itself. It
// came from internal/scanner together with the rule (ADR-0021).
func TestEhPastaTemporada(t *testing.T) {
	for _, s := range []string{"Season 01", "Temporada 1", "S01", "specials", "Especiais"} {
		if !EhPastaTemporada(s) {
			t.Errorf("%q deveria ser pasta de temporada", s)
		}
	}
	for _, s := range []string{"Springfield Tales", "extras", ""} {
		if EhPastaTemporada(s) {
			t.Errorf("%q NAO deveria ser pasta de temporada", s)
		}
	}
}

// TestPastaDaSerie covers the derivation of the series folder from the file: it
// goes up one level in the season folders and stays in the immediate one
// otherwise.
func TestPastaDaSerie(t *testing.T) {
	casos := []struct{ arquivo, pasta string }{
		{"/m/Series Infantis/Os Gárgulas (1994)/Season 03/Os gárgulas - S03E08 - Genesis Undone.mkv",
			"/m/Series Infantis/Os Gárgulas (1994)"},
		{"/m/Series/Chuck (2007)/Temporada 1/Chuck - S01E01.mkv", "/m/Series/Chuck (2007)"},
		{"/m/Series/Chuck (2007)/S1/Chuck - S01E01.mkv", "/m/Series/Chuck (2007)"},
		{"/m/Series/Chuck (2007)/Specials/Chuck - S00E01.mkv", "/m/Series/Chuck (2007)"},
		// Folder that is not a season folder: the series is the folder of the file itself.
		{"/m/Series/Star Wars - The Clone Wars (2008)/Star Wars - S04E14.mkv",
			"/m/Series/Star Wars - The Clone Wars (2008)"},
	}
	for _, c := range casos {
		if got := PastaDaSerie(c.arquivo); got != c.pasta {
			t.Errorf("PastaDaSerie(%q) = %q, quero %q", c.arquivo, got, c.pasta)
		}
	}
}

// TestPastaDaSerieAlimentaBusca closes the loop (ADR-0021): the title extracted
// from the series FOLDER is what the external search needs — never the episode
// file name, which carries the SxxEyy and the episode title.
func TestPastaDaSerieAlimentaBusca(t *testing.T) {
	casos := []struct{ arquivo, titulo string }{
		{"/m/Series Infantis/Os Gárgulas (1994)/Season 03/Os gárgulas - S03E08 - Genesis Undone.mkv", "Os Gárgulas"},
		{"/m/Series Infantis/Star Wars - The Clone Wars (2008)/Season 04/Star Wars A Guerra dos Clones - S04E14 - Amigo em perigo.mkv", "Star Wars The Clone Wars"},
	}
	for _, c := range casos {
		info := Parse(filepath.Base(PastaDaSerie(c.arquivo)))
		if info.Title != c.titulo {
			t.Errorf("título da pasta de %q = %q, quero %q", c.arquivo, info.Title, c.titulo)
		}
	}
}
