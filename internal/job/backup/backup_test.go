package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // registra o driver "sqlite" (sem CGO)
)

// abrirBancoTeste cria um sqlite de teste com uma tabela/linha (para o VACUUM
// INTO ter algo de fato para copiar) e devolve a conexão.
func abrirBancoTeste(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "origem.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := db.Exec("INSERT INTO t VALUES (1)"); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	return db
}

func TestNome(t *testing.T) {
	h := New(abrirBancoTeste(t), t.TempDir())
	if h.Nome() != "backup_vacuum" {
		t.Fatalf("Nome() = %q, quero backup_vacuum", h.Nome())
	}
}

// TestExecutarCriaArquivoSqlite prova o criterio de aceite: o job produz de
// fato um arquivo .sqlite via VACUUM INTO no diretorio configurado.
func TestExecutarCriaArquivoSqlite(t *testing.T) {
	dir := t.TempDir()
	h := New(abrirBancoTeste(t), dir)

	msg, err := h.Executar(context.Background(), nil)
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if msg == "" {
		t.Fatal("mensagem vazia")
	}

	arquivos := listarSqlite(t, dir)
	if len(arquivos) != 1 {
		t.Fatalf("arquivos no dir = %v, quero exatamente 1", arquivos)
	}

	// O arquivo gerado precisa ser um sqlite valido e consistente (a linha
	// inserida no banco de origem deve estar la).
	copia, err := sql.Open("sqlite", filepath.Join(dir, arquivos[0]))
	if err != nil {
		t.Fatalf("abrir copia: %v", err)
	}
	defer func() { _ = copia.Close() }()
	var n int
	if err := copia.QueryRow("SELECT COUNT(*) FROM t").Scan(&n); err != nil {
		t.Fatalf("SELECT na copia: %v", err)
	}
	if n != 1 {
		t.Fatalf("linhas na copia = %d, quero 1", n)
	}
}

// TestExecutarMensagem confere o formato da mensagem de sucesso.
func TestExecutarMensagem(t *testing.T) {
	dir := t.TempDir()
	h := New(abrirBancoTeste(t), dir)
	h.now = func() time.Time { return time.Date(2026, 7, 13, 3, 0, 0, 0, time.UTC) }

	msg, err := h.Executar(context.Background(), json.RawMessage(`{"keep":7}`))
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	quero := "backup gravado em mymediadb-20260713-030000.sqlite (1 mantidos)"
	if msg != quero {
		t.Fatalf("mensagem = %q, quero %q", msg, quero)
	}
}

// TestRotacaoMantemKeep prova o criterio de aceite da rotacao: mantem so os
// `keep` mais recentes (do config_json, default 7).
func TestRotacaoMantemKeep(t *testing.T) {
	dir := t.TempDir()
	h := New(abrirBancoTeste(t), dir)

	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		h.now = func(i int) func() time.Time {
			return func() time.Time { return base.AddDate(0, 0, i) }
		}(i)
		if _, err := h.Executar(context.Background(), json.RawMessage(`{"keep":3}`)); err != nil {
			t.Fatalf("Executar #%d: %v", i, err)
		}
	}

	arquivos := listarSqlite(t, dir)
	if len(arquivos) != 3 {
		t.Fatalf("arquivos mantidos = %d, quero 3 (%v)", len(arquivos), arquivos)
	}
	// Os 3 mais recentes (dias 2,3,4) devem sobreviver -- os dias 0 e 1 foram
	// rotacionados para fora.
	for _, indesejado := range []string{"mymediadb-20260101-030000.sqlite", "mymediadb-20260102-030000.sqlite"} {
		if _, err := os.Stat(filepath.Join(dir, indesejado)); err == nil {
			t.Fatalf("%q deveria ter sido removido pela rotacao", indesejado)
		}
	}
}

// TestKeepDefaultSemConfig prova o default de 7 quando config_json esta
// ausente/vazio.
func TestKeepDefaultSemConfig(t *testing.T) {
	if got := parseKeep(nil); got != keepPadrao {
		t.Fatalf("parseKeep(nil) = %d, quero %d", got, keepPadrao)
	}
	if got := parseKeep(json.RawMessage(``)); got != keepPadrao {
		t.Fatalf("parseKeep(vazio) = %d, quero %d", got, keepPadrao)
	}
	if got := parseKeep(json.RawMessage(`{"keep":0}`)); got != keepPadrao {
		t.Fatalf("parseKeep(keep=0) = %d, quero %d (invalido cai no default)", got, keepPadrao)
	}
	if got := parseKeep(json.RawMessage(`not-json`)); got != keepPadrao {
		t.Fatalf("parseKeep(invalido) = %d, quero %d", got, keepPadrao)
	}
	if got := parseKeep(json.RawMessage(`{"keep":14}`)); got != 14 {
		t.Fatalf("parseKeep(keep=14) = %d, quero 14", got)
	}
}

func listarSqlite(t *testing.T, dir string) []string {
	t.Helper()
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var out []string
	for _, e := range entradas {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}
