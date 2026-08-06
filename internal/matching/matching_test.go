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

// TestPontuarComAlternativos_AlternativoBateMelhorQueOTituloPrincipal cobre o
// caso central do briefing 0084: um arquivo "A Pantera Cor-De-Rosa" deve
// pontuar alto mesmo quando o título PRINCIPAL do candidato é "The Pink
// Panther" (não-traduzido), porque a lista de alternativos tem a tradução.
func TestPontuarComAlternativos_AlternativoBateMelhorQueOTituloPrincipal(t *testing.T) {
	scoreUnico, _ := Pontuar("A Pantera Cor-De-Rosa", 0, "The Pink Panther", 0)
	scoreAlt, vencedor, descartado := PontuarComAlternativos(
		"A Pantera Cor-De-Rosa", 0, "The Pink Panther",
		[]string{"A Pantera Cor-De-Rosa", "La Pantera Rosa"}, 0)
	if descartado {
		t.Fatal("não deveria descartar")
	}
	if scoreAlt <= scoreUnico {
		t.Errorf("score com alternativos (%v) deveria bater o score do título único (%v)", scoreAlt, scoreUnico)
	}
	if vencedor != "A Pantera Cor-De-Rosa" {
		t.Errorf("tituloVencedor = %q, quero %q", vencedor, "A Pantera Cor-De-Rosa")
	}
	if scoreAlt != 100 {
		t.Errorf("score = %v, quero 100 (título idêntico ao alternativo)", scoreAlt)
	}
}

// TestPontuarComAlternativos_TituloPrincipalJaEOMelhor garante que o vencedor
// devolvido é o próprio título principal quando nenhum alternativo bate
// melhor — o chamador usa essa igualdade para decidir se expõe matched_title.
func TestPontuarComAlternativos_TituloPrincipalJaEOMelhor(t *testing.T) {
	score, vencedor, descartado := PontuarComAlternativos(
		"The Matrix", 1999, "The Matrix",
		[]string{"Matrix Reloaded", "A Matriz"}, 1999)
	if descartado {
		t.Fatal("não deveria descartar")
	}
	if vencedor != "The Matrix" {
		t.Errorf("tituloVencedor = %q, quero o título principal %q", vencedor, "The Matrix")
	}
	if score != 100 {
		t.Errorf("score = %v, quero 100", score)
	}
}

// TestPontuarComAlternativos_ListaVaziaDegradaParaTituloUnico garante que uma
// lista de alternativos vazia (ou só com strings vazias) se comporta como
// Pontuar puro, sem pânico nem viés.
func TestPontuarComAlternativos_ListaVaziaDegradaParaTituloUnico(t *testing.T) {
	scoreUnico, descartadoUnico := Pontuar("The Matrix", 1999, "The Matrix", 1999)
	score, vencedor, descartado := PontuarComAlternativos("The Matrix", 1999, "The Matrix", nil, 1999)
	if descartado != descartadoUnico || score != scoreUnico {
		t.Errorf("PontuarComAlternativos sem alternativos = (%v,%v), quero igual a Pontuar (%v,%v)",
			score, descartado, scoreUnico, descartadoUnico)
	}
	if vencedor != "The Matrix" {
		t.Errorf("tituloVencedor = %q, quero o título principal", vencedor)
	}

	_, vencedor2, _ := PontuarComAlternativos("The Matrix", 0, "The Matrix", []string{"", ""}, 0)
	if vencedor2 != "The Matrix" {
		t.Errorf("strings vazias na lista de alternativos não deveriam vencer: vencedor=%q", vencedor2)
	}
}

// TestPontuarComAlternativos_AnoDivergenteDescartaIndependenteDoAlternativo
// garante que a regra de ano (diff>1 descarta) continua valendo mesmo quando
// um alternativo bateria perfeitamente no título — o ano é decisivo antes da
// comparação textual importar.
func TestPontuarComAlternativos_AnoDivergenteDescartaIndependenteDoAlternativo(t *testing.T) {
	_, _, descartado := PontuarComAlternativos("The Matrix", 1999, "The Matrix", []string{"The Matrix"}, 2005)
	if !descartado {
		t.Error("diferença de anos >1 deveria descartar mesmo com alternativo idêntico")
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
