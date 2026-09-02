package integration

import (
	"reflect"
	"testing"
)

// descFake é um Descriptor mínimo para os testes deste pacote.
type descFake struct {
	slug        string
	nome        string
	descricao   string
	campos      []Campo
	configurada bool
}

func (d descFake) Slug() string      { return d.slug }
func (d descFake) Nome() string      { return d.nome }
func (d descFake) Descricao() string { return d.descricao }
func (d descFake) Campos() []Campo   { return d.campos }
func (d descFake) Configurada(map[string]string) bool {
	return d.configurada
}

// TestRegistryZeroValueUtilizavel prova que um Registry não inicializado
// (zero-value) funciona sem panicar — mesmo contrato de provider.Registry.
func TestRegistryZeroValueUtilizavel(t *testing.T) {
	var r Registry
	if !r.Vazio() {
		t.Error("Registry zero-value deveria estar vazio")
	}
	if _, ok := r.Get("tmdb"); ok {
		t.Error("Get num Registry vazio deveria devolver ok=false")
	}
	if got := r.All(); len(got) != 0 {
		t.Errorf("All() num Registry vazio = %v, quero vazio", got)
	}
	if got := r.Nomes(); len(got) != 0 {
		t.Errorf("Nomes() num Registry vazio = %v, quero vazio", got)
	}
}

// TestRegistrarEGet prova que Registrar + Get funcionam pelo Slug().
func TestRegistrarEGet(t *testing.T) {
	var r Registry
	r.Registrar(descFake{slug: "tmdb", nome: "TMDB"})

	d, ok := r.Get("tmdb")
	if !ok {
		t.Fatal("Get(tmdb) deveria achar")
	}
	if d.Nome() != "TMDB" {
		t.Errorf("Nome() = %q, quero TMDB", d.Nome())
	}
	if r.Vazio() {
		t.Error("Registry não deveria estar vazio após Registrar")
	}
}

// TestRegistrarNilIgnorado prova que Registrar(nil) não quebra nem some ser
// contado — mesmo contrato de provider.Registry.Registrar.
func TestRegistrarNilIgnorado(t *testing.T) {
	var r Registry
	r.Registrar(nil)
	if !r.Vazio() {
		t.Error("Registrar(nil) não deveria contar como registrado")
	}
}

// TestUltimoARegistrarVence prova que registrar duas vezes sob o MESMO slug
// substitui, mas preserva a posição na ordem — mesmo contrato de
// provider.Registry.
func TestUltimoARegistrarVence(t *testing.T) {
	var r Registry
	r.Registrar(descFake{slug: "tmdb", nome: "TMDB v1"})
	r.Registrar(descFake{slug: "tvdb", nome: "TVDB"})
	r.Registrar(descFake{slug: "tmdb", nome: "TMDB v2"})

	d, ok := r.Get("tmdb")
	if !ok || d.Nome() != "TMDB v2" {
		t.Fatalf("Get(tmdb) = %+v, ok=%v; quero TMDB v2", d, ok)
	}
	if got := r.Nomes(); !reflect.DeepEqual(got, []string{"tmdb", "tvdb"}) {
		t.Errorf("Nomes() = %v, quero [tmdb tvdb] (ordem de 1ª aparição preservada)", got)
	}
}

// TestAllOrdemDeterministica prova que All() devolve na ordem de registro.
func TestAllOrdemDeterministica(t *testing.T) {
	r := NovaRegistry(
		descFake{slug: "b", nome: "B"},
		descFake{slug: "a", nome: "A"},
	)
	all := r.All()
	if len(all) != 2 || all[0].Slug() != "b" || all[1].Slug() != "a" {
		t.Errorf("All() fora de ordem: %+v", all)
	}
}

// TestNomesECopiaDefensiva prova que mutar o slice devolvido por Nomes() não
// afeta o Registry interno.
func TestNomesECopiaDefensiva(t *testing.T) {
	r := NovaRegistry(descFake{slug: "tmdb"})
	nomes := r.Nomes()
	nomes[0] = "mutado"
	if got, _ := r.Get("tmdb"); got == nil {
		t.Fatal("mutar Nomes() não deveria afetar o Registry")
	}
	if r.Nomes()[0] != "tmdb" {
		t.Error("Nomes() deveria continuar devolvendo tmdb após mutação externa")
	}
}
