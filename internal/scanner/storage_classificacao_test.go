package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// scanSyncRegistrado roda o scan sincronamente -- mesmo caminho de scanSync
// (scanner_test.go). Desde o briefing 0108, StorageNaoVideo é populado
// diretamente por Manager.executarScan (chave = source.ID, sem depender de
// nenhum registro prévio em mapa de job), então este helper é hoje só um
// alias nomeado -- mantido para não precisar tocar nos call-sites deste
// arquivo.
func scanSyncRegistrado(t *testing.T, m *Manager, src library.Source) error {
	t.Helper()
	return scanSync(t, m, src)
}

// TestScanClassificaArquivoNaoVideoManagedEUnmanaged cobre o núcleo do
// briefing 0063: durante a MESMA varredura que já detecta o vídeo, um
// poster.jpg (nome canônico do 0062) é somado em managed e um sample.txt
// (lixo) é somado em unmanaged -- consultável depois via
// Manager.StorageNaoVideo, a API que internal/job/libraryhealth usa.
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

// TestStorageNaoVideoSemScanDevolveOkFalse prova que uma fonte nunca
// escaneada devolve ok=false (não zero mentiroso).
func TestStorageNaoVideoSemScanDevolveOkFalse(t *testing.T) {
	d := abrirDB(t)
	m := managerFake(t, d)
	if _, _, ok := m.StorageNaoVideo("nunca-existiu"); ok {
		t.Fatal("ok = true para fonte nunca escaneada, quero false")
	}
}

// TestStorageNaoVideoMantemUltimoValorQuandoFonteFicaOffline prova a
// política do briefing 0063 (mesma do 0055 para Statfs): depois de um scan
// bem-sucedido, se a fonte ficar inacessível, StorageNaoVideo continua
// devolvendo o ÚLTIMO valor calculado (Start() falha ANTES de tocar o
// estado do job -- nenhuma lógica extra precisa existir para isso).
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

	// Fonte fica inacessível -- Start() recusaria com ErrSourceOffline, sem
	// tocar em m.jobs[src.ID].
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

// TestExecutarScanUsaUmaUnicaPassadaDeWalkDir é a guarda estrutural do
// critério de aceite "nenhuma varredura de disco NOVA é introduzida"
// (briefing 0063 §5): scan.go só pode ter UM ponto de chamada a
// filepath.WalkDir -- coletarArquivos precisa classificar vídeo E não-vídeo
// na MESMA passada. Uma regressão que adicionasse uma segunda passada (ex.:
// um WalkDir dedicado só para a classificação) quebraria este teste.
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

// escreverTamanho cria um arquivo com exatamente `n` bytes (diferente de
// escrever, em scanner_test.go, que grava o próprio path como conteúdo --
// aqui o TAMANHO exato importa para a classificação de storage).
func escreverTamanho(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}
