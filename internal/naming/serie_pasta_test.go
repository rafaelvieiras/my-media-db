package naming

import (
	"path/filepath"
	"testing"
)

// TestEhPastaTemporada cobre os reconhecimentos de pasta de temporada (incluindo
// os specials/temporada 0) e a rejeição de nomes que são a própria série. Veio
// de internal/scanner junto com a regra (briefing 0129).
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

// TestPastaDaSerie cobre a derivação da pasta da série a partir do arquivo:
// sobe um nível nas pastas de temporada, fica na imediata nos outros casos.
func TestPastaDaSerie(t *testing.T) {
	casos := []struct{ arquivo, pasta string }{
		{"/m/Series Infantis/Os Gárgulas (1994)/Season 03/Os gárgulas - S03E08 - Genesis Undone.mkv",
			"/m/Series Infantis/Os Gárgulas (1994)"},
		{"/m/Series/Chuck (2007)/Temporada 1/Chuck - S01E01.mkv", "/m/Series/Chuck (2007)"},
		{"/m/Series/Chuck (2007)/S1/Chuck - S01E01.mkv", "/m/Series/Chuck (2007)"},
		{"/m/Series/Chuck (2007)/Specials/Chuck - S00E01.mkv", "/m/Series/Chuck (2007)"},
		// Pasta que não é de temporada: a série é a própria pasta do arquivo.
		{"/m/Series/Star Wars - The Clone Wars (2008)/Star Wars - S04E14.mkv",
			"/m/Series/Star Wars - The Clone Wars (2008)"},
	}
	for _, c := range casos {
		if got := PastaDaSerie(c.arquivo); got != c.pasta {
			t.Errorf("PastaDaSerie(%q) = %q, quero %q", c.arquivo, got, c.pasta)
		}
	}
}

// TestPastaDaSerieAlimentaBusca fecha o laço do briefing 0129: o título extraído
// da PASTA da série é o que a busca externa precisa — nunca o nome do arquivo de
// episódio, que carrega SxxEyy e o título do episódio.
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
