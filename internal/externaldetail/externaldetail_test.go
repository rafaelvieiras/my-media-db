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

// fakeProv implements provider.Provider without network: BuscarDetalhe counts the
// calls and returns a fixed raw payload (or an error). Nome() matches the requested
// `source`.
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

// TestPersisteNaPrimeiraServeDoCache: the 1st call pulls and writes; the 2nd one
// (fresh, same set of languages) serves from the cache WITHOUT hitting the source.
func TestPersisteNaPrimeiraServeDoCache(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{"detail":{"id":603},"tags_by_lang":{}}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	langs := []string{"pt-BR", "en"}
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs, false); err != nil {
		t.Fatalf("PayloadFresco #1: %v", err)
	}
	if fake.chamadas != 1 {
		t.Fatalf("chamadas apos #1 = %d, quero 1", fake.chamadas)
	}
	// Persisted?
	row, err := sqlcgen.New(database.Read).ObterExternalDetail(ctx, sqlcgen.ObterExternalDetailParams{Source: "tmdb", SourceID: "603"})
	if err != nil {
		t.Fatalf("cache nao persistiu: %v", err)
	}
	if row.Type != "movie" {
		t.Errorf("type gravado = %q, quero movie", row.Type)
	}

	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs, false); err != nil {
		t.Fatalf("PayloadFresco #2: %v", err)
	}
	if fake.chamadas != 1 {
		t.Errorf("chamadas apos #2 = %d, quero 1 (cache)", fake.chamadas)
	}
}

// TestRefreshQuandoIdiomaNovo: a newly allowed language is not in the stored payload,
// so the next read re-pulls (trigger by set of languages).
func TestRefreshQuandoIdiomaNovo(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{"detail":{"id":603}}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", []string{"pt-BR"}, false); err != nil {
		t.Fatalf("PayloadFresco #1: %v", err)
	}
	// Now "en" was allowed: it is not in the stored payload -> re-pull.
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", []string{"pt-BR", "en"}, false); err != nil {
		t.Fatalf("PayloadFresco #2: %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("chamadas = %d, quero 2 (refresh por idioma novo)", fake.chamadas)
	}
}

// TestRefreshQuandoTTLExpira: once the TTL has passed, the next read re-pulls.
func TestRefreshQuandoTTLExpira(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{"detail":{"id":603}}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)
	// Anchor on the REAL clock: the fetched_at of the cache is stamped by the database
	// (strftime 'now'), so a fixed ABSOLUTE date falls into the past as time goes by and
	// the real stamp starts to look like it is "in the future" -> false-fresh cache ->
	// the test breaks (which happened when real-now crossed the old date). base = real
	// now, frozen; afterwards we advance past the TTL to force the refresh.
	base := svc.agora()
	svc.agora = func() time.Time { return base }

	langs := []string{"pt-BR"}
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs, false); err != nil {
		t.Fatalf("PayloadFresco #1: %v", err)
	}
	// Advance the clock past the TTL.
	svc.agora = func() time.Time { return base.Add(2 * time.Hour) }
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs, false); err != nil {
		t.Fatalf("PayloadFresco #2: %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("chamadas = %d, quero 2 (refresh por TTL)", fake.chamadas)
	}
}

// TestForcePulaCacheFrescoEAtualiza: with a FRESH cache (within the TTL) for the same
// source/source_id, force=false keeps serving from the cache (provider NOT consulted
// again); force=true goes to the provider EVEN WITH a fresh cache, and the new result
// stays written in the cache (the next read without force already serves the updated
// payload, without a new trip to the source).
func TestForcePulaCacheFrescoEAtualiza(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{"detail":{"id":603,"v":1}}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	langs := []string{"pt-BR"}
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs, false); err != nil {
		t.Fatalf("PayloadFresco #1: %v", err)
	}
	if fake.chamadas != 1 {
		t.Fatalf("chamadas apos #1 = %d, quero 1", fake.chamadas)
	}

	// Cache still fresh (1h TTL, nothing advanced the clock): force=false serves from the
	// cache, without a new trip to the provider.
	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs, false); err != nil {
		t.Fatalf("PayloadFresco #2 (force=false): %v", err)
	}
	if fake.chamadas != 1 {
		t.Errorf("chamadas apos #2 (force=false) = %d, quero 1 (serviu do cache)", fake.chamadas)
	}

	// force=true ignores the fresh cache and goes to the provider again, with a new payload.
	fake.raw = json.RawMessage(`{"detail":{"id":603,"v":2}}`)
	raw, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs, true)
	if err != nil {
		t.Fatalf("PayloadFresco #3 (force=true): %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("chamadas apos #3 (force=true) = %d, quero 2 (ignorou o cache fresco)", fake.chamadas)
	}
	if string(raw) != string(fake.raw) {
		t.Errorf("payload devolvido = %s, quero o payload novo %s", raw, fake.raw)
	}

	// The cache was UPDATED with the new result: the next read without force already
	// serves the v2 payload, without a new trip to the provider.
	raw2, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", langs, false)
	if err != nil {
		t.Fatalf("PayloadFresco #4 (force=false apos force=true): %v", err)
	}
	if fake.chamadas != 2 {
		t.Errorf("chamadas apos #4 = %d, quero 2 (cache ja atualizado pelo force)", fake.chamadas)
	}
	if string(raw2) != string(fake.raw) {
		t.Errorf("payload do cache apos force = %s, quero o payload novo %s", raw2, fake.raw)
	}
}

// TestFonteDesconhecida: a source with no corresponding provider returns
// ErrNaoEncontrado.
func TestFonteDesconhecida(t *testing.T) {
	database := setupDB(t)
	fake := &fakeProv{nome: "tmdb", raw: json.RawMessage(`{}`)}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	_, err := svc.PayloadFresco(context.Background(), "tvdb", "1", "movie", []string{"pt-BR"}, false)
	if !errors.Is(err, provider.ErrNaoEncontrado) {
		t.Fatalf("erro = %v, quero ErrNaoEncontrado", err)
	}
}

// TestServeCacheQualquerSemProviderDeBusca (ADR-0024): with NO live search provider
// registered for the source (empty registry), an already cached payload (even outside
// the TTL) is served instead of propagating ErrNaoEncontrado -- data already visited
// does not disappear for lack of a credential.
func TestServeCacheQualquerSemProviderDeBusca(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	// Seed the cache directly (with no provider at all to persist through PayloadFresco):
	// svc with NO provider registered (empty Registry).
	svc := New(database, provider.Registry{}, time.Hour)
	if err := sqlcgen.New(database.Write).UpsertExternalDetail(ctx, sqlcgen.UpsertExternalDetailParams{
		Source: "tmdb", SourceID: "603", Type: "movie",
		PayloadJson: nsValid(`{"detail":{"id":603}}`),
		LangsJson:   nsValid(`["pt-BR"]`),
	}); err != nil {
		t.Fatalf("UpsertExternalDetail: %v", err)
	}
	// Force the stamp into the past, beyond the configured 1h TTL.
	if _, err := database.Write.ExecContext(ctx,
		`UPDATE external_detail SET fetched_at = '2000-01-01T00:00:00.000Z' WHERE source='tmdb' AND source_id='603'`); err != nil {
		t.Fatalf("forcar staleness: %v", err)
	}

	raw, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", []string{"pt-BR"}, false)
	if err != nil {
		t.Fatalf("PayloadFresco: quero cache stale servido, veio erro: %v", err)
	}
	if string(raw) != `{"detail":{"id":603}}` {
		t.Errorf("payload = %s, quero o cache stale intacto", raw)
	}
}

// TestServeCacheStaleQuandoBuscaAoVivoFalha (ADR-0024): with a REGISTERED provider
// whose live search fails (network down/revoked key), the cached payload (even stale)
// is served instead of the raw error -- it never loses data already saved because of a
// momentary/permanent unavailability of the source.
func TestServeCacheStaleQuandoBuscaAoVivoFalha(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", erro: errors.New("timeout")}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	if err := sqlcgen.New(database.Write).UpsertExternalDetail(ctx, sqlcgen.UpsertExternalDetailParams{
		Source: "tmdb", SourceID: "603", Type: "movie",
		PayloadJson: nsValid(`{"detail":{"id":603}}`),
		LangsJson:   nsValid(`["pt-BR"]`),
	}); err != nil {
		t.Fatalf("UpsertExternalDetail: %v", err)
	}
	if _, err := database.Write.ExecContext(ctx,
		`UPDATE external_detail SET fetched_at = '2000-01-01T00:00:00.000Z' WHERE source='tmdb' AND source_id='603'`); err != nil {
		t.Fatalf("forcar staleness: %v", err)
	}

	raw, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", []string{"pt-BR"}, false)
	if err != nil {
		t.Fatalf("PayloadFresco: quero cache stale servido apos falha da busca, veio erro: %v", err)
	}
	if string(raw) != `{"detail":{"id":603}}` {
		t.Errorf("payload = %s, quero o cache stale intacto", raw)
	}
	if fake.chamadas != 1 {
		t.Errorf("chamadas = %d, quero 1 (tentou buscar ao vivo antes de cair no stale)", fake.chamadas)
	}
}

// TestErroDaFontePropaga: a real error from the source propagates and nothing is
// cached.
func TestErroDaFontePropaga(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	fake := &fakeProv{nome: "tmdb", erro: errors.New("timeout")}
	svc := New(database, provider.NovaRegistry(fake), time.Hour)

	if _, err := svc.PayloadFresco(ctx, "tmdb", "603", "movie", []string{"pt-BR"}, false); err == nil {
		t.Fatal("quero erro da fonte propagado")
	}
	if _, err := sqlcgen.New(database.Read).ObterExternalDetail(ctx, sqlcgen.ObterExternalDetailParams{Source: "tmdb", SourceID: "603"}); err == nil {
		t.Error("nao deveria ter cacheado apos falha da fonte")
	}
}
