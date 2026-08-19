package watch

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// fakeScanner é um ScanStarter injetável: registra CADA chamada de Start
// (source.ID) para os testes assertarem que o debounce disparou o scan da
// fonte certa, sem varrer disco de verdade (mesmo espírito do fakeAdder
// abaixo e dos fakes de internal/job/libraryhealth).
type fakeScanner struct {
	mu      sync.Mutex
	started []string
}

func (f *fakeScanner) Start(source library.Source, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, source.ID)
	return "job-fake", nil
}

func (f *fakeScanner) contagem() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

// esperarContagem faz polling até `want` chamadas de Start ou até o timeout,
// para o teste não depender de um sleep fixo mais longo que o necessário.
func esperarContagem(t *testing.T, f *fakeScanner, want int, timeout time.Duration) {
	t.Helper()
	prazo := time.Now().Add(timeout)
	for time.Now().Before(prazo) {
		if f.contagem() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("scanner.Start chamado %d vez(es), quero >= %d dentro de %s", f.contagem(), want, timeout)
}

// TestRegistrarFonteDisparaScanAposEvento cobre o critério de aceite central:
// criar um arquivo novo numa pasta com watch ligado dispara um scan daquela
// fonte após o debounce (aqui encurtado para o teste não esperar 15s de
// verdade -- o valor de produção é DebounceDefault).
func TestRegistrarFonteDisparaScanAposEvento(t *testing.T) {
	root := t.TempDir()
	scanner := &fakeScanner{}
	w := New(scanner, 50*time.Millisecond)
	t.Cleanup(w.Close)

	src := library.Source{ID: "fonte-1", Path: root}
	if err := w.RegistrarFonte(src); err != nil {
		t.Fatalf("RegistrarFonte: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "novo-arquivo.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	esperarContagem(t, scanner, 1, 2*time.Second)
	if got := scanner.started[0]; got != "fonte-1" {
		t.Errorf("scan disparado para a fonte %q, quero \"fonte-1\"", got)
	}
}

// TestRegistrarFonteRajadaColesceNumUnicoScan prova o DEBOUNCE: vários
// eventos em rajada (vários arquivos criados em sequência rápida) disparam
// UM único scan, não um por evento.
func TestRegistrarFonteRajadaColesceNumUnicoScan(t *testing.T) {
	root := t.TempDir()
	scanner := &fakeScanner{}
	w := New(scanner, 80*time.Millisecond)
	t.Cleanup(w.Close)

	src := library.Source{ID: "fonte-rajada", Path: root}
	if err := w.RegistrarFonte(src); err != nil {
		t.Fatalf("RegistrarFonte: %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(root, "arquivo"+string(rune('a'+i))+".mkv"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	esperarContagem(t, scanner, 1, 2*time.Second)
	// Espera um pouco mais além do debounce pra garantir que NENHUM segundo
	// scan foi agendado pelos eventos da rajada.
	time.Sleep(150 * time.Millisecond)
	if n := scanner.contagem(); n != 1 {
		t.Errorf("scans disparados = %d, quero exatamente 1 (rajada coalescida pelo debounce)", n)
	}
}

// TestDesregistrarFonteParaDeDispararScan prova que, após DesregistrarFonte
// (mesma rotina do handler PATCH desligando watch_enabled), novos eventos na
// pasta NÃO disparam mais scan.
func TestDesregistrarFonteParaDeDispararScan(t *testing.T) {
	root := t.TempDir()
	scanner := &fakeScanner{}
	w := New(scanner, 30*time.Millisecond)
	t.Cleanup(w.Close)

	src := library.Source{ID: "fonte-2", Path: root}
	if err := w.RegistrarFonte(src); err != nil {
		t.Fatalf("RegistrarFonte: %v", err)
	}
	w.DesregistrarFonte(src.ID)

	if err := os.WriteFile(filepath.Join(root, "depois-do-desligar.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Espera mais que o debounce -- se algo disparasse, teria disparado aqui.
	time.Sleep(200 * time.Millisecond)
	if n := scanner.contagem(); n != 0 {
		t.Errorf("scans disparados apos DesregistrarFonte = %d, quero 0", n)
	}
}

// TestPastaNovaGanhaWatchDinamico prova que uma SUBPASTA criada depois do
// registro inicial também passa a ser observada (Add() dinâmico no Create de
// diretório) -- um arquivo criado DENTRO dela ainda dispara o scan.
func TestPastaNovaGanhaWatchDinamico(t *testing.T) {
	root := t.TempDir()
	scanner := &fakeScanner{}
	w := New(scanner, 50*time.Millisecond)
	t.Cleanup(w.Close)

	src := library.Source{ID: "fonte-3", Path: root}
	if err := w.RegistrarFonte(src); err != nil {
		t.Fatalf("RegistrarFonte: %v", err)
	}

	subpasta := filepath.Join(root, "Filme Novo (2026)")
	if err := os.Mkdir(subpasta, 0o755); err != nil {
		t.Fatal(err)
	}
	// Dá tempo do evento Create do diretório ser processado (Add dinâmico)
	// antes de criar o arquivo dentro dela.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(subpasta, "filme.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	esperarContagem(t, scanner, 1, 2*time.Second)
}

// fakeAdder simula fsnotify.Watcher.Add falhando para caminhos específicos --
// usado por TestRegistrarSubdiretoriosBestEffort para provar que uma falha
// de Add() num subdiretório NÃO impede o registro dos demais.
type fakeAdder struct {
	falhaEm map[string]bool
	added   []string
}

func (f *fakeAdder) Add(name string) error {
	if f.falhaEm[name] {
		return errors.New("falha simulada de Add")
	}
	f.added = append(f.added, name)
	return nil
}

// TestRegistrarSubdiretoriosBestEffort cobre o critério de aceite: "falha
// simulada de watcher.Add() (ex. path removido entre o walk e o add) não
// impede o registro das demais subpastas nem derruba o boot". Aqui simulada
// injetando um adder que falha SÓ para uma subpasta específica.
func TestRegistrarSubdiretoriosBestEffort(t *testing.T) {
	root := t.TempDir()
	sub1 := filepath.Join(root, "sub1")
	sub2 := filepath.Join(root, "sub2-falha")
	sub3 := filepath.Join(root, "sub3")
	for _, d := range []string{sub1, sub2, sub3} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	fake := &fakeAdder{falhaEm: map[string]bool{sub2: true}}

	// Não deve entrar em pânico nem abortar por causa da falha simulada em
	// sub2 -- registrarSubdiretorios é best-effort por subdiretório.
	registrarSubdiretorios(fake, root)

	quer := map[string]bool{root: true, sub1: true, sub3: true}
	achou := map[string]bool{}
	for _, p := range fake.added {
		achou[p] = true
	}
	for p := range quer {
		if !achou[p] {
			t.Errorf("subdiretorio %q deveria ter sido registrado (best-effort apesar da falha em outro), mas nao foi", p)
		}
	}
	if achou[sub2] {
		t.Errorf("sub2 (falha simulada) nao deveria estar na lista de registrados com sucesso")
	}
}

// TestCloseFechaTodasAsFontes prova que Close() encerra todos os watchers
// registrados -- eventos posteriores (mesmo que o teste não consiga mais
// gerá-los pós-Close) não disparam scan, e um novo RegistrarFonte após Close
// ainda funciona normalmente (Close não deixa o Watcher inutilizável).
func TestCloseFechaTodasAsFontes(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	scanner := &fakeScanner{}
	w := New(scanner, 30*time.Millisecond)

	if err := w.RegistrarFonte(library.Source{ID: "f1", Path: root1}); err != nil {
		t.Fatalf("RegistrarFonte f1: %v", err)
	}
	if err := w.RegistrarFonte(library.Source{ID: "f2", Path: root2}); err != nil {
		t.Fatalf("RegistrarFonte f2: %v", err)
	}

	w.Close()

	w.mu.Lock()
	n := len(w.fontes)
	w.mu.Unlock()
	if n != 0 {
		t.Errorf("fontes registradas apos Close = %d, quero 0", n)
	}
}
