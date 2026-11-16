package resolver

import (
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Tests of the PER-LANGUAGE COMPOSITION (ADR-0019). The layers are injected
// straight into the engine, with the Cadeia already derived — the same contract
// that the handler builds from the settings.

// entIdioma is a core entity with file title/description/tags (neutral base) and an
// original language, reused by the language tests.
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

// externoMapa builds an external layer whose field is a language→value map (the
// per-language format).
func externoMapa(campo string, porIdioma map[string]any) []CamadaExterna {
	return []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{campo: porIdioma}}}
}

// TestCadeiaFallbackOrdem covers §5: pedido → fallback → original → arquivo, in
// order, choosing the FIRST step with a value and recording the provenance.
func TestCadeiaFallbackOrdem(t *testing.T) {
	cadeia := []string{"pt-BR", "en", "ja"} // pedido, fallback, original

	// (1) the requested one has a value → wins.
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("title", map[string]any{"pt-BR": "PT", "en": "EN", "ja": "JA"}),
	})
	eq(t, "title(pedido)", got.Title, "PT")
	eq(t, "prov title(pedido)", got.ResolvedLanguages["title"], "pt-BR")
	eq(t, "lang eco", got.Lang, "pt-BR")
	eqPtr(t, "original_language", got.OriginalLanguage, "ja")

	// (2) no value for the requested one → falls back to en.
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("title", map[string]any{"en": "EN", "ja": "JA"}),
	})
	eq(t, "title(fallback)", got.Title, "EN")
	eq(t, "prov title(fallback)", got.ResolvedLanguages["title"], "en")

	// (3) neither requested nor fallback → falls back to the original (ja).
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("title", map[string]any{"ja": "JA"}),
	})
	eq(t, "title(original)", got.Title, "JA")
	eq(t, "prov title(original)", got.ResolvedLanguages["title"], "ja")

	// (4) no language of the chain → file base (neutral), provenance "".
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("title", map[string]any{"xx": "XX"}),
	})
	eq(t, "title(arquivo)", got.Title, "Titulo Arquivo")
	eq(t, "prov title(arquivo)", got.ResolvedLanguages["title"], "")
}

// TestNullPorIdiomaApagaEInterrompe covers §5/R6: a null override in the requested
// language erases the field and does NOT fall back; an absent key follows the
// chain.
func TestNullPorIdiomaApagaEInterrompe(t *testing.T) {
	cadeia := []string{"pt-BR", "en"}

	// null in pt-BR erases and interrupts: does NOT use the external's "EN".
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo:  externoMapa("description", map[string]any{"en": "EN"}),
		Override: ov(map[string]string{"description": `{"pt-BR": null}`}),
	})
	if got.Description != nil {
		t.Errorf("description = %v, quero nil (apagado por null em pt-BR)", *got.Description)
	}
	eq(t, "prov description(null)", got.ResolvedLanguages["description"], "pt-BR")

	// the key is absent in pt-BR (the override only has en): follows the chain and
	// uses the external pt-BR.
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: cadeia, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo:  externoMapa("description", map[string]any{"pt-BR": "PT"}),
		Override: ov(map[string]string{"description": `{"en": "OV-EN"}`}),
	})
	eqPtr(t, "description(ausente segue cadeia)", got.Description, "PT")
	eq(t, "prov description(cadeia)", got.ResolvedLanguages["description"], "pt-BR")
}

// TestOverrideVenceExternoNoIdioma covers the override ⟵ externo precedence WITHIN
// the language: in the en fallback, the en override wins over the en external.
func TestOverrideVenceExternoNoIdioma(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo:  externoMapa("title", map[string]any{"en": "EXT-EN"}),
		Override: ov(map[string]string{"title": `{"en": "OV-EN"}`}),
	})
	eq(t, "title", got.Title, "OV-EN")
	eq(t, "prov title", got.ResolvedLanguages["title"], "en")
}

// TestTagsMergePorIdioma covers §5/R3 per language: the override's {add,remove}
// merge is applied over the external base of THAT language.
func TestTagsMergePorIdioma(t *testing.T) {
	// pt-BR: ext base [a,b]; remove a, add c → [b,c].
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

	// Without pt-BR (neither ext nor override): falls back to en, using the en
	// external.
	got = Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: externoMapa("tags", map[string]any{"en": []any{"x", "y"}}),
	})
	if len(got.Tags) != 2 || got.Tags[0] != "x" || got.Tags[1] != "y" {
		t.Fatalf("tags(fallback) = %v, quero [x y]", got.Tags)
	}
	eq(t, "prov tags(fallback)", got.ResolvedLanguages["tags"], "en")
}

// TestExternoLegadoSobPrincipal covers the LEGACY external REGRESSION: a SIMPLE
// scalar/list external (with no language key) is treated as a value of the MAIN
// language.
func TestExternoLegadoSobPrincipal(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Externo: []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{
			"title":       "Titulo Legado",         // plain scalar (legacy)
			"description": "Sinopse Legada",        // plain scalar (legacy)
			"tags":        []any{"leg-1", "leg-2"}, // plain list (legacy)
		}}},
	})
	eq(t, "title legado", got.Title, "Titulo Legado")
	eq(t, "prov title legado", got.ResolvedLanguages["title"], "pt-BR")
	eqPtr(t, "description legado", got.Description, "Sinopse Legada")
	if len(got.Tags) != 2 || got.Tags[0] != "leg-1" {
		t.Errorf("tags legado = %v, quero [leg-1 leg-2]", got.Tags)
	}
}

// TestOverrideLegadoSobPrincipal covers the tolerance to a LEGACY override (the flat
// pre-language format) in language mode: it applies to the main language.
func TestOverrideLegadoSobPrincipal(t *testing.T) {
	got := Resolver(Camadas{
		Entity: entIdioma(), Cadeia: []string{"pt-BR", "en"}, LangPedido: "pt-BR", IdiomaPrincipal: "pt-BR",
		Override: ov(map[string]string{
			"title": `"Titulo do Dono"`,     // flat scalar → main language
			"tags":  `{"remove":["arq-a"]}`, // flat operation → main language, file base
		}),
	})
	eq(t, "title override legado", got.Title, "Titulo do Dono")
	eq(t, "prov title", got.ResolvedLanguages["title"], "pt-BR")
	// file base [arq-a, arq-b], removes arq-a → [arq-b].
	if len(got.Tags) != 1 || got.Tags[0] != "arq-b" {
		t.Errorf("tags = %v, quero [arq-b]", got.Tags)
	}
}

// TestSemCadeiaPreserva0013 covers §5: with no language context (empty Cadeia), the
// composition is exactly the pre-multilanguage one and resolved_languages comes out
// empty (non-nil).
func TestSemCadeiaPreserva0013(t *testing.T) {
	got := Resolver(Camadas{
		Entity:  entIdioma(),
		Externo: []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{"title": "Externo 0013"}}},
	})
	eq(t, "title", got.Title, "Externo 0013") // scalar external as in the pre-multilanguage model
	eq(t, "lang vazio", got.Lang, "")
	if got.ResolvedLanguages == nil {
		t.Error("resolved_languages nil, quero mapa vazio não-nil")
	}
	eq(t, "len(resolved_languages)", len(got.ResolvedLanguages), 0)
}
