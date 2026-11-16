package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupBanco brings up a migrated test database, isolated per test (same pattern
// as internal/curation/curation_test.go).
func setupBanco(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "integration.sqlite")
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

// gravarValores performs a direct upsert of integration_config, without going
// through the Resolver (which only reads) — it simulates what the PUT handler
// writes.
func gravarValores(t *testing.T, d *db.DB, slug, valuesJSON string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).UpsertIntegrationConfig(context.Background(), sqlcgen.UpsertIntegrationConfigParams{
		Slug: slug, ValuesJson: valuesJSON,
	}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}
}

// TestEfetivoSemBancoSemEnvNenhum covers the acceptance criterion: neither the
// database nor an env var has a value ⇒ ok=false, origem="nenhum".
func TestEfetivoSemBancoSemEnvNenhum(t *testing.T) {
	d := setupBanco(t)
	r := NovoResolver(d)
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_INEXISTENTE_XYZ"}

	valor, origem, ok := r.Efetivo(context.Background(), "tmdb", campo)
	if ok {
		t.Errorf("Efetivo deveria devolver ok=false, valor=%q", valor)
	}
	if origem != "nenhum" {
		t.Errorf("origem = %q, quero nenhum", origem)
	}
}

// TestEfetivoCaiParaEnvVar covers the acceptance criterion: only the env var is
// set ⇒ origem="env", definido=true (through ok=true).
func TestEfetivoCaiParaEnvVar(t *testing.T) {
	d := setupBanco(t)
	r := NovoResolver(d)
	t.Setenv("MYMEDIADB_TESTE_TOKEN", "do-ambiente")
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN"}

	valor, origem, ok := r.Efetivo(context.Background(), "tmdb", campo)
	if !ok || valor != "do-ambiente" || origem != "env" {
		t.Errorf("Efetivo = (%q,%q,%v), quero (do-ambiente,env,true)", valor, origem, ok)
	}
}

// TestEfetivoBancoTemPrecedenciaSobreEnv covers the central acceptance criterion
// of ADR-0023: a value stored in the database wins over the env var, even with
// it set.
func TestEfetivoBancoTemPrecedenciaSobreEnv(t *testing.T) {
	d := setupBanco(t)
	t.Setenv("MYMEDIADB_TESTE_TOKEN", "do-ambiente")
	gravarValores(t, d, "tmdb", `{"token":"do-banco"}`)
	r := NovoResolver(d)
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN"}

	valor, origem, ok := r.Efetivo(context.Background(), "tmdb", campo)
	if !ok || valor != "do-banco" || origem != "banco" {
		t.Errorf("Efetivo = (%q,%q,%v), quero (do-banco,banco,true)", valor, origem, ok)
	}
}

// TestEfetivoRemoverChaveVoltaParaEnv covers the acceptance criterion: a PUT that
// removes the key from values_json (simulated here by storing the object without
// it) makes the Resolver fall back to the env var again.
func TestEfetivoRemoverChaveVoltaParaEnv(t *testing.T) {
	d := setupBanco(t)
	t.Setenv("MYMEDIADB_TESTE_TOKEN", "do-ambiente")
	gravarValores(t, d, "tmdb", `{"token":"do-banco"}`)
	r := NovoResolver(d)
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN"}

	// 1st read: the database wins.
	if _, origem, _ := r.Efetivo(context.Background(), "tmdb", campo); origem != "banco" {
		t.Fatalf("pré-condição: origem = %q, quero banco", origem)
	}

	// Removes the "token" key from values_json (empty object) -- equivalent to
	// the PUT {"valores":{"token":null}}.
	gravarValores(t, d, "tmdb", `{}`)

	valor, origem, ok := r.Efetivo(context.Background(), "tmdb", campo)
	if !ok || valor != "do-ambiente" || origem != "env" {
		t.Errorf("Efetivo após remover a chave = (%q,%q,%v), quero (do-ambiente,env,true)", valor, origem, ok)
	}
}

// TestEfetivoSemSlugCaiParaEnv covers the "slug never written" case
// (sql.ErrNoRows inside the Resolver): it degrades silently to the env var,
// without propagating an error.
func TestEfetivoSemSlugCaiParaEnv(t *testing.T) {
	d := setupBanco(t)
	t.Setenv("MYMEDIADB_TESTE_TOKEN", "do-ambiente")
	r := NovoResolver(d)
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN"}

	valor, origem, ok := r.Efetivo(context.Background(), "slug-nunca-gravado", campo)
	if !ok || valor != "do-ambiente" || origem != "env" {
		t.Errorf("Efetivo = (%q,%q,%v), quero (do-ambiente,env,true)", valor, origem, ok)
	}
}

// TestEfetivoJSONCorrompidoCaiParaEnv proves that a corrupted values_json in the
// database (defensive -- it should not happen through the API) degrades to the
// env var, instead of breaking the resolution.
func TestEfetivoJSONCorrompidoCaiParaEnv(t *testing.T) {
	d := setupBanco(t)
	t.Setenv("MYMEDIADB_TESTE_TOKEN", "do-ambiente")
	gravarValores(t, d, "tmdb", `{nao e json valido`)
	r := NovoResolver(d)
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN"}

	valor, origem, ok := r.Efetivo(context.Background(), "tmdb", campo)
	if !ok || valor != "do-ambiente" || origem != "env" {
		t.Errorf("Efetivo = (%q,%q,%v), quero (do-ambiente,env,true)", valor, origem, ok)
	}
}

// TestResolverNilSeguro proves that a nil *Resolver (defensive, route-test
// scenario with no database) does not panic and degrades straight to the env
// var.
func TestResolverNilSeguro(t *testing.T) {
	t.Setenv("MYMEDIADB_TESTE_TOKEN", "do-ambiente")
	var r *Resolver
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN"}

	valor, origem, ok := r.Efetivo(context.Background(), "tmdb", campo)
	if !ok || valor != "do-ambiente" || origem != "env" {
		t.Errorf("Efetivo (resolver nil) = (%q,%q,%v), quero (do-ambiente,env,true)", valor, origem, ok)
	}
}

// TestValoresAgregaTodosOsCampos proves that Valores aggregates Efetivo for each
// Campo, skipping the ones left without a value (ok=false).
func TestValoresAgregaTodosOsCampos(t *testing.T) {
	d := setupBanco(t)
	gravarValores(t, d, "tmdb", `{"token":"abc"}`)
	r := NovoResolver(d)
	campos := []Campo{
		{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN_INEXISTENTE"},
		{Nome: "api_key", EnvVar: "MYMEDIADB_TESTE_APIKEY_INEXISTENTE"},
	}

	got := r.Valores(context.Background(), "tmdb", campos)
	want := map[string]string{"token": "abc"}
	if len(got) != len(want) || got["token"] != "abc" {
		t.Errorf("Valores() = %v, quero %v", got, want)
	}
	if _, existe := got["api_key"]; existe {
		t.Error("api_key não deveria aparecer no mapa (sem valor efetivo)")
	}
}
