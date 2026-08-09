package scanner

// Testes do auto-match imediato de FILME e da materialização automática
// opcional (briefing 0089): filme novo detectado no scan dispara
// FilmeImportador.TentarAutoMatchFilme exatamente uma vez por entity NOVA
// (nunca para entity já existente reencontrada); matched=true aciona o
// Materializador OPCIONAL quando configurado. Ambas as interfaces são
// consumidas via fakes locais -- a implementação real (*Server) é de outro
// pacote/agente.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// filmeImportadorFake é um fake de teste do FilmeImportador (0089): conta as
// chamadas, registra os entityID recebidos (na ordem) e devolve um resultado
// fixo configurável.
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

// materializadorFake é um fake de teste do Materializador (0089): conta as
// chamadas e registra os entityID recebidos.
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

// TestScanFilmeNovoDisparaAutoMatchUmaVez cobre o critério central: uma
// entity de filme NOVA criada pelo scan dispara TentarAutoMatchFilme
// exatamente uma vez.
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

// TestScanFilmeDuasVersoesMesmaEntityDisparaUmaSoVez cobre o mesmo critério
// quando duas qualidades do MESMO filme (mesma pasta) resultam numa única
// entity (cache do scan) -- o auto-match não deve disparar duas vezes para a
// mesma entity nova.
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

// TestScanFilmeReencontradoNaoDisparaAutoMatchDeNovo cobre: entity JÁ
// EXISTENTE, reencontrada por content_hash num re-scan (arquivo inalterado),
// NÃO dispara TentarAutoMatchFilme de novo -- só entity recém-criada nesta
// passada de scan aciona o auto-match.
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

	// Re-scan sem tocar no arquivo: dedup por content_hash (reencontrar),
	// nunca passa por resolverEntity/criarEntity de novo.
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if fake.chamadas != 1 {
		t.Errorf("scan 2 (reencontro): TentarAutoMatchFilme total=%d, quero 1 (sem disparo de novo)", fake.chamadas)
	}
}

// TestScanFilmeAutoMatchErroNaoAbortaGravacao cobre: TentarAutoMatchFilme
// devolvendo erro é best-effort -- o media_file/entity já gravados
// permanecem, o scan não propaga o erro nem entra em pânico.
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

// TestScanFilmeMaterializaQuandoAutoMatchAcerta cobre: matched=true +
// Materializador configurado -> MaterializarSeAutoDownloadLigado chamado uma
// vez com o entityID auto-importado.
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

// TestScanFilmeNaoMaterializaQuandoAutoMatchNaoAcerta cobre: matched=false
// (sem candidato forte o bastante) -> MaterializarSeAutoDownloadLigado NÃO é
// chamado, mesmo com Materializador configurado.
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

// TestScanFilmeSemFilmeImportadorNemMaterializadorNaoQuebra cobre a
// degradação: sem ConfigurarFilmes/ConfigurarMaterializacao (ambos nil,
// modo pré-0089), o scan de filme continua funcionando normalmente, sem
// pânico.
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
