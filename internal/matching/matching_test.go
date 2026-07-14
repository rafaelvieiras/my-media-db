package matching

import "testing"

// Testes migrados de internal/server/automatch_test.go (TestPontuarCandidato_*)
// quando a pontuação foi extraída para este pacote (briefing 0054) — mesmas
// expectativas, só o símbolo mudou de pontuarCandidato (privado) para
// Pontuar (exportado).

func TestPontuar_AnosIguaisSomamBonus(t *testing.T) {
	score, descartado := Pontuar("The Matrix", 1999, "The Matrix", 1999)
	if descartado {
		t.Fatal("não deveria descartar (anos iguais)")
	}
	if score != 100 { // 100 (identico) + 5, com cap em 100
		t.Errorf("score = %v, quero 100 (cap)", score)
	}
}

func TestPontuar_DiferencaDeUmAnoNaoAjusta(t *testing.T) {
	comAno, descartado := Pontuar("The Matrix", 1999, "The Matrix", 2000)
	if descartado {
		t.Fatal("diferença de 1 ano não deveria descartar")
	}
	semAno, _ := Pontuar("The Matrix", 0, "The Matrix", 0)
	if comAno != semAno {
		t.Errorf("diferença de 1 ano não deveria ajustar o score: comAno=%v semAno=%v", comAno, semAno)
	}
}

func TestPontuar_DiferencaDeMaisDeUmAnoDescarta(t *testing.T) {
	_, descartado := Pontuar("The Matrix", 1999, "The Matrix", 2001)
	if !descartado {
		t.Error("diferença de 2 anos deveria descartar o candidato, mesmo com título idêntico")
	}
}

func TestPontuar_AnoAusenteNaoAjustaNemDescarta(t *testing.T) {
	score, descartado := Pontuar("The Matrix", 1999, "The Matrix", 0)
	if descartado {
		t.Error("ano ausente no candidato não deveria descartar")
	}
	if score != 100 {
		t.Errorf("score = %v, quero 100 (título idêntico, sem bônus/penalidade de ano)", score)
	}
}

func TestAnoDeReleaseDate(t *testing.T) {
	casos := map[string]int{
		"1999-03-31": 1999,
		"1999":       1999,
		"":           0,
		"abc":        0,
		"19":         0,
	}
	for entrada, esperado := range casos {
		if got := AnoDeReleaseDate(entrada); got != esperado {
			t.Errorf("AnoDeReleaseDate(%q) = %v, quero %v", entrada, got, esperado)
		}
	}
}
