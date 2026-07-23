// Testes do navegador de pastas (briefing 0071). Fica no pacote interno
// library (não library_test) porque exercita browse() diretamente, com um
// limite de corte pequeno injetado, para testar o truncamento sem precisar
// criar milhares de diretórios reais.
package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBrowseRaizDefault(t *testing.T) {
	got, err := browse(context.Background(), "", maxEntradasBrowse)
	if err != nil {
		t.Fatalf("browse(\"\"): %v", err)
	}
	if got.Path != "/" {
		t.Errorf("path = %q, quero /", got.Path)
	}
	if got.Parent != nil {
		t.Errorf("parent = %v, quero nil (raiz)", *got.Parent)
	}
}

func TestBrowseListaSubpastas(t *testing.T) {
	dir := t.TempDir()
	for _, nome := range []string{"zebra", "Abacate", "melancia"} {
		if err := os.Mkdir(filepath.Join(dir, nome), 0o755); err != nil {
			t.Fatalf("Mkdir %q: %v", nome, err)
		}
	}
	// Um arquivo comum: não deve aparecer em Entries.
	if err := os.WriteFile(filepath.Join(dir, "arquivo.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := browse(context.Background(), dir, maxEntradasBrowse)
	if err != nil {
		t.Fatalf("browse(%q): %v", dir, err)
	}
	if got.Path != dir {
		t.Errorf("path = %q, quero %q", got.Path, dir)
	}
	if got.Truncated {
		t.Error("truncated = true, quero false")
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries = %d, quero 3 (arquivo comum deve ser ignorado): %+v", len(got.Entries), got.Entries)
	}
	// Ordenação alfabética case-insensitive: Abacate, melancia, zebra.
	quero := []string{"Abacate", "melancia", "zebra"}
	for i, e := range got.Entries {
		if e.Name != quero[i] {
			t.Errorf("entries[%d].Name = %q, quero %q (ordem case-insensitive)", i, e.Name, quero[i])
		}
		if !e.Readable {
			t.Errorf("entries[%d] (%q) readable = false, quero true", i, e.Name)
		}
		if e.Path != filepath.Join(dir, e.Name) {
			t.Errorf("entries[%d].Path = %q, quero %q", i, e.Path, filepath.Join(dir, e.Name))
		}
	}

	// Parent aponta um nível acima, não-nil (dir não é raiz).
	if got.Parent == nil {
		t.Fatal("parent = nil, quero não-nil (dir não é raiz)")
	}
	if *got.Parent != filepath.Dir(dir) {
		t.Errorf("parent = %q, quero %q", *got.Parent, filepath.Dir(dir))
	}
}

func TestBrowseParentNilNaRaiz(t *testing.T) {
	got, err := browse(context.Background(), "/", maxEntradasBrowse)
	if err != nil {
		t.Fatalf("browse(\"/\"): %v", err)
	}
	if got.Parent != nil {
		t.Errorf("parent = %v, quero nil", *got.Parent)
	}
}

func TestBrowsePathInexistente(t *testing.T) {
	inexistente := filepath.Join(t.TempDir(), "nao-existe")
	_, err := browse(context.Background(), inexistente, maxEntradasBrowse)
	if !errors.Is(err, ErrPathInexistente) {
		t.Fatalf("err = %v, quero ErrPathInexistente", err)
	}
}

func TestBrowsePathNaoDiretorio(t *testing.T) {
	arquivo := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(arquivo, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := browse(context.Background(), arquivo, maxEntradasBrowse)
	if !errors.Is(err, ErrNaoDiretorio) {
		t.Fatalf("err = %v, quero ErrNaoDiretorio", err)
	}
}

// TestBrowseEntradaSemPermissao cobre readable:false numa entrada filha sem
// permissão de leitura. Pulado quando o processo roda como root (dentro de
// container Docker, por exemplo): chmod 0000 não bloqueia leitura para o
// dono/root, então o teste não conseguiria simular a condição de forma
// confiável (mesma guarda já usada em TestPostLibrarySemPermissao,
// internal/server/library_test.go).
func TestBrowseEntradaSemPermissao(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("rodando como root: chmod 0000 não bloqueia leitura para o dono; não dá para simular sem permissão de forma confiável")
	}
	dir := t.TempDir()
	semPermissao := filepath.Join(dir, "sem-permissao")
	if err := os.Mkdir(semPermissao, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(semPermissao, 0o000); err != nil {
		t.Fatalf("Chmod 0000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(semPermissao, 0o755) }) // restaura p/ o TempDir limpar

	got, err := browse(context.Background(), dir, maxEntradasBrowse)
	if err != nil {
		t.Fatalf("browse(%q): %v", dir, err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, quero 1: %+v", len(got.Entries), got.Entries)
	}
	if got.Entries[0].Readable {
		t.Error("readable = true, quero false (sem permissão)")
	}
	if got.Entries[0].Name != "sem-permissao" {
		t.Errorf("name = %q, quero sem-permissao", got.Entries[0].Name)
	}
}

// TestBrowseTruncamento cobre o cap de segurança: com um limite pequeno
// injetado (3), um diretório com mais subpastas do que o limite vem cortado
// nas primeiras N (ordem alfabética) e truncated:true.
func TestBrowseTruncamento(t *testing.T) {
	dir := t.TempDir()
	nomes := []string{"a", "b", "c", "d", "e"}
	for _, nome := range nomes {
		if err := os.Mkdir(filepath.Join(dir, nome), 0o755); err != nil {
			t.Fatalf("Mkdir %q: %v", nome, err)
		}
	}

	const limite = 3
	got, err := browse(context.Background(), dir, limite)
	if err != nil {
		t.Fatalf("browse(%q): %v", dir, err)
	}
	if !got.Truncated {
		t.Error("truncated = false, quero true")
	}
	if len(got.Entries) != limite {
		t.Fatalf("entries = %d, quero %d", len(got.Entries), limite)
	}
	quero := []string{"a", "b", "c"}
	for i, e := range got.Entries {
		if e.Name != quero[i] {
			t.Errorf("entries[%d].Name = %q, quero %q", i, e.Name, quero[i])
		}
	}
}

// TestBrowseSemTruncamentoAbaixoDoLimite garante que truncated fica false
// quando a contagem de entradas é exatamente igual (ou menor) ao limite.
func TestBrowseSemTruncamentoAbaixoDoLimite(t *testing.T) {
	dir := t.TempDir()
	for _, nome := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(dir, nome), 0o755); err != nil {
			t.Fatalf("Mkdir %q: %v", nome, err)
		}
	}

	got, err := browse(context.Background(), dir, 3)
	if err != nil {
		t.Fatalf("browse(%q): %v", dir, err)
	}
	if got.Truncated {
		t.Error("truncated = true, quero false (contagem == limite)")
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries = %d, quero 3", len(got.Entries))
	}
}
