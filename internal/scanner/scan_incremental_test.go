package scanner

// Testes do SCAN INCREMENTAL (briefing 0064): o scan para de re-hashear TODO
// arquivo de vídeo em TODA execução -- só re-hasheia quando mtime OU size
// divergem do media_file já gravado para aquele path (ver hashEMediainfo/
// mediaFileInalteradoPorPath, scan.go). Instrumenta hashDoArquivoFn (a
// indireção só-de-teste) para CONTAR quantas vezes o hash de verdade rodou --
// o critério de aceite explícito do briefing.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// instrumentarHash substitui hashDoArquivoFn por um contador que ainda chama
// o hash real por baixo (o teste continua exercitando o pipeline de verdade),
// e devolve um ponteiro para o contador + a função de restauração (via
// t.Cleanup, chamada automaticamente).
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

// TestScanIncrementalPulaReHashSemMudanca cobre o critério central: rodar o
// scan duas vezes seguidas sem tocar no arquivo NÃO recalcula content_hash na
// segunda rodada.
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

	// O media_file continua íntegro (content_hash reusado, não perdido).
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE content_hash IS NOT NULL AND content_hash != ''"); n != 1 {
		t.Errorf("media_file com content_hash preenchido = %d, quero 1", n)
	}
}

// TestScanIncrementalReHasheiaQuandoMtimeMuda cobre o segundo critério: mudar
// o mtime de um arquivo já indexado (mesmo conteúdo) faz o PRÓXIMO scan
// re-hashear aquele arquivo especificamente.
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

	// Muda só o mtime (mesmo conteúdo/tamanho) -- o próximo scan deve
	// re-hashear ESTE arquivo especificamente.
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

// Nota: um terceiro teste espelhando este para o sinal de SIZE (sobrescrever
// o arquivo com conteúdo de outro tamanho no MESMO path, recarimbando o mtime
// antigo de propósito) foi cogitado e removido -- ele esbarra num gap
// PRÉ-EXISTENTE e ortogonal a este briefing: o dedup do scanner (gravarArquivo,
// match.go) só reencontra pelo content_hash antigo; uma reescrita in-place que
// MUDA o hash no mesmo path cai no caminho de "arquivo novo" e colide com o
// UNIQUE de media_file.path (a linha antiga ainda aponta pra ele). Esse
// comportamento já existia antes da migration 0064 (nunca foi exercitado por
// nenhum teste anterior) -- corrigi-lo é fora do escopo deste briefing
// (scan incremental por mtime/size), não uma decisão deste código.
