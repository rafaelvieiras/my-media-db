// Package tvmatch concentra a INDEXAÇÃO e o CASAMENTO por número da árvore de
// série (briefing 0034), extraído de internal/scanner/serie.go para um pacote
// FOLHA sem I/O (mesmo padrão do internal/matching extraído no 0054): monta um
// mapa em memória número→episode_id a partir do extension_data (namespace
// core.tv@1) e casa o EpisodeInfo de um nome de arquivo contra esse mapa.
//
// É pacote neutro porque internal/scanner já importa internal/enrich (não pode
// inverter a dependência) e o briefing 0069 precisa da MESMA lógica de
// casamento dentro de internal/enrich (recasar arquivos presos numa série
// antes dela ter árvore) — sem duplicar as ~100 linhas de indexação/ordenação/
// absoluto entre os dois pacotes.
package tvmatch

import (
	"sort"

	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
)

// Index é o mapa EM MEMÓRIA número→episode_id de uma série, montado uma vez a
// partir do extension_data dos episódios. byOrdering casa (ordenação,
// temporada, número); byAbsolute casa o número absoluto (anime). defaultType
// é a ordenação default da série; orderings são todas as ordens que ela usa.
type Index struct {
	byOrdering  map[string]map[[2]int]string
	byAbsolute  map[int]string
	defaultType string
	orderings   []string
}

// NovoIndex devolve um Index vazio, pronto para PreencherOrdenacoes/
// IndexarEpisodio.
func NovoIndex() *Index {
	return &Index{byOrdering: map[string]map[[2]int]string{}, byAbsolute: map[int]string{}}
}

// PreencherOrdenacoes lê a ordenação default e a lista de season_types do
// namespace core.tv@1 da série e as escreve no índice. Ausência de qualquer
// campo é degradação silenciosa (índice sem ordenação explícita).
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

// IndexarEpisodio insere UM episódio no índice a partir do seu extension_data
// (namespace core.tv@1, já extraído pelo chamador via tvext.Namespace): cada
// (ordenação, temporada, número) e o absoluto apontam para o SEU id (a
// âncora, ADR-0021).
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

// Casar casa o EpisodeInfo do arquivo aos ids de episódio, em ordem. Passos
// (0034 §C.4): (1) ordenação default; (2) fallback às demais ordens; (3)
// absoluto contra absolute_number. Range → lista de ids (ordinal 0,1,2…).
// Ambíguo (ordens diferentes → episódios distintos) ou número sem match →
// (nil,false) = revisão/pendente.
func Casar(idx *Index, ep naming.EpisodeInfo) ([]string, bool) {
	if ep.IsAbsolute {
		return casarAbsoluto(idx, ep.Absolute)
	}

	if idx.defaultType != "" {
		if ids, ok := casarNaOrdem(idx, idx.defaultType, ep.Season, ep.Episodes); ok {
			return ids, true
		}
	}

	// Fallback: tenta CADA outra ordenação presente. Coleta as resoluções completas;
	// se todas apontam para a MESMA sequência de episódios, não há ambiguidade real.
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
			return nil, false // mesmo número casa episódios distintos em ordens diferentes
		}
	}
	return primeira, true
}

// ordensFallback devolve as ordenações a tentar no fallback (todas menos a default),
// em ordem estável. Usa a união de season_types com as chaves realmente presentes no
// índice (uma série pode numerar numa ordem que não listou em season_types).
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

// casarNaOrdem resolve TODOS os episódios de uma temporada numa dada ordenação;
// (nil,false) se qualquer número faltar (nada de vínculo parcial nesta ordem).
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

// casarAbsoluto resolve TODOS os números absolutos; (nil,false) se algum faltar.
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

// mesmaSequencia diz se duas listas de ids são idênticas em conteúdo e ordem.
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
