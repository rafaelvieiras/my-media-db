package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTamanhoDiretorioSomaArquivosRecursivo prova o critério de aceite do
// briefing 0063: app_cache_bytes reflete o tamanho REAL de assetsDir, com
// arquivo de tamanho conhecido, inclusive em subdiretório.
func TestTamanhoDiretorioSomaArquivosRecursivo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.png"), make([]byte, 250), 0o644); err != nil {
		t.Fatal(err)
	}

	total, err := TamanhoDiretorio(dir)
	if err != nil {
		t.Fatalf("TamanhoDiretorio: %v", err)
	}
	if total != 350 {
		t.Fatalf("total = %d, quero 350 (100+250)", total)
	}
}

// TestTamanhoDiretorioInexistenteDevolveZero prova que um assetsDir ainda
// não criado (instalação nova) não é erro -- devolve 0.
func TestTamanhoDiretorioInexistenteDevolveZero(t *testing.T) {
	total, err := TamanhoDiretorio(filepath.Join(t.TempDir(), "nao-existe"))
	if err != nil {
		t.Fatalf("TamanhoDiretorio: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, quero 0", total)
	}
}

// TestTamanhoDiretorioVazioDevolveZero prova o caso trivial: diretório
// existente e vazio soma 0.
func TestTamanhoDiretorioVazioDevolveZero(t *testing.T) {
	total, err := TamanhoDiretorio(t.TempDir())
	if err != nil {
		t.Fatalf("TamanhoDiretorio: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, quero 0", total)
	}
}
