package tvmatch

import (
	"reflect"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/naming"
)

// Testes migrados de internal/scanner/serie_test.go (os que cobrem o casamento
// PURO por número) quando a lógica foi extraída para este pacote (briefing
// 0069) — mesmas expectativas, só o símbolo mudou (episodeIndex→Index,
// casarEpisodios→Casar).

// idxTeste monta um índice sintético: official/dvd divergem no 3º episódio (official
// {1,3} vs dvd {1,5}), e há absolutos 1..3. "absolute" só existe como número absoluto.
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
	// (1,5) não existe em official (default) mas existe em dvd → fallback resolve.
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

// TestCasarEpisodiosAmbiguo: o mesmo número casa episódios DISTINTOS em ordens
// diferentes (sem default) → revisão (nem tenta vincular parcial).
func TestCasarEpisodiosAmbiguo(t *testing.T) {
	idx := &Index{
		defaultType: "official",
		orderings:   []string{"official", "a", "b"},
		byOrdering: map[string]map[[2]int]string{
			"official": {{1, 1}: "e1"}, // não tem (1,7)
			"a":        {{1, 7}: "eX"},
			"b":        {{1, 7}: "eY"}, // mesmo número, episódio distinto
		},
		byAbsolute: map[int]string{},
	}
	if ids, ok := Casar(idx, naming.EpisodeInfo{Season: 1, Episodes: []int{7}}); ok {
		t.Fatalf("ordens divergentes deveriam ser ambíguas (revisão), got %v", ids)
	}
}

// TestMesmaSequencia cobre os 3 ramos: iguais, tamanhos distintos, conteudo distinto.
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

// TestIndexarEpisodioEPreencherOrdenacoes cobre a montagem em memória: numbering
// multi-ordem, absolute_number e as ordenações (default_season_type/season_types)
// lidas do namespace core.tv@1 já extraído (tvext.Namespace fica fora deste pacote).
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
