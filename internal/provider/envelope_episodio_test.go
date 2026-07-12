package provider

import (
	"encoding/json"
	"testing"
)

// TestMontarEMapearEpisodioRoundtrip: o envelope montado (mapas por idioma +
// original_language) volta ao mapa canônico do resolver — title/description como
// map[idioma]any e original_language escalar, com o discriminador reconhecido.
func TestMontarEMapearEpisodioRoundtrip(t *testing.T) {
	ep := EpisodioExterno{
		SourceID: "9103",
		Title:    map[string]string{"en": "Episode 103", "pt-BR": "Episodio 103"},
		Overview: map[string]string{"en": "Overview", "pt-BR": "Sinopse"},
	}
	raw, err := MontarEnvelopeEpisodio(ep, "en")
	if err != nil {
		t.Fatalf("MontarEnvelopeEpisodio: %v", err)
	}
	campos, ok := MapearEpisodio(raw)
	if !ok {
		t.Fatal("MapearEpisodio ok=false, quero true (discriminador presente)")
	}
	title, _ := campos["title"].(map[string]any)
	if title["en"] != "Episode 103" || title["pt-BR"] != "Episodio 103" {
		t.Errorf("title = %v, quero map en/pt-BR", campos["title"])
	}
	desc, _ := campos["description"].(map[string]any)
	if desc["en"] != "Overview" || desc["pt-BR"] != "Sinopse" {
		t.Errorf("description = %v, quero overview mapeado por idioma", campos["description"])
	}
	if campos["original_language"] != "en" {
		t.Errorf("original_language = %v, quero en", campos["original_language"])
	}
}

// TestMapearEpisodioSemOriginalLanguage: sem original_language o campo é omitido do
// canônico (a cadeia do resolver funciona como [lang, fallback]).
func TestMapearEpisodioSemOriginalLanguage(t *testing.T) {
	raw, err := MontarEnvelopeEpisodio(EpisodioExterno{Title: map[string]string{"en": "T"}}, "")
	if err != nil {
		t.Fatalf("MontarEnvelopeEpisodio: %v", err)
	}
	campos, ok := MapearEpisodio(raw)
	if !ok {
		t.Fatal("ok=false, quero true")
	}
	if _, existe := campos["original_language"]; existe {
		t.Errorf("original_language presente sem valor: %v", campos["original_language"])
	}
}

// TestMapearEpisodioNaoEpisodio: um payload SEM o discriminador (ex.: detalhe de série
// ou JSON qualquer) devolve ok=false SEM panic — o provider segue para o ramo de
// série/filme e o payload legado degrada gracioso.
func TestMapearEpisodioNaoEpisodio(t *testing.T) {
	casos := map[string]json.RawMessage{
		"serie":         json.RawMessage(`{"detail":{"data":{"name":"X"}}}`),
		"escalar":       json.RawMessage(`{"title":"X"}`),
		"outro_kind":    json.RawMessage(`{"entity_kind":"movie","title":{"en":"X"}}`),
		"json_invalido": json.RawMessage(`nao e json`),
		"vazio":         json.RawMessage(`{}`),
	}
	for nome, raw := range casos {
		if campos, ok := MapearEpisodio(raw); ok || campos != nil {
			t.Errorf("%s: ok=%v campos=%v, quero ok=false campos=nil", nome, ok, campos)
		}
	}
}
