package tvmatch

import (
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
)

// indiceComOrdens builds an index of one season with 4 episodes numbered 1..4 in the
// official order, plus the owner order of the test.
func indiceComOrdens(ordem *tvext.OrdemDono) *Index {
	idx := NovoIndex()
	idx.PreencherOrdenacoes(map[string]any{"default_season_type": "official", "season_types": []any{"official"}})
	for i, id := range []string{"e1", "e2", "e3", "e4"} {
		idx.IndexarEpisodio(id, map[string]any{
			"numbering": map[string]any{"official": map[string]any{"season": float64(1), "number": float64(i + 1)}},
		})
	}
	idx.AplicarOrdemDoDono(ordem)
	return idx
}

// TestCasarOrdemDoDonoMesclada: one owner unit that merges three aired episodes. A file
// named S01E01 links the THREE episodes, in the declared order — the merge the providers
// cannot express.
func TestCasarOrdemDoDonoMesclada(t *testing.T) {
	ordem := &tvext.OrdemDono{Units: []tvext.UnidadeDono{
		{Season: 1, Number: 1, Episodes: []string{"e2", "e3", "e4"}},
	}}
	idx := indiceComOrdens(ordem)

	ids, ok := Casar(idx, naming.EpisodeInfo{Season: 1, Episodes: []int{1}})
	if !ok {
		t.Fatal("nao casou a unidade do dono")
	}
	quero := []string{"e2", "e3", "e4"}
	if len(ids) != 3 {
		t.Fatalf("ids = %v, quero %v (unidade mesclada)", ids, quero)
	}
	for i := range quero {
		if ids[i] != quero[i] {
			t.Fatalf("ids = %v, quero %v (ordem declarada)", ids, quero)
		}
	}
}

// TestCasarOrdemDoDonoVenceOProvider: the whole point of the owner order is that it wins
// when the provider disagrees. Official S01E01 is "e1"; the owner says the release entry
// S01E01 is "e3". The owner wins.
func TestCasarOrdemDoDonoVenceOProvider(t *testing.T) {
	ordem := &tvext.OrdemDono{Units: []tvext.UnidadeDono{{Season: 1, Number: 1, Episodes: []string{"e3"}}}}
	idx := indiceComOrdens(ordem)

	ids, ok := Casar(idx, naming.EpisodeInfo{Season: 1, Episodes: []int{1}})
	if !ok || len(ids) != 1 || ids[0] != "e3" {
		t.Fatalf("ids = %v ok=%v, quero [e3] (a ordem do dono vence a oficial)", ids, ok)
	}
}

// TestCasarSemUnidadeCaiNoProvider: the owner order only decides where it declares a
// unit; everywhere else the provider numbering still matches.
func TestCasarSemUnidadeCaiNoProvider(t *testing.T) {
	ordem := &tvext.OrdemDono{Units: []tvext.UnidadeDono{{Season: 1, Number: 1, Episodes: []string{"e2"}}}}
	idx := indiceComOrdens(ordem)

	ids, ok := Casar(idx, naming.EpisodeInfo{Season: 1, Episodes: []int{4}})
	if !ok || len(ids) != 1 || ids[0] != "e4" {
		t.Fatalf("ids = %v ok=%v, quero [e4] pela numeracao oficial", ids, ok)
	}
}

// TestCasarOrdemDoDonoRange: a range that starts on a merged unit glues the unit and the
// following episodes, keeping the order (the ordinal of the link).
func TestCasarOrdemDoDonoRange(t *testing.T) {
	ordem := &tvext.OrdemDono{Units: []tvext.UnidadeDono{
		{Season: 1, Number: 1, Episodes: []string{"e3", "e4"}},
		{Season: 1, Number: 2, Episodes: []string{"e2"}},
	}}
	idx := indiceComOrdens(ordem)

	ids, ok := Casar(idx, naming.EpisodeInfo{Season: 1, Episodes: []int{1, 2}})
	if !ok {
		t.Fatal("nao casou o range")
	}
	quero := []string{"e3", "e4", "e2"}
	if len(ids) != len(quero) {
		t.Fatalf("ids = %v, quero %v", ids, quero)
	}
	for i := range quero {
		if ids[i] != quero[i] {
			t.Fatalf("ids = %v, quero %v", ids, quero)
		}
	}
}

// TestDonoAtivo: the index knows whether it carries an owner order.
func TestDonoAtivo(t *testing.T) {
	if indiceComOrdens(nil).DonoAtivo() {
		t.Error("sem ordem do dono, DonoAtivo deveria ser false")
	}
	if !indiceComOrdens(&tvext.OrdemDono{Units: []tvext.UnidadeDono{{Season: 1, Number: 1, Episodes: []string{"e1"}}}}).DonoAtivo() {
		t.Error("com ordem do dono, DonoAtivo deveria ser true")
	}
}
