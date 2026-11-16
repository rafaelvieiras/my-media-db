// Package textsim implements PURE GO textual similarity (no new external
// dependency) for the auto-match of the review queue (ADR-0008): Jaro-Winkler
// over a normalized title (lowercase, no accents/punctuation, collapsed
// spaces). It is a pure READER, without I/O and without state.
package textsim

import (
	"strings"
	"unicode"
)

// prefixoMaximo is the cap of common prefix characters that Winkler rewards
// (classic constant of the algorithm).
const prefixoMaximo = 4

// pesoPrefixo is the `p` weight of the Winkler prefix bonus (classic constant;
// kept low enough to never exceed 1.0 — see jaroWinkler).
const pesoPrefixo = 0.1

// semAcento maps accented Latin runes (Portuguese/Spanish/French/German —
// enough coverage for a movie/series title) to the ASCII equivalent. Without
// depending on unicode/norm (outside the stdlib): the table is small and the
// domain (media titles) does not hit scripts outside Latin.
var semAcento = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i',
	'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u',
	'ý': 'y', 'ÿ': 'y',
	'ñ': 'n', 'ç': 'c',
}

// normalizar prepares a string for comparison: lowercase, no accents,
// letters/digits preserved, everything else becomes a space, spaces collapsed
// and trimmed. "The Matrix (2020)!!" and "the   matrix 2020" normalize the same.
func normalizar(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	ultimoEspaco := true // avoids a leading space
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

// Similaridade returns the Jaro-Winkler similarity between `a` and `b` in
// [0,1] — 1.0 identical (after normalization), close to 0 for completely
// different ones. Both strings are normalized internally (the caller passes raw
// text).
func Similaridade(a, b string) float64 {
	return jaroWinkler(normalizar(a), normalizar(b))
}

// jaro computes the classic Jaro similarity between two ALREADY normalized
// strings, operating per rune (safe for leftover accents/unicode outside the
// semAcento map).
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

// jaroWinkler applies the common prefix bonus (up to prefixoMaximo runes) on
// top of the classic Jaro: strings that start the same score higher, a
// deliberate property of Winkler (titles usually diverge more at the end —
// subtitle, year, edition).
func jaroWinkler(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	j := jaro(ra, rb)

	prefixo := 0
	for prefixo < prefixoMaximo && prefixo < len(ra) && prefixo < len(rb) && ra[prefixo] == rb[prefixo] {
		prefixo++
	}
	return j + float64(prefixo)*pesoPrefixo*(1-j)
}
