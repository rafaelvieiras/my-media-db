package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupBanco sobe um banco de teste migrado, isolado por teste (mesmo padrão
// de internal/curation/curation_test.go).
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

// gravarValores faz upsert direto de integration_config, sem passar pelo
// Resolver (que só lê) — simula o que o handler PUT grava.
func gravarValores(t *testing.T, d *db.DB, slug, valuesJSON string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).UpsertIntegrationConfig(context.Background(), sqlcgen.UpsertIntegrationConfigParams{
		Slug: slug, ValuesJson: valuesJSON,
	}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}
}

// TestEfetivoSemBancoSemEnvNenhum cobre o critério de aceite: nem banco nem
// env var têm valor ⇒ ok=false, origem="nenhum".
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

// TestEfetivoCaiParaEnvVar cobre o critério de aceite: só env var setada ⇒
// origem="env", definido=true (via ok=true).
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

// TestEfetivoBancoTemPrecedenciaSobreEnv cobre o critério de aceite central
// do ADR-0023: valor gravado no banco vence a env var, mesmo com ela setada.
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

// TestEfetivoRemoverChaveVoltaParaEnv cobre o critério de aceite: um PUT que
// remove a chave do values_json (simulado aqui gravando o objeto sem ela)
// faz o Resolver voltar a cair na env var.
func TestEfetivoRemoverChaveVoltaParaEnv(t *testing.T) {
	d := setupBanco(t)
	t.Setenv("MYMEDIADB_TESTE_TOKEN", "do-ambiente")
	gravarValores(t, d, "tmdb", `{"token":"do-banco"}`)
	r := NovoResolver(d)
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN"}

	// 1ª leitura: banco vence.
	if _, origem, _ := r.Efetivo(context.Background(), "tmdb", campo); origem != "banco" {
		t.Fatalf("pré-condição: origem = %q, quero banco", origem)
	}

	// Remove a chave "token" do values_json (objeto vazio) -- equivalente ao
	// PUT {"valores":{"token":null}}.
	gravarValores(t, d, "tmdb", `{}`)

	valor, origem, ok := r.Efetivo(context.Background(), "tmdb", campo)
	if !ok || valor != "do-ambiente" || origem != "env" {
		t.Errorf("Efetivo após remover a chave = (%q,%q,%v), quero (do-ambiente,env,true)", valor, origem, ok)
	}
}

// TestEfetivoSemSlugCaiParaEnv cobre o caso "slug nunca gravado" (sql.ErrNoRows
// dentro do Resolver): degrada em silêncio para a env var, sem propagar erro.
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

// TestEfetivoJSONCorrompidoCaiParaEnv prova que um values_json corrompido no
// banco (defensivo -- não deveria acontecer via API) degrada para a env var,
// em vez de quebrar a resolução.
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

// TestResolverNilSeguro prova que um *Resolver nil (defensivo, cenário de
// teste de rota sem banco) não panica e degrada direto para a env var.
func TestResolverNilSeguro(t *testing.T) {
	t.Setenv("MYMEDIADB_TESTE_TOKEN", "do-ambiente")
	var r *Resolver
	campo := Campo{Nome: "token", EnvVar: "MYMEDIADB_TESTE_TOKEN"}

	valor, origem, ok := r.Efetivo(context.Background(), "tmdb", campo)
	if !ok || valor != "do-ambiente" || origem != "env" {
		t.Errorf("Efetivo (resolver nil) = (%q,%q,%v), quero (do-ambiente,env,true)", valor, origem, ok)
	}
}

// TestValoresAgregaTodosOsCampos prova que Valores agrega Efetivo para cada
// Campo, ignorando os que ficaram sem valor (ok=false).
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
