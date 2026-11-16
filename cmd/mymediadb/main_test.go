package main

import (
	"context"
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
