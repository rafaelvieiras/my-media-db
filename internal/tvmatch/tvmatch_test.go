package tvmatch

import (
	"reflect"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/naming"
)

// Tests migrated from internal/scanner/serie_test.go (the ones covering the PURE
// number matching) when the logic was extracted into this package — same
// expectations, only the symbols changed (episodeIndex→Index,
// casarEpisodios→Casar).

// idxTeste builds a synthetic index: official/dvd diverge on the 3rd episode
// (official {1,3} vs dvd {1,5}), and there are absolutes 1..3. "absolute" only
// exists as an absolute number.
func idxTeste() *Index {
	return &Index{
		defaultType: "official",
		orderings:   []string{"official", "dvd", "absolute"},
		byOrdering: map[string]map[[2]int]string{
			"official": {{1, 1}: "e101", {1, 2}: "e102", {1, 3}: "e103"},
			"dvd":      {{1, 1}: "e101", {1, 2}: "e102", {1, 5}: "e103"},
		},
		byAbsolute: map[int]string{1: "e101", 2: "e102", 3: "e103"},
	}
}

func TestCasarEpisodiosDefault(t *testing.T) {
	ids, ok := Casar(idxTeste(), naming.EpisodeInfo{Season: 1, Episodes: []int{2}})
	if !ok || !reflect.DeepEqual(ids, []string{"e102"}) {
		t.Fatalf("default: got %v,%v quero [e102],true", ids, ok)
	}
}

func TestCasarEpisodiosRange(t *testing.T) {
	ids, ok := Casar(idxTeste(), naming.EpisodeInfo{Season: 1, Episodes: []int{1, 2, 3}})
	if !ok || !reflect.DeepEqual(ids, []string{"e101", "e102", "e103"}) {
		t.Fatalf("range: got %v,%v quero [e101 e102 e103],true", ids, ok)
	}
}

func TestCasarEpisodiosFallbackOrdem(t *testing.T) {
	// (1,5) does not exist in official (default) but exists in dvd → the fallback
	// resolves it.
	ids, ok := Casar(idxTeste(), naming.EpisodeInfo{Season: 1, Episodes: []int{5}})
	if !ok || !reflect.DeepEqual(ids, []string{"e103"}) {
		t.Fatalf("fallback: got %v,%v quero [e103],true", ids, ok)
	}
}

func TestCasarEpisodiosAbsoluto(t *testing.T) {
	ids, ok := Casar(idxTeste(), naming.EpisodeInfo{IsAbsolute: true, Absolute: []int{2}})
	if !ok || !reflect.DeepEqual(ids, []string{"e102"}) {
		t.Fatalf("absoluto: got %v,%v quero [e102],true", ids, ok)
	}
}

func TestCasarEpisodiosSemMatch(t *testing.T) {
	if ids, ok := Casar(idxTeste(), naming.EpisodeInfo{Season: 1, Episodes: []int{99}}); ok {
		t.Fatalf("número inexistente deveria ir para revisão, got %v", ids)
	}
	if _, ok := Casar(idxTeste(), naming.EpisodeInfo{IsAbsolute: true, Absolute: []int{99}}); ok {
		t.Fatal("absoluto inexistente deveria ir para revisão")
	}
}

// TestCasarEpisodiosAmbiguo: the same number matches DISTINCT episodes in
// different orderings (no default) → review (it does not even try a partial
// link).
func TestCasarEpisodiosAmbiguo(t *testing.T) {
	idx := &Index{
		defaultType: "official",
		orderings:   []string{"official", "a", "b"},
		byOrdering: map[string]map[[2]int]string{
			"official": {{1, 1}: "e1"}, // does not have (1,7)
			"a":        {{1, 7}: "eX"},
			"b":        {{1, 7}: "eY"}, // same number, distinct episode
		},
		byAbsolute: map[int]string{},
	}
	if ids, ok := Casar(idx, naming.EpisodeInfo{Season: 1, Episodes: []int{7}}); ok {
		t.Fatalf("ordens divergentes deveriam ser ambíguas (revisão), got %v", ids)
	}
}

// TestMesmaSequencia covers the 3 branches: equal, different lengths, different
// content.
func TestMesmaSequencia(t *testing.T) {
	if !mesmaSequencia([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("sequencias iguais deveriam casar")
	}
	if mesmaSequencia([]string{"a"}, []string{"a", "b"}) {
		t.Error("tamanhos diferentes NAO deveriam casar")
	}
	if mesmaSequencia([]string{"a", "b"}, []string{"a", "c"}) {
		t.Error("conteudo diferente NAO deveria casar")
	}
}

// TestIndexarEpisodioEPreencherOrdenacoes covers the in-memory building:
// multi-ordering numbering, absolute_number and the orderings
// (default_season_type/season_types) read from the already extracted core.tv@1
// namespace (tvext.Namespace stays out of this package).
func TestIndexarEpisodioEPreencherOrdenacoes(t *testing.T) {
	idx := NovoIndex()
	idx.PreencherOrdenacoes(map[string]any{
		"default_season_type": "official",
		"season_types":        []any{"official", "dvd"},
	})
	idx.IndexarEpisodio("e101", map[string]any{
		"numbering": map[string]any{
			"official": map[string]any{"season": float64(1), "number": float64(1)},
			"dvd":      map[string]any{"season": float64(1), "number": float64(1)},
		},
		"absolute_number": float64(1),
	})
	ids, ok := Casar(idx, naming.EpisodeInfo{Season: 1, Episodes: []int{1}})
	if !ok || !reflect.DeepEqual(ids, []string{"e101"}) {
		t.Fatalf("casar por ordenação default: got %v,%v quero [e101],true", ids, ok)
	}
	ids, ok = Casar(idx, naming.EpisodeInfo{IsAbsolute: true, Absolute: []int{1}})
	if !ok || !reflect.DeepEqual(ids, []string{"e101"}) {
		t.Fatalf("casar por absoluto: got %v,%v quero [e101],true", ids, ok)
	}
}
