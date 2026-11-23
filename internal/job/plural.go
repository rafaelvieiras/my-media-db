package job

import "fmt"

// Plural renders "<n> <noun>" with the pt-BR inflection a human would write.
//
// The final message of every job lands verbatim on the queue screen (contract
// #34: it is a SERVER message, in Portuguese, never translated by the client),
// so it cannot carry the old programmer placeholder "entity(ies) elegível(is),
// processada(s), arte(s) materializada(s)" — the owner read that on production
// with the real numbers around it. Each caller passes the two whole phrases
// (singular and plural), which also covers the compound ones where every word
// agrees ("1 tema musical baixado" / "2 temas musicais baixados").
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
