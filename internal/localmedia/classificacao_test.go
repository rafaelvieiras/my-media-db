package localmedia

import "testing"

// TestEhArquivoGerenciadoCasosConhecidos covers the acceptance criteria of the
// artwork classification (see doc 08, §5): canonical names are recognized as
// managed; junk/a foreign file is not.
func TestEhArquivoGerenciadoCasosConhecidos(t *testing.T) {
	videos := []string{"Matrix.1999"}

	casos := []struct {
		nome  string
		quero bool
	}{
		{"poster.jpg", true},
		{"poster.png", true},
		{"fanart.jpg", true},
		{"fanart1.jpg", true},
		{"fanart23.jpg", true},
		{"banner.jpg", true},
		{"clearlogo.png", true},
		{"discart.png", true},
		{"landscape.jpg", true},
		{"tvshow.nfo", true},
		{"Matrix.1999.nfo", true},
		{"Matrix.1999-poster.jpg", true},
		{"Matrix.1999-poster2.jpg", true},
		{"Matrix.1999-thumb.jpg", true},
		{"season01-poster.jpg", true},
		{"season01-poster1.jpg", true},
		{"season12-poster.png", true},

		// Not identified: junk, a loose subtitle, an nfo of another tool whose
		// name does not match any video in the folder, an unknown extension.
		{"sample.txt", false},
		{"Legenda.Portugues.srt", false},
		{"OutroFilme.nfo", false},
		{"posterinho.jpg", false},
		{"posterX.jpg", false},
		{"random.jpg", false},
		{"season1-poster.jpg", false}, // NN must have 2 digits
		{"seasonAB-poster.jpg", false},
	}
	for _, c := range casos {
		got := EhArquivoGerenciado(c.nome, videos)
		if got != c.quero {
			t.Errorf("EhArquivoGerenciado(%q) = %v, quero %v", c.nome, got, c.quero)
		}
	}
}

// TestEhArquivoGerenciadoSemVideoNaPasta proves that the BARE names (artwork
// of an exclusive folder, tvshow.nfo, season poster) are still recognized even
// with no video in the list (a season/series-only folder, with no video of its
// own).
func TestEhArquivoGerenciadoSemVideoNaPasta(t *testing.T) {
	if !EhArquivoGerenciado("poster.jpg", nil) {
		t.Error("poster.jpg bare deveria ser gerenciado mesmo sem vídeos na pasta")
	}
	if !EhArquivoGerenciado("tvshow.nfo", nil) {
		t.Error("tvshow.nfo deveria ser gerenciado mesmo sem vídeos na pasta")
	}
	if EhArquivoGerenciado("Matrix.1999.nfo", nil) {
		t.Error("<video>.nfo não deveria casar sem o vídeo correspondente na lista")
	}
}
