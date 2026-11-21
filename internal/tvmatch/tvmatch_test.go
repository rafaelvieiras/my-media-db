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

// idxDaSerieAbsoluta builds an index shaped like the owner's X-Men: 76 episodes that the
// provider numbers in TWO split layouts and never marks with absolute_number. "dvd-order" is
// 13/13/25/15/10 and "official" is 13/13/19/17/14 — the folder layout of the library is
// 13/13/26/14/10, which follows the dvd one for all but one file.
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

// TestAbsolutoPelaOrdemDaSerie cobre o mapeamento que faltava (issue #104): um arquivo cujo
// nome só traz o número ABSOLUTO não tem temporada, e o provedor não preenche absolute_number
// para essa série. A ordem da série resolve o número, e a temporada da PASTA desempata qual
// ordem usar.
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
		{46, 3, "ep46", 3, false}, // o "official" poria na temporada 4; o dvd mantém na 3
		{52, 3, "ep52", 4, true},  // o único arquivo em que o dvd discorda da pasta
		{52, 4, "ep52", 4, false}, // na pasta 4 as duas concordam
		{66, 4, "ep66", 4, false},
		{76, 5, "ep76", 5, false}, // o "official" poria em S5E14; o dvd dá S5E10
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

// TestAbsolutoDoProvedorVenceAOrdem garante a precedência: quando o provedor PREENCHE
// absolute_number, ele é a âncora (ADR-0021) e a derivação pela ordem não é consultada.
func TestAbsolutoDoProvedorVenceAOrdem(t *testing.T) {
	idx := idxDaSerieAbsoluta()
	idx.IndexarEpisodio("especial-do-provedor", map[string]any{"absolute_number": float64(3)})
	ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{3}, IsAbsolute: true}, 1, "")
	if !ok || len(ids) != 1 || ids[0] != "especial-do-provedor" {
		t.Fatalf("absoluto 3 = %v (%v), quero especial-do-provedor (o absolute_number do provedor vence)", ids, ok)
	}
}

// TestAbsolutoSemOrdemConfiavelNaoCasa garante o outro lado: uma ordem com BURACO na
// numeração (temporada que começa no episódio 5) é descartada, porque contar posições sobre
// uma grade furada deslocaria todos os números seguintes. Melhor mandar para a revisão.
func TestAbsolutoSemOrdemConfiavelNaoCasa(t *testing.T) {
	idx := NovoIndex()
	idx.IndexarEpisodio("a", map[string]any{"numbering": map[string]any{"official": map[string]any{"season": float64(1), "number": float64(1)}}})
	idx.IndexarEpisodio("b", map[string]any{"numbering": map[string]any{"official": map[string]any{"season": float64(2), "number": float64(5)}}})
	idx.IndexarEpisodio("c", map[string]any{"numbering": map[string]any{"official": map[string]any{"season": float64(2), "number": float64(6)}}})
	if ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{2}, IsAbsolute: true}, 2, ""); ok {
		t.Fatalf("absoluto 2 = %v, quero não casar (temporada 2 começa no episódio 5)", ids)
	}
}

// TestTituloDoArquivoVenceAFilaDaPasta cobre a licao medida em producao: a pasta diz de qual
// BLOCO de episodios o arquivo e, mas nao QUAL episodio e. Na biblioteca do dono os nomes dos
// arquivos seguem a ordem "disney" (74 de 76 nomes batem), enquanto o "dvd-order" casa melhor
// com a CONTAGEM das pastas e aponta para o episodio errado ("X-Men - 46 - A Volta do
// Juggernaut" caiu em "Um Trato com o Diabo"). O titulo que o arquivo declara e o voto que
// desempata; a pasta so decide quando o voto e inconclusivo.
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

	// Sem titulo, a prioridade escolhe o "dvd-order": posicao 2 = z.
	if ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{2}, IsAbsolute: true}, 0, ""); !ok || ids[0] != "z" {
		t.Fatalf("sem titulo, absoluto 2 = %v (%v), quero z (prioridade dvd-order)", ids, ok)
	}
	// Com o titulo que o arquivo declara, o voto resolve para y.
	if ids, _, ok := CasarComPasta(idx, naming.EpisodeInfo{Absolute: []int{2}, IsAbsolute: true}, 0, "Serie - 02 - Segredos Revelados"); !ok || ids[0] != "y" {
		t.Fatalf("com titulo, absoluto 2 = %v (%v), quero y (o titulo desempata)", ids, ok)
	}
}
