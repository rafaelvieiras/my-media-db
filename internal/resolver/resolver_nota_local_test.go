package resolver

import (
	"encoding/json"
	"testing"
)

// Testes da NOTA LOCAL do dono (ADR-0012): a avaliação PRÓPRIA, gravada na camada de
// OVERRIDE como número neutro (kindNumero), que COMPLEMENTA/SOBREPÕE a nota da fonte
// (vote_average) sem destruí-la. Limpar (override ausente ou `null`) volta a nil — a
// UI então mostra a nota da fonte.

// TestResolverNotaLocalOverride: sem override, sem nota local (nil) mesmo com a nota
// da fonte presente; com override numérico, a nota do dono aparece e a da fonte segue
// intacta; com `null`, a nota local volta a nil (limpar é não-destrutivo).
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
	// A nota da fonte NÃO é destruída pela nota local (campos distintos).
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

// TestResolverNotaLocalZero: a nota 0 é um valor VÁLIDO (não confundir com ausência).
func TestResolverNotaLocalZero(t *testing.T) {
	got := Resolver(Camadas{
		Entity:     entRica(),
		Cadeia:     []string{"pt-BR"},
		LangPedido: "pt-BR",
		Override:   map[string]json.RawMessage{"local_rating": json.RawMessage("0")},
	})
	eqPtr(t, "LocalRating zero", got.LocalRating, 0)
}

// TestCamposEditaveisIncluiNotaLocal: a nota local É editável por override (entra em
// CamposEditaveis), é NEUTRA (não traduzível) e da classe numérica.
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
