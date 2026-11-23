package job

import "testing"

// TestPlural covers the three cases that matter: one (singular), zero and many
// (plural) — pt-BR uses the plural for zero ("0 artes materializadas").
func TestPlural(t *testing.T) {
	casos := []struct {
		n        int
		singular string
		plural   string
		quer     string
	}{
		{0, "entity elegível", "entities elegíveis", "0 entities elegíveis"},
		{1, "entity elegível", "entities elegíveis", "1 entity elegível"},
		{18967, "entity elegível", "entities elegíveis", "18967 entities elegíveis"},
		{1, "tema musical baixado", "temas musicais baixados", "1 tema musical baixado"},
		{2, "tema musical baixado", "temas musicais baixados", "2 temas musicais baixados"},
	}
	for _, c := range casos {
		if got := Plural(c.n, c.singular, c.plural); got != c.quer {
			t.Errorf("Plural(%d) = %q, quero %q", c.n, got, c.quer)
		}
	}
}
