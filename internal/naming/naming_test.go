package naming

import (
	"reflect"
	"testing"
)

// TestParseFilmesConvencao covers reading the Kodi/Jellyfin pattern for movies:
// title, year, embedded id, edition and part. It is the pure battery of the heart
// of matching (no I/O).
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
			want: Info{Title: "The Matrix 1999"}, // no "(year)": 1999 stays in the title
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

// TestParseIMDBBracketPrioridade guarantees that the IMDb id between brackets wins
// over the bare form and is normalized to lowercase.
func TestParseIMDBBracketPrioridade(t *testing.T) {
	got := Parse("Filme (2000) [imdbid-TT0111161]")
	if got.IMDBID != "tt0111161" {
		t.Errorf("IMDBID = %q, quero tt0111161", got.IMDBID)
	}
}

// TestParseEpisodeReconhecidos covers, pattern by pattern, the names that MUST
// become an episode: plain SxxEyy (with case/digit variations), hyphen ranges,
// glued episodes (Kodi) and the anime absolute (with a range). It is the most
// fragile battery of the parser — an exhaustive table on purpose.
func TestParseEpisodeReconhecidos(t *testing.T) {
	casos := []struct {
		nome string
		in   string
		want EpisodeInfo
	}{
		// ── Plain SxxEyy ──
		{"S01E05 padrao", "Os Simpsons S01E05", EpisodeInfo{Season: 1, Episodes: []int{5}}},
		{"minusculo s01e05", "show s01e05", EpisodeInfo{Season: 1, Episodes: []int{5}}},
		{"um digito S1E5", "Show S1E5", EpisodeInfo{Season: 1, Episodes: []int{5}}},
		{"specials temporada 0", "Show S00E01", EpisodeInfo{Season: 0, Episodes: []int{1}}},
		{"temporada alta 3 digitos", "Show S121E07", EpisodeInfo{Season: 121, Episodes: []int{7}}},
		{"com qualidade depois nao arrasta", "Show S01E05 1080p x264", EpisodeInfo{Season: 1, Episodes: []int{5}}},
		{"separador por ponto no nome", "Show.Name.S02E10.1080p.WEB-DL", EpisodeInfo{Season: 2, Episodes: []int{10}}},
		{"separador ponto entre S e E", "Show S01.E03", EpisodeInfo{Season: 1, Episodes: []int{3}}},
		// The underscore is a word character in RE2: it used to kill the match.
		{"underscore separator", "Aaahh.Monstros_S01E01", EpisodeInfo{Season: 1, Episodes: []int{1}}},
		{"underscore with noise before", "ALF_Ridler182_GOm_S03E01", EpisodeInfo{Season: 3, Episodes: []int{1}}},
		{"underscore then release tags", "Serie_S02E10_1080p_WEB-DL", EpisodeInfo{Season: 2, Episodes: []int{10}}},
		// ── Hyphen ranges ──
		{"range E01-E03", "Show S01E01-E03", EpisodeInfo{Season: 1, Episodes: []int{1, 2, 3}}},
		{"range E01-03 sem E", "Show S01E01-03", EpisodeInfo{Season: 1, Episodes: []int{1, 2, 3}}},
		{"range minusculo e05-e06", "show s01e05-e06", EpisodeInfo{Season: 1, Episodes: []int{5, 6}}},
		// ── Glued (Kodi) ──
		{"colado E01E02", "Show S01E01E02", EpisodeInfo{Season: 1, Episodes: []int{1, 2}}},
		{"colado E01E02E03", "Show S01E01E02E03", EpisodeInfo{Season: 1, Episodes: []int{1, 2, 3}}},
		// ── Absolute (anime) ──
		{"absoluto Show - 013", "Naruto - 013", EpisodeInfo{Absolute: []int{13}, IsAbsolute: true}},
		{"absoluto com grupo", "[HorribleSubs] One Piece - 1000 [1080p]", EpisodeInfo{Absolute: []int{1000}, IsAbsolute: true}},
		{"absoluto range 013-015", "Show - 013-015", EpisodeInfo{Absolute: []int{13, 14, 15}, IsAbsolute: true}},
		{"absoluto com qualidade depois", "Bleach - 042 - 720p", EpisodeInfo{Absolute: []int{42}, IsAbsolute: true}},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, ok := ParseEpisode(c.in)
			if !ok {
				t.Fatalf("ParseEpisode(%q) = _, false; queria reconhecer", c.in)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseEpisode(%q)\n  got  = %+v\n  want = %+v", c.in, got, c.want)
			}
		})
	}
}

// TestParseEpisodeNegativos guarantees that names that are NOT an episode (movies,
// technical tokens, invalid ranges, ambiguous absolutes) return false — the
// scanner sends them to review instead of inventing a number.
func TestParseEpisodeNegativos(t *testing.T) {
	casos := []struct {
		nome string
		in   string
	}{
		{"filme jellyfin", "Blade Runner 2049 (2017) [tmdbid-335984] - 2160p"},
		{"filme kodi", "The Matrix (1999) {tmdb-603}"},
		{"filme com edicao hifen", "Blade Runner (1982) - Directors Cut"},
		{"filme stacking cd1", "Kill Bill (2003) - cd1"},
		{"so resolucao", "1080p"},
		{"pontos sem SxxEyy", "The.Matrix.1999.1080p.x264"},
		{"absoluto exige hifen espacado", "Naruto 013"},
		{"qualidade apos hifen nao e absoluto", "Show - 1080p"},
		{"codec apos hifen nao e absoluto", "Show - 264x"},
		{"ano apos hifen nao e absoluto", "Making Of - 2019"},
		{"range invertido", "Show S01E05-E03"},
		{"range gigante", "Show S01E01-E99"},
		{"absoluto zero", "Show - 000"},
		{"nada reconhecivel", "Uma Obra Qualquer"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got, ok := ParseEpisode(c.in); ok {
				t.Errorf("ParseEpisode(%q) = %+v, true; queria false (revisão)", c.in, got)
			}
		})
	}
}
