package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// openTest opens a database in a temporary file and schedules Close.
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

// assertPragmas checks, on one connection of the given pool, the four PRAGMAs of
// ADR-0010. Each read takes a new connection from the pool, proving that the PRAGMAs
// hold per connection (and not only on the first one).
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
	// The write pool must cap at 1 open connection (single writer).
	if got := d.Write.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("Write.MaxOpenConnections = %d, quero 1", got)
	}
	if got := d.Read.Stats().MaxOpenConnections; got != defaultMaxReadConns {
		t.Errorf("Read.MaxOpenConnections = %d, quero %d", got, defaultMaxReadConns)
	}
}

// mustSchemaVersion reads the schema version, failing the test on error.
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
	// There are nineteen migrations (0001 seed + 0002 core schema + 0003
	// media_file.library_source_id + 0004 media_external/media_override + 0005
	// app_settings/entity.original_language + 0006 external_detail + 0007
	// series: entity_relation.sort_index + entity.external_id_tvdb + 0008
	// library_source.content_type + 0009 media_file_entity + 0010
	// person/company/credit + 0011 job_definition/job_execution + 0012 seed
	// of the saude_biblioteca job + 0013 entity.last_external_match_attempt_at
	// + 0014 entity_catalog_completeness +
	// 0015 asset.local_path/is_principal + entity.nfo_path/nfo_written_at
	// + 0016 library_source.watch_enabled + seed of the
	// midias_faltantes job + 0017 library_source.preferred_language
	// + 0018 library_source.retroactive_enrichment_days
	// + 0019 entity.watched/play_count/user_rating/
	// last_played_at/resume_position_seconds/resume_total_seconds
	// + 0020 historico_evento + 0021
	// historico_evento.entity_id nullable + raw_* (orphan events, 0099) + 0022
	// seed of the trailer_tema_faltantes job + 0023
	// entity.external_id_mbid (foundation of the music vertical) +
	// 0024 queue_item (unified execution queue, ADR-0028 --
	// job_execution stays intact in this layer, see the phasing note in the
	// migration itself) + 0025 drops job_execution (LAYER 3
	// of the same ADR: internal/job/manager.go now enqueues through
	// execqueue, so job_execution becomes a redundant second source of
	// truth and is dropped) + 0026 integration_config (foundation of the
	// integrations configurable through the UI, ADR-0023).
	// The expected version comes from the EMBEDDED migrations instead of a literal: a new
	// migration must not require editing this assertion (it did, every time).
	alvo, err := versaoEmbarcada()
	if err != nil {
		t.Fatalf("versaoEmbarcada: %v", err)
	}
	if v := mustSchemaVersion(t, d); v != alvo {
		t.Errorf("versão após up = %d, quero %d (última migration embarcada)", v, alvo)
	}

	// The seed migration created the metadata table with one row.
	var valor string
	if err := d.Read.QueryRowContext(ctx,
		"SELECT valor FROM mymediadb_meta WHERE chave = 'fundacao'").Scan(&valor); err != nil {
		t.Fatalf("SELECT meta: %v", err)
	}
	if valor != "0006" {
		t.Errorf("meta.valor = %q, quero \"0006\"", valor)
	}

	// Reapplying is idempotent (no pending migration).
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate idempotente: %v", err)
	}
}

// downPara reverts one migration and checks the resulting version.
func downPara(t *testing.T, d *DB, querVersao int64) {
	t.Helper()
	if err := d.MigrateDown(context.Background()); err != nil {
		t.Fatalf("MigrateDown para %d: %v", querVersao, err)
	}
	if v := mustSchemaVersion(t, d); v != querVersao {
		t.Errorf("versão após down = %d, quero %d", v, querVersao)
	}
}

// querFalha checks that a query FAILS (the object it touches is gone after the down).
// If the query unexpectedly SUCCEEDS (a down-migration bug), it closes the
// *sql.Rows before reporting the failure -- without that, the connection of the
// read pool (only 4 in total, db.go) stays stuck forever and ANY following
// read query in the test HANGS waiting for a free connection that never comes back.
func querFalha(t *testing.T, d *DB, query, msg string) {
	t.Helper()
	rows, err := d.Read.QueryContext(context.Background(), query)
	if err == nil {
		_ = rows.Close()
		t.Error(msg)
	}
}

func TestMigrateDown(t *testing.T) {
	d := openTest(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	alvo, err := versaoEmbarcada()
	if err != nil {
		t.Fatalf("versaoEmbarcada: %v", err)
	}
	// Down reverts one migration at a time, from the top down to the base.
	downPara(t, d, alvo-1) // 0028: removes match_suggestion (the "did you mean this?" hint).
	querFalha(t, d, "SELECT 1 FROM match_suggestion",
		"tabela match_suggestion ainda existe após o down do 0028")

	downPara(t, d, 26) // 0027: reverts the cron of the midias_faltantes job from 0h05 back to 0h04.

	downPara(t, d, 25) // 0026: removes integration_config.
	querFalha(t, d, "SELECT 1 FROM integration_config",
		"tabela integration_config ainda existe após o down do 0026")

	downPara(t, d, 24) // 0025: recreates job_execution (SCHEMA only, no data -- see the note in the Down of the migration).
	if rows, err := d.Read.QueryContext(context.Background(), "SELECT 1 FROM job_execution WHERE 0"); err != nil {
		t.Errorf("job_execution deveria existir de novo após o down do 0025: %v", err)
	} else {
		_ = rows.Close()
	}

	downPara(t, d, 23) // 0024: removes queue_item (job_execution is not touched, see the phasing note in the migration).
	querFalha(t, d, "SELECT 1 FROM queue_item",
		"tabela queue_item ainda existe após o down do 0024")

	downPara(t, d, 22) // 0023: removes entity.external_id_mbid.
	querFalha(t, d, "SELECT external_id_mbid FROM entity",
		"coluna external_id_mbid ainda existe após o down do 0023")

	downPara(t, d, 21) // 0022: removes the seed of the trailer_tema_faltantes job.

	downPara(t, d, 20) // 0021: reverts historico_evento.entity_id back to NOT NULL, removes the raw_* columns.
	querFalha(t, d, "SELECT raw_tmdb_id, raw_imdb_id, raw_tvdb_id, raw_title, raw_year FROM historico_evento",
		"colunas raw_* ainda existem após o down do 0021")

	downPara(t, d, 19) // 0020: removes historico_evento.
	querFalha(t, d, "SELECT 1 FROM historico_evento",
		"tabela historico_evento ainda existe após o down do 0020")

	downPara(t, d, 18) // 0019: removes the local playback fields of entity.
	querFalha(t, d, "SELECT watched, play_count, user_rating, last_played_at, resume_position_seconds, resume_total_seconds FROM entity",
		"campos locais de reprodução ainda existem após o down do 0019")

	downPara(t, d, 17) // 0018: removes library_source.retroactive_enrichment_days.
	querFalha(t, d, "SELECT retroactive_enrichment_days FROM library_source",
		"coluna retroactive_enrichment_days ainda existe após o down do 0018")

	downPara(t, d, 16) // 0017: removes library_source.preferred_language.
	querFalha(t, d, "SELECT preferred_language FROM library_source",
		"coluna preferred_language ainda existe após o down do 0017")

	downPara(t, d, 15) // 0016: removes library_source.watch_enabled and the seed of the midias_faltantes job.
	querFalha(t, d, "SELECT watch_enabled FROM library_source",
		"coluna watch_enabled ainda existe após o down do 0016")

	downPara(t, d, 14) // 0015: removes asset.local_path/is_principal and entity.nfo_path/nfo_written_at.
	querFalha(t, d, "SELECT local_path, is_principal FROM asset",
		"colunas local_path/is_principal ainda existem após o down do 0015")
	querFalha(t, d, "SELECT nfo_path, nfo_written_at FROM entity",
		"colunas nfo_path/nfo_written_at ainda existem após o down do 0015")

	downPara(t, d, 13) // 0014: removes entity_catalog_completeness.
	querFalha(t, d, "SELECT 1 FROM entity_catalog_completeness",
		"tabela entity_catalog_completeness ainda existe após o down do 0014")

	downPara(t, d, 12) // 0013: removes entity.last_external_match_attempt_at.
	querFalha(t, d, "SELECT last_external_match_attempt_at FROM entity",
		"coluna last_external_match_attempt_at ainda existe após o down do 0013")

	downPara(t, d, 11) // 0012: removes the seed of the saude_biblioteca job.

	downPara(t, d, 10) // 0011: removes job_definition/job_execution.
	querFalha(t, d, "SELECT 1 FROM job_execution",
		"tabela job_execution ainda existe após o down do 0011")
	querFalha(t, d, "SELECT 1 FROM job_definition",
		"tabela job_definition ainda existe após o down do 0011")

	downPara(t, d, 9) // 0010: removes person/company/credit (cast/crew/production companies).
	querFalha(t, d, "SELECT 1 FROM credit",
		"tabela credit ainda existe após o down do 0010")
	querFalha(t, d, "SELECT 1 FROM person",
		"tabela person ainda existe após o down do 0010")
	querFalha(t, d, "SELECT 1 FROM company",
		"tabela company ainda existe após o down do 0010")

	downPara(t, d, 8) // 0009: removes the generic media_file_entity junction.
	querFalha(t, d, "SELECT 1 FROM media_file_entity",
		"tabela media_file_entity ainda existe após o down do 0009")

	downPara(t, d, 7) // 0008: removes library_source.content_type.
	querFalha(t, d, "SELECT content_type FROM library_source",
		"coluna content_type ainda existe após o down do 0008")

	downPara(t, d, 6) // 0007: removes entity_relation.sort_index and entity.external_id_tvdb.
	querFalha(t, d, "SELECT sort_index FROM entity_relation",
		"coluna sort_index ainda existe após o down do 0007")
	querFalha(t, d, "SELECT external_id_tvdb FROM entity",
		"coluna external_id_tvdb ainda existe após o down do 0007")

	downPara(t, d, 5) // 0006: removes external_detail.
	querFalha(t, d, "SELECT 1 FROM external_detail",
		"tabela external_detail ainda existe após o down do 0006")

	downPara(t, d, 4) // 0005: removes app_settings and entity.original_language.
	querFalha(t, d, "SELECT 1 FROM app_settings",
		"tabela app_settings ainda existe após o down do 0005")
	querFalha(t, d, "SELECT original_language FROM entity",
		"coluna original_language ainda existe após o down do 0005")

	downPara(t, d, 3) // 0004: removes media_external and media_override.
	querFalha(t, d, "SELECT 1 FROM media_external",
		"tabela media_external ainda existe após o down do 0004")
	querFalha(t, d, "SELECT 1 FROM media_override",
		"tabela media_override ainda existe após o down do 0004")

	downPara(t, d, 2) // 0003: removes the library_source_id column from media_file.
	querFalha(t, d, "SELECT library_source_id FROM media_file",
		"coluna library_source_id ainda existe após o down do 0003")

	downPara(t, d, 1) // 0002: removes the core schema (entity table).
	querFalha(t, d, "SELECT 1 FROM entity", "tabela entity ainda existe após o down do 0002")

	downPara(t, d, 0) // 0001: removes the seed (mymediadb_meta table).
	querFalha(t, d, "SELECT 1 FROM mymediadb_meta", "tabela mymediadb_meta ainda existe após o down")
}

// TestLeituraConcorrenteDuranteEscrita guarantees that reads do not get
// `database is locked` while a write transaction is open (WAL + separate
// pools -- ADR-0010).
func TestLeituraConcorrenteDuranteEscrita(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Opens a write transaction and inserts without committing, holding the writer.
	tx, err := d.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO mymediadb_meta (chave, valor) VALUES ('em_voo', 'x')"); err != nil {
		t.Fatalf("INSERT na tx: %v", err)
	}

	// While the write is open, several concurrent reads must
	// succeed (seeing the committed snapshot, without the in-flight row).
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
			if n != 1 { // only the committed 'fundacao' row
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
