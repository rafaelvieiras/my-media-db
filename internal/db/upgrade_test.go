package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// novoBancoDeTeste opens a database in a temporary directory. It is deliberately the
// production Open (same pragmas, same pools): the backup under test runs against the
// same connection recipe.
func novoBancoDeTeste(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	caminho := filepath.Join(dir, "teste.sqlite")
	d, err := Open(Config{Path: caminho})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, caminho
}

func TestVersaoEmbarcadaBateComOSchemaMigrado(t *testing.T) {
	d, _ := novoBancoDeTeste(t)
	ctx := context.Background()

	alvo, err := versaoEmbarcada()
	if err != nil {
		t.Fatalf("versaoEmbarcada: %v", err)
	}
	if alvo < 27 {
		t.Fatalf("versao embarcada = %d, quero o schema real do projeto (>= 27)", alvo)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	atual, err := d.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if atual != alvo {
		t.Fatalf("depois de migrar, schema = %d e a maior migracao embarcada = %d; deviam coincidir", atual, alvo)
	}
}

func TestPlanoDeMigracaoBancoNovo(t *testing.T) {
	d, _ := novoBancoDeTeste(t)
	atual, alvo, err := d.PlanoDeMigracao(context.Background())
	if err != nil {
		t.Fatalf("PlanoDeMigracao: %v", err)
	}
	if atual != 0 {
		t.Fatalf("banco novo devia estar no schema 0, veio %d", atual)
	}
	if alvo == 0 {
		t.Fatal("alvo nao pode ser 0: as migracoes embarcadas nao foram lidas")
	}
}

func TestVerificarDowngrade(t *testing.T) {
	casos := []struct {
		nome  string
		atual int64
		alvo  int64
		erro  bool
	}{
		{"banco na mesma versao", 27, 27, false},
		{"banco atras, upgrade normal", 25, 27, false},
		{"banco novo, imagem antiga", 28, 27, true},
		{"banco muito a frente", 40, 27, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := VerificarDowngrade(c.atual, c.alvo)
			if c.erro && !errors.Is(err, ErrBancoMaisNovo) {
				t.Fatalf("queria ErrBancoMaisNovo, veio %v", err)
			}
			if !c.erro && err != nil {
				t.Fatalf("nao queria erro, veio %v", err)
			}
		})
	}
}

func TestBackupAntesDeMigrarGravaCopiaLegivel(t *testing.T) {
	d, _ := novoBancoDeTeste(t)
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := d.Write.ExecContext(ctx,
		`INSERT INTO entity (id, type, title, status, created_at, updated_at) VALUES ('e1','movie','Um Filme','cataloged',datetime('now'),datetime('now'))`); err != nil {
		t.Fatalf("inserir entity: %v", err)
	}
	atual, err := d.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "backups")

	// Simulate a pending migration: the copy must be written when atual < alvo.
	caminho, err := d.BackupAntesDeMigrar(ctx, dir, atual, atual+1, 3)
	if err != nil {
		t.Fatalf("BackupAntesDeMigrar: %v", err)
	}
	if caminho == "" {
		t.Fatal("era para gravar a copia")
	}
	if _, err := os.Stat(caminho); err != nil {
		t.Fatalf("copia nao existe: %v", err)
	}
	if !strings.Contains(filepath.Base(caminho), "pre-migration-schema-") {
		t.Fatalf("nome da copia fora do padrao: %s", filepath.Base(caminho))
	}
	// The copy is a real, self-contained database with the data in it.
	copia, err := sql.Open(driverName, caminho)
	if err != nil {
		t.Fatalf("abrir a copia: %v", err)
	}
	defer func() { _ = copia.Close() }()
	var titulo string
	if err := copia.QueryRow(`SELECT title FROM entity WHERE id = 'e1'`).Scan(&titulo); err != nil {
		t.Fatalf("ler a copia: %v", err)
	}
	if titulo != "Um Filme" {
		t.Fatalf("titulo na copia = %q", titulo)
	}
	var versao int64
	if err := copia.QueryRow(`SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1`).Scan(&versao); err != nil {
		t.Fatalf("ler a versao na copia: %v", err)
	}
	if versao != atual {
		t.Fatalf("versao na copia = %d, queria %d", versao, atual)
	}
}

func TestBackupAntesDeMigrarNaoCopiaQuandoNaoHaMigracao(t *testing.T) {
	d, _ := novoBancoDeTeste(t)
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	atual, _ := d.SchemaVersion(ctx)
	dir := filepath.Join(t.TempDir(), "backups")

	caminho, err := d.BackupAntesDeMigrar(ctx, dir, atual, atual, 3)
	if err != nil {
		t.Fatalf("BackupAntesDeMigrar: %v", err)
	}
	if caminho != "" {
		t.Fatalf("nao devia copiar sem migracao pendente, veio %q", caminho)
	}
	// A fresh database (schema 0) has nothing to lose either.
	caminho, err = d.BackupAntesDeMigrar(ctx, dir, 0, atual, 3)
	if err != nil {
		t.Fatalf("BackupAntesDeMigrar (schema 0): %v", err)
	}
	if caminho != "" {
		t.Fatalf("banco novo nao devia ser copiado, veio %q", caminho)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("o diretorio nao devia nem ser criado: %v", err)
	}
}

func TestBackupAntesDeMigrarPodasMantemAsMaisNovas(t *testing.T) {
	d, _ := novoBancoDeTeste(t)
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	atual, _ := d.SchemaVersion(ctx)
	dir := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Copies from previous upgrades, plus a file the pruning must NOT touch.
	antigas := []string{
		"pre-migration-schema-24-20260101-000000.sqlite",
		"pre-migration-schema-25-20260201-000000.sqlite",
		"pre-migration-schema-26-20260301-000000.sqlite",
		"backup-2026-01-01.sqlite",
	}
	for _, nome := range antigas {
		if err := os.WriteFile(filepath.Join(dir, nome), []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	nova, err := d.BackupAntesDeMigrar(ctx, dir, atual, atual+1, 2)
	if err != nil {
		t.Fatalf("BackupAntesDeMigrar: %v", err)
	}
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var copias []string
	for _, e := range entradas {
		if reBackupDeMigracao.MatchString(e.Name()) {
			copias = append(copias, e.Name())
		}
	}
	if len(copias) != 2 {
		t.Fatalf("queria 2 copias de migracao, tenho %d: %v", len(copias), copias)
	}
	if _, err := os.Stat(nova); err != nil {
		t.Fatalf("a copia nova devia sobreviver a poda: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backup-2026-01-01.sqlite")); err != nil {
		t.Fatalf("a poda apagou um backup que nao e de migracao: %v", err)
	}
}
