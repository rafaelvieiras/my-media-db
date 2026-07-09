package resolver

import (
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Testes da COMPOSIÇÃO POR IDIOMA (briefing 0019 / ADR-0019). As camadas são
// injetadas direto no motor, com a Cadeia já derivada — o mesmo contrato que o
// handler monta a partir das settings.

// entIdioma é uma entity de núcleo com título/descrição/tags de arquivo (base
// neutra) e um idioma original, reutilizada pelos testes de idioma.
func entIdioma() sqlcgen.Entity {
	return sqlcgen.Entity{
		ID:               "x",
		Type:             "movie",
		Title:            "Titulo Arquivo",
		Description:      ns("Descricao Arquivo"),
		Status:           "cataloged",
		TagsJson:         ns(`["arq-a","arq-b"]`),
		OriginalLanguage: ns("ja"),
	}
}

// externoMapa monta uma camada externa cujo campo é um mapa idioma→valor (o
// formato do futuro 0020).
func externoMapa(campo string, porIdioma map[string]any) []CamadaExterna {
	return []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{campo: porIdioma}}}
}

// TestCadeiaFallbackOrdem cobre §5: pedido → fallback → original → arquivo, na
// ordem, escolhendo a PRIMEIRA etapa com valor e registrando a proveniência.
func TestCadeiaFallbackOrdem(t *testing.T) {
	cadeia := []string{"pt-BR", "en", "ja"} // pedido, fallback, original

	// (1) pedido tem valor → vence.
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("title", map[string]any{"pt-BR": "PT", "en": "EN", "ja": "JA"}),
	})
	eq(t, "title(pedido)", got.Title, "PT")
	eq(t, "prov title(pedido)", got.ResolvedLanguages["title"], "pt-BR")
	eq(t, "lang eco", got.Lang, "pt-BR")
	eqPtr(t, "original_language", got.OriginalLanguage, "ja")

	// (2) sem pedido → cai no fallback en.
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("title", map[string]any{"en": "EN", "ja": "JA"}),
	})
	eq(t, "title(fallback)", got.Title, "EN")
	eq(t, "prov title(fallback)", got.ResolvedLanguages["title"], "en")

	// (3) sem pedido nem fallback → cai no original (ja).
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("title", map[string]any{"ja": "JA"}),
	})
	eq(t, "title(original)", got.Title, "JA")
	eq(t, "prov title(original)", got.ResolvedLanguages["title"], "ja")

	// (4) nenhum idioma da cadeia → base arquivo (neutra), proveniência "".
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("title", map[string]any{"xx": "XX"}),
	})
	eq(t, "title(arquivo)", got.Title, "Titulo Arquivo")
	eq(t, "prov title(arquivo)", got.ResolvedLanguages["title"], "")
}

// TestNullPorIdiomaApagaEInterrompe cobre §5/R6: override null no idioma pedido
// apaga o campo e NÃO cai no fallback; chave ausente segue a cadeia.
func TestNullPorIdiomaApagaEInterrompe(t *testing.T) {
	cadeia := []string{"pt-BR", "en"}

	// null em pt-BR apaga e interrompe: NÃO usa o "EN" do externo.
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo:  externoMapa("description", map[string]any{"en": "EN"}),
		Override: ov(map[string]string{"description": `{"pt-BR": null}`}),
	})
	if got.Description != nil {
		t.Errorf("description = %v, quero nil (apagado por null em pt-BR)", *got.Description)
	}
	eq(t, "prov description(null)", got.ResolvedLanguages["description"], "pt-BR")

	// chave ausente em pt-BR (override só tem en): segue a cadeia e usa o externo pt-BR.
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo:  externoMapa("description", map[string]any{"pt-BR": "PT"}),
		Override: ov(map[string]string{"description": `{"en": "OV-EN"}`}),
	})
	eqPtr(t, "description(ausente segue cadeia)", got.Description, "PT")
	eq(t, "prov description(cadeia)", got.ResolvedLanguages["description"], "pt-BR")
}

// TestOverrideVenceExternoNoIdioma cobre a precedência override ⟵ externo DENTRO do
// idioma: no fallback en, o override en vence o externo en.
func TestOverrideVenceExternoNoIdioma(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo:  externoMapa("title", map[string]any{"en": "EXT-EN"}),
		Override: ov(map[string]string{"title": `{"en": "OV-EN"}`}),
	})
	eq(t, "title", got.Title, "OV-EN")
	eq(t, "prov title", got.ResolvedLanguages["title"], "en")
}

// TestTagsMergePorIdioma cobre §5/R3 por idioma: o merge {add,remove} do override é
// aplicado sobre a base externa DAQUELE idioma.
func TestTagsMergePorIdioma(t *testing.T) {
	// pt-BR: base ext [a,b]; remove a, add c → [b,c].
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo:  externoMapa("tags", map[string]any{"pt-BR": []any{"a", "b"}, "en": []any{"x"}}),
		Override: ov(map[string]string{"tags": `{"pt-BR": {"add":["c"],"remove":["a"]}}`}),
	})
	want := []string{"b", "c"}
	if len(got.Tags) != len(want) || got.Tags[0] != "b" || got.Tags[1] != "c" {
		t.Fatalf("tags = %v, quero %v", got.Tags, want)
	}
	eq(t, "prov tags", got.ResolvedLanguages["tags"], "pt-BR")

	// Sem pt-BR (nem ext nem override): cai no fallback en, usando o externo en.
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("tags", map[string]any{"en": []any{"x", "y"}}),
	})
	if len(got.Tags) != 2 || got.Tags[0] != "x" || got.Tags[1] != "y" {
		t.Fatalf("tags(fallback) = %v, quero [x y]", got.Tags)
	}
	eq(t, "prov tags(fallback)", got.ResolvedLanguages["tags"], "en")
}

// TestExternoLegadoSobPrincipal cobre a REGRESSÃO do 0015: um externo escalar/lista
// SIMPLES (sem chave de idioma) é tratado como valor do idioma PRINCIPAL.
func TestExternoLegadoSobPrincipal(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{
			"title":       "Titulo Legado",         // escalar simples (0015)
			"description": "Sinopse Legada",        // escalar simples (0015)
			"tags":        []any{"leg-1", "leg-2"}, // lista simples (0015)
		}}},
	})
	eq(t, "title legado", got.Title, "Titulo Legado")
	eq(t, "prov title legado", got.ResolvedLanguages["title"], "pt-BR")
	eqPtr(t, "description legado", got.Description, "Sinopse Legada")
	if len(got.Tags) != 2 || got.Tags[0] != "leg-1" {
		t.Errorf("tags legado = %v, quero [leg-1 leg-2]", got.Tags)
	}
}

// TestOverrideLegadoSobPrincipal cobre a tolerância a override LEGADO (formato plano
// do 0016 pré-idioma) em modo idioma: aplica-se ao principal.
func TestOverrideLegadoSobPrincipal(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Override: ov(map[string]string{
			"title": `"Titulo do Dono"`,     // escalar plano → principal
			"tags":  `{"remove":["arq-a"]}`, // operação plana → principal, base arquivo
		}),
	})
	eq(t, "title override legado", got.Title, "Titulo do Dono")
	eq(t, "prov title", got.ResolvedLanguages["title"], "pt-BR")
	// base arquivo [arq-a, arq-b], remove arq-a → [arq-b].
	if len(got.Tags) != 1 || got.Tags[0] != "arq-b" {
		t.Errorf("tags = %v, quero [arq-b]", got.Tags)
	}
}

// TestSemCadeiaPreserva0013 cobre §5: sem contexto de idioma (Cadeia vazia), a
// composição é exatamente a do 0013 e resolved_languages sai vazio (não-nil).
func TestSemCadeiaPreserva0013(t *testing.T) {
	got := Resolver(Camadas{
		Entity:  entIdioma(),
		Externo: []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{"title": "Externo 0013"}}},
	})
	eq(t, "title", got.Title, "Externo 0013") // externo escalar como no 0013
	eq(t, "lang vazio", got.Lang, "")
	if got.ResolvedLanguages == nil {
		t.Error("resolved_languages nil, quero mapa vazio não-nil")
	}
	eq(t, "len(resolved_languages)", len(got.ResolvedLanguages), 0)
}
