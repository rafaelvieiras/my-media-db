package job

import (
	"testing"
)

// handlerFake (the fake Handler shared with the Manager tests) lives in
// manager_test.go.

func TestRegistryZeroValueUtilizavel(t *testing.T) {
	var r Registry
	if _, ok := r.Get("qualquer"); ok {
		t.Fatal("Get em Registry vazio deveria devolver ok=false")
	}
	if len(r.All()) != 0 || len(r.Nomes()) != 0 {
		t.Fatal("Registry vazio deveria ter All()/Nomes() vazios")
	}
}

func TestRegistryRegistrarGetOrdem(t *testing.T) {
	var r Registry
	h1 := &handlerFake{nome: "a"}
	h2 := &handlerFake{nome: "b"}
	r.Registrar(h1)
	r.Registrar(h2)

	got, ok := r.Get("a")
	if !ok || got != Handler(h1) {
		t.Fatalf("Get(a) = %v,%v", got, ok)
	}
	if nomes := r.Nomes(); len(nomes) != 2 || nomes[0] != "a" || nomes[1] != "b" {
		t.Fatalf("Nomes() = %v, quero [a b]", nomes)
	}
	if all := r.All(); len(all) != 2 {
		t.Fatalf("All() len = %d, quero 2", len(all))
	}
}

func TestRegistryUltimoRegistroVence(t *testing.T) {
	var r Registry
	h1 := &handlerFake{nome: "a", mensagem: "primeiro"}
	h2 := &handlerFake{nome: "a", mensagem: "segundo"}
	r.Registrar(h1)
	r.Registrar(h2)

	got, ok := r.Get("a")
	if !ok {
		t.Fatal("Get(a) deveria achar")
	}
	if got != Handler(h2) {
		t.Fatal("o último a registrar deveria vencer")
	}
	// The order (first appearance) is preserved even with a replacement.
	if nomes := r.Nomes(); len(nomes) != 1 || nomes[0] != "a" {
		t.Fatalf("Nomes() = %v, quero [a] (sem duplicar por substituição)", nomes)
	}
}

func TestRegistryRegistrarNilIgnorado(t *testing.T) {
	var r Registry
	r.Registrar(nil)
	if len(r.All()) != 0 {
		t.Fatal("Registrar(nil) não deveria adicionar nada")
	}
}
