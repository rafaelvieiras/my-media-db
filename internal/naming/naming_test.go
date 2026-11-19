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
			nome: "jellyfin com tvdbid",
			in:   "2 Broke Girls (2011) [tvdbid-248741]",
			want: Info{Title: "2 Broke Girls", Year: 2011, TVDBID: "248741"},
		},
		{
			nome: "kodi com {tvdb-N}",
			in:   "2 Broke Girls (2011) {tvdb-248741}",
			want: Info{Title: "2 Broke Girls", Year: 2011, TVDBID: "248741"},
		},
		{
			nome: "tvdb e tmdb juntos (tmdb tem prioridade no scanner)",
			in:   "2 Broke Girls (2011) [tmdbid-39340][tvdbid-248741]",
			want: Info{Title: "2 Broke Girls", Year: 2011, TMDBID: "39340", TVDBID: "248741"},
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
			nome: "separadores por ponto: o titulo acaba no release e o ano sai do nome",
			in:   "The.Matrix.1999.1080p.x264",
			want: Info{Title: "The Matrix", Year: 1999},
		},
		{
			// O caso real que destravou a correção: pasta com "O Filme" a mais e arquivo
			// nomeado em ponto com o release colado na resolução.
			nome: "release em ponto com cauda longa",
			in:   "101.Dálmatas.1996.WEB-DL.MAIOR.1080p.DualÁudio.AC3.H264-ACHEIOFILME.COM-trailer",
			want: Info{Title: "101 Dálmatas", Year: 1996},
		},
		{
			nome: "release sem ano nao inventa ano",
			in:   "Toy.Story.1080p.BluRay.x264",
			want: Info{Title: "Toy Story"},
		},
		{
			nome: "resolucao colada na fonte tambem corta",
			in:   "Housekeeping.87.BD1080p.MemoriadaTV.Maior",
			want: Info{Title: "Housekeeping", Year: 1987},
		},
		{
			nome: "ano do titulo (2049) nao vira ano de release",
			in:   "Blade.Runner.2049.2160p.BluRay",
			want: Info{Title: "Blade Runner 2049"},
		},
		{
			nome: "um unico campo com cara de ano continua titulo",
			in:   "2012.1080p.BluRay",
			want: Info{Title: "2012"},
		},
		{
			nome: "token ambiguo sobrevive em nome que nao e de release",
			in:   "Charlotte's Web (2006)",
			want: Info{Title: "Charlotte's Web", Year: 2006},
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
		// ── Season x Episode (Kodi/Jellyfin old style) ──
		{"cruzado 1x05", "Show 1x05", EpisodeInfo{Season: 1, Episodes: []int{5}}},
		{"cruzado minusculo 1x05", "show 1x05", EpisodeInfo{Season: 1, Episodes: []int{5}}},
		{"cruzado X maiusculo 01X01", "01X01 - The New Woody Woodpecker Show (Dublado)", EpisodeInfo{Season: 1, Episodes: []int{1}}},
		{"cruzado com release", "CC.Dub.3x01.By.Abc.Downloads", EpisodeInfo{Season: 3, Episodes: []int{1}}},
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
		// Resolution must never be read as season x episode.
		{"resolucao 1920x1080", "Show 1920x1080"},
		{"resolucao 720x480", "Show.720x480.1080p"},
		{"resolucao 3840x2160", "Filme 3840x2160 HDR"},
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

// TestNumeroEpisodioInicial covers the form where the file states only the episode
// number — the one Kodi and Jellyfin accept inside a season folder ("01 - Episode
// name.mkv", "Episódio 16 - ...", "14.mkv"). The season is NOT here: it comes from
// the folder, so the caller decides whether there is an anchor.
//
// The cases are real names from the library that this scan sent to review.
func TestNumeroEpisodioInicial(t *testing.T) {
	casos := []struct {
		nome string
		want int
		ok   bool
	}{
		{"01 - O Doutor Fanático", 1, true},
		{"01. Amor em Erupção - Volta ao Mundo em Macarrão - Pucca Ping-Pong", 1, true},
		{"07. Amor em Erupção", 7, true},
		{"Episódio 16 - O Trem do Terror", 16, true},
		{"Episódio 24 - Terror Químico", 24, true},
		{"Ep 03 - Algo", 3, true},
		{"Episode 4 - Something", 4, true},
		{"14", 14, true},
		// Negatives: the number alone is not enough to claim an episode.
		{"1080p", 0, false},
		{"1931", 0, false},        // a year, not an episode
		{"Show S01E01", 0, false}, // the SxxEyy form is handled by ParseEpisode
		{"Trailer", 0, false},
		{"", 0, false},
	}
	for _, c := range casos {
		got, ok := NumeroEpisodioInicial(c.nome)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("NumeroEpisodioInicial(%q) = (%d,%v), quero (%d,%v)", c.nome, got, ok, c.want, c.ok)
		}
	}
}

// TestSemEnfeite covers the trailing filler that separates the folder style from the
// catalogue title: the folder says "101 Dálmatas - O Filme", the source knows
// "101 Dálmatas". Only the trailing phrase goes, and only when something is left.
func TestSemEnfeite(t *testing.T) {
	casos := []struct{ in, want string }{
		{"101 Dálmatas O Filme", "101 Dálmatas"},
		{"Mortal Kombat O Filme", "Mortal Kombat"},
		{"Aladdin A Serie", "Aladdin"},
		{"Aladdin A Série", "Aladdin"},
		{"Space Jam The Movie", "Space Jam"},
		{"O Filme", "O Filme"},                       // só o enfeite: não vira título vazio
		{"Toy Story", "Toy Story"},                   // nada a remover
		{"O Filme do Meu Pai", "O Filme do Meu Pai"}, // o enfeite está no começo
	}
	for _, c := range casos {
		if got := SemEnfeite(c.in); got != c.want {
			t.Errorf("SemEnfeite(%q) = %q, quero %q", c.in, got, c.want)
		}
	}
}

// TestTituloSemSubtitulo covers the shape the Kodi convention gives a folder:
// "<Title> - <Subtitle or edition> (year)". The folder is the authoritative name of the work,
// and the part before the dash is what a source indexes.
func TestTituloSemSubtitulo(t *testing.T) {
	casos := []struct{ in, want string }{
		{"101 Dálmatas - O Filme (1996)", "101 Dálmatas"},
		{"Mortal Kombat - O Filme (1995)", "Mortal Kombat"},
		{"Dungeons & Dragons 3 - O Livro da Escuridão (2012)", "Dungeons & Dragons 3"},
		{"Spider-Man - Homecoming (2017)", "Spider-Man"}, // o hífen sem espaços NÃO separa
		{"Toy Story (1995)", ""},                         // sem separador: nada a cortar
		{"- O Filme (1996)", ""},                         // só o subtítulo: não vira título vazio
		{"Show (2011–2013) - A Série", "Show"},
	}
	for _, c := range casos {
		if got := TituloSemSubtitulo(c.in); got != c.want {
			t.Errorf("TituloSemSubtitulo(%q) = %q, quero %q", c.in, got, c.want)
		}
	}
}
