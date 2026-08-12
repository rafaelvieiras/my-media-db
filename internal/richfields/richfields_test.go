package richfields

import "testing"

// Helpers lineares (baixa complexidade ciclomática) para asserções ponteiro/valor.
func eqStr(t *testing.T, nome string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, quero %q", nome, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %q, quero %q", nome, *got, want)
	}
}

func nilStr(t *testing.T, nome string, got *string) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %q, quero nil", nome, *got)
	}
}

func TestResolverEscalarTraduzivel(t *testing.T) {
	campos := map[string]any{
		"title":  map[string]any{"pt-BR": "Duna", "en": "Dune"},
		"legado": "Valor Legado",
		"vazio":  "",
	}
	// Cadeia acha o pt-BR.
	v, prov := ResolverEscalarTraduzivel(campos, "title", []string{"pt-BR", "en"})
	eqStr(t, "title", v, "Duna")
	if prov != "pt-BR" {
		t.Errorf("prov = %q, quero pt-BR", prov)
	}
	// Cadeia sem o idioma cai em "qualquer presente" (ordem estável: en).
	v, prov = ResolverEscalarTraduzivel(campos, "title", []string{"ja"})
	eqStr(t, "title fallback", v, "Dune")
	if prov != "en" {
		t.Errorf("prov fallback = %q, quero en", prov)
	}
	// Escalar simples (legado): proveniência "".
	v, prov = ResolverEscalarTraduzivel(campos, "legado", []string{"pt-BR"})
	eqStr(t, "legado", v, "Valor Legado")
	if prov != "" {
		t.Errorf("prov legado = %q, quero vazio", prov)
	}
	// Ausente e vazio → nil.
	v, _ = ResolverEscalarTraduzivel(campos, "ausente", []string{"pt-BR"})
	nilStr(t, "ausente", v)
	v, _ = ResolverEscalarTraduzivel(campos, "vazio", []string{"pt-BR"})
	nilStr(t, "vazio", v)
}

func TestResolverListaTraduzivel(t *testing.T) {
	campos := map[string]any{
		"keywords": map[string]any{"en": []any{"a", "b"}},
		"legado":   []any{"x", "y"},
	}
	lst, prov := ResolverListaTraduzivel(campos, "keywords", []string{"pt-BR", "en"})
	if len(lst) != 2 || lst[0] != "a" || prov != "en" {
		t.Errorf("keywords = %v (prov %q), quero [a b] en", lst, prov)
	}
	lst, prov = ResolverListaTraduzivel(campos, "legado", []string{"pt-BR"})
	if len(lst) != 2 || prov != "" {
		t.Errorf("legado = %v (prov %q), quero [x y] \"\"", lst, prov)
	}
	lst, _ = ResolverListaTraduzivel(campos, "ausente", []string{"pt-BR"})
	if lst != nil {
		t.Errorf("ausente = %v, quero nil", lst)
	}
}

func TestCertificacaoDaRegiao(t *testing.T) {
	campos := map[string]any{
		"certification_by_region": map[string]any{"BR": "14", "US": "R"},
	}
	eqStr(t, "pt-BR", CertificacaoDaRegiao(campos, "pt-BR"), "14")
	// en não tem região → cai em US.
	eqStr(t, "en→US", CertificacaoDaRegiao(campos, "en"), "R")
	// Região inexistente na fonte (fr-FR → FR ausente) → US.
	eqStr(t, "fr→US", CertificacaoDaRegiao(campos, "fr-FR"), "R")
	// Sem o mapa → nil.
	nilStr(t, "sem mapa", CertificacaoDaRegiao(map[string]any{}, "pt-BR"))
}

func TestRegiaoDoIdioma(t *testing.T) {
	casos := map[string]string{"pt-BR": "BR", "en": "US", "es-419": "419", "de-DE": "DE"}
	for lang, want := range casos {
		if got := RegiaoDoIdioma(lang); got != want {
			t.Errorf("RegiaoDoIdioma(%q) = %q, quero %q", lang, got, want)
		}
	}
}

func TestVideosDe(t *testing.T) {
	campos := map[string]any{"videos": []any{
		map[string]any{"name": "Trailer", "key": "abc", "site": "YouTube", "type": "Trailer", "official": true, "url": "https://youtu.be/abc"},
		map[string]any{"name": "SemURL", "key": "def", "site": "Desconhecido", "type": "Clip", "official": false, "url": ""},
		"lixo-nao-mapa",
	}}
	vs := VideosDe(campos)
	if len(vs) != 2 {
		t.Fatalf("videos = %d, quero 2 (ignora o não-mapa)", len(vs))
	}
	if vs[0].Name != "Trailer" || vs[0].URL == nil || *vs[0].URL != "https://youtu.be/abc" {
		t.Errorf("video[0] = %+v", vs[0])
	}
	if vs[1].URL != nil {
		t.Errorf("video[1].URL = %v, quero nil (site desconhecido)", *vs[1].URL)
	}
	// Sem a chave → fatia vazia (não-nil).
	if v := VideosDe(map[string]any{}); v == nil || len(v) != 0 {
		t.Errorf("videos vazio = %v, quero []", v)
	}
}

// TestPrimeiroTrailerYoutube cobre o critério do briefing 0102: acha o
// primeiro vídeo type=="Trailer" && site=="YouTube" com key, ignorando
// Teaser/Clip e vídeos de outro site (MESMO critério de `primeiroTrailer`
// do frontend, rich-detail-view.tsx).
func TestPrimeiroTrailerYoutube(t *testing.T) {
	videos := []Video{
		{Name: "Teaser", Key: "t1", Site: "YouTube", Type: "Teaser"},
		{Name: "Trailer Vimeo", Key: "v1", Site: "Vimeo", Type: "Trailer"},
		{Name: "Trailer Oficial", Key: "abc123", Site: "YouTube", Type: "Trailer"},
		{Name: "Outro Trailer", Key: "def456", Site: "YouTube", Type: "Trailer"},
	}
	got, ok := PrimeiroTrailerYoutube(videos)
	if !ok || got.Key != "abc123" {
		t.Fatalf("PrimeiroTrailerYoutube = (%+v, %v), quero (key=abc123, true)", got, ok)
	}

	if _, ok := PrimeiroTrailerYoutube(nil); ok {
		t.Error("PrimeiroTrailerYoutube(nil) = true, quero false")
	}

	semTrailer := []Video{{Name: "Teaser", Key: "t1", Site: "YouTube", Type: "Teaser"}}
	if _, ok := PrimeiroTrailerYoutube(semTrailer); ok {
		t.Error("PrimeiroTrailerYoutube sem Trailer YouTube = true, quero false")
	}

	semKey := []Video{{Name: "Trailer sem key", Key: "", Site: "YouTube", Type: "Trailer"}}
	if _, ok := PrimeiroTrailerYoutube(semKey); ok {
		t.Error("PrimeiroTrailerYoutube com key vazia = true, quero false")
	}
}

func TestColecaoDe(t *testing.T) {
	campos := map[string]any{"collection": map[string]any{"source_id": "2344", "name": "Colecao", "poster_url": "https://img/c.jpg"}}
	c := ColecaoDe(campos)
	if c == nil || c.SourceID != "2344" || c.Name != "Colecao" || c.PosterURL == nil {
		t.Fatalf("colecao = %+v", c)
	}
	if ColecaoDe(map[string]any{}) != nil {
		t.Error("colecao ausente deveria ser nil")
	}
}

func TestSimilaresDe(t *testing.T) {
	campos := map[string]any{"similar": []any{
		map[string]any{"source_id": "604", "type": "movie", "title": "Reloaded", "year": "2003", "poster_url": ""},
	}}
	s := SimilaresDe(campos)
	if len(s) != 1 || s[0].SourceID != "604" || s[0].Type != "movie" || s[0].PosterURL != nil {
		t.Fatalf("similar = %+v", s)
	}
}

func TestLinksExternos(t *testing.T) {
	campos := map[string]any{"external_links": map[string]any{"imdb": "https://imdb/x", "vazio": ""}}
	// Com id TMDB → acrescenta o link do TMDB; ignora entradas vazias.
	links := LinksExternos(campos, "movie", "603", "", "")
	if links["imdb"] != "https://imdb/x" || links["tmdb"] != "https://www.themoviedb.org/movie/603" {
		t.Errorf("links = %v", links)
	}
	if _, ok := links["vazio"]; ok {
		t.Error("entrada vazia não deveria entrar")
	}
	// Sem id TMDB → sem link do TMDB.
	links = LinksExternos(campos, "", "", "", "")
	if _, ok := links["tmdb"]; ok {
		t.Error("sem id TMDB, não deveria ter link tmdb")
	}
}

// TestLinksExternosImdbTvdb (0086) cobre os ids crus de imdb/tvdb: preenchem o mapa
// quando a fonte não os modelou em `external_links`, mas NUNCA sobrescrevem o que a
// fonte já mandou (precedência do canônico sobre o id cru).
func TestLinksExternosImdbTvdb(t *testing.T) {
	// Sem external_links algum: imdb/tvdb crus montam as URLs canônicas.
	links := LinksExternos(map[string]any{}, "tv", "1399", "tt0944947", "121361")
	if links["tmdb"] != "https://www.themoviedb.org/tv/1399" {
		t.Errorf("tmdb = %q", links["tmdb"])
	}
	if links["imdb"] != "https://www.imdb.com/title/tt0944947/" {
		t.Errorf("imdb = %q", links["imdb"])
	}
	if links["tvdb"] != "https://www.thetvdb.com/dereferrer/series/121361" {
		t.Errorf("tvdb = %q", links["tvdb"])
	}

	// external_links já tem um imdb (mais específico) → o id cru NÃO sobrescreve.
	campos := map[string]any{"external_links": map[string]any{"imdb": "https://imdb/da-fonte"}}
	links = LinksExternos(campos, "tv", "1399", "tt0944947", "121361")
	if links["imdb"] != "https://imdb/da-fonte" {
		t.Errorf("imdb deveria preservar o valor da fonte, veio %q", links["imdb"])
	}
	// tvdb ainda entra (a fonte não modelou esse).
	if links["tvdb"] != "https://www.thetvdb.com/dereferrer/series/121361" {
		t.Errorf("tvdb = %q", links["tvdb"])
	}
}

// intPtr/floatPtr asseveram um ponteiro numérico esperado (linear p/ baixa cyclo).
func intPtr(t *testing.T, nome string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, quero %d", nome, got, want)
	}
}

func floatPtr(t *testing.T, nome string, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, quero %v", nome, got, want)
	}
}

func TestIntPtrDe(t *testing.T) {
	m := map[string]any{"i": 136, "fi": float64(120)}
	intPtr(t, "IntPtrDe(i)", IntPtrDe(m, "i"), 136)
	intPtr(t, "IntPtrDe(fi float)", IntPtrDe(m, "fi"), 120)
	if IntPtrDe(m, "ausente") != nil {
		t.Error("IntPtrDe ausente deveria ser nil")
	}
}

func TestFloatPtrDe(t *testing.T) {
	m := map[string]any{"f": 8.2, "if": 7}
	floatPtr(t, "FloatPtrDe(f)", FloatPtrDe(m, "f"), 8.2)
	floatPtr(t, "FloatPtrDe(if int)", FloatPtrDe(m, "if"), 7)
	if FloatPtrDe(m, "ausente") != nil {
		t.Error("FloatPtrDe ausente deveria ser nil")
	}
}

func TestConversoresStringELista(t *testing.T) {
	if ComoStringMapa(map[string]any{"a": "x"}, "a") != "x" {
		t.Error("ComoStringMapa")
	}
	if ComoStringMapa(map[string]any{"a": 1}, "a") != "" {
		t.Error("ComoStringMapa tipo errado deveria ser vazio")
	}
	eqStr(t, "StrPtrDe", StrPtrDe(map[string]any{"a": "x"}, "a"), "x")
	nilStr(t, "StrPtrDe vazio", StrPtrDe(map[string]any{"a": ""}, "a"))
	eqStr(t, "PtrSeNaoVazio", PtrSeNaoVazio("y"), "y")
	nilStr(t, "PtrSeNaoVazio vazio", PtrSeNaoVazio(""))
	if DerefOuVazio(nil) != "" {
		t.Error("DerefOuVazio(nil) deveria ser vazio")
	}
	s := "z"
	if DerefOuVazio(&s) != "z" {
		t.Error("DerefOuVazio(&z)")
	}
	if v := OuListaVazia(nil); v == nil || len(v) != 0 {
		t.Error("OuListaVazia(nil) deveria ser []")
	}
	ano := "2021-09-15"
	eqStr(t, "AnoDePtr", AnoDePtr(&ano), "2021")
	nilStr(t, "AnoDePtr nil", AnoDePtr(nil))
	curto := "abc"
	nilStr(t, "AnoDePtr curto", AnoDePtr(&curto))
}

// ── Créditos (0029) ──────────────────────────────────────────────────────────

// TestCastDeFilme cobre o shape de FILME: sem episode_count, foto quando presente,
// nil quando ausente, e o não-mapa (lixo) ignorado.
func TestCastDeFilme(t *testing.T) {
	campos := map[string]any{"cast": []any{
		map[string]any{"source_id": "6384", "name": "Keanu Reeves", "character": "Neo", "order": 0, "profile_path": "/keanu.jpg"},
		map[string]any{"source_id": "2975", "name": "Laurence Fishburne", "character": "Morpheus", "order": 1},
		"lixo-nao-mapa",
	}}
	cast := CastDe(campos)
	if len(cast) != 2 {
		t.Fatalf("cast = %d itens, quero 2 (ignora o não-mapa)", len(cast))
	}
	if cast[0].SourceID != "6384" || cast[0].Name != "Keanu Reeves" || cast[0].Character != "Neo" || cast[0].Order != 0 {
		t.Errorf("cast[0] = %+v", cast[0])
	}
	eqStr(t, "cast[0].ProfileURL", cast[0].ProfileURL, "https://image.tmdb.org/t/p/w185/keanu.jpg")
	if cast[0].EpisodeCount != nil {
		t.Errorf("cast[0].EpisodeCount = %v, quero nil (filme)", *cast[0].EpisodeCount)
	}
	if cast[1].ProfileURL != nil {
		t.Errorf("cast[1].ProfileURL = %v, quero nil (sem profile_path)", *cast[1].ProfileURL)
	}
}

// TestCastDeSerieEAusente cobre o shape de SÉRIE (episode_count do papel agregado) e
// a fatia vazia (não-nil) quando a chave `cast` está ausente do canônico.
func TestCastDeSerieEAusente(t *testing.T) {
	campos := map[string]any{"cast": []any{
		map[string]any{"source_id": "1", "name": "Ator Serie", "character": "Peter / Brian", "order": 0, "episode_count": 42, "profile_path": "/a.jpg"},
	}}
	cast := CastDe(campos)
	if len(cast) != 1 {
		t.Fatalf("cast serie = %d itens, quero 1", len(cast))
	}
	intPtr(t, "cast[0].EpisodeCount", cast[0].EpisodeCount, 42)
	// Ausente → fatia vazia não-nil.
	if v := CastDe(map[string]any{}); v == nil || len(v) != 0 {
		t.Errorf("cast ausente = %v, quero []", v)
	}
}

func TestCrewDe(t *testing.T) {
	campos := map[string]any{"crew": []any{
		map[string]any{"source_id": "9339", "name": "Lana Wachowski", "department": "Directing", "job": "Director", "profile_path": "/lw.jpg"},
	}}
	crew := CrewDe(campos)
	if len(crew) != 1 {
		t.Fatalf("crew = %d itens, quero 1", len(crew))
	}
	if crew[0].SourceID != "9339" || crew[0].Department != "Directing" || crew[0].Job != "Director" {
		t.Errorf("crew[0] = %+v", crew[0])
	}
	eqStr(t, "crew[0].ProfileURL", crew[0].ProfileURL, "https://image.tmdb.org/t/p/w185/lw.jpg")
	if crew[0].EpisodeCount != nil {
		t.Errorf("crew[0].EpisodeCount = %v, quero nil (filme)", *crew[0].EpisodeCount)
	}
	// Série: com episode_count (cargo agregado por episódios).
	campos = map[string]any{"crew": []any{
		map[string]any{"source_id": "2", "name": "Roteirista Serie", "department": "Writing", "job": "Writer", "episode_count": 10},
	}}
	crew = CrewDe(campos)
	intPtr(t, "crew[0].EpisodeCount serie", crew[0].EpisodeCount, 10)
	if v := CrewDe(map[string]any{}); v == nil || len(v) != 0 {
		t.Errorf("crew ausente = %v, quero []", v)
	}
}

func TestCompaniesDe(t *testing.T) {
	campos := map[string]any{"companies": []any{
		map[string]any{"source_id": "174", "name": "Warner Bros.", "logo_path": "/wb.png"},
		map[string]any{"source_id": "175", "name": "Sem Logo"},
	}}
	comps := CompaniesDe(campos)
	if len(comps) != 2 {
		t.Fatalf("companies = %d itens, quero 2", len(comps))
	}
	eqStr(t, "companies[0].LogoURL", comps[0].LogoURL, "https://image.tmdb.org/t/p/w185/wb.png")
	if comps[1].LogoURL != nil {
		t.Errorf("companies[1].LogoURL = %v, quero nil", *comps[1].LogoURL)
	}
	if v := CompaniesDe(map[string]any{}); v == nil || len(v) != 0 {
		t.Errorf("companies ausente = %v, quero []", v)
	}
}
