package matching

import (
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/textsim"
)

// Tests migrated from internal/server/automatch_test.go (TestPontuarCandidato_*)
// when the scoring was extracted into this package — same
// expectations, only the symbol changed from pontuarCandidato (private) to
// Pontuar (exported).

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

// TestPontuarComAlternativos_AlternativoBateMelhorQueOTituloPrincipal covers the
// central case: a file "A Pantera Cor-De-Rosa" must
// score high even when the MAIN title of the candidate is "The Pink
// Panther" (untranslated), because the list of alternatives has the translation.
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

// TestPontuarComAlternativos_TituloPrincipalJaEOMelhor guarantees that the winner
// returned is the main title itself when no alternative scores
// better — the caller uses that equality to decide whether to expose matched_title.
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

// TestPontuarComAlternativos_ListaVaziaDegradaParaTituloUnico guarantees that an
// empty list of alternatives (or one with only empty strings) behaves like
// plain Pontuar, with no panic and no bias.
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
// guarantees that the year rule (diff>1 discards) still holds even when
// an alternative would match the title perfectly — the year is decisive before the
// textual comparison matters.
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

// ── Normalization of work names ──────────────────────────────────────────────

// TestNormalizarNome covers the REAL cases measured in the owner's catalog (08/Nov/
// 2026): the albums that fell into a draft because of the edition qualifier in
// the tag, and the ones that must NOT be touched (a qualifier that changes the work, or a name
// without any qualifier).
func TestNormalizarNome(t *testing.T) {
	casos := []struct{ entrada, quero string }{
		{"Hybrid Theory (Bonus Edition)", "Hybrid Theory"},
		{"Rio (Collector's Edition)", "Rio"},
		{"The Singles (Expanded)", "The Singles"},
		{"Notorious (Deluxe Edition)", "Notorious"},
		{"Notorious (1984)", "Notorious"},
		{"The Hurting (Deluxe Edition)", "The Hurting"},
		{"Hunting High and Low (Deluxe Edition)", "Hunting High and Low"},
		{"Songs From The Big Chair (Deluxe)", "Songs From The Big Chair"},
		{"Right Here, Right Now (EP)", "Right Here, Right Now"},
		{"Alright (Remastered)", "Alright"},
		{"Question And Answer (Album Version)", "Question And Answer"},
		{"The Fat of the Land - Expanded Edition", "The Fat of the Land"},
		{"Twilight Original Motion Picture Soundtrack (International Special Edition)", "Twilight Original Motion Picture Soundtrack"},
		{"Emergency on Planet Earth (Remastered)", "Emergency on Planet Earth"},
		// VERSION LABEL of the SAME recording (the segment ends in a
		// version word and has no differentiator) -- the soundtrack case, which
		// alone accounted for 20 of the 37 tracks left over in a draft.
		{"Full Moon (Twilight Soundtrack Version)", "Full Moon"},
		{"Spotlight (Twilight Soundtrack Version)", "Spotlight"},
		{"Love Is Worth The Fall (Twilight Version)", "Love Is Worth The Fall"},
		{"The Prisoner (Original 7\" Version)", "The Prisoner"},
		{"Pale Shelter (2nd single version)", "Pale Shelter"},
		{"Change (Radio Edit)", "Change (Radio Edit)"},
		{"Ideas As Opiates (Alternate Version)", "Ideas As Opiates"},
		// preserved: a qualifier that changes the work (ANOTHER recording), an album without
		// a qualifier, and a name that would become empty (fallback to the original).
		{"Papercut (Live at BBC1)", "Papercut (Live at BBC1)"},
		{"Full Moon (Appleblim & Komonazmuk Remix)", "Full Moon (Appleblim & Komonazmuk Remix)"},
		{"Suffer The Children (Demo)", "Suffer The Children (Demo)"},
		{"Take On Me (2017 Acoustic)", "Take On Me (2017 Acoustic)"},
		{"Notorious (Extended Version)", "Notorious (Extended Version)"},
		{"Unplugged (Live)", "Unplugged (Live)"},
		{"Goodbye Country (Hello Nightclub)", "Goodbye Country (Hello Nightclub)"},
		{"Born to Be Alive (The Original)", "Born to Be Alive (The Original)"},
		{"Summer Songs", "Summer Songs"},
		{"(Deluxe)", "(Deluxe)"},
		{"", ""},
	}
	for _, c := range casos {
		if got := NormalizarNome(c.entrada); got != c.quero {
			t.Errorf("NormalizarNome(%q) = %q, quero %q", c.entrada, got, c.quero)
		}
	}
}

// TestSimilaridadeNormalizadaPassaDoLimiar locks in the practical effect: the pairs that
// today stay BELOW the auto-match threshold now clear it (>= 92), without names
// of different works starting to match.
func TestSimilaridadeNormalizadaPassaDoLimiar(t *testing.T) {
	pares := [][2]string{
		{"Hybrid Theory (Bonus Edition)", "Hybrid Theory"},
		{"Rio (Collector's Edition)", "Rio"},
		{"The Singles (Expanded)", "The Singles"},
		{"Alright (Remastered)", "Alright"},
		{"Question And Answer (Album Version)", "Question And Answer"},
	}
	for _, par := range pares {
		if crua := textsim.Similaridade(par[0], par[1]) * 100; crua >= LimiarAutoMatch {
			t.Fatalf("par de teste inválido: %q e %q já casavam cru (%.1f)", par[0], par[1], crua)
		}
		if got := SimilaridadeNormalizada(par[0], par[1]); got < LimiarAutoMatch {
			t.Errorf("SimilaridadeNormalizada(%q, %q) = %.1f, quero >= %.1f", par[0], par[1], got, LimiarAutoMatch)
		}
	}

	diferentes := [][2]string{
		{"The Hurting (Deluxe Edition)", "Songs From The Big Chair"},
		{"Hybrid Theory (Bonus Edition)", "Meteora"},
		{"Alright (Remastered)", "Alright Already"},
	}
	for _, par := range diferentes {
		if got := SimilaridadeNormalizada(par[0], par[1]); got >= LimiarAutoMatch {
			t.Errorf("obras diferentes passaram a casar: %q x %q = %.1f", par[0], par[1], got)
		}
	}
}

// TestArtigoNaoEOutroTitulo proves the article tolerance: a catalog writes "O Clube do Terror"
// and the folder writes "Clube do Terror". Raw Jaro-Winkler gives 82.75 for that pair — 9 points
// below LimiarAutoMatch — which is how the correct show lost to a same-name remake with an exact
// title (ADR-0046).
func TestArtigoNaoEOutroTitulo(t *testing.T) {
	if bruto := textsim.Similaridade("Clube do Terror", "O Clube do Terror") * 100; bruto >= LimiarAutoMatch {
		t.Fatalf("o par sem artigo já pontua %.2f: o teste não prova nada", bruto)
	}
	score, _, descartado := PontuarComAlternativosTolerancia("Clube do Terror", 0, "O Clube do Terror", nil, 0, ToleranciaAnoSerie)
	if descartado || score < LimiarAutoMatch {
		t.Fatalf("score = %.2f (descartado=%v), quero >= %.0f: o artigo não é parte do nome", score, descartado, LimiarAutoMatch)
	}
	// O artigo não salva um título DIFERENTE.
	if score, _, _ := PontuarComAlternativosTolerancia("Clube do Terror", 0, "O Clube das Bruxas", nil, 0, ToleranciaAnoSerie); score >= LimiarAutoMatch {
		t.Fatalf("score = %.2f para outro nome: o artigo está engolindo a diferença", score)
	}
}

// TestToleranciaDeAnoDaSerie: the 2-year slack of a series absorbs an imprecise folder year and
// still keeps a remake out; the movie slack stays at 1.
func TestToleranciaDeAnoDaSerie(t *testing.T) {
	// Clube do Terror (1990) vs the 1992 show: passa na série, cai no filme.
	if _, _, descartado := PontuarComAlternativosTolerancia("Clube do Terror", 1990, "O Clube do Terror", nil, 1992, ToleranciaAnoSerie); descartado {
		t.Fatal("2 anos de diferença descartou o candidato da série")
	}
	if _, _, descartado := PontuarComAlternativos("Clube do Terror", 1990, "O Clube do Terror", nil, 1992); !descartado {
		t.Fatal("o filme perdeu a tolerância de 1 ano")
	}
	// O remake, 29 anos depois, cai nas duas.
	if _, _, descartado := PontuarComAlternativosTolerancia("Clube do Terror", 1990, "Clube do Terror", nil, 2019, ToleranciaAnoSerie); !descartado {
		t.Fatal("o reboot de 2019 sobreviveu à tolerância da série")
	}
}
