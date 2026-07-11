package resolver

import (
	"encoding/json"
	"testing"
)

// TestCamposEditaveis prova que o helper R1 expõe o registroOrigem como fonte
// única: as chaves editoriais e suas etiquetas (traduzível × kind) que o server
// consome sem redigitar a lista.
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

	// A cópia devolvida não deve mutar o registro interno.
	campos["title"] = CampoInfo{}
	if CamposEditaveis()["title"].Traduzivel != true {
		t.Error("CamposEditaveis vazou uma referência mutável do registro interno")
	}

	// Campo não editorial não aparece (status é intenção do dono, não editorial).
	if _, ok := campos["status"]; ok {
		t.Error("status não deveria ser editorial")
	}
}

// TestMapaIdiomaDoOverride cobre a normalização usada no merge de 2 níveis: já por
// idioma (cópia), legado escalar → {principal}, legado {add,remove} de lista →
// {principal}, e o descarte de campo não traduzível.
func TestMapaIdiomaDoOverride(t *testing.T) {
	// Já no formato por idioma: devolve os mesmos idiomas.
	m := MapaIdiomaDoOverride("title", json.RawMessage(`{"pt-BR":"Duna","en":null}`), "pt-BR")
	if len(m) != 2 || string(m["pt-BR"]) != `"Duna"` || string(m["en"]) != "null" {
		t.Errorf("por idioma = %v, quero {pt-BR:\"Duna\", en:null}", m)
	}

	// Legado escalar (sem chave de idioma) → embrulha sob o principal.
	m = MapaIdiomaDoOverride("title", json.RawMessage(`"Legado"`), "pt-BR")
	if len(m) != 1 || string(m["pt-BR"]) != `"Legado"` {
		t.Errorf("legado escalar = %v, quero {pt-BR:\"Legado\"}", m)
	}

	// Legado de LISTA {add,remove} não é mapa por idioma → vale sob o principal.
	m = MapaIdiomaDoOverride("tags", json.RawMessage(`{"add":["x"],"remove":["y"]}`), "en")
	if len(m) != 1 || string(m["en"]) != `{"add":["x"],"remove":["y"]}` {
		t.Errorf("legado lista = %v, quero {en:{add,remove}}", m)
	}

	// Campo não traduzível (ou inexistente) → nil.
	if MapaIdiomaDoOverride("original_title", json.RawMessage(`"Dune"`), "pt-BR") != nil {
		t.Error("campo neutro deveria devolver nil")
	}
	if MapaIdiomaDoOverride("inexistente", json.RawMessage(`"x"`), "pt-BR") != nil {
		t.Error("campo inexistente deveria devolver nil")
	}
}
