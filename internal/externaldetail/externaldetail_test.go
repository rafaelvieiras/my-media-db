package externaldetail

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// fakeProv implementa provider.Provider sem rede: BuscarDetalhe conta as chamadas e
// devolve um raw fixo (ou erro). Nome() casa o `source` pedido.
type fakeProv struct {
	nome     string
	raw      json.RawMessage
	erro     error
	chamadas int
}

func (f *fakeProv) Nome() string { return f.nome }
func (f *fakeProv) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (f *fakeProv) BuscarDetalhe(_ context.Context, _, _ string, _ []string) (json.RawMessage, error) {
	f.chamadas++
	if f.erro != nil {
		return nil, f.erro
	}
	return f.raw, nil
}
func (f *fakeProv) MapearDetalhe(json.RawMessage) (map[string]any, error) { return nil, nil }

func setupDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(db.Config{Path: filepath.Join(t.TempDir(), "ed.sqlite")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return database
}

// TestPersisteNaPrimeiraServeDoCache: a 1a chamada puxa e grava; a 2a (fresca, mesmo
// conjunto de idiomas) serve do cache SEM bater na fonte.
func TestPersisteNaPrimeiraServeDoCache(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{"detail":{"id":603},"tags_by_lang":{}}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	langs := []string{"pt-BR", "en"}
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs); err != nil {
		t.Fatalf("PayloadFresco #1: %v", err)
	}
	if fake.chamadas != 1 {
		t.Fatalf("chamadas apos #1 = %d, quero 1", fake.chamadas)
	}
	// Persistiu?
	row, err := sqlcgen.New(database.Read).ObterExternalDetail(ctx, sqlcgen.ObterExternalDetailParams{Source: "tmdb", SourceID: "603"})
	if err != nil {
		t.Fatalf("cache nao persistiu: %v", err)
	}
	if row.Type != "movie" {
		t.Errorf("type gravado = %q, quero movie", row.Type)
	}

	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs); err != nil {
		t.Fatalf("PayloadFresco #2: %v", err)
	}
	if fake.chamadas != 1 {
		t.Errorf("chamadas apos #2 = %d, quero 1 (cache)", fake.chamadas)
	}
}

// TestRefreshQuandoIdiomaNovo: um idioma recem-permitido nao esta no payload guardado,
// entao a proxima leitura re-puxa (gatilho por conjunto de idiomas).
func TestRefreshQuandoIdiomaNovo(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{"detail":{"id":603}}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", []string{"pt-BR"}); err != nil {
		t.Fatalf("PayloadFresco #1: %v", err)
	}
	// Agora "en" foi permitido: nao esta no payload guardado -> re-puxa.
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", []string{"pt-BR", "en"}); err != nil {
		t.Fatalf("PayloadFresco #2: %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("chamadas = %d, quero 2 (refresh por idioma novo)", fake.chamadas)
	}
}

// TestRefreshQuandoTTLExpira: passado o TTL, a proxima leitura re-puxa.
func TestRefreshQuandoTTLExpira(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{"detail":{"id":603}}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)
	// Ancora no relogio REAL: o fetched_at do cache e carimbado pelo banco (strftime
	// 'now'), entao uma data ABSOLUTA fixa fica no passado com o tempo e o carimbo real
	// passa a parecer "no futuro" -> cache falso-fresco -> teste quebra (o que ocorreu
	// ao real-now cruzar a data antiga). base = agora real, congelado; depois avancamos
	// alem do TTL para forcar o refresh.
	base := svc.agora()
	svc.agora = func() time.Time { return base }

	langs := []string{"pt-BR"}
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs); err != nil {
		t.Fatalf("PayloadFresco #1: %v", err)
	}
	// Avanca o relogio alem do TTL.
	svc.agora = func() time.Time { return base.Add(2 * time.Hour) }
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs); err != nil {
		t.Fatalf("PayloadFresco #2: %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("chamadas = %d, quero 2 (refresh por TTL)", fake.chamadas)
	}
}

// TestFonteDesconhecida: uma fonte sem provider correspondente devolve ErrNaoEncontrado.
func TestFonteDesconhecida(t *testing.T) {
	database := setupDB(t)
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	_, err := svc.PayloadFresco(context.Background(), "tvdb", "1", "movie", []string{"pt-BR"})
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

// TestErroDaFontePropaga: um erro real da fonte propaga e nada e cacheado.
func TestErroDaFontePropaga(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", erro: errors.New("timeout")}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", []string{"pt-BR"}); err == nil {
		t.Fatal("quero erro da fonte propagado")
	}
	if _, err := sqlcgen.New(database.Read).ObterExternalDetail(ctx, sqlcgen.ObterExternalDetailParams{Source: "tmdb", SourceID: "603"}); err == nil {
		t.Error("nao deveria ter cacheado apos falha da fonte")
	}
}
