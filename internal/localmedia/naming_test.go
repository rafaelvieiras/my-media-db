package localmedia

import (
	"os"
	"testing"
)

// TestNomeArquivoArteBare: pasta EXCLUSIVA (1 único vídeo) + principal ->
// nome bare (briefing 0062 §5, critério "poster.jpg no mesmo diretório").
func TestNomeArquivoArteBare(t *testing.T) {
	nome := NomeArquivoArte(KindPoster, true, 0, true, "Matrix.1999", "")
	if nome != "poster.jpg" {
		t.Fatalf("nome = %q, quero %q", nome, "poster.jpg")
	}
}

// TestNomeArquivoArtePrefixado: pasta NÃO-exclusiva (2+ vídeos) -> nome
// prefixado pelo vídeo (briefing 0062 §5, "<nome-do-vídeo>-poster.jpg").
func TestNomeArquivoArtePrefixado(t *testing.T) {
	nome := NomeArquivoArte(KindPoster, true, 0, false, "Matrix.1999.EXTENDED", "")
	if nome != "Matrix.1999.EXTENDED-poster.jpg" {
		t.Fatalf("nome = %q, quero %q", nome, "Matrix.1999.EXTENDED-poster.jpg")
	}
}

// TestNomeArquivoArteRotacaoCrescente: marcar principal 3x produz sufixos
// crescentes SEM colisão nem reindexação (briefing 0062 §5/§6).
func TestNomeArquivoArteRotacaoCrescente(t *testing.T) {
	// 1a rotacao: so o antigo principal (bare) existe -> vira indice 1.
	i1 := IndiceRotacaoLivre(nil, KindPoster, true, "", "")
	if i1 != 1 {
		t.Fatalf("1a rotacao: indice = %d, quero 1", i1)
	}
	nomeRotacionado1 := NomeArquivoArte(KindPoster, false, i1, true, "", "")
	if nomeRotacionado1 != "poster1.jpg" {
		t.Fatalf("nome rotacionado 1 = %q, quero %q", nomeRotacionado1, "poster1.jpg")
	}

	// 2a rotacao: ja existe poster1.jpg (o penultimo principal, agora
	// rotacionado) -- o novo antigo-principal (bare) vira indice 2, nunca
	// reaproveita o 1.
	existentes := []string{"poster1.jpg"}
	i2 := IndiceRotacaoLivre(existentes, KindPoster, true, "", "")
	if i2 != 2 {
		t.Fatalf("2a rotacao: indice = %d, quero 2", i2)
	}
	nomeRotacionado2 := NomeArquivoArte(KindPoster, false, i2, true, "", "")
	if nomeRotacionado2 != "poster2.jpg" {
		t.Fatalf("nome rotacionado 2 = %q, quero %q", nomeRotacionado2, "poster2.jpg")
	}

	// 3a rotacao: existem poster1.jpg e poster2.jpg -- proximo livre e 3.
	existentes = []string{"poster1.jpg", "poster2.jpg"}
	i3 := IndiceRotacaoLivre(existentes, KindPoster, true, "", "")
	if i3 != 3 {
		t.Fatalf("3a rotacao: indice = %d, quero 3", i3)
	}

	// Mesmo com um "buraco" (poster1 removido/apagado), o proximo livre NAO
	// reindexa -- continua maior+1 sobre o que resta.
	existentes = []string{"poster2.jpg"}
	iSemReindex := IndiceRotacaoLivre(existentes, KindPoster, true, "", "")
	if iSemReindex != 3 {
		t.Fatalf("sem reindexacao: indice = %d, quero 3 (nunca reaproveita buraco)", iSemReindex)
	}
}

// TestNomeArquivoArteSeasonPoster: pôster de temporada usa season{NN}-poster,
// nunca prefixado por vídeo (briefing 0062 §5, "season01-poster.jpg").
func TestNomeArquivoArteSeasonPoster(t *testing.T) {
	base := NomeSeasonBase(1)
	if base != "season01" {
		t.Fatalf("NomeSeasonBase(1) = %q, quero %q", base, "season01")
	}
	// pastaExclusiva=false (a pasta da serie tem varios videos de episodio)
	// nao deve prefixar o season poster.
	nome := NomeArquivoArte(KindSeasonPoster, true, 0, false, base, "")
	if nome != "season01-poster.jpg" {
		t.Fatalf("nome = %q, quero %q", nome, "season01-poster.jpg")
	}
	nomeRotacionado := NomeArquivoArte(KindSeasonPoster, false, 1, false, base, "")
	if nomeRotacionado != "season01-poster1.jpg" {
		t.Fatalf("nome rotacionado = %q, quero %q", nomeRotacionado, "season01-poster1.jpg")
	}
}

// TestNomeArquivoArteThumbEpisodioSemBare: thumb de episódio NUNCA tem
// variante bare, mesmo em pasta exclusiva (briefing 0062 §4).
func TestNomeArquivoArteThumbEpisodioSemBare(t *testing.T) {
	nome := NomeArquivoArte(KindThumb, true, 0, true, "Show.S01E01", "")
	if nome != "Show.S01E01-thumb.jpg" {
		t.Fatalf("nome = %q, quero %q (sem variante bare)", nome, "Show.S01E01-thumb.jpg")
	}
}

// TestNomeArquivoArteExtensoesPadrao: clearlogo/discart usam .png; os demais
// caem em .jpg (briefing 0062 §4).
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

// TestNomeArquivoArteBackdropSinonimo: kind "backdrop" (sinônimo de entrada)
// usa o mesmo nome-base que "fanart".
func TestNomeArquivoArteBackdropSinonimo(t *testing.T) {
	a := NomeArquivoArte(KindFanart, true, 0, true, "", "")
	b := NomeArquivoArte(KindBackdrop, true, 0, true, "", "")
	if a != b || a != "fanart.jpg" {
		t.Fatalf("fanart=%q backdrop=%q, quero ambos %q", a, b, "fanart.jpg")
	}
}

// TestDiretorioDestinoMovie: movie usa o media_file mais antigo (created_at
// asc, state=present) da PRÓPRIA entity.
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

// TestDiretorioDestinoMovieIgnoraMissing: media_file state=missing não conta
// para o cálculo (só present).
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

// TestDiretorioDestinoSemArquivo: entity sem nenhum media_file present ->
// ok=false (sem pasta de destino, briefing 0062 §5 "404/erro claro").
func TestDiretorioDestinoSemArquivo(t *testing.T) {
	ent := Entity{ID: "movie1", Type: "movie"}
	if _, ok := DiretorioDestino(ent, nil, nil); ok {
		t.Fatal("ok = true, queria false (sem media_file)")
	}
}

// TestDiretorioDestinoSerie: tv usa os próprios media_file (que no banco já
// são os arquivos de TODOS os episódios, via media_file_entity/entity_id=
// série) -- não precisa de travessia extra.
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

// TestDiretorioDestinoSeasonUsaRaizDaSerie: season (pôster de temporada) usa
// SEMPRE a raiz da série -- acha o ancestral "tv" na trilha e resolve pelos
// media_file dele, nunca uma subpasta de temporada (briefing 0062 §5).
func TestDiretorioDestinoSeasonUsaRaizDaSerie(t *testing.T) {
	ent := Entity{ID: "season1", Type: TipoTemporada}
	ancestors := []Entity{{ID: "serie1", Type: TipoSerie}} // trilha RAIZ->PAI
	files := []MediaFile{
		// season1 nao tem media_file proprio (entity_id sempre aponta a serie).
		{EntityID: "serie1", Path: "/lib/Show/S01E01.mkv", State: "present", CreatedAt: "2024-01-01T00:00:00.000Z"},
	}
	dir, ok := DiretorioDestino(ent, files, ancestors)
	if !ok || dir != "/lib/Show" {
		t.Fatalf("dir = %q ok=%v, quero /lib/Show true (raiz da serie)", dir, ok)
	}
}

// TestEhPastaExclusivaUmVideo: pasta com 1 único vídeo present -> exclusiva.
func TestEhPastaExclusivaUmVideo(t *testing.T) {
	files := []MediaFile{
		{Path: "/lib/Matrix/matrix.mkv", State: "present"},
	}
	if !EhPastaExclusiva("/lib/Matrix", files) {
		t.Fatal("EhPastaExclusiva = false, queria true (1 unico video)")
	}
}

// TestEhPastaExclusivaDoisVideos: pasta com 2 vídeos (mesma ou outra entity)
// -> NÃO exclusiva (briefing 0062 §5, "duas entities na mesma pasta").
func TestEhPastaExclusivaDoisVideos(t *testing.T) {
	files := []MediaFile{
		{EntityID: "e1", Path: "/lib/Pasta/a.mkv", State: "present"},
		{EntityID: "e2", Path: "/lib/Pasta/b.mkv", State: "present"},
	}
	if EhPastaExclusiva("/lib/Pasta", files) {
		t.Fatal("EhPastaExclusiva = true, queria false (2 videos na pasta)")
	}
}

// TestEhPastaExclusivaIgnoraMissing: um segundo arquivo MISSING na mesma
// pasta não quebra a exclusividade (só present conta).
func TestEhPastaExclusivaIgnoraMissing(t *testing.T) {
	files := []MediaFile{
		{Path: "/lib/Pasta/a.mkv", State: "present"},
		{Path: "/lib/Pasta/velho.mkv", State: "missing"},
	}
	if !EhPastaExclusiva("/lib/Pasta", files) {
		t.Fatal("EhPastaExclusiva = false, queria true (missing nao conta)")
	}
}

// TestNomeArquivoNFO: tvshow.nfo fixo para serie; <video>.nfo para os demais.
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

// TestNomeBaseVideo: strip de diretorio e extensao.
func TestNomeBaseVideo(t *testing.T) {
	if got := NomeBaseVideo("/lib/Matrix/Matrix.1999.mkv"); got != "Matrix.1999" {
		t.Fatalf("NomeBaseVideo = %q, quero %q", got, "Matrix.1999")
	}
}

// TestEscreverArquivoAtomicoSemOrfao: escreve com sucesso e nao deixa
// .tmp-* orfao no diretorio.
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

// listarNomes lista os nomes das entradas de um diretorio (para conferir
// ausencia de .tmp-* orfao).
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

// TestEscreverArquivoAtomicoFalhaSemOrfao: falha de I/O (destino é um
// diretório, não um arquivo -- Rename falha) não deixa .tmp-* órfão
// (briefing 0062 §5).
func TestEscreverArquivoAtomicoFalhaSemOrfao(t *testing.T) {
	dir := t.TempDir()
	// destino aponta para um DIRETORIO existente -- os.Rename(tmp, destino)
	// falha (nao pode renomear arquivo por cima de diretorio nao-vazio/existente).
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
