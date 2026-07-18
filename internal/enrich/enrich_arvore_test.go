package enrich

// enrich_arvore_test.go cobre o briefing 0067: Enriquecer (usado por Corrigir,
// Confirmar candidato, auto-match e o retroativo/enrich-missing) passa a sincronizar
// a árvore de temporada/episódio sempre que tipo=='tv', em vez de só POST /import
// (importarSerie) fazer isso.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// TestEnriquecerSerieSincronizaArvore: Enriquecer sobre uma entity type='tv' JÁ
// EXISTENTE (sem árvore ainda, como uma série draft do scanner que ganhou id externo
// via Corrigir/auto-match) constrói a árvore de temporada/episódio, exatamente como
// importarSerie (POST /import) já construía.
func TestEnriquecerSerieSincronizaArvore(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/png", []byte("PNG-FAKE"))

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	svc := New(database, provider.NovaRegistry(fake.New(img.URL)), t.TempDir())

	if err := svc.Enriquecer(ctx, "s1", "tmdb", fake.SerieFixtureID, tipoSerie, []string{"en"}); err != nil {
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

// TestEnriquecerSerieArvoreFalhaDegrada: quando a busca da arvore falha (fonte sem
// suporte a SerieProvider, ex.: provider indisponivel/degradado), Enriquecer NAO
// retorna erro -- a identidade/titulo/poster continuam aplicados normalmente, so a
// arvore fica ausente (best-effort, so loga).
func TestEnriquecerSerieArvoreFalhaDegrada(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "s1"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	// fakeProvider (definido em enrich_test.go) NAO implementa provider.SerieProvider:
	// arvoreDaSerie falha na asserção de capacidade, exatamente como um provider que
	// nunca suportou série.
	fk := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Serie X"}`),
		campos: map[string]any{"title": "Serie X", "poster_url": img.URL + "/p.jpg"},
	}
	svc, _ := novoServico(t, database, fk)

	if err := svc.Enriquecer(ctx, "s1", "tmdb", "999", tipoSerie, []string{"en"}); err != nil {
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

// provSerieContavel embrulha o fake.Provider (que implementa provider.SerieProvider)
// contando as chamadas a BuscarSerieArvore -- usado para provar que Enriquecer sobre
// movie/collection NUNCA tenta sincronizar arvore (o parametro sincronizarArvore de
// enriquecerBase e um no-op fora de tipo=='tv').
type provSerieContavel struct {
	*fake.Provider
	chamadasArvore int
}

func (p *provSerieContavel) BuscarSerieArvore(ctx context.Context, sourceID string, langs []string) (provider.SerieArvore, error) {
	p.chamadasArvore++
	return p.Provider.BuscarSerieArvore(ctx, sourceID, langs)
}

// TestEnriquecerMovieNaoSincronizaArvore: Enriquecer sobre um movie continua se
// comportando exatamente como antes do 0067 -- em especial, NUNCA tenta buscar a
// arvore de serie (o fake conta as chamadas a BuscarSerieArvore).
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

	if err := svc.Enriquecer(ctx, "m1", "tmdb", "603", "movie", []string{"en"}); err != nil {
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
