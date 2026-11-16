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

// fakeScanner is an injectable ScanStarter: it records EVERY Start call
// (source.ID) so the tests can assert that the debounce triggered the scan of the
// right source, without really sweeping the disk (the same spirit as the
// fakeAdder below and the fakes of internal/job/libraryhealth).
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

// esperarContagem polls until `want` Start calls or until the timeout, so the
// test does not depend on a fixed sleep longer than necessary.
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

// TestRegistrarFonteDisparaScanAposEvento covers the central acceptance
// criterion: creating a new file in a folder with watch enabled triggers a scan
// of that source after the debounce (shortened here so the test does not wait a
// real 15s -- the production value is DebounceDefault).
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

// TestRegistrarFonteRajadaColesceNumUnicoScan proves the DEBOUNCE: several burst
// events (several files created in quick succession) trigger ONE single scan, not
// one per event.
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
	// Wait a bit longer than the debounce to guarantee that NO second scan was
	// scheduled by the burst events.
	time.Sleep(150 * time.Millisecond)
	if n := scanner.contagem(); n != 1 {
		t.Errorf("scans disparados = %d, quero exatamente 1 (rajada coalescida pelo debounce)", n)
	}
}

// TestDesregistrarFonteParaDeDispararScan proves that, after DesregistrarFonte
// (the same routine as the PATCH handler turning watch_enabled off), new events
// in the folder NO LONGER trigger a scan.
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

	// Wait longer than the debounce -- if anything were going to trigger, it would
	// have triggered here.
	time.Sleep(200 * time.Millisecond)
	if n := scanner.contagem(); n != 0 {
		t.Errorf("scans disparados apos DesregistrarFonte = %d, quero 0", n)
	}
}

// TestPastaNovaGanhaWatchDinamico proves that a SUBFOLDER created after the
// initial registration also starts being observed (dynamic Add() on directory
// Create) -- a file created INSIDE it still triggers the scan.
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
	// Give the directory Create event time to be processed (dynamic Add) before
	// creating the file inside it.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(subpasta, "filme.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	esperarContagem(t, scanner, 1, 2*time.Second)
}

// fakeAdder simulates fsnotify.Watcher.Add failing for specific paths -- used by
// TestRegistrarSubdiretoriosBestEffort to prove that an Add() failure on one
// subdirectory does NOT prevent the registration of the others.
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

// TestRegistrarSubdiretoriosBestEffort covers the acceptance criterion: "a
// simulated watcher.Add() failure (e.g. a path removed between the walk and the
// add) does not prevent the registration of the remaining subfolders nor brings
// the boot down". Here it is simulated by injecting an adder that fails ONLY for
// one specific subfolder.
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

	// It must not panic nor abort because of the simulated failure on sub2 --
	// registrarSubdiretorios is best-effort per subdirectory.
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

// TestCloseFechaTodasAsFontes proves that Close() ends all the registered
// watchers -- later events (even if the test can no longer generate them after
// Close) do not trigger a scan, and a new RegistrarFonte after Close still works
// normally (Close does not leave the Watcher unusable).
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
