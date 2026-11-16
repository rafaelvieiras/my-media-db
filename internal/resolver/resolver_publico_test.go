package resolver

import (
	"encoding/json"
	"testing"
)

// TestCamposEditaveis proves that the R1 helper exposes registroOrigem as the single
// source: the editorial keys and their labels (translatable × kind) that the server
// consumes without retyping the list.
func TestCamposEditaveis(t *testing.T) {
	campos := CamposEditaveis()

	esperado := map[string]CampoInfo{
		"title":          {Traduzivel: true, Kind: KindEscalar},
		"original_title": {Traduzivel: false, Kind: KindEscalar},
		"description":    {Traduzivel: true, Kind: KindEscalar},
		"release_date":   {Traduzivel: false, Kind: KindEscalar},
		"tags":           {Traduzivel: true, Kind: KindLista},
		"extension_data": {Traduzivel: false, Kind: KindExtensao},
		"local_rating":   {Traduzivel: false, Kind: KindNumero},
		"credits":        {Traduzivel: false, Kind: KindCreditos},
	}
	if len(campos) != len(esperado) {
		t.Fatalf("CamposEditaveis tem %d campos, quero %d", len(campos), len(esperado))
	}
	for campo, want := range esperado {
		got, ok := campos[campo]
		if !ok {
			t.Errorf("campo %q ausente em CamposEditaveis", campo)
			continue
		}
		if got != want {
			t.Errorf("%s = %+v, quero %+v", campo, got, want)
		}
	}

	// The returned copy must not mutate the internal registry.
	campos["title"] = CampoInfo{}
	if CamposEditaveis()["title"].Traduzivel != true {
		t.Error("CamposEditaveis vazou uma referência mutável do registro interno")
	}

	// A non-editorial field does not appear (status is the owner's intent, not
	// editorial).
	if _, ok := campos["status"]; ok {
		t.Error("status não deveria ser editorial")
	}
}

// TestMapaIdiomaDoOverride covers the normalization used in the 2-level merge:
// already per language (copy), legacy scalar → {principal}, legacy list
// {add,remove} → {principal}, and the discard of a non-translatable field.
func TestMapaIdiomaDoOverride(t *testing.T) {
	// Already in the per-language format: returns the same languages.
	m := MapaIdiomaDoOverride("title", json.RawMessage(`{"pt-BR":"Duna","en":null}`), "pt-BR")
	if len(m) != 2 || string(m["pt-BR"]) != `"Duna"` || string(m["en"]) != "null" {
		t.Errorf("por idioma = %v, quero {pt-BR:\"Duna\", en:null}", m)
	}

	// Legacy scalar (no language key) → wraps it under the main language.
	m = MapaIdiomaDoOverride("title", json.RawMessage(`"Legado"`), "pt-BR")
	if len(m) != 1 || string(m["pt-BR"]) != `"Legado"` {
		t.Errorf("legado escalar = %v, quero {pt-BR:\"Legado\"}", m)
	}

	// LIST legacy {add,remove} is not a per-language map → applies under the main
	// language.
	m = MapaIdiomaDoOverride("tags", json.RawMessage(`{"add":["x"],"remove":["y"]}`), "en")
	if len(m) != 1 || string(m["en"]) != `{"add":["x"],"remove":["y"]}` {
		t.Errorf("legado lista = %v, quero {en:{add,remove}}", m)
	}

	// Non-translatable (or nonexistent) field → nil.
	if MapaIdiomaDoOverride("original_title", json.RawMessage(`"Dune"`), "pt-BR") != nil {
		t.Error("campo neutro deveria devolver nil")
	}
	if MapaIdiomaDoOverride("inexistente", json.RawMessage(`"x"`), "pt-BR") != nil {
		t.Error("campo inexistente deveria devolver nil")
	}
}
