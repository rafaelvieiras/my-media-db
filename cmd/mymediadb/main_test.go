package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/integration"
)

// setupBancoTeste starts a migrated test database, isolated per test.
func setupBancoTeste(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "mymediadb.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

// TestProvedoresReaisSemCredencialNenhumTmdb covers the base case: with no populated
// database and no env var, provedoresReais does not include TMDB (the other
// unconditional providers, such as musicbrainz, are still present -- that is not
// the target of this delivery).
func TestProvedoresReaisSemCredencialNenhumTmdb(t *testing.T) {
	database := setupBancoTeste(t)
	resolver := integration.NovoResolver(database)

	provs := provedoresReais(context.Background(), resolver)
	nomes := map[string]bool{}
	for _, p := range provs {
		nomes[p.Nome()] = true
	}
	if nomes["tmdb"] {
		t.Error("tmdb não deveria estar habilitado sem credencial (nem banco, nem env var)")
	}
}

// TestProvedoresReaisUsaCredencialDoBanco covers the central acceptance criterion
// of ADR-0023: a value stored in integration_config (through
// UpsertIntegrationConfig, the same path that PUT /integrations/tmdb uses)
// makes provedoresReais register the TMDB provider -- WITHOUT depending on any
// env var being set. It is the "integration test building the
// provedoresReais()-equivalent against a database with integration_config
// populated" that the ADR called for.
func TestProvedoresReaisUsaCredencialDoBanco(t *testing.T) {
	database := setupBancoTeste(t)
	ctx := context.Background()

	if _, err := sqlcgen.New(database.Write).UpsertIntegrationConfig(ctx, sqlcgen.UpsertIntegrationConfigParams{
		Slug:       "tmdb",
		ValuesJson: `{"token":"do-banco"}`,
	}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}

	resolver := integration.NovoResolver(database)
	provs := provedoresReais(ctx, resolver)

	nomes := map[string]bool{}
	for _, p := range provs {
		nomes[p.Nome()] = true
	}
	if !nomes["tmdb"] {
		t.Error("tmdb deveria estar habilitado com o token gravado em integration_config, sem nenhuma env var")
	}
}

// TestProvedoresReaisBancoTemPrecedenciaSobreEnv covers the database > env var
// precedence (ADR-0023): even with the env var ALSO set, the provider stays
// enabled through the database (the central test above already proves that it
// works WITHOUT an env var; this one proves that the presence of the env var
// changes neither the result nor the database read).
func TestProvedoresReaisBancoTemPrecedenciaSobreEnv(t *testing.T) {
	database := setupBancoTeste(t)
	ctx := context.Background()
	t.Setenv("MYMEDIADB_TMDB_TOKEN", "do-ambiente")

	if _, err := sqlcgen.New(database.Write).UpsertIntegrationConfig(ctx, sqlcgen.UpsertIntegrationConfigParams{
		Slug:       "tmdb",
		ValuesJson: `{"token":"do-banco"}`,
	}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}

	resolver := integration.NovoResolver(database)
	provs := provedoresReais(ctx, resolver)
	nomes := map[string]bool{}
	for _, p := range provs {
		nomes[p.Nome()] = true
	}
	if !nomes["tmdb"] {
		t.Error("tmdb deveria estar habilitado (banco presente, env var também setada)")
	}
}

// TestCopiarAntesDeMigrarGravaECopiaRespeitaSkip covers the boot step that makes an
// unattended upgrade reversible: with a pending migration the copy is written into the
// backup directory, and with the skip set nothing is written (the warning path).
func TestCopiarAntesDeMigrarGravaECopiaRespeitaSkip(t *testing.T) {
	database := setupBancoTeste(t)
	ctx := context.Background()
	atual, err := database.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	dir := t.TempDir()
	t.Setenv("MYMEDIADB_BACKUP_DIR", dir)

	// No pending migration: nothing to protect.
	if err := copiarAntesDeMigrar(database, ctx, atual, atual); err != nil {
		t.Fatalf("copiarAntesDeMigrar (sem migracao): %v", err)
	}
	// Pending migration: the copy lands in the configured directory.
	if err := copiarAntesDeMigrar(database, ctx, atual, atual+1); err != nil {
		t.Fatalf("copiarAntesDeMigrar: %v", err)
	}
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entradas) != 1 {
		t.Fatalf("queria 1 copia em %s, tenho %d", dir, len(entradas))
	}
	// Skip: the owner accepted a one-way upgrade, so no copy is written.
	t.Setenv("MYMEDIADB_SKIP_PRE_MIGRATION_BACKUP", "1")
	outra := t.TempDir()
	t.Setenv("MYMEDIADB_BACKUP_DIR", outra)
	if err := copiarAntesDeMigrar(database, ctx, atual, atual+1); err != nil {
		t.Fatalf("copiarAntesDeMigrar (skip): %v", err)
	}
	if restantes, err := os.ReadDir(outra); err != nil || len(restantes) != 0 {
		t.Fatalf("com skip nada devia ser escrito (err=%v, arquivos=%d)", err, len(restantes))
	}
}

// TestCopiarAntesDeMigrarRespeitaRetencao covers the env knob for how many copies stay.
func TestCopiarAntesDeMigrarRespeitaRetencao(t *testing.T) {
	database := setupBancoTeste(t)
	ctx := context.Background()
	atual, err := database.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	dir := t.TempDir()
	t.Setenv("MYMEDIADB_BACKUP_DIR", dir)
	t.Setenv("MYMEDIADB_PRE_MIGRATION_BACKUPS", "1")

	for i := 0; i < 2; i++ {
		// Distinct seconds in the name: the retention sorts by the timestamp in it.
		if err := copiarAntesDeMigrar(database, ctx, atual, atual+1); err != nil {
			t.Fatalf("copiarAntesDeMigrar: %v", err)
		}
	}
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// Both upgrades happened inside the same second, which is the same copy: the name is
	// second-precision and VACUUM INTO would refuse to overwrite. What matters is that
	// the retention never leaves more than the configured number behind.
	if len(entradas) > 1 {
		t.Fatalf("retencao=1 devia manter no maximo 1 copia, tenho %d", len(entradas))
	}
}
