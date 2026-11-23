package acoustid

import (
	"strings"
	"testing"
	"unicode"
)

// TestDescriptorTextHasNoMissingAccents guards what the owner actually sees: the
// Integrations screen renders Descricao/Ajuda verbatim, so an ASCII-only
// "acustico"/"musica"/"audio"/"confiavel" is a visible defect (the audit read
// exactly those words on production).
func TestDescriptorTextHasNoMissingAccents(t *testing.T) {
	d := NovoDescriptorIntegracao()
	textos := []string{d.Descricao()}
	for _, c := range d.Campos() {
		textos = append(textos, c.Label, c.Ajuda, c.Placeholder)
	}
	// Whole words, never substrings: "musical" is correct and must not be flagged
	// by "musica".
	proibidas := []string{"acustico", "musica", "audio", "confiavel", "aplicacao", "nao", "sao", "so"}
	for _, texto := range textos {
		for _, palavra := range palavrasDe(texto) {
			for _, proibida := range proibidas {
				if palavra == proibida {
					t.Errorf("texto %q tem %q sem acento", texto, palavra)
				}
			}
		}
	}
}

// palavrasDe lowercases and splits a text on anything that is not a letter,
// keeping the accented ones (so "não-comercial" yields "não" and "comercial").
func palavrasDe(texto string) []string {
	limpo := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, texto)
	return strings.Fields(limpo)
}
