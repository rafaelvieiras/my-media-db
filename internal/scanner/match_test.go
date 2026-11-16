package scanner

// Tests of the immediate MOVIE auto-match and of the optional automatic
// materialization: a new movie detected in the scan fires
// FilmeImportador.TentarAutoMatchFilme exactly once per NEW entity (never for an
// already existing entity found again); matched=true triggers the OPTIONAL
// Materializador when it is configured. Both interfaces are consumed through local
// fakes -- the real implementation (*Server) belongs to another package/agent.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// filmeImportadorFake is a test fake of FilmeImportador: it counts the calls,
// records the entityIDs received (in order) and returns a configurable fixed
// result.
type filmeImportadorFake struct {
	matched   bool
	err       error
	chamadas  int
	entityIDs []string
}

func (f *filmeImportadorFake) TentarAutoMatchFilme(_ context.Context, entityID string) (bool, error) {
	f.chamadas++
	f.entityIDs = append(f.entityIDs, entityID)
	return f.matched, f.err
}

// materializadorFake is a test fake of Materializador: it counts the calls and
// records the entityIDs received.
type materializadorFake struct {
	err       error
	chamadas  int
	entityIDs []string
}

func (m *materializadorFake) MaterializarSeAutoDownloadLigado(_ context.Context, entityID string) error {
	m.chamadas++
	m.entityIDs = append(m.entityIDs, entityID)
	return m.err
}

// TestScanFilmeNovoDisparaAutoMatchUmaVez covers the central criterion: a NEW movie
// entity created by the scan fires TentarAutoMatchFilme exactly once.
func TestScanFilmeNovoDisparaAutoMatchUmaVez(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Blade Runner 2049 (2017) [tmdbid-335984]")
	escrever(t, filepath.Join(pasta, "Blade Runner 2049 (2017) [tmdbid-335984] - 1080p.mkv"))

	fake := &filmeImportadorFake{matched: true}
	m := managerFake(t, d)
	m.ConfigurarFilmes(fake)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if fake.chamadas != 1 {
		t.Errorf("TentarAutoMatchFilme chamado %d vez(es), quero 1", fake.chamadas)
	}
}

// TestScanFilmeDuasVersoesMesmaEntityDisparaUmaSoVez covers the same criterion when
// two qualities of the SAME movie (same folder) result in a single entity (scan
// cache) -- the auto-match must not fire twice for the same new entity.
func TestScanFilmeDuasVersoesMesmaEntityDisparaUmaSoVez(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Blade Runner 2049 (2017) [tmdbid-335984]")
	escrever(t, filepath.Join(pasta, "Blade Runner 2049 (2017) [tmdbid-335984] - 2160p.mkv"))
	escrever(t, filepath.Join(pasta, "Blade Runner 2049 (2017) [tmdbid-335984] - 1080p.mkv"))

	fake := &filmeImportadorFake{matched: true}
	m := managerFake(t, d)
	m.ConfigurarFilmes(fake)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 2 {
		t.Fatalf("media_file = %d, quero 2 (duas qualidades)", n)
	}
	if fake.chamadas != 1 {
		t.Errorf("TentarAutoMatchFilme chamado %d vez(es), quero 1 (mesma entity, cache do scan)", fake.chamadas)
	}
}

// TestScanFilmeReencontradoNaoDisparaAutoMatchDeNovo covers: an ALREADY EXISTING
// entity, found again by content_hash in a re-scan (unchanged file), does NOT fire
// TentarAutoMatchFilme again -- only an entity newly created in this scan pass
// triggers the auto-match.
func TestScanFilmeReencontradoNaoDisparaAutoMatchDeNovo(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Duna (2021) [tmdbid-438631]")
	arq := filepath.Join(pasta, "Duna (2021) [tmdbid-438631] - 1080p.mkv")
	escrever(t, arq)

	fake := &filmeImportadorFake{matched: true}
	m := managerFake(t, d)
	m.ConfigurarFilmes(fake)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	if fake.chamadas != 1 {
		t.Fatalf("scan 1: TentarAutoMatchFilme chamado %d vez(es), quero 1 (entity nova)", fake.chamadas)
	}

	// Re-scan without touching the file: dedup by content_hash (reencontrar), it
	// never goes through resolverEntity/criarEntity again.
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if fake.chamadas != 1 {
		t.Errorf("scan 2 (reencontro): TentarAutoMatchFilme total=%d, quero 1 (sem disparo de novo)", fake.chamadas)
	}
}

// TestScanFilmeAutoMatchErroNaoAbortaGravacao covers: TentarAutoMatchFilme
// returning an error is best-effort -- the media_file/entity already stored remain,
// the scan neither propagates the error nor panics.
func TestScanFilmeAutoMatchErroNaoAbortaGravacao(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Duna (2021) [tmdbid-438631]")
	escrever(t, filepath.Join(pasta, "Duna (2021) [tmdbid-438631] - 1080p.mkv"))

	fake := &filmeImportadorFake{err: errors.New("provider indisponível")}
	m := managerFake(t, d)
	m.ConfigurarFilmes(fake)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan não deveria propagar o erro do auto-match: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file = %d, quero 1 (gravação preservada apesar do erro)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entity = %d, quero 1 (gravação preservada apesar do erro)", n)
	}
	if fake.chamadas != 1 {
		t.Errorf("TentarAutoMatchFilme chamado %d vez(es), quero 1", fake.chamadas)
	}
}

// TestScanFilmeMaterializaQuandoAutoMatchAcerta covers: matched=true + Materializador
// configured -> MaterializarSeAutoDownloadLigado called once with the auto-imported
// entityID.
func TestScanFilmeMaterializaQuandoAutoMatchAcerta(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Duna (2021) [tmdbid-438631]")
	escrever(t, filepath.Join(pasta, "Duna (2021) [tmdbid-438631] - 1080p.mkv"))

	imp := &filmeImportadorFake{matched: true}
	mat := &materializadorFake{}
	m := managerFake(t, d)
	m.ConfigurarFilmes(imp)
	m.ConfigurarMaterializacao(mat)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if mat.chamadas != 1 {
		t.Fatalf("MaterializarSeAutoDownloadLigado chamado %d vez(es), quero 1", mat.chamadas)
	}
	if len(imp.entityIDs) != 1 || len(mat.entityIDs) != 1 || imp.entityIDs[0] != mat.entityIDs[0] {
		t.Errorf("entityID divergente entre auto-match (%v) e materialização (%v)", imp.entityIDs, mat.entityIDs)
	}
}

// TestScanFilmeNaoMaterializaQuandoAutoMatchNaoAcerta covers: matched=false (no
// strong enough candidate) -> MaterializarSeAutoDownloadLigado is NOT called, even
// with Materializador configured.
func TestScanFilmeNaoMaterializaQuandoAutoMatchNaoAcerta(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Duna (2021) [tmdbid-438631]")
	escrever(t, filepath.Join(pasta, "Duna (2021) [tmdbid-438631] - 1080p.mkv"))

	imp := &filmeImportadorFake{matched: false}
	mat := &materializadorFake{}
	m := managerFake(t, d)
	m.ConfigurarFilmes(imp)
	m.ConfigurarMaterializacao(mat)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if mat.chamadas != 0 {
		t.Errorf("MaterializarSeAutoDownloadLigado chamado %d vez(es), quero 0 (sem match)", mat.chamadas)
	}
}

// TestScanFilmeSemFilmeImportadorNemMaterializadorNaoQuebra covers the degradation:
// without ConfigurarFilmes/ConfigurarMaterializacao (both nil, the mode prior to the
// auto-match), the movie scan keeps working normally, without panicking.
func TestScanFilmeSemFilmeImportadorNemMaterializadorNaoQuebra(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	pasta := filepath.Join(root, "Duna (2021) [tmdbid-438631]")
	escrever(t, filepath.Join(pasta, "Duna (2021) [tmdbid-438631] - 1080p.mkv"))

	m := managerFake(t, d)
	src := criarSource(t, d, root)

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file = %d, quero 1", n)
	}
}
