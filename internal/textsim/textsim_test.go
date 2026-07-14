package textsim

import "testing"

func TestSimilaridade_Identicas(t *testing.T) {
	if got := Similaridade("The Matrix", "The Matrix"); got != 1.0 {
		t.Errorf("strings idênticas: got %v, want 1.0", got)
	}
}

func TestSimilaridade_VaziasSaoIdenticas(t *testing.T) {
	if got := Similaridade("", ""); got != 1.0 {
		t.Errorf("duas vazias: got %v, want 1.0", got)
	}
}

func TestSimilaridade_UmaVazia(t *testing.T) {
	if got := Similaridade("Matrix", ""); got != 0.0 {
		t.Errorf("uma vazia: got %v, want 0.0", got)
	}
}

func TestSimilaridade_CompletamenteDiferentes(t *testing.T) {
	got := Similaridade("abcdefgh", "zyxwvuts")
	if got > 0.15 {
		t.Errorf("strings sem letras em comum: got %v, want próximo de 0", got)
	}
}

func TestSimilaridade_PrefixoComumPesaMais(t *testing.T) {
	// Mesmo par de letras trocadas, um caso com prefixo comum e outro sem.
	comPrefixo := Similaridade("Matrxi", "Matrix") // prefixo "Matr" bate
	semPrefixo := Similaridade("xirtaM", "Matrix") // nenhum prefixo comum
	if comPrefixo <= semPrefixo {
		t.Errorf("prefixo comum deveria pesar mais: comPrefixo=%v semPrefixo=%v", comPrefixo, semPrefixo)
	}
}

func TestSimilaridade_VetorClassico(t *testing.T) {
	// Caso canônico de Jaro-Winkler (MARTHA/MARHTA): Jaro=0.944, JW=0.961.
	got := Similaridade("MARTHA", "MARHTA")
	if got < 0.95 || got > 0.97 {
		t.Errorf("vetor clássico MARTHA/MARHTA: got %v, want ~0.961", got)
	}
}

func TestSimilaridade_AcentoNaoAfeta(t *testing.T) {
	got := Similaridade("Amélie", "amelie")
	if got != 1.0 {
		t.Errorf("acento/maiúsculas não deveriam afetar: got %v, want 1.0", got)
	}
}

func TestSimilaridade_PontuacaoNaoAfeta(t *testing.T) {
	got := Similaridade("Spider-Man: No Way Home", "Spider Man No Way Home")
	if got != 1.0 {
		t.Errorf("pontuação não deveria afetar: got %v, want 1.0", got)
	}
}

func TestSimilaridade_Simetrica(t *testing.T) {
	a, b := "Blade Runner 2049", "Blade Runner"
	if got, inv := Similaridade(a, b), Similaridade(b, a); got != inv {
		t.Errorf("Similaridade deveria ser simétrica: %v != %v", got, inv)
	}
}
