// The OWNER ORDER in the matcher (ADR-0033). The owner order is the only ordering in
// which SEVERAL episodes can share the same (season, number) — that is exactly what a
// merged release entry is. So it cannot live in byOrdering (one id per pair, the shape
// that makes a plain order unambiguous); it gets its own map from the pair to the
// ORDERED list of episode ids, which is also the `ordinal` of the file link.
//
// Precedence: owner → default order → the other orders → absolute. When the owner
// declared an order, that is the truth about the release the owner owns, so it is
// tried first and never cross-checked against the provider orders (the whole reason it
// exists is that they disagree).
package tvmatch

import (
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
)

// AplicarOrdemDoDono inserts the owner units into the index. A nil/empty order is a
// no-op (the normal state: most series have no owner order). The units are keyed by
// (season, number); each one keeps the ids in the declared order.
func (idx *Index) AplicarOrdemDoDono(ordem *tvext.OrdemDono) {
	if ordem.Vazia() {
		return
	}
	if idx.byDono == nil {
		idx.byDono = map[[2]int][]string{}
	}
	for _, u := range ordem.Units {
		chave := [2]int{u.Season, u.Number}
		// Two units with the same (season, number) would be a contradiction: the
		// writer refuses it; if it happens anyway, the first one wins.
		if _, existe := idx.byDono[chave]; existe {
			continue
		}
		ids := make([]string, 0, len(u.Episodes))
		ids = append(ids, u.Episodes...)
		idx.byDono[chave] = ids
	}
}

// casarNaOrdemDoDono resolves the (season, number) of the file in the owner order,
// gluing the units of a range in ordinal order. (nil,false) when there is no unit for
// the pair — the caller falls through to the provider orders.
func casarNaOrdemDoDono(idx *Index, season int, numeros []int) ([]string, bool) {
	if len(idx.byDono) == 0 {
		return nil, false
	}
	var out []string
	for _, n := range numeros {
		ids, ok := idx.byDono[[2]int{season, n}]
		if !ok {
			return nil, false
		}
		out = append(out, ids...)
	}
	return out, true
}

// DonoAtivo tells whether the index carries an owner order (the readers use it to
// decide whether to show the order in the selector).
func (idx *Index) DonoAtivo() bool {
	return len(idx.byDono) > 0
}

// casarDono is the owner step of Casar, kept apart so the precedence stays readable.
func casarDono(idx *Index, ep naming.EpisodeInfo) ([]string, bool) {
	if ep.IsAbsolute {
		return nil, false // absolute numbering is a provider concept, not the owner's
	}
	return casarNaOrdemDoDono(idx, ep.Season, ep.Episodes)
}
