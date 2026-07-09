package naming

import "testing"

// TestParseFilmesConvencao cobre a leitura do padrão Kodi/Jellyfin para filmes:
// título, ano, id embutido, edição e parte. É a bateria pura do coração do
// matching (sem I/O).
func TestParseFilmesConvencao(t *testing.T) {
	casos := []struct {
		nome string
		in   string
		want Info
	}{
		{
			nome: "jellyfin com tmdbid e qualidade",
			in:   "Blade Runner 2049 (2017) [tmdbid-335984] - 2160p",
			want: Info{Title: "Blade Runner 2049", Year: 2017, TMDBID: "335984"},
		},
		{
			nome: "kodi com {tmdb-N}",
			in:   "The Matrix (1999) {tmdb-603}",
			want: Info{Title: "The Matrix", Year: 1999, TMDBID: "603"},
		},
		{
			nome: "imdb pelado",
			in:   "Inception (2010) [tt1375666]",
			want: Info{Title: "Inception", Year: 2010, IMDBID: "tt1375666"},
		},
		{
			nome: "edicao por palavra-chave",
			in:   "Blade Runner (1982) - Directors Cut",
			want: Info{Title: "Blade Runner", Year: 1982, Edition: "Directors Cut"},
		},
		{
			nome: "edicao estilo kodi",
			in:   "Dune (2021) {edition-IMAX Enhanced}",
			want: Info{Title: "Dune", Year: 2021, Edition: "IMAX Enhanced"},
		},
		{
			nome: "stacking cd1",
			in:   "Kill Bill (2003) - cd1",
			want: Info{Title: "Kill Bill", Year: 2003, PartNo: 1},
		},
		{
			nome: "stacking part2",
			in:   "Nymphomaniac (2013) - part2",
			want: Info{Title: "Nymphomaniac", Year: 2013, PartNo: 2},
		},
		{
			nome: "separadores por ponto",
			in:   "The.Matrix.1999.1080p.x264",
			want: Info{Title: "The Matrix 1999"}, // sem "(ano)": 1999 fica no título
		},
		{
			nome: "so token tecnico vira titulo vazio (draft)",
			in:   "2160p",
			want: Info{Title: ""},
		},
		{
			nome: "sem nada reconhecivel",
			in:   "Uma Obra Qualquer",
			want: Info{Title: "Uma Obra Qualquer"},
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := Parse(c.in)
			if got != c.want {
				t.Errorf("Parse(%q)\n  got  = %+v\n  want = %+v", c.in, got, c.want)
			}
		})
	}
}

// TestParseIMDBBracketPrioridade garante que o id IMDb entre colchetes vence a
// forma pelada e é normalizado para minúsculas.
func TestParseIMDBBracketPrioridade(t *testing.T) {
	got := Parse("Filme (2000) [imdbid-TT0111161]")
	if got.IMDBID != "tt0111161" {
		t.Errorf("IMDBID = %q, quero tt0111161", got.IMDBID)
	}
}
