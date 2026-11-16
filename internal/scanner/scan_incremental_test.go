package scanner

// SCAN INCREMENTAL tests: the scan stops re-hashing EVERY video file on EVERY
// execution -- it only re-hashes when mtime OR size diverge from the media_file
// already stored for that path (see hashEMediainfo/mediaFileInalteradoPorPath,
// scan.go). It instruments hashDoArquivoFn (the test-only indirection) to COUNT how
// many times the real hash ran -- the explicit acceptance criterion of the
// incrementality change.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// instrumentarHash replaces hashDoArquivoFn with a counter that still calls the
// real hash underneath (the test keeps exercising the real pipeline), and returns a
// pointer to the counter + the restore function (via t.Cleanup, called
// automatically).
func instrumentarHash(t *testing.T) *int {
	t.Helper()
	original := hashDoArquivoFn
	chamadas := 0
	hashDoArquivoFn = func(path string, size int64) (string, error) {
		chamadas++
		return original(path, size)
	}
	t.Cleanup(func() { hashDoArquivoFn = original })
	return &chamadas
}

// TestScanIncrementalPulaReHashSemMudanca covers the central criterion: running
// the scan twice in a row without touching the file does NOT recompute content_hash
// on the second run.
func TestScanIncrementalPulaReHashSemMudanca(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Duna (2021) [tmdbid-438631]")
	arq := filepath.Join(pasta, "Duna (2021) [tmdbid-438631] - 1080p.mkv")
	escrever(t, arq)

	chamadas := instrumentarHash(t)

	m := managerFake(t, d)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	if *chamadas != 1 {
		t.Fatalf("scan 1: hashDoArquivoFn chamado %d vez(es), quero 1 (arquivo novo)", *chamadas)
	}

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if *chamadas != 1 {
		t.Errorf("scan 2 (sem mudança no arquivo): hashDoArquivoFn total=%d, quero 1 -- scan incremental deveria pular o re-hash", *chamadas)
	}

	// The media_file stays intact (content_hash reused, not lost).
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE content_hash IS NOT NULL AND content_hash != ''"); n != 1 {
		t.Errorf("media_file com content_hash preenchido = %d, quero 1", n)
	}
}

// TestScanIncrementalReHasheiaQuandoMtimeMuda covers the second criterion:
// changing the mtime of an already indexed file (same content) makes the NEXT scan
// re-hash that file specifically.
func TestScanIncrementalReHasheiaQuandoMtimeMuda(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Arrival (2016) [tmdbid-329865]")
	arq := filepath.Join(pasta, "Arrival (2016) [tmdbid-329865] - 1080p.mkv")
	escrever(t, arq)

	chamadas := instrumentarHash(t)

	m := managerFake(t, d)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	if *chamadas != 1 {
		t.Fatalf("scan 1: hashDoArquivoFn chamado %d vez(es), quero 1", *chamadas)
	}

	// It changes only the mtime (same content/size) -- the next scan must re-hash
	// THIS file specifically.
	novoMtime := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(arq, novoMtime, novoMtime); err != nil {
		t.Fatal(err)
	}

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if *chamadas != 2 {
		t.Errorf("scan 2 (mtime mudou): hashDoArquivoFn total=%d, quero 2 (re-hash do arquivo com mtime alterado)", *chamadas)
	}
}

// Note: a third test mirroring this one for the SIZE signal (overwriting the file
// with content of another size at the SAME path, restamping the old mtime on
// purpose) was considered and removed -- it runs into a PRE-EXISTING gap orthogonal
// to the incrementality work: the scanner dedup (gravarArquivo, match.go) only finds
// the file again by the old content_hash; an in-place rewrite that CHANGES the hash
// at the same path falls into the "new file" path and collides with the UNIQUE of
// media_file.path (the old row still points to it). That behavior already existed
// before the incremental scan (it was never exercised by any previous test) --
// fixing it is out of scope here (incremental scan by mtime/size), not a decision of
// this code.
