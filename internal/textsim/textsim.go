// Package textsim implementa similaridade textual PURA GO (sem dependência
// externa nova) para o auto-match da fila de revisão (briefing 0050):
// Jaro-Winkler sobre título normalizado (minúsculo, sem acento/pontuação,
// espaços colapsados). É um LEITOR puro, sem I/O e sem estado.
package textsim

import (
	"strings"
	"unicode"
)

// prefixoMaximo é o teto de caracteres de prefixo comum que o Winkler
// bonifica (constante clássica do algoritmo).
const prefixoMaximo = 4

// pesoPrefixo é o peso `p` do bônus de prefixo do Winkler (constante clássica;
// mantido baixo o bastante para nunca estourar 1.0 — ver jaroWinkler).
const pesoPrefixo = 0.1

// semAcento mapeia runas latinas acentuadas (Português/Espanhol/Francês/
// Alemão — cobertura suficiente para título de filme/série) para o
// equivalente ASCII. Sem depender de unicode/norm (fora do stdlib): a tabela é
// pequena e o domínio (títulos de mídia) não pega scripts fora do latino.
var semAcento = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i',
	'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u',
	'ý': 'y', 'ÿ': 'y',
	'ñ': 'n', 'ç': 'c',
}

// normalizar prepara uma string para comparação: minúsculas, sem acento,
// letras/dígitos preservados, o resto vira espaço, espaços colapsados e
// aparados. "The Matrix (2020)!!" e "the   matrix 2020" normalizam igual.
func normalizar(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	ultimoEspaco := true // evita espaço líder
	for _, r := range s {
		if repl, ok := semAcento[r]; ok {
			r = repl
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			ultimoEspaco = false
			continue
		}
		if !ultimoEspaco {
			b.WriteRune(' ')
			ultimoEspaco = true
		}
	}
	return strings.TrimSpace(b.String())
}

// Similaridade devolve a similaridade Jaro-Winkler entre `a` e `b` em [0,1] —
// 1.0 idênticas (após normalização), próximo de 0 completamente diferentes.
// Ambas as strings são normalizadas internamente (o chamador passa texto cru).
func Similaridade(a, b string) float64 {
	return jaroWinkler(normalizar(a), normalizar(b))
}

// jaro calcula a similaridade de Jaro clássica entre duas strings JÁ
// normalizadas, operando por runa (seguro para acentuação residual/unicode
// fora do mapa semAcento).
func jaro(a, b []rune) float64 {
	lenA, lenB := len(a), len(b)
	if lenA == 0 && lenB == 0 {
		return 1.0
	}
	if lenA == 0 || lenB == 0 {
		return 0.0
	}

	raio := max(lenA, lenB)/2 - 1
	if raio < 0 {
		raio = 0
	}

	marcadosA := make([]bool, lenA)
	marcadosB := make([]bool, lenB)
	matches := 0
	for i := range lenA {
		inicio := max(0, i-raio)
		fim := min(lenB, i+raio+1)
		for j := inicio; j < fim; j++ {
			if marcadosB[j] || a[i] != b[j] {
				continue
			}
			marcadosA[i] = true
			marcadosB[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0.0
	}

	transposicoes := 0
	k := 0
	for i := range lenA {
		if !marcadosA[i] {
			continue
		}
		for !marcadosB[k] {
			k++
		}
		if a[i] != b[k] {
			transposicoes++
		}
		k++
	}
	transposicoes /= 2

	m := float64(matches)
	return (m/float64(lenA) + m/float64(lenB) + (m-float64(transposicoes))/m) / 3.0
}

// jaroWinkler aplica o bônus de prefixo comum (até prefixoMaximo runas) sobre
// o Jaro clássico: strings que começam iguais pontuam mais alto, propriedade
// deliberada do Winkler (títulos costumam divergir mais no fim — subtítulo,
// ano, edição).
func jaroWinkler(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	j := jaro(ra, rb)

	prefixo := 0
	for prefixo < prefixoMaximo && prefixo < len(ra) && prefixo < len(rb) && ra[prefixo] == rb[prefixo] {
		prefixo++
	}
	return j + float64(prefixo)*pesoPrefixo*(1-j)
}
