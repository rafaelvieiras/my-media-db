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
	byOrdering  map[string]map[[2]int]string
	byAbsolute  map[int]string
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
// Steps: (1) default ordering; (2) fallback to the other orderings; (3)
// absolute number against absolute_number. Range → list of ids (ordinal 0,1,2…).
// Ambiguous (different orderings → distinct episodes) or a number without a
// match → (nil,false) = review/pending.
func Casar(idx *Index, ep naming.EpisodeInfo) ([]string, bool) {
	if ep.IsAbsolute {
		return casarAbsoluto(idx, ep.Absolute)
	}

	if idx.defaultType != "" {
		if ids, ok := casarNaOrdem(idx, idx.defaultType, ep.Season, ep.Episodes); ok {
			return ids, true
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
		return nil, false
	}
	primeira := achadas[0]
	for _, a := range achadas[1:] {
		if !mesmaSequencia(primeira, a) {
			return nil, false // the same number matches distinct episodes in different orderings
		}
	}
	return primeira, true
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

// casarAbsoluto resolves ALL the absolute numbers; (nil,false) if any is
// missing.
func casarAbsoluto(idx *Index, absolutos []int) ([]string, bool) {
	out := make([]string, 0, len(absolutos))
	for _, n := range absolutos {
		id, ok := idx.byAbsolute[n]
		if !ok {
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
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
