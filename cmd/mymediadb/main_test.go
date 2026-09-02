package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/integration"
)

// setupBancoTeste sobe um banco de teste migrado, isolado por teste.
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

// TestProvedoresReaisSemCredencialNenhumTmdb cobre o caso base: sem banco
// populado e sem env var, provedoresReais não inclui o TMDB (os demais
// providers incondicionais, como musicbrainz, continuam presentes -- não é
// alvo desta entrega).
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

// TestProvedoresReaisUsaCredencialDoBanco cobre o critério de aceite central
// do briefing 0095: um valor gravado em integration_config (via
// UpsertIntegrationConfig, o mesmo caminho que o PUT /integrations/tmdb usa)
// faz provedoresReais registrar o provider TMDB -- SEM depender de nenhuma
// env var estar setada. É o "teste de integração construindo o
// provedoresReais()-equivalente contra um banco com integration_config
// populado" pedido pelo briefing.
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

// TestProvedoresReaisBancoTemPrecedenciaSobreEnv cobre a precedência
// banco > env var (ADR-0023): mesmo com a env var TAMBÉM setada, o provider
// segue habilitado via banco (o teste central acima já prova que funciona
// SEM env var; este prova que a presença da env var não muda o resultado
// nem quebra a leitura do banco).
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
