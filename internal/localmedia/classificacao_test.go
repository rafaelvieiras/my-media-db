package localmedia

import "testing"

// TestEhArquivoGerenciadoCasosConhecidos cobre os critérios de aceite do
// briefing 0063 (§5): nomes canônicos do 0062 são reconhecidos como
// gerenciados; lixo/arquivo estranho não.
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

		// Não identificado: lixo, legenda solta, nfo de outra ferramenta com
		// nome que não bate com nenhum vídeo da pasta, extensão desconhecida.
		{"sample.txt", false},
		{"Legenda.Portugues.srt", false},
		{"OutroFilme.nfo", false},
		{"posterinho.jpg", false},
		{"posterX.jpg", false},
		{"random.jpg", false},
		{"season1-poster.jpg", false}, // NN precisa ter 2 dígitos
		{"seasonAB-poster.jpg", false},
	}
	for _, c := range casos {
		got := EhArquivoGerenciado(c.nome, videos)
		if got != c.quero {
			t.Errorf("EhArquivoGerenciado(%q) = %v, quero %v", c.nome, got, c.quero)
		}
	}
}

// TestEhArquivoGerenciadoSemVideoNaPasta prova que os nomes BARE (arte de
// pasta exclusiva, tvshow.nfo, season poster) continuam reconhecidos mesmo
// sem nenhum vídeo na lista (pasta só de temporada/série, sem vídeo próprio).
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
