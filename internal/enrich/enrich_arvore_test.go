package enrich

// enrich_arvore_test.go covers the tree sync on Enriquecer: Enriquecer (used by Corrigir,
// Confirmar candidato, auto-match and retroactive/enrich-missing) now syncs
// the season/episode tree whenever tipo=='tv', instead of only POST /import
// (importarSerie) doing that.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// TestEnriquecerSerieSincronizaArvore: Enriquecer on an ALREADY EXISTING entity with
// type='tv' (without a tree yet, like a draft series from the scanner that gained an external id
// through Corrigir/auto-match) builds the season/episode tree, exactly as
// importarSerie (POST /import) already built it.
func TestEnriquecerSerieSincronizaArvore(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/png", []byte("PNG-FAKE"))

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	svc := New(database, provider.NovaRegistry(fake.New(img.URL)), t.TempDir())

	if err := svc.Enriquecer(ctx, "s1", "tmdb", fake.SerieFixtureID, tipoSerie, []string{"en"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTmdb.String != fake.SerieFixtureID {
		t.Errorf("external_id_tmdb = %q, quero %s (identidade aplicada)", ent.ExternalIDTmdb.String, fake.SerieFixtureID)
	}
	temps := filhosPorKind(t, database, "s1", relacaoTemporada)
	if len(temps) != 2 {
		t.Fatalf("temporadas = %d, quero 2 (Enriquecer sincronizou a arvore)", len(temps))
	}
	total := 0
	for _, tp := range temps {
		total += len(filhosPorKind(t, database, tp.ChildID, relacaoEpisodio))
	}
	if total != 6 {
		t.Errorf("episodios = %d, quero 6", total)
	}
}

// TestEnriquecerSerieArvoreFalhaDegrada: when the tree fetch fails (source without
// SerieProvider support, e.g.: provider unavailable/degraded), Enriquecer does NOT
// return an error -- the identity/title/poster are still applied normally, only the
// tree is missing (best-effort, it only logs).
func TestEnriquecerSerieArvoreFalhaDegrada(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "s1"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	// fakeProvider (defined in enrich_test.go) does NOT implement provider.SerieProvider:
	// arvoreDaSerie fails on the capability assertion, exactly like a provider that
	// never supported series.
	fk := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Serie X"}`),
		campos: map[string]any{"title": "Serie X", "poster_url": img.URL + "/p.jpg"},
	}
	svc, _ := novoServico(t, database, fk)

	if err := svc.Enriquecer(ctx, "s1", "tmdb", "999", tipoSerie, []string{"en"}, false); err != nil {
		t.Fatalf("Enriquecer nao deveria falhar por causa da arvore: %v", err)
	}

	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTmdb.String != "999" {
		t.Errorf("external_id_tmdb = %q, quero 999 (identidade aplicada mesmo com arvore falhando)", ent.ExternalIDTmdb.String)
	}
	if _, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: "s1", Kind: "poster", Origin: "external"}); err != nil {
		t.Errorf("poster deveria ter sido cacheado mesmo com a arvore falhando: %v", err)
	}
	if temps := filhosPorKind(t, database, "s1", relacaoTemporada); len(temps) != 0 {
		t.Errorf("temporadas = %d, quero 0 (arvore falhou, degradou)", len(temps))
	}
}

// provSerieContavel wraps the fake.Provider (which implements provider.SerieProvider)
// counting the calls to BuscarSerieArvore -- used to prove that Enriquecer on
// movie/collection NEVER tries to sync a tree (the sincronizarArvore parameter of
// enriquecerBase is a no-op outside tipo=='tv').
type provSerieContavel struct {
	*fake.Provider
	chamadasArvore int
}

func (p *provSerieContavel) BuscarSerieArvore(ctx context.Context, sourceID string, langs []string) (provider.SerieArvore, error) {
	p.chamadasArvore++
	return p.Provider.BuscarSerieArvore(ctx, sourceID, langs)
}

// TestEnriquecerMovieNaoSincronizaArvore: Enriquecer on a movie keeps behaving
// exactly as before -- in particular, it NEVER tries to fetch the
// series tree (the fake counts the calls to BuscarSerieArvore).
func TestEnriquecerMovieNaoSincronizaArvore(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/png", []byte("PNG-FAKE"))

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "m1", Type: "movie", Title: "arquivo.mkv"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	contavel := &provSerieContavel{Provider: fake.New(img.URL)}
	svc := New(database, provider.NovaRegistry(contavel), t.TempDir())

	if err := svc.Enriquecer(ctx, "m1", "tmdb", "603", "movie", []string{"en"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}
	if contavel.chamadasArvore != 0 {
		t.Errorf("chamadas a BuscarSerieArvore = %d, quero 0 (movie nao sincroniza arvore)", contavel.chamadasArvore)
	}
	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, "m1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTmdb.String != "603" {
		t.Errorf("external_id_tmdb = %q, quero 603 (identidade aplicada normalmente)", ent.ExternalIDTmdb.String)
	}
}
