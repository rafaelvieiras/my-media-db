package resolver

import (
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Estes testes cobrem os CAMPOS RICOS (paridade com o detalhe do externo,
// 0022/0023) que o resolver expõe a partir da camada externa canônica: traduzíveis
// (tagline/keywords) compostos por idioma, e neutros/estruturados (runtime,
// certification por região, vote_average, backdrop, videos, collection, similar,
// external_links) extraídos direto do canônico via richfields.

// entRica é uma entity de núcleo com id externo TMDB (para o link do TMDB) e idioma
// original ancorando a cadeia de fallback.
func entRica() sqlcgen.Entity {
	return sqlcgen.Entity{
		ID:               "x",
		Type:             "movie",
		Title:            "Titulo Arquivo",
		Status:           "cataloged",
		ExternalIdsJson:  ns(`{"tmdb":"603","imdb":"tt0133093"}`),
		OriginalLanguage: ns("en"),
	}
}

// camadaRica devolve uma camada externa canônica com os campos ricos no MESMO
// formato do MapearDetalhe (traduzíveis como map[idioma]valor; neutros como
// escalar/lista/mapa; estruturas como []any de map[string]any).
func camadaRica() []CamadaExterna {
	return []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{
		"tagline":                 map[string]any{"pt-BR": "Bem-vindo ao mundo real.", "en": "Welcome to the real world."},
		"keywords":                map[string]any{"en": []any{"simulated reality", "artificial intelligence"}},
		"runtime":                 136,
		"vote_average":            8.2,
		"backdrop_url":            "https://img/backdrop.jpg",
		"certification_by_region": map[string]any{"BR": "14", "US": "R"},
		"videos": []any{
			map[string]any{"name": "Trailer", "key": "abc", "site": "YouTube", "type": "Trailer", "official": true, "url": "https://youtu.be/abc"},
		},
		"collection": map[string]any{"source_id": "2344", "name": "Colecao Matrix", "poster_url": "https://img/col.jpg"},
		"similar": []any{
			map[string]any{"source_id": "604", "type": "movie", "title": "Matrix Reloaded", "year": "2003", "poster_url": "https://img/604.jpg"},
		},
		"external_links": map[string]any{"imdb": "https://www.imdb.com/title/tt0133093"},
	}}}
}

func TestResolverCamposRicosTraduziveis(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
	})

	// tagline resolve no idioma pedido (pt-BR); keywords caem no en (só existe lá,
	// ancorado no original_language da cadeia).
	eqPtr(t, "tagline", got.Tagline, "Bem-vindo ao mundo real.")
	eq(t, "tagline prov", got.ResolvedLanguages["tagline"], "pt-BR")
	if len(got.Keywords) != 2 || got.Keywords[0] != "simulated reality" {
		t.Errorf("keywords = %v, quero [simulated reality, artificial intelligence]", got.Keywords)
	}
	eq(t, "keywords prov", got.ResolvedLanguages["keywords"], "en")
}

func TestResolverCamposRicosNeutros(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
	})

	eqPtr(t, "runtime", got.Runtime, 136)
	eqPtr(t, "vote_average", got.VoteAverage, 8.2)
	eqPtr(t, "backdrop_url", got.BackdropURL, "https://img/backdrop.jpg")
	// Certificacao pela regiao do idioma ativo (pt-BR → BR).
	eqPtr(t, "certification", got.Certification, "14")

	if len(got.Videos) != 1 || got.Videos[0].Name != "Trailer" || got.Videos[0].URL == nil {
		t.Fatalf("videos = %+v, quero 1 trailer com url", got.Videos)
	}
	if got.Collection == nil || got.Collection.Name != "Colecao Matrix" {
		t.Fatalf("collection = %+v, quero Colecao Matrix", got.Collection)
	}
	if len(got.Similar) != 1 || got.Similar[0].SourceID != "604" {
		t.Fatalf("similar = %+v, quero 1 similar 604", got.Similar)
	}
	// external_links: imdb do canônico + tmdb montado do external_ids[tmdb]+type.
	eq(t, "link imdb", got.ExternalLinks["imdb"], "https://www.imdb.com/title/tt0133093")
	eq(t, "link tmdb", got.ExternalLinks["tmdb"], "https://www.themoviedb.org/movie/603")
}

// TestResolverBackdropLocalVenceCanonico: quando o chamador passa BackdropURL
// (fundo cacheado localmente), o resolver a serve no lugar da URL direta da fonte;
// sem ela, cai na `backdrop_url` do canônico externo.
func TestResolverBackdropLocalVenceCanonico(t *testing.T) {
	local := "/api/v1/entities/x/assets/fanart"
	comLocal := Resolver(Camadas{
		Entity:      entRica(),
		Cadeia:      []string{"pt-BR"},
		LangPedido:  "pt-BR",
		Externo:     camadaRica(),
		BackdropURL: &local,
	})
	eqPtr(t, "backdrop local", comLocal.BackdropURL, local)

	semLocal := Resolver(Camadas{
		Entity:     entRica(),
		Cadeia:     []string{"pt-BR"},
		LangPedido: "pt-BR",
		Externo:    camadaRica(),
	})
	eqPtr(t, "backdrop canonico", semLocal.BackdropURL, "https://img/backdrop.jpg")
}

// TestResolverCertificacaoFallbackUS: idioma sem região (en) cai na região US.
func TestResolverCertificacaoFallbackUS(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"en"},
		LangPedido:      "en",
		IdiomaPrincipal: "en",
		Externo:         camadaRica(),
	})
	eqPtr(t, "certification US", got.Certification, "R")
}

// TestResolverSemExternoCamposRicosVazios: sem camada externa, os campos ricos saem
// vazios/nil e as listas como `[]` (nunca null) — o front não quebra.
func TestResolverSemExternoCamposRicosVazios(t *testing.T) {
	got := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
	})
	if got.Tagline != nil || got.Runtime != nil || got.Certification != nil || got.Collection != nil {
		t.Errorf("esperava campos ricos nil sem externo, veio tagline=%v runtime=%v cert=%v col=%v",
			got.Tagline, got.Runtime, got.Certification, got.Collection)
	}
	if got.Videos == nil || got.Similar == nil || got.Keywords == nil {
		t.Errorf("listas ricas devem ser [] (nao nil): videos=%v similar=%v keywords=%v",
			got.Videos, got.Similar, got.Keywords)
	}
}

// TestResolverAiringStatusSerie: a SITUAÇÃO de exibição da série (0045) é exposta
// VERBATIM do canônico externo (neutra, read-only); ausente → nil. É distinta do
// Status de CATÁLOGO (owned/cataloged) — não o contamina.
func TestResolverAiringStatusSerie(t *testing.T) {
	ent := entRica()
	ent.Type = "tv"
	ent.Status = "cataloged" // status de CATÁLOGO, distinto da situação de exibição

	comStatus := Resolver(Camadas{
		Entity:          ent,
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo: []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{
			"airing_status": "Ended",
		}}},
	})
	eqPtr(t, "airing_status", comStatus.AiringStatus, "Ended")
	eq(t, "status catalogo intacto", comStatus.Status, "cataloged")

	// Canônico rico SEM airing_status → nil (série só-TVDB sem match / filme).
	semStatus := Resolver(Camadas{
		Entity:          ent,
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
	})
	if semStatus.AiringStatus != nil {
		t.Errorf("airing_status = %v, quero nil (ausente)", *semStatus.AiringStatus)
	}
}

// TestCamposEditaveisExcluiRicos: os campos ricos read-only (tagline/keywords/
// airing_status) NÃO aparecem na superfície de edição por override (0016), embora
// sejam resolvidos.
func TestCamposEditaveisExcluiRicos(t *testing.T) {
	editaveis := CamposEditaveis()
	if _, ok := editaveis["tagline"]; ok {
		t.Error("tagline não deveria ser editável (soLeitura)")
	}
	if _, ok := editaveis["keywords"]; ok {
		t.Error("keywords não deveria ser editável (soLeitura)")
	}
	if _, ok := editaveis["airing_status"]; ok {
		t.Error("airing_status não deveria ser editável (soLeitura)")
	}
	// Sanidade: os editoriais de sempre continuam lá.
	if _, ok := editaveis["title"]; !ok {
		t.Error("title deveria seguir editável")
	}
}
