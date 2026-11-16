package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// scanSyncRegistrado runs the scan synchronously -- the same path as scanSync
// (scanner_test.go). Since the unified queue, StorageNaoVideo is populated directly
// by Manager.executarScan (key = source.ID, without depending on any prior
// registration in a job map), so this helper is today only a named alias -- kept so
// as not to have to touch the call sites of this file.
func scanSyncRegistrado(t *testing.T, m *Manager, src library.Source) error {
	t.Helper()
	return scanSync(t, m, src)
}

// TestScanClassificaArquivoNaoVideoManagedEUnmanaged covers the core of the
// non-video storage classification: during the SAME walk that already detects the
// video, a poster.jpg (canonical name) is added to managed and a sample.txt (junk)
// is added to unmanaged -- queryable afterwards via Manager.StorageNaoVideo, the API
// that internal/job/libraryhealth uses.
func TestScanClassificaArquivoNaoVideoManagedEUnmanaged(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Filme Teste (2020)")
	escrever(t, filepath.Join(pasta, "Filme Teste (2020).mkv"))
	escreverTamanho(t, filepath.Join(pasta, "poster.jpg"), 777)
	escreverTamanho(t, filepath.Join(pasta, "sample.txt"), 333)

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSyncRegistrado(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	managed, unmanaged, ok := m.StorageNaoVideo(src.ID)
	if !ok {
		t.Fatal("StorageNaoVideo ok=false depois de um scan bem-sucedido")
	}
	if managed != 777 {
		t.Errorf("managedBytes = %d, quero 777 (poster.jpg)", managed)
	}
	if unmanaged != 333 {
		t.Errorf("unmanagedBytes = %d, quero 333 (sample.txt)", unmanaged)
	}
}

// TestStorageNaoVideoSemScanDevolveOkFalse proves that a source never scanned
// returns ok=false (not a lying zero).
func TestStorageNaoVideoSemScanDevolveOkFalse(t *testing.T) {
	d := abrirDB(t)
	m := managerFake(t, d)
	if _, _, ok := m.StorageNaoVideo("nunca-existiu"); ok {
		t.Fatal("ok = true para fonte nunca escaneada, quero false")
	}
}

// TestStorageNaoVideoMantemUltimoValorQuandoFonteFicaOffline proves the policy of
// the non-video storage classification (the same one used for Statfs): after a
// successful scan, if the source becomes unreachable, StorageNaoVideo keeps
// returning the LAST computed value (Start() fails BEFORE touching the job state --
// no extra logic needs to exist for that).
func TestStorageNaoVideoMantemUltimoValorQuandoFonteFicaOffline(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Filme (2020)")
	escrever(t, filepath.Join(pasta, "Filme (2020).mkv"))
	escreverTamanho(t, filepath.Join(pasta, "poster.jpg"), 111)

	m := managerFake(t, d)
	src := criarSource(t, d, root)
	if err := scanSyncRegistrado(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	managedAntes, _, _ := m.StorageNaoVideo(src.ID)
	if managedAntes != 111 {
		t.Fatalf("managedBytes antes = %d, quero 111", managedAntes)
	}

	// The source becomes unreachable -- Start() would refuse with ErrSourceOffline,
	// without touching m.jobs[src.ID].
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(src, TriggerManual); err == nil {
		t.Fatal("Start deveria falhar com a fonte removida")
	}

	managedDepois, _, ok := m.StorageNaoVideo(src.ID)
	if !ok {
		t.Fatal("ok = false depois da fonte ficar offline, quero true (mantém o último valor)")
	}
	if managedDepois != 111 {
		t.Errorf("managedBytes depois de offline = %d, quero 111 (mantido, não recalculado)", managedDepois)
	}
}

// TestExecutarScanUsaUmaUnicaPassadaDeWalkDir is the structural guard of the
// acceptance criterion "no NEW disk walk is introduced": scan.go may only have ONE
// call site to filepath.WalkDir -- coletarArquivos must classify video AND non-video
// in the SAME pass. A regression adding a second pass (e.g. a dedicated WalkDir only
// for the classification) would break this test.
func TestExecutarScanUsaUmaUnicaPassadaDeWalkDir(t *testing.T) {
	src, err := os.ReadFile("scan.go")
	if err != nil {
		t.Fatalf("ler scan.go: %v", err)
	}
	n := strings.Count(string(src), "filepath.WalkDir(")
	if n != 1 {
		t.Fatalf("scan.go chama filepath.WalkDir %d vez(es), quero exatamente 1", n)
	}
}

// escreverTamanho creates a file with exactly `n` bytes (unlike escrever, in
// scanner_test.go, which writes the path itself as content -- here the exact SIZE
// matters for the storage classification).
func escreverTamanho(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}
