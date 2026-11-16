package resolver

import (
	"encoding/json"
	"testing"
)

// Tests of the owner's LOCAL RATING (ADR-0012): the OWN evaluation, stored in the
// OVERRIDE layer as a neutral number (kindNumero), which COMPLEMENTS/OVERRIDES the
// source rating (vote_average) without destroying it. Clearing it (absent override
// or `null`) goes back to nil — the UI then shows the source rating.

// TestResolverNotaLocalOverride: with no override, no local rating (nil) even with
// the source rating present; with a numeric override, the owner's rating appears and
// the source one stays intact; with `null`, the local rating goes back to nil
// (clearing is non-destructive).
func TestResolverNotaLocalOverride(t *testing.T) {
	base := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
	})
	if base.LocalRating != nil {
		t.Errorf("sem override, LocalRating = %v, quero nil", base.LocalRating)
	}
	eqPtr(t, "vote_average da fonte", base.VoteAverage, 8.2)

	comNota := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
		Override:        map[string]json.RawMessage{"local_rating": json.RawMessage("9.5")},
	})
	eqPtr(t, "LocalRating do dono", comNota.LocalRating, 9.5)
	// The source rating is NOT destroyed by the local rating (distinct fields).
	eqPtr(t, "vote_average intacto", comNota.VoteAverage, 8.2)

	limpa := Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaRica(),
		Override:        map[string]json.RawMessage{"local_rating": json.RawMessage("null")},
	})
	if limpa.LocalRating != nil {
		t.Errorf("override null, LocalRating = %v, quero nil (volta a mostrar a da fonte)", limpa.LocalRating)
	}
}

// TestResolverNotaLocalZero: the rating 0 is a VALID value (do not confuse it with
// absence).
func TestResolverNotaLocalZero(t *testing.T) {
	got := Resolver(Camadas{
		Entity:     entRica(),
		Cadeia:     []string{"pt-BR"},
		LangPedido: "pt-BR",
		Override:   map[string]json.RawMessage{"local_rating": json.RawMessage("0")},
	})
	eqPtr(t, "LocalRating zero", got.LocalRating, 0)
}

// TestCamposEditaveisIncluiNotaLocal: the local rating IS editable by override (it
// enters CamposEditaveis), it is NEUTRAL (not translatable) and of the numeric
// class.
func TestCamposEditaveisIncluiNotaLocal(t *testing.T) {
	info, ok := CamposEditaveis()["local_rating"]
	if !ok {
		t.Fatal("local_rating deveria ser editável (a nota do dono é override)")
	}
	if info.Traduzivel {
		t.Error("local_rating é NEUTRA (uma nota não tem idioma)")
	}
	if info.Kind != KindNumero {
		t.Errorf("local_rating kind = %q, quero %q", info.Kind, KindNumero)
	}
}
