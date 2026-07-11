package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// openTest abre um banco num arquivo temporário e agenda o Close.
func openTest(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "teste.sqlite")
	d, err := Open(Config{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return d
}

// assertPragmas confere, numa conexão do pool dado, os quatro PRAGMAs da
// ADR-0010. Cada leitura pega uma conexão nova do pool, provando que os PRAGMAs
// valem por conexão (e não só na primeira).
func assertPragmas(t *testing.T, pool *sql.DB, nome string) {
	t.Helper()
	ctx := context.Background()

	var journal string
	if err := pool.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("[%s] PRAGMA journal_mode: %v", nome, err)
	}
	if journal != "wal" {
		t.Errorf("[%s] journal_mode = %q, quero \"wal\"", nome, journal)
	}

	var busy int
	if err := pool.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("[%s] PRAGMA busy_timeout: %v", nome, err)
	}
	if busy != 5000 {
		t.Errorf("[%s] busy_timeout = %d, quero 5000", nome, busy)
	}

	var sync int
	if err := pool.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync); err != nil {
		t.Fatalf("[%s] PRAGMA synchronous: %v", nome, err)
	}
	if sync != 1 { // NORMAL == 1
		t.Errorf("[%s] synchronous = %d, quero 1 (NORMAL)", nome, sync)
	}

	var fk int
	if err := pool.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("[%s] PRAGMA foreign_keys: %v", nome, err)
	}
	if fk != 1 {
		t.Errorf("[%s] foreign_keys = %d, quero 1 (ON)", nome, fk)
	}
}

func TestOpenAplicaPragmas(t *testing.T) {
	d := openTest(t)
	assertPragmas(t, d.Read, "read")
	assertPragmas(t, d.Write, "write")
}

func TestOpenPathVazio(t *testing.T) {
	if _, err := Open(Config{Path: ""}); err == nil {
		t.Fatal("Open com Path vazio deveria falhar")
	}
}

func TestEscritorUnico(t *testing.T) {
	d := openTest(t)
	// O pool de escrita precisa limitar a 1 conexão aberta (escritor único).
	if got := d.Write.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("Write.MaxOpenConnections = %d, quero 1", got)
	}
	if got := d.Read.Stats().MaxOpenConnections; got != defaultMaxReadConns {
		t.Errorf("Read.MaxOpenConnections = %d, quero %d", got, defaultMaxReadConns)
	}
}

// mustSchemaVersion lê a versão de schema, falhando o teste em erro.
func mustSchemaVersion(t *testing.T, d *DB) int64 {
	t.Helper()
	v, err := d.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	return v
}

func TestMigrateUp(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()

	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Há nove migrations (0001 semente + 0002 schema do núcleo + 0003
	// media_file.library_source_id + 0004 media_external/media_override + 0005
	// app_settings/entity.original_language + 0006 external_detail + 0007
	// series: entity_relation.sort_index + entity.external_id_tvdb + 0008
	// library_source.content_type + 0009 media_file_entity).
	if v := mustSchemaVersion(t, d); v != 9 {
		t.Errorf("versão após up = %d, quero 9", v)
	}

	// A migration-semente criou a tabela de metadados com uma linha.
	var valor string
	if err := d.Read.QueryRowContext(ctx,
		"SELECT valor FROM mymediadb_meta WHERE chave = 'fundacao'").Scan(&valor); err != nil {
		t.Fatalf("SELECT meta: %v", err)
	}
	if valor != "0006" {
		t.Errorf("meta.valor = %q, quero \"0006\"", valor)
	}

	// Reaplicar é idempotente (nenhuma migration pendente).
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate idempotente: %v", err)
	}
}

// downPara reverte uma migration e confere a versão resultante.
func downPara(t *testing.T, d *DB, querVersao int64) {
	t.Helper()
	if err := d.MigrateDown(context.Background()); err != nil {
		t.Fatalf("MigrateDown para %d: %v", querVersao, err)
	}
	if v := mustSchemaVersion(t, d); v != querVersao {
		t.Errorf("versão após down = %d, quero %d", v, querVersao)
	}
}

// querFalha confere que uma query FALHA (o objeto que ela toca sumiu no down).
func querFalha(t *testing.T, d *DB, query, msg string) {
	t.Helper()
	if _, err := d.Read.QueryContext(context.Background(), query); err == nil {
		t.Error(msg)
	}
}

func TestMigrateDown(t *testing.T) {
	d := openTest(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Down reverte uma migration por vez, do topo para a base.
	downPara(t, d, 8) // 0009: remove a juncao generica media_file_entity.
	querFalha(t, d, "SELECT 1 FROM media_file_entity",
		"tabela media_file_entity ainda existe após o down do 0009")

	downPara(t, d, 7) // 0008: remove library_source.content_type.
	querFalha(t, d, "SELECT content_type FROM library_source",
		"coluna content_type ainda existe após o down do 0008")

	downPara(t, d, 6) // 0007: remove entity_relation.sort_index e entity.external_id_tvdb.
	querFalha(t, d, "SELECT sort_index FROM entity_relation",
		"coluna sort_index ainda existe após o down do 0007")
	querFalha(t, d, "SELECT external_id_tvdb FROM entity",
		"coluna external_id_tvdb ainda existe após o down do 0007")

	downPara(t, d, 5) // 0006: remove external_detail.
	querFalha(t, d, "SELECT 1 FROM external_detail",
		"tabela external_detail ainda existe após o down do 0006")

	downPara(t, d, 4) // 0005: remove app_settings e entity.original_language.
	querFalha(t, d, "SELECT 1 FROM app_settings",
		"tabela app_settings ainda existe após o down do 0005")
	querFalha(t, d, "SELECT original_language FROM entity",
		"coluna original_language ainda existe após o down do 0005")

	downPara(t, d, 3) // 0004: remove media_external e media_override.
	querFalha(t, d, "SELECT 1 FROM media_external",
		"tabela media_external ainda existe após o down do 0004")
	querFalha(t, d, "SELECT 1 FROM media_override",
		"tabela media_override ainda existe após o down do 0004")

	downPara(t, d, 2) // 0003: remove a coluna library_source_id de media_file.
	querFalha(t, d, "SELECT library_source_id FROM media_file",
		"coluna library_source_id ainda existe após o down do 0003")

	downPara(t, d, 1) // 0002: remove o schema do núcleo (tabela entity).
	querFalha(t, d, "SELECT 1 FROM entity", "tabela entity ainda existe após o down do 0002")

	downPara(t, d, 0) // 0001: remove a semente (tabela mymediadb_meta).
	querFalha(t, d, "SELECT 1 FROM mymediadb_meta", "tabela mymediadb_meta ainda existe após o down")
}

// TestLeituraConcorrenteDuranteEscrita garante que leituras não recebem
// `database is locked` enquanto uma transação de escrita está aberta (WAL +
// pools separados — ADR-0010).
func TestLeituraConcorrenteDuranteEscrita(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Abre uma transação de escrita e insere sem comitar, segurando o escritor.
	tx, err := d.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO mymediadb_meta (chave, valor) VALUES ('em_voo', 'x')"); err != nil {
		t.Fatalf("INSERT na tx: %v", err)
	}

	// Enquanto a escrita está aberta, várias leituras concorrentes devem
	// suceder (vendo o snapshot commitado, sem a linha em voo).
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			var n int
			if err := d.Read.QueryRowContext(readCtx,
				"SELECT count(*) FROM mymediadb_meta").Scan(&n); err != nil {
				errs <- err
				return
			}
			if n != 1 { // só a linha 'fundacao' commitada
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("leitura concorrente falhou (database is locked?): %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}
