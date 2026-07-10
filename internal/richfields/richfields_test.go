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
	links := LinksExternos(campos, "movie", "603")
	if links["imdb"] != "https://imdb/x" || links["tmdb"] != "https://www.themoviedb.org/movie/603" {
		t.Errorf("links = %v", links)
	}
	if _, ok := links["vazio"]; ok {
		t.Error("entrada vazia não deveria entrar")
	}
	// Sem id TMDB → sem link do TMDB.
	links = LinksExternos(campos, "", "")
	if _, ok := links["tmdb"]; ok {
		t.Error("sem id TMDB, não deveria ter link tmdb")
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
