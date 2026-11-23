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
	byDono map[[2]int][]string
	// titulos is episode id → title. The title is the only evidence the FILE carries about
	// WHICH episode it is, and it is what settles an absolute number when more than one
	// ordering can serve it (see resolverAbsoluto).
	titulos     map[string]string
	defaultType string
	orderings   []string
}

// NovoIndex returns an empty Index, ready for PreencherOrdenacoes/
// IndexarEpisodio.
func NovoIndex() *Index {
	return &Index{
		byOrdering: map[string]map[[2]int]string{},
		byAbsolute: map[int]string{},
		titulos:    map[string]string{},
	}
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
	ids, _, ok := CasarComPasta(idx, ep, 0, "")
	return ids, ok
}

// CasarComPasta is Casar with the SEASON OF THE FOLDER as a hint: an absolute number has no
// season of its own, and the folder says which block of episodes the file belongs to. The
// ordering that puts the absolute number in that season wins; when none does, the fixed
// priority decides and temporadaResolvida tells the caller which season it landed in, so the
// disagreement with the folder can be reported instead of silently accepted (measured: 1 of
// 76 files of a series, which the folder keeps in season 3 while the DVD order puts in
// season 4).
//
// temporadaResolvida is 0 when the number came from the provider's own absolute_number (that
// field carries no season) or when nothing resolved.
func CasarComPasta(idx *Index, ep naming.EpisodeInfo, temporadaDaPasta int, tituloDoArquivo string) (ids []string, temporadaResolvida int, ok bool) {
	// 0) The owner order wins over every provider order: it exists exactly because
	// the release the owner has does not follow the provider (ADR-0033).
	if ids, ok := casarDono(idx, ep); ok {
		return ids, 0, true
	}
	if ep.IsAbsolute {
		return casarAbsoluto(idx, ep.Absolute, temporadaDaPasta, tituloDoArquivo)
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
// absolute position ("<series> - 27 - <title>.mkv") reach the real episode instead of becoming
// a draft.
func casarAbsoluto(idx *Index, absolutos []int, temporadaDaPasta int, titulo string) ([]string, int, bool) {
	out := make([]string, 0, len(absolutos))
	temporada := 0
	for _, n := range absolutos {
		if id, ok := idx.byAbsolute[n]; ok {
			out = append(out, id)
			continue
		}
		ep, ok := idx.resolverAbsoluto(n, temporadaDaPasta, titulo)
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

// IndexarEpisodioComTitulo indexes the episode AND remembers its title, so an absolute
// number can be resolved by the only thing the file states about the episode besides the
// number: its name.
func (idx *Index) IndexarEpisodioComTitulo(epID, titulo string, ns map[string]any) {
	idx.IndexarEpisodio(epID, ns)
	if idx.titulos == nil {
		idx.titulos = map[string]string{}
	}
	idx.titulos[epID] = titulo
}

// prioridadeDeOrdem ranks the orderings when more than one of them can serve an absolute
// number.
//
// The split of a folder in a library comes from a RELEASE (a disc set, an iTunes season), not
// from the airing schedule. Measured on a series with 76 files: the folder split is
// 13/13/26/14/10, which follows the TMDB "dvd-order" (13/13/25/15/10) for 75 of the 76 files
// and the "official" one (13/13/19/17/14) for only 65. So the release orderings come first
// and official comes last; alphabetical among the release orderings keeps the choice
// deterministic.
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

// resolverAbsoluto resolves ONE absolute number through the orderings, in this order:
//
//  1. the TITLE: an ordering whose episode at that position carries the name the FILE
//     states wins. Measured on a series with 76 files, this is the only reliable vote — the
//     files follow the "disney" order (74 of 76 names match exactly), while the "dvd-order"
//     fits the FOLDER COUNTS better and points at the wrong episodes (a file stating one
//     title landed on a different episode);
//  2. the SEASON OF THE FOLDER: the folder says which block of episodes the file belongs to;
//  3. the fixed priority (release orderings first, official last).
//
// A title vote only counts when it is strict: one candidate must score better than every
// other, otherwise the vote is inconclusive and the folder decides.
func (idx *Index) resolverAbsoluto(n, temporadaDaPasta int, titulo string) (absolutoDerivado, bool) {
	var candidatos []candidatoPorTitulo
	for _, ordem := range idx.ordensPorPrioridade() {
		m, ok := idx.absolutoDaOrdem(ordem)
		if !ok {
			continue
		}
		ep, ok := m[n]
		if !ok {
			continue
		}
		candidatos = append(candidatos, candidatoPorTitulo{ep: ep, titulo: idx.titulos[ep.id]})
	}
	if len(candidatos) == 0 {
		return absolutoDerivado{}, false
	}

	// 1) the title, when it gives a strict winner.
	if vencedor, ok := vencedorPorTitulo(candidatos, titulo); ok {
		return vencedor, true
	}
	// The file STATES a title and it matches no candidate at all: the number points at a
	// different block of the release, or the name is simply not in the provider's list. Falling
	// back to a priority here is how two files end up on ONE episode (measured: a name that
	// exists in no provider list stole the episode of a real one). Review, not a guess.
	if tituloDeclarado(titulo) && nenhumaSemelhanca(candidatos, titulo) {
		return absolutoDerivado{}, false
	}
	// 2) the season of the folder, taking the candidates in priority order.
	var reserva absolutoDerivado
	temReserva := false
	for _, c := range candidatos {
		if temporadaDaPasta == 0 || c.ep.season == temporadaDaPasta {
			return c.ep, true
		}
		if !temReserva {
			reserva, temReserva = c.ep, true
		}
	}
	if temReserva {
		return reserva, true
	}
	return absolutoDerivado{}, false
}

// candidatoPorTitulo is one candidate plus the title of its episode.
type candidatoPorTitulo struct {
	ep     absolutoDerivado
	titulo string
}

// tituloDeclarado tells whether the file states a TITLE to compare: at least two meaningful
// words. One word is the series name ("<series> - 02.mkv"), which matches nothing and would
// condemn every file with no episode title to review.
func tituloDeclarado(titulo string) bool {
	palavras := 0
	for _, p := range palavrasSignificativas(titulo) {
		if !soDigitos([]rune(p)) {
			palavras++
		}
	}
	return palavras >= 2
}

// nenhumaSemelhanca tells whether NO candidate shares a word with the file title.
func nenhumaSemelhanca(cands []candidatoPorTitulo, titulo string) bool {
	for _, c := range cands {
		if semelhancaDeTitulo(titulo, c.titulo) > 0 {
			return false
		}
	}
	return true
}

// vencedorPorTitulo returns the single candidate whose episode name best matches the name of
// the file. Inconclusive (no title, no overlap, or a tie) → false.
func vencedorPorTitulo(cands []candidatoPorTitulo, titulo string) (absolutoDerivado, bool) {
	if titulo == "" || len(cands) < 2 {
		return absolutoDerivado{}, false
	}
	melhor := -1
	melhorNota := 0.0
	empate := false
	for i, c := range cands {
		nota := semelhancaDeTitulo(titulo, c.titulo)
		switch {
		case nota > melhorNota:
			melhor, melhorNota, empate = i, nota, false
		case nota == melhorNota && nota > 0:
			// Equal notes are only a real tie when they point at DIFFERENT episodes: two
			// orderings often carry the same episode at the same position, and that is
			// agreement, not ambiguity.
			if melhor >= 0 && cands[melhor].ep.id != c.ep.id {
				empate = true
			}
		}
	}
	if melhor < 0 || melhorNota <= 0 || empate {
		return absolutoDerivado{}, false
	}
	return cands[melhor].ep, true
}

// semelhancaDeTitulo is the fraction of the meaningful words of the FILE title that also
// appear in the episode title (words of 3+ letters, case and accents folded). It is a coarse
// measure on purpose: the two titles come from different translations ("Juggernaut" vs
// "Fanático"), and the vote only needs to separate two or three candidates.
func semelhancaDeTitulo(tituloArquivo, tituloEpisodio string) float64 {
	doArquivo := palavrasSignificativas(tituloArquivo)
	if len(doArquivo) == 0 {
		return 0
	}
	doEpisodio := map[string]bool{}
	for _, p := range palavrasSignificativas(tituloEpisodio) {
		doEpisodio[p] = true
	}
	achadas := 0
	for _, p := range doArquivo {
		if doEpisodio[p] {
			achadas++
		}
	}
	return float64(achadas) / float64(len(doArquivo))
}

// palavrasSignificativas splits a title into its meaningful words: 3+ letters, folded
// (lowercase, accents removed), and DIGITS — a part number is the only thing that tells
// "O Valor de um Homem (Parte 1)" from "(Parte 2)". Shorter words and punctuation stay out.
func palavrasSignificativas(titulo string) []string {
	var out []string
	var atual []rune
	descarregar := func() {
		if len(atual) >= 3 || (len(atual) > 0 && soDigitos(atual)) {
			out = append(out, string(atual))
		}
		atual = atual[:0]
	}
	for _, r := range titulo {
		if letra, ok := dobrarLetra(r); ok {
			atual = append(atual, letra)
			continue
		}
		descarregar()
	}
	descarregar()
	return out
}

// soDigitos tells whether every rune is a digit.
func soDigitos(rs []rune) bool {
	for _, r := range rs {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// dobrarLetra returns the folded form of a letter (lowercase, without accent) and whether r is
// a letter at all.
func dobrarLetra(r rune) (rune, bool) {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return r, true
	case r >= 'A' && r <= 'Z':
		return r + ('a' - 'A'), true
	}
	if dobrada, ok := letrasDobradas[r]; ok {
		return dobrada, true
	}
	return 0, false
}

// letrasDobradas folds the accents the titles of a Portuguese library actually use.
var letrasDobradas = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
	'Á': 'a', 'À': 'a', 'Â': 'a', 'Ã': 'a', 'Ä': 'a',
	'É': 'e', 'È': 'e', 'Ê': 'e', 'Ë': 'e',
	'Í': 'i', 'Ì': 'i', 'Î': 'i', 'Ï': 'i',
	'Ó': 'o', 'Ò': 'o', 'Ô': 'o', 'Õ': 'o', 'Ö': 'o',
	'Ú': 'u', 'Ù': 'u', 'Û': 'u', 'Ü': 'u',
	'Ç': 'c', 'Ñ': 'n',
}
