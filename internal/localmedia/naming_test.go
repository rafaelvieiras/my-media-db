package localmedia

import (
	"os"
	"testing"
)

// TestNomeArquivoArteBare: EXCLUSIVE folder (1 single video) + main artwork ->
// bare name (doc 08, §5, the "poster.jpg in the same directory" criterion).
func TestNomeArquivoArteBare(t *testing.T) {
	nome := NomeArquivoArte(KindPoster, true, 0, true, "Matrix.1999", "")
	if nome != "poster.jpg" {
		t.Fatalf("nome = %q, quero %q", nome, "poster.jpg")
	}
}

// TestNomeArquivoArtePrefixado: NON-exclusive folder (2+ videos) -> name
// prefixed by the video (doc 08, §5, "<video-name>-poster.jpg").
func TestNomeArquivoArtePrefixado(t *testing.T) {
	nome := NomeArquivoArte(KindPoster, true, 0, false, "Matrix.1999.EXTENDED", "")
	if nome != "Matrix.1999.EXTENDED-poster.jpg" {
		t.Fatalf("nome = %q, quero %q", nome, "Matrix.1999.EXTENDED-poster.jpg")
	}
}

// TestNomeArquivoArteRotacaoCrescente: marking the main artwork 3x produces
// increasing suffixes WITHOUT collision or reindexing (doc 08, §5/§6).
func TestNomeArquivoArteRotacaoCrescente(t *testing.T) {
	// 1st rotation: only the old main artwork (bare) exists -> it becomes index 1.
	i1 := IndiceRotacaoLivre(nil, KindPoster, true, "", "")
	if i1 != 1 {
		t.Fatalf("1a rotacao: indice = %d, quero 1", i1)
	}
	nomeRotacionado1 := NomeArquivoArte(KindPoster, false, i1, true, "", "")
	if nomeRotacionado1 != "poster1.jpg" {
		t.Fatalf("nome rotacionado 1 = %q, quero %q", nomeRotacionado1, "poster1.jpg")
	}

	// 2nd rotation: poster1.jpg already exists (the previous main artwork, now
	// rotated) -- the new former main artwork (bare) becomes index 2 and never
	// reuses 1.
	existentes := []string{"poster1.jpg"}
	i2 := IndiceRotacaoLivre(existentes, KindPoster, true, "", "")
	if i2 != 2 {
		t.Fatalf("2a rotacao: indice = %d, quero 2", i2)
	}
	nomeRotacionado2 := NomeArquivoArte(KindPoster, false, i2, true, "", "")
	if nomeRotacionado2 != "poster2.jpg" {
		t.Fatalf("nome rotacionado 2 = %q, quero %q", nomeRotacionado2, "poster2.jpg")
	}

	// 3rd rotation: poster1.jpg and poster2.jpg exist -- the next free one is 3.
	existentes = []string{"poster1.jpg", "poster2.jpg"}
	i3 := IndiceRotacaoLivre(existentes, KindPoster, true, "", "")
	if i3 != 3 {
		t.Fatalf("3a rotacao: indice = %d, quero 3", i3)
	}

	// Even with a "hole" (poster1 removed/deleted), the next free one does NOT
	// reindex -- it keeps being highest+1 over what is left.
	existentes = []string{"poster2.jpg"}
	iSemReindex := IndiceRotacaoLivre(existentes, KindPoster, true, "", "")
	if iSemReindex != 3 {
		t.Fatalf("sem reindexacao: indice = %d, quero 3 (nunca reaproveita buraco)", iSemReindex)
	}
}

// TestNomeArquivoArteSeasonPoster: a season poster uses season{NN}-poster,
// never prefixed by the video (doc 08, §5, "season01-poster.jpg").
func TestNomeArquivoArteSeasonPoster(t *testing.T) {
	base := NomeSeasonBase(1)
	if base != "season01" {
		t.Fatalf("NomeSeasonBase(1) = %q, quero %q", base, "season01")
	}
	// pastaExclusiva=false (the series folder has several episode videos)
	// must not prefix the season poster.
	nome := NomeArquivoArte(KindSeasonPoster, true, 0, false, base, "")
	if nome != "season01-poster.jpg" {
		t.Fatalf("nome = %q, quero %q", nome, "season01-poster.jpg")
	}
	nomeRotacionado := NomeArquivoArte(KindSeasonPoster, false, 1, false, base, "")
	if nomeRotacionado != "season01-poster1.jpg" {
		t.Fatalf("nome rotacionado = %q, quero %q", nomeRotacionado, "season01-poster1.jpg")
	}
}

// TestNomeArquivoArteThumbEpisodioSemBare: an episode thumb NEVER has a bare
// variant, even in an exclusive folder (doc 08, §4).
func TestNomeArquivoArteThumbEpisodioSemBare(t *testing.T) {
	nome := NomeArquivoArte(KindThumb, true, 0, true, "Show.S01E01", "")
	if nome != "Show.S01E01-thumb.jpg" {
		t.Fatalf("nome = %q, quero %q (sem variante bare)", nome, "Show.S01E01-thumb.jpg")
	}
}

// TestNomeArquivoArteExtensoesPadrao: clearlogo/discart use .png; the others
// fall back to .jpg (doc 08, §4).
func TestNomeArquivoArteExtensoesPadrao(t *testing.T) {
	casos := map[string]string{
		KindClearlogo: ".png",
		KindDiscart:   ".png",
		KindPoster:    ".jpg",
		KindFanart:    ".jpg",
		KindBanner:    ".jpg",
		KindLandscape: ".jpg",
	}
	for kind, ext := range casos {
		if got := ExtensaoPadrao(kind); got != ext {
			t.Errorf("ExtensaoPadrao(%q) = %q, quero %q", kind, got, ext)
		}
	}
}

// TestNomeArquivoArteBackdropSinonimo: the kind "backdrop" (an input synonym)
// uses the same base name as "fanart".
func TestNomeArquivoArteBackdropSinonimo(t *testing.T) {
	a := NomeArquivoArte(KindFanart, true, 0, true, "", "")
	b := NomeArquivoArte(KindBackdrop, true, 0, true, "", "")
	if a != b || a != "fanart.jpg" {
		t.Fatalf("fanart=%q backdrop=%q, quero ambos %q", a, b, "fanart.jpg")
	}
}

// TestNomeArquivoArteTrailer covers criterion 5.1 of ADR-0025: a kind unknown
// to baseDeKind (KindTrailer) falls into the `base = kind` fallback
// (baseComPrefixo), producing "<videoBaseName>-trailer.mp4" (non-exclusive)
// or "trailer.mp4" (exclusive), with no new rule in baseComPrefixo.
func TestNomeArquivoArteTrailer(t *testing.T) {
	naoExclusiva := NomeArquivoArte(KindTrailer, true, 0, false, "Filme (2026)", ".mp4")
	if naoExclusiva != "Filme (2026)-trailer.mp4" {
		t.Fatalf("nao-exclusiva = %q, quero %q", naoExclusiva, "Filme (2026)-trailer.mp4")
	}
	exclusiva := NomeArquivoArte(KindTrailer, true, 0, true, "Filme (2026)", ".mp4")
	if exclusiva != "trailer.mp4" {
		t.Fatalf("exclusiva = %q, quero %q", exclusiva, "trailer.mp4")
	}
	// ExtensaoPadrao(KindTrailer) == .mp4 -- ext "" falls back to the video default.
	comExtDefault := NomeArquivoArte(KindTrailer, true, 0, true, "Filme (2026)", "")
	if comExtDefault != "trailer.mp4" {
		t.Fatalf("ext default = %q, quero %q", comExtDefault, "trailer.mp4")
	}
}

// TestDiretorioDestinoMovie: movie uses the oldest media_file (created_at
// asc, state=present) of the entity ITSELF.
func TestDiretorioDestinoMovie(t *testing.T) {
	ent := Entity{ID: "movie1", Type: "movie"}
	files := []MediaFile{
		{EntityID: "movie1", Path: "/lib/Matrix/mais-novo.mkv", State: "present", CreatedAt: "2024-02-01T00:00:00.000Z"},
		{EntityID: "movie1", Path: "/lib/Matrix/matrix.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
		{EntityID: "outra", Path: "/lib/Outra/outra.mkv", State: "present", CreatedAt: "2023-01-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, nil)
	if !ok {
		t.Fatal("DiretorioDestino: ok = false, queria true")
	}
	if dir != "/lib/Matrix" {
		t.Fatalf("dir = %q, quero %q", dir, "/lib/Matrix")
	}
}

// TestDiretorioDestinoMovieIgnoraMissing: a media_file with state=missing does
// not count for the calculation (only present does).
func TestDiretorioDestinoMovieIgnoraMissing(t *testing.T) {
	ent := Entity{ID: "movie1", Type: "movie"}
	files := []MediaFile{
		{EntityID: "movie1", Path: "/lib/Missing/x.mkv", State: "missing", CreatedAt: "2020-01-01T00:00:00.000Z"},
		{EntityID: "movie1", Path: "/lib/Present/x.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, nil)
	if !ok || dir != "/lib/Present" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Present true", dir, ok)
	}
}

// TestDiretorioDestinoSemArquivo: entity with no present media_file ->
// ok=false (no destination folder, doc 08, §5 "404/clear error").
func TestDiretorioDestinoSemArquivo(t *testing.T) {
	ent := Entity{ID: "movie1", Type: "movie"}
	if _, ok := DiretorioDestino(ent, nil, nil); ok {
		t.Fatal("ok = true, queria false (sem media_file)")
	}
}

// TestDiretorioDestinoSerie: tv uses its own media_file (which in the database
// already are the files of ALL episodes, through media_file_entity/entity_id=
// series) -- it needs no extra traversal.
func TestDiretorioDestinoSerie(t *testing.T) {
	ent := Entity{ID: "serie1", Type: TipoSerie}
	files := []MediaFile{
		{EntityID: "serie1", Path: "/lib/Show/S01E02.mkv", State: "present", CreatedAt: "2024-02-01T00:00:00.000Z"},
		{EntityID: "serie1", Path: "/lib/Show/S01E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, nil)
	if !ok || dir != "/lib/Show" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Show true", dir, ok)
	}
}

// TestDiretorioDestinoSeasonUsaRaizDaSerie: season (season poster) ALWAYS uses
// the series root -- it finds the "tv" ancestor in the trail and resolves
// through its media_file, never a season subfolder (doc 08, §5).
func TestDiretorioDestinoSeasonUsaRaizDaSerie(t *testing.T) {
	ent := Entity{ID: "season1", Type: TipoTemporada}
	ancestors := []Entity{{ID: "serie1", Type: TipoSerie}} // ROOT->PARENT trail
	files := []MediaFile{
		// season1 has no media_file of its own (entity_id always points to the series).
		{EntityID: "serie1", Path: "/lib/Show/S01E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, ancestors)
	if !ok || dir != "/lib/Show" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Show true (raiz da serie)", dir, ok)
	}
}

// TestDiretorioDestinoSerieMultiTemporadaPrefixoComum: a series with 2+ SIBLING
// season folders -- the common prefix already resolves the real root, without
// needing the go-up-a-level fallback (ADR-0021, the "multi-season" case).
func TestDiretorioDestinoSerieMultiTemporadaPrefixoComum(t *testing.T) {
	ent := Entity{ID: "serie1", Type: TipoSerie}
	files := []MediaFile{
		{EntityID: "serie1", Path: "/lib/Show/Season 01/S01E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
		{EntityID: "serie1", Path: "/lib/Show/Season 01/S01E02.mkv", State: "present", CreatedAt: "2024-01-02T00:00:00.000Z"},
		{EntityID: "serie1", Path: "/lib/Show/Season 02/S02E01.mkv", State: "present", CreatedAt: "2024-02-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, nil)
	if !ok || dir != "/lib/Show" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Show true (raiz, nao Season 01/02)", dir, ok)
	}
}

// TestDiretorioDestinoSerieTemporadaUnicaSobeNivel: a series with a SINGLE
// season (every present file concentrated in "Season 01/") -- the common
// prefix falls on the season folder itself, requiring the go-up-a-level
// fallback (ADR-0021, the "single season" case).
func TestDiretorioDestinoSerieTemporadaUnicaSobeNivel(t *testing.T) {
	ent := Entity{ID: "serie1", Type: TipoSerie}
	files := []MediaFile{
		{EntityID: "serie1", Path: "/lib/Show/Season 01/S01E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
		{EntityID: "serie1", Path: "/lib/Show/Season 01/S01E02.mkv", State: "present", CreatedAt: "2024-01-02T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, nil)
	if !ok || dir != "/lib/Show" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Show true (subiu de Season 01)", dir, ok)
	}
}

// TestDiretorioDestinoSeriePastaDeTemporadaEmOutroIdioma: the go-up-a-level
// fallback must recognise season folder names that are not English (issue #89).
// A library organised in Portuguese puts every track inside "Temporada 01"; when
// the name was not recognised, the SERIES root resolved to the SEASON folder and
// tvshow.nfo/poster/fanart landed inside it -- invisible to Kodi/Jellyfin.
func TestDiretorioDestinoSeriePastaDeTemporadaEmOutroIdioma(t *testing.T) {
	for _, temporada := range []string{
		"Temporada 01", "Temporada 1", "temporada 2",
		"Staffel 3", "Saison 2", "Stagione 1", "Seizoen 4",
		"Temporada Specials", "Season_specials",
	} {
		ent := Entity{ID: "serie1", Type: TipoSerie}
		files := []MediaFile{
			{EntityID: "serie1", Path: "/lib/Show/" + temporada + "/E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
			{EntityID: "serie1", Path: "/lib/Show/" + temporada + "/E02.mkv", State: "present", CreatedAt: "2024-01-02T00:00:00.000Z"},
		}
		dir, ok := DiretorioDestino(ent, files, nil)
		if !ok || dir != "/lib/Show" {
			t.Fatalf("pasta %q: dir = %q ok=%v, quero /lib/Show true", temporada, dir, ok)
		}
	}
}

// TestDiretorioDestinoSeriePastaQueNaoEhDeTemporada: the fallback must not climb
// out of a folder that only LOOKS like a season by a substring ("Temporadas
// Especiais" without a number is still ambiguous, but "Temporada Especial" is
// accepted; a plain "Extras" folder is not).
func TestDiretorioDestinoSeriePastaQueNaoEhDeTemporada(t *testing.T) {
	ent := Entity{ID: "serie1", Type: TipoSerie}
	files := []MediaFile{
		{EntityID: "serie1", Path: "/lib/Show/Extras/E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, nil)
	if !ok || dir != "/lib/Show/Extras" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Show/Extras (nao sobe de Extras)", dir, ok)
	}
}

// TestDiretorioDestinoSerieTemporadaUnicaFormatoCurto: the "S01" variant (short
// form, without "Season") must also trigger the fallback.
func TestDiretorioDestinoSerieTemporadaUnicaFormatoCurto(t *testing.T) {
	ent := Entity{ID: "serie1", Type: TipoSerie}
	files := []MediaFile{
		{EntityID: "serie1", Path: "/lib/Show/S01/E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, nil)
	if !ok || dir != "/lib/Show" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Show true (subiu de S01)", dir, ok)
	}
}

// TestDiretorioDestinoEpisodioNaoSobeDeSeasonFolder: the "go up a level"
// fallback (regexPastaTemporada) is EXCLUSIVE to tv/season -- an episode whose
// own folder is named "Season 01" (the normal Kodi convention) keeps that
// folder as its destination and NEVER goes up to the series root (ADR-0021,
// contract note: DiretorioDestino does not change for movie/episode).
func TestDiretorioDestinoEpisodioNaoSobeDeSeasonFolder(t *testing.T) {
	ent := Entity{ID: "ep1", Type: TipoEpisodio}
	files := []MediaFile{
		{EntityID: "ep1", Path: "/lib/Show/Season 01/S01E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, nil)
	if !ok || dir != "/lib/Show/Season 01" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Show/Season 01 true (episodio fica na propria pasta)", dir, ok)
	}
}

// TestEhPastaExclusivaUmVideo: a folder with 1 single present video -> exclusive.
func TestEhPastaExclusivaUmVideo(t *testing.T) {
	files := []MediaFile{
		{Path: "/lib/Matrix/matrix.mkv", State: "present"},
	}
	if !EhPastaExclusiva("/lib/Matrix", files) {
		t.Fatal("EhPastaExclusiva = false, queria true (1 unico video)")
	}
}

// TestEhPastaExclusivaDoisVideos: a folder with 2 videos (same or another
// entity) -> NOT exclusive (doc 08, §5, "two entities in the same folder").
func TestEhPastaExclusivaDoisVideos(t *testing.T) {
	files := []MediaFile{
		{EntityID: "e1", Path: "/lib/Pasta/a.mkv", State: "present"},
		{EntityID: "e2", Path: "/lib/Pasta/b.mkv", State: "present"},
	}
	if EhPastaExclusiva("/lib/Pasta", files) {
		t.Fatal("EhPastaExclusiva = true, queria false (2 videos na pasta)")
	}
}

// TestEhPastaExclusivaIgnoraMissing: a second MISSING file in the same folder
// does not break the exclusivity (only present counts).
func TestEhPastaExclusivaIgnoraMissing(t *testing.T) {
	files := []MediaFile{
		{Path: "/lib/Pasta/a.mkv", State: "present"},
		{Path: "/lib/Pasta/velho.mkv", State: "missing"},
	}
	if !EhPastaExclusiva("/lib/Pasta", files) {
		t.Fatal("EhPastaExclusiva = false, queria true (missing nao conta)")
	}
}

// TestNomeArquivoNFO: a fixed tvshow.nfo for a series; <video>.nfo for the others.
func TestNomeArquivoNFO(t *testing.T) {
	if got := NomeArquivoNFO(TipoSerie, "Matrix.1999"); got != "tvshow.nfo" {
		t.Errorf("serie: %q, quero tvshow.nfo", got)
	}
	if got := NomeArquivoNFO("movie", "Matrix.1999"); got != "Matrix.1999.nfo" {
		t.Errorf("movie: %q, quero Matrix.1999.nfo", got)
	}
	if got := NomeArquivoNFO(TipoEpisodio, "Show.S01E01"); got != "Show.S01E01.nfo" {
		t.Errorf("episodio: %q, quero Show.S01E01.nfo", got)
	}
}

// TestNomeBaseVideo: strips the directory and the extension.
func TestNomeBaseVideo(t *testing.T) {
	if got := NomeBaseVideo("/lib/Matrix/Matrix.1999.mkv"); got != "Matrix.1999" {
		t.Fatalf("NomeBaseVideo = %q, quero %q", got, "Matrix.1999")
	}
}

// TestEscreverArquivoAtomicoSemOrfao: writes successfully and leaves no orphan
// .tmp-* in the directory.
func TestEscreverArquivoAtomicoSemOrfao(t *testing.T) {
	dir := t.TempDir()
	destino := dir + "/poster.jpg"
	if err := EscreverArquivoAtomico(destino, []byte("dados")); err != nil {
		t.Fatalf("EscreverArquivoAtomico: %v", err)
	}
	entradas := listarNomes(t, dir)
	if len(entradas) != 1 || entradas[0] != "poster.jpg" {
		t.Fatalf("entradas do dir = %v, quero so [poster.jpg]", entradas)
	}
}

// listarNomes lists the entry names of a directory (to check that no orphan
// .tmp-* is left behind).
func listarNomes(t *testing.T, dir string) []string {
	t.Helper()
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	nomes := make([]string, 0, len(entradas))
	for _, e := range entradas {
		nomes = append(nomes, e.Name())
	}
	return nomes
}

// TestEscreverArquivoAtomicoFalhaSemOrfao: an I/O failure (the destination is a
// directory, not a file -- Rename fails) leaves no orphan .tmp-*
// (doc 08, §5).
func TestEscreverArquivoAtomicoFalhaSemOrfao(t *testing.T) {
	dir := t.TempDir()
	// the destination points to an existing DIRECTORY -- os.Rename(tmp, destino)
	// fails (a file cannot be renamed over an existing/non-empty directory).
	destino := dir + "/alvo"
	if err := os.Mkdir(destino, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := EscreverArquivoAtomico(destino, []byte("dados")); err == nil {
		t.Fatal("EscreverArquivoAtomico: err = nil, queria falha (destino e diretorio)")
	}
	entradas := listarNomes(t, dir)
	if len(entradas) != 1 || entradas[0] != "alvo" {
		t.Fatalf("entradas do dir = %v, quero so [alvo] (sem .tmp-* orfao)", entradas)
	}
}

// TestNomeArquivoArteMusica fixes the music naming convention (issue #79): a bare name per
// kind, the SAME words the library already has on disk (folder/backdrop/banner/logo/cdart), no
// "<video>-poster" prefix -- a music folder has no video to lend its name and the folder itself
// already identifies the work.
func TestNomeArquivoArteMusica(t *testing.T) {
	casos := []struct{ kind, quero string }{
		{KindPoster, "folder.jpg"},
		{KindFanart, "backdrop.jpg"},
		{KindBackdrop, "backdrop.jpg"},
		{KindBanner, "banner.jpg"},
		{KindClearlogo, "logo.jpg"},
		{"logo", "logo.jpg"}, // aggregator vocabulary (assets.go)
		{KindDiscart, "cdart.jpg"},
		{"disc", "cdart.jpg"}, // aggregator vocabulary (assets.go)
		{KindThumb, "thumb.jpg"},
		{"qualquer", "qualquer.jpg"}, // unknown kind keeps its own name
	}
	for _, c := range casos {
		if got := NomeArquivoArteMusica(c.kind, true, 0, ".jpg"); got != c.quero {
			t.Errorf("NomeArquivoArteMusica(%q) = %q, quero %q", c.kind, got, c.quero)
		}
	}
	// the extension of the asset wins, and the rotation suffix marks a non-principal artwork
	if got := NomeArquivoArteMusica(KindPoster, true, 0, ".png"); got != "folder.png" {
		t.Errorf("poster principal com .png = %q, quero folder.png", got)
	}
	if got := NomeArquivoArteMusica(KindPoster, false, 2, ".jpg"); got != "folder2.jpg" {
		t.Errorf("poster secundario = %q, quero folder2.jpg", got)
	}
}

// TestIndiceRotacaoLivreMusica covers the next-free-index rule on the music names: it counts
// the suffix AFTER "folder"/"cdart", never reindexes a hole, and ignores names of other kinds.
func TestIndiceRotacaoLivreMusica(t *testing.T) {
	existentes := []string{"/m/folder.jpg", "/m/folder2.jpg", "/m/cdart.png", "/m/cdart3.png", "/m/outra.jpg"}
	if got := IndiceRotacaoLivreMusica(existentes, KindPoster, ".jpg"); got != 3 {
		t.Errorf("indice do poster = %d, quero 3 (folder e folder2 ocupados)", got)
	}
	if got := IndiceRotacaoLivreMusica(existentes, KindDiscart, ".png"); got != 4 {
		t.Errorf("indice do cdart = %d, quero 4 (cdart e cdart3 ocupados)", got)
	}
	if got := IndiceRotacaoLivreMusica(nil, KindPoster, ".jpg"); got != 1 {
		t.Errorf("indice sem nenhum arquivo = %d, quero 1", got)
	}
}

// TestCaminhoNFODeVideo fixes the rule that names an episode/movie .nfo after the VIDEO (Kodi's
// convention): the extension is swapped, the rest of the path is untouched.
func TestCaminhoNFODeVideo(t *testing.T) {
	casos := []struct{ entrada, quero string }{
		{"/lib/Show.S01E01.mkv", "/lib/Show.S01E01.nfo"},
		{"/lib/pasta/Show.S01E01E02.mkv", "/lib/pasta/Show.S01E01E02.nfo"},
		{"/lib/Filme.2020.1080p.mp4", "/lib/Filme.2020.1080p.nfo"},
		{"/lib/pasta/semext", "/lib/pasta/semext.nfo"},
	}
	for _, c := range casos {
		if got := CaminhoNFODeVideo(c.entrada); got != c.quero {
			t.Errorf("CaminhoNFODeVideo(%q) = %q, quero %q", c.entrada, got, c.quero)
		}
	}
	// sem extensao informada, o kind decide a padrao (poster -> .jpg)
	if got := NomeArquivoArteMusica(KindPoster, true, 0, ""); got != "folder.jpg" {
		t.Errorf("NomeArquivoArteMusica sem extensao = %q, quero folder.jpg", got)
	}
	if got := IndiceRotacaoLivreMusica([]string{"/m/folder2.jpg"}, KindPoster, ""); got != 3 {
		t.Errorf("IndiceRotacaoLivreMusica sem extensao = %d, quero 3", got)
	}
}
