// Package tvmatch concentrates the INDEXING and the NUMBER matching of the
// series tree (ADR-0021), extracted from internal/scanner/serie.go into a LEAF
// package without I/O (the same pattern as internal/matching): it builds an
// in-memory number→episode_id map from the extension_data (namespace core.tv@1)
// and matches the EpisodeInfo of a file name against that map.
//
// It is a neutral package because internal/scanner already imports
// internal/enrich (it cannot invert the dependency) and internal/enrich needs
// the SAME matching logic (re-matching files stuck in a series before it had a
// tree) — without duplicating the ~100 lines of indexing/sorting/absolute
// numbering between the two packages.
package tvmatch

import (
	"sort"

	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
)

// Index is the IN-MEMORY number→episode_id map of a series, built once from the
// extension_data of the episodes. byOrdering matches (ordering, season, number);
// byAbsolute matches the absolute number (anime). defaultType is the default
// ordering of the series; orderings are all the orderings it uses.
type Index struct {
	byOrdering map[string]map[[2]int]string
	byAbsolute map[int]string
	// byDono is the OWNER order (ADR-0033): the pair maps to the ORDERED list of
	// episode ids, because a merged release entry puts several episodes on the same
	// (season, number). Nil/empty when the owner declared no order.
	byDono      map[[2]int][]string
	defaultType string
	orderings   []string
}

// NovoIndex returns an empty Index, ready for PreencherOrdenacoes/
// IndexarEpisodio.
func NovoIndex() *Index {
	return &Index{byOrdering: map[string]map[[2]int]string{}, byAbsolute: map[int]string{}}
}

// PreencherOrdenacoes reads the default ordering and the list of season_types
// from the core.tv@1 namespace of the series and writes them into the index.
// Absence of any field is silent degradation (an index without an explicit
// ordering).
func (idx *Index) PreencherOrdenacoes(ns map[string]any) {
	if s, ok := ns["default_season_type"].(string); ok {
		idx.defaultType = s
	}
	if tipos, ok := ns["season_types"].([]any); ok {
		for _, o := range tipos {
			if s, ok := o.(string); ok {
				idx.orderings = append(idx.orderings, s)
			}
		}
	}
}

// IndexarEpisodio inserts ONE episode into the index from its extension_data
// (namespace core.tv@1, already extracted by the caller through
// tvext.Namespace): each (ordering, season, number) and the absolute number
// point to ITS id (the anchor, ADR-0021).
func (idx *Index) IndexarEpisodio(epID string, ns map[string]any) {
	if numbering, ok := ns["numbering"].(map[string]any); ok {
		for ordem, v := range numbering {
			par, ok := v.(map[string]any)
			if !ok {
				continue
			}
			season, sOK := tvext.AsInt(par["season"])
			number, nOK := tvext.AsInt(par["number"])
			if !sOK || !nOK {
				continue
			}
			if idx.byOrdering[ordem] == nil {
				idx.byOrdering[ordem] = map[[2]int]string{}
			}
			idx.byOrdering[ordem][[2]int{season, number}] = epID
		}
	}
	if abs, ok := tvext.AsInt(ns["absolute_number"]); ok {
		idx.byAbsolute[abs] = epID
	}
}

// Casar matches the EpisodeInfo of the file against the episode ids, in order.
// Steps: (0) owner order, when the owner declared one; (1) default ordering; (2)
// fallback to the other orderings; (3) absolute number, first against the provider's
// absolute_number and then through the series' ORDERINGS (CasarComPasta).
// Range → list of ids (ordinal 0,1,2…). Ambiguous (different orderings → distinct
// episodes) or a number without a match → (nil,false) = review/pending.
func Casar(idx *Index, ep naming.EpisodeInfo) ([]string, bool) {
	ids, _, ok := CasarComPasta(idx, ep, 0)
	return ids, ok
}

// CasarComPasta is Casar with the SEASON OF THE FOLDER as a hint: an absolute number has no
// season of its own, and the folder says which block of episodes the file belongs to. The
// ordering that puts the absolute number in that season wins; when none does, the fixed
// priority decides and temporadaResolvida tells the caller which season it landed in, so the
// disagreement with the folder can be reported instead of silently accepted (measured: 1 of
// the 76 files of the owner's X-Men, which the folder keeps in season 3 while the DVD order
// puts in season 4).
//
// temporadaResolvida is 0 when the number came from the provider's own absolute_number (that
// field carries no season) or when nothing resolved.
func CasarComPasta(idx *Index, ep naming.EpisodeInfo, temporadaDaPasta int) (ids []string, temporadaResolvida int, ok bool) {
	// 0) The owner order wins over every provider order: it exists exactly because
	// the release the owner has does not follow the provider (ADR-0033).
	if ids, ok := casarDono(idx, ep); ok {
		return ids, 0, true
	}
	if ep.IsAbsolute {
		return casarAbsoluto(idx, ep.Absolute, temporadaDaPasta)
	}

	if idx.defaultType != "" {
		if ids, ok := casarNaOrdem(idx, idx.defaultType, ep.Season, ep.Episodes); ok {
			return ids, 0, true
		}
	}

	// Fallback: try EACH other ordering present. Collect the complete resolutions;
	// if all of them point to the SAME sequence of episodes, there is no real
	// ambiguity.
	var achadas [][]string
	for _, ord := range ordensFallback(idx) {
		if ids, ok := casarNaOrdem(idx, ord, ep.Season, ep.Episodes); ok {
			achadas = append(achadas, ids)
		}
	}
	if len(achadas) == 0 {
		return nil, 0, false
	}
	primeira := achadas[0]
	for _, a := range achadas[1:] {
		if !mesmaSequencia(primeira, a) {
			return nil, 0, false // the same number matches distinct episodes in different orderings
		}
	}
	return primeira, 0, true
}

// ordensFallback returns the orderings to try in the fallback (all but the
// default), in a stable order. It uses the union of season_types with the keys
// actually present in the index (a series may number in an ordering it did not
// list in season_types).
func ordensFallback(idx *Index) []string {
	visto := map[string]bool{idx.defaultType: true}
	var out []string
	add := func(o string) {
		if !visto[o] {
			visto[o] = true
			out = append(out, o)
		}
	}
	for _, o := range idx.orderings {
		add(o)
	}
	presentes := make([]string, 0, len(idx.byOrdering))
	for o := range idx.byOrdering {
		presentes = append(presentes, o)
	}
	sort.Strings(presentes)
	for _, o := range presentes {
		add(o)
	}
	return out
}

// casarNaOrdem resolves ALL the episodes of a season in a given ordering;
// (nil,false) if any number is missing (no partial link in this ordering).
func casarNaOrdem(idx *Index, ordem string, season int, numeros []int) ([]string, bool) {
	m, ok := idx.byOrdering[ordem]
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(numeros))
	for _, n := range numeros {
		id, ok := m[[2]int{season, n}]
		if !ok {
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
}

// casarAbsoluto resolves ALL the absolute numbers; (nil,false) if any is missing. The
// provider's absolute_number wins when it exists; otherwise the number goes through the
// series' orderings (resolverAbsoluto), which is what makes a release numbered only by
// absolute position ("X-Men - 27 - …") reach the real episode instead of becoming a draft.
func casarAbsoluto(idx *Index, absolutos []int, temporadaDaPasta int) ([]string, int, bool) {
	out := make([]string, 0, len(absolutos))
	temporada := 0
	for _, n := range absolutos {
		if id, ok := idx.byAbsolute[n]; ok {
			out = append(out, id)
			continue
		}
		ep, ok := idx.resolverAbsoluto(n, temporadaDaPasta)
		if !ok {
			return nil, 0, false
		}
		if temporada == 0 {
			temporada = ep.season
		}
		out = append(out, ep.id)
	}
	return out, temporada, true
}

// mesmaSequencia tells whether two lists of ids are identical in content and
// order.
func mesmaSequencia(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// prioridadeDeOrdem ranks the orderings when more than one of them can serve an absolute
// number.
//
// The split of a folder in a library comes from a RELEASE (a disc set, an iTunes season), not
// from the airing schedule. Measured on the owner's X-Men: the folder split is 13/13/26/14/10,
// which follows the TMDB "dvd-order" (13/13/25/15/10) for 75 of the 76 files and the "official"
// one (13/13/19/17/14) for only 65. So the release orderings come first and official comes
// last; alphabetical among the release orderings keeps the choice deterministic.
func prioridadeDeOrdem(ordem string) int {
	switch ordem {
	case "dvd-order":
		return 0
	case "itunes-order":
		return 1
	case "disney":
		return 2
	case "production-order":
		return 3
	case "official":
		return 9
	default:
		return 5
	}
}

// absolutoDerivado is one absolute number resolved through an ordering.
type absolutoDerivado struct {
	id     string
	season int
}

// absolutoDaOrdem builds the ABSOLUTE number → episode map of ONE ordering by flattening it in
// (season, number) order. It returns false when the ordering cannot produce a trustworthy
// numbering: a hole inside a season (season 2 starting at 5 would shift every number after
// it), a single episode, or nothing outside season 0.
//
// Season 0 (specials) stays OUT: the absolute numbering of a release does not count them.
func (idx *Index) absolutoDaOrdem(ordem string) (map[int]absolutoDerivado, bool) {
	m, ok := idx.byOrdering[ordem]
	if !ok {
		return nil, false
	}
	type par struct {
		season, number int
		id             string
	}
	pares := make([]par, 0, len(m))
	for chave, id := range m {
		if chave[0] == 0 {
			continue
		}
		pares = append(pares, par{season: chave[0], number: chave[1], id: id})
	}
	if len(pares) < 2 {
		return nil, false
	}
	sort.Slice(pares, func(i, j int) bool {
		if pares[i].season != pares[j].season {
			return pares[i].season < pares[j].season
		}
		return pares[i].number < pares[j].number
	})
	out := make(map[int]absolutoDerivado, len(pares))
	esperado := 1
	for i, p := range pares {
		if p.number != esperado {
			return nil, false // hole inside the season: the counting would be a lie
		}
		out[i+1] = absolutoDerivado{id: p.id, season: p.season}
		if i+1 < len(pares) && pares[i+1].season == p.season {
			esperado = p.number + 1
		} else {
			esperado = 1
		}
	}
	return out, true
}

// ordensPorPrioridade returns the orderings present in the index in the sequence the derived
// absolute tries them.
func (idx *Index) ordensPorPrioridade() []string {
	out := make([]string, 0, len(idx.byOrdering))
	for o := range idx.byOrdering {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := prioridadeDeOrdem(out[i]), prioridadeDeOrdem(out[j])
		if pi != pj {
			return pi < pj
		}
		return out[i] < out[j]
	})
	return out
}

// resolverAbsoluto resolves ONE absolute number through the orderings. The ordering that puts
// the number in the season of the FOLDER wins (the folder is the anchor: it says which block of
// episodes the file belongs to); when no ordering agrees with the folder, the fixed priority
// decides and the divergence is reported to the caller.
func (idx *Index) resolverAbsoluto(n, temporadaDaPasta int) (absolutoDerivado, bool) {
	var reserva absolutoDerivado
	temReserva := false
	for _, ordem := range idx.ordensPorPrioridade() {
		m, ok := idx.absolutoDaOrdem(ordem)
		if !ok {
			continue
		}
		ep, ok := m[n]
		if !ok {
			continue
		}
		if temporadaDaPasta == 0 || ep.season == temporadaDaPasta {
			return ep, true
		}
		if !temReserva {
			reserva, temReserva = ep, true
		}
	}
	if temReserva {
		return reserva, true
	}
	return absolutoDerivado{}, false
}
