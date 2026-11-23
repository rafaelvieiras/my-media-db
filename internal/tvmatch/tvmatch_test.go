package tvmatch

import (
	"fmt"
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

// idxDaSerieAbsoluta builds an index shaped like a series with 76 files: the provider numbers
// it in TWO split layouts and never marks absolute_number. "dvd-order" is 13/13/25/15/10 and
// "official" is 13/13/19/17/14 — the folder layout of the library is 13/13/26/14/10, which
// follows the dvd one for all but one file.
func idxDaSerieAbsoluta() *Index {
	idx := NovoIndex()
	idx.PreencherOrdenacoes(map[string]any{
		"default_season_type": "official",
		"season_types":        []any{"official", "dvd-order"},
	})
	dividir := func(tamanhos []int) [][2]int {
		var out [][2]int
		for season, n := range tamanhos {
			for numero := 1; numero <= n; numero++ {
				out = append(out, [2]int{season + 1, numero})
			}
		}
		return out
	}
	dvd := dividir([]int{13, 13, 25, 15, 10})
	oficial := dividir([]int{13, 13, 19, 17, 14})
	for i := range dvd {
		idx.IndexarEpisodio(fmt.Sprintf("ep%02d", i+1), map[string]any{
			"numbering": map[string]any{
				"official":  map[string]any{"season": float64(oficial[i][0]), "number": float64(oficial[i][1])},
				"dvd-order": map[string]any{"season": float64(dvd[i][0]), "number": float64(dvd[i][1])},
			},
		})
	}
	return idx
}

// TestAbsolutoPelaOrdemDaSerie covers the mapping that was missing (issue #104): a file whose
// name only carries the ABSOLUTE number has no season, and the provider does not fill
// absolute_number for that series. The order of the series resolves the number, and the
// season of the FOLDER breaks the tie on which order to use.
func TestAbsolutoPelaOrdemDaSerie(t *testing.T) {
	idx := idxDaSerieAbsoluta()
	casos := []struct {
		absoluto       int
		pasta          int
		querID         string
		querTemporada  int
		divergeDaPasta bool
	}{
		{1, 1, "ep01", 1, false},
		{14, 2, "ep14", 2, false},
		{27, 3, "ep27", 3, false},
		{46, 3, "ep46", 3, false}, // "official" would put it in season 4; dvd keeps it in 3
		{52, 3, "ep52", 4, true},  // the only file where dvd disagrees with the folder
		{52, 4, "ep52", 4, false}, // in folder 4 both agree
		{66, 4, "ep66", 4, false},
		{76, 5, "ep76", 5, false}, // "official" would put it in S5E14; dvd gives S5E10
	}
	for _, c := range casos {
		ids, temporada, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{c.absoluto}, IsAbsolute: true}, c.pasta, "")
		if !ok {
			t.Errorf("absoluto %d (pasta %d) não casou", c.absoluto, c.pasta)
			continue
		}
		if len(ids) != 1 || ids[0] != c.querID {
			t.Errorf("absoluto %d (pasta %d) = %v, quero %s", c.absoluto, c.pasta, ids, c.querID)
		}
		if temporada != c.querTemporada {
			t.Errorf("absoluto %d (pasta %d) resolveu na temporada %d, quero %d", c.absoluto, c.pasta, temporada, c.querTemporada)
		}
		if diverge := c.pasta != 0 && temporada != c.pasta; diverge != c.divergeDaPasta {
			t.Errorf("absoluto %d (pasta %d): divergência = %v, quero %v", c.absoluto, c.pasta, diverge, c.divergeDaPasta)
		}
	}
}

// TestAbsolutoDoProvedorVenceAOrdem guarantees the precedence: when the provider FILLS
// absolute_number, it is the anchor (ADR-0021) and the derivation through the order is not
// consulted.
func TestAbsolutoDoProvedorVenceAOrdem(t *testing.T) {
	idx := idxDaSerieAbsoluta()
	idx.IndexarEpisodio("especial-do-provedor", map[string]any{"absolute_number": float64(3)})
	ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{3}, IsAbsolute: true}, 1, "")
	if !ok || len(ids) != 1 || ids[0] != "especial-do-provedor" {
		t.Fatalf("absoluto 3 = %v (%v), quero especial-do-provedor (o absolute_number do provedor vence)", ids, ok)
	}
}

// TestAbsolutoSemOrdemConfiavelNaoCasa guarantees the other side: an order with a HOLE in the
// numbering (a season that starts at episode 5) is discarded, because counting positions over
// a holed grid would shift every following number. Better to send it to review.
func TestAbsolutoSemOrdemConfiavelNaoCasa(t *testing.T) {
	idx := NovoIndex()
	idx.IndexarEpisodio("a", map[string]any{"numbering": map[string]any{"official": map[string]any{"season": float64(1), "number": float64(1)}}})
	idx.IndexarEpisodio("b", map[string]any{"numbering": map[string]any{"official": map[string]any{"season": float64(2), "number": float64(5)}}})
	idx.IndexarEpisodio("c", map[string]any{"numbering": map[string]any{"official": map[string]any{"season": float64(2), "number": float64(6)}}})
	if ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{2}, IsAbsolute: true}, 2, ""); ok {
		t.Fatalf("absoluto 2 = %v, quero não casar (temporada 2 começa no episódio 5)", ids)
	}
}

// TestTituloDoArquivoVenceAFilaDaPasta covers the lesson measured in production: the folder
// says which BLOCK of episodes the file is in, but not WHICH episode it is. In the library the
// file names follow the "disney" order (74 of 76 names match), while the "dvd-order" fits the
// folder COUNT better and points at the wrong episode (a file stating one title landed on a
// different one). The title the file states is the vote that breaks the
// tie; the folder only decides when the vote is inconclusive.
func TestTituloDoArquivoVenceAFilaDaPasta(t *testing.T) {
	idx := NovoIndex()
	idx.PreencherOrdenacoes(map[string]any{"default_season_type": "official"})
	idx.IndexarEpisodioComTitulo("x", "A Volta do Jugger", map[string]any{"numbering": map[string]any{
		"official":  map[string]any{"season": float64(1), "number": float64(1)},
		"dvd-order": map[string]any{"season": float64(1), "number": float64(1)}}})
	idx.IndexarEpisodioComTitulo("y", "Segredos Revelados", map[string]any{"numbering": map[string]any{
		"official":  map[string]any{"season": float64(1), "number": float64(2)},
		"dvd-order": map[string]any{"season": float64(2), "number": float64(1)}}})
	idx.IndexarEpisodioComTitulo("z", "Um Trato com o Diabo", map[string]any{"numbering": map[string]any{
		"official":  map[string]any{"season": float64(2), "number": float64(1)},
		"dvd-order": map[string]any{"season": float64(1), "number": float64(2)}}})

	// Without a title, the priority chooses the "dvd-order": position 2 = z.
	if ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{2}, IsAbsolute: true}, 0, ""); !ok || ids[0] != "z" {
		t.Fatalf("sem titulo, absoluto 2 = %v (%v), quero z (prioridade dvd-order)", ids, ok)
	}
	// With the title the file states, the vote resolves to y.
	if ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{2}, IsAbsolute: true}, 0, "Serie - 02 - Segredos Revelados"); !ok || ids[0] != "y" {
		t.Fatalf("com titulo, absoluto 2 = %v (%v), quero y (o titulo desempata)", ids, ok)
	}
}

// TestAbsolutoComTituloQueNaoExisteVaiParaRevisao covers the dangerous tie-break: the file
// states a title and NO candidate shares a word with it. Falling back to the priority is how
// two files ended up on the SAME episode in production (a name that exists in no provider
// list stole the episode of a real one). Review, not a guess.
func TestAbsolutoComTituloQueNaoExisteVaiParaRevisao(t *testing.T) {
	idx := NovoIndex()
	idx.IndexarEpisodioComTitulo("a", "O Valor de um Homem (Parte 1)", map[string]any{"numbering": map[string]any{
		"official": map[string]any{"season": float64(4), "number": float64(1)}}})
	idx.IndexarEpisodioComTitulo("b", "Proteus (Parte 1)", map[string]any{"numbering": map[string]any{
		"official": map[string]any{"season": float64(4), "number": float64(2)}}})
	if ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{53}, IsAbsolute: true}, 4, "X-Men - 53 - Um Mutante dos Diabos"); ok {
		t.Fatalf("absoluto 53 = %v, quero não casar (nenhum episódio tem esse título)", ids)
	}
	// Without a declared title the file cannot vote: the folder keeps deciding.
	if ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{2}, IsAbsolute: true}, 4, "X-Men - 02"); !ok || ids[0] != "b" {
		t.Fatalf("sem titulo, absoluto 2 = %v (%v), quero b pela ordem", ids, ok)
	}
}
