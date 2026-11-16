package enrich

// serie_reenrich_test.go covers the re-enrichment of a series: re-importing a series that is already
// in the catalog UPDATES the base of the existing seasons and episodes (numbering/still_url, title,
// synopsis, date, external_ids) — before, the reuse returned early without rewriting — WITHOUT touching
// status/tags nor the owner's media_override, and without re-downloading a season poster already present.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// TestReimportarSerieAtualizaEpisodioBase: an episode imported BEFORE the still_url support
// (with no still_url in the extension_data and with a stale title/synopsis/date/ids) starts to
// reflect the current provider data on re-import — in particular the still_url and the
// numbering, which only the create used to write.
func TestReimportarSerieAtualizaEpisodioBase(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	r := sqlcgen.New(database.Read)
	epID := idPorTmdb(t, r, "103")

	// Simulates the "pre still_url" state: stale base and extension_data WITHOUT still_url/numbering.
	w := sqlcgen.New(database.Write)
	if err := w.AtualizarEpisodioBase(ctx, sqlcgen.AtualizarEpisodioBaseParams{
		Title:             "TITULO VELHO",
		Description:       nsOrNull(""),
		ReleaseDate:       nsOrNull(""),
		ExternalIdsJson:   nsValid(`{"tmdb":"103"}`),
		ExtensionDataJson: nsValid("{}"),
		ID:                epID,
	}); err != nil {
		t.Fatalf("clobber base do ep103: %v", err)
	}

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("2o import (re-enrich): %v", err)
	}

	exigirEpisodio103Refrescado(t, r, epID)
}

// exigirEpisodio103Refrescado checks that the base of ep103 reflects the current provider data
// after the re-import: title/synopsis/date/ids and the extension_data (still_url +
// numbering) that only the create used to write.
func exigirEpisodio103Refrescado(t *testing.T, r *sqlcgen.Queries, epID string) {
	t.Helper()
	ep, err := r.ObterEntity(context.Background(), epID)
	if err != nil {
		t.Fatalf("ObterEntity ep103: %v", err)
	}
	if ep.Title != "Episode 103" {
		t.Errorf("title apos re-import = %q, quero Episode 103 (refrescado)", ep.Title)
	}
	if ep.Description.String != "Overview of episode 103" {
		t.Errorf("description apos re-import = %q, quero a sinopse do provider", ep.Description.String)
	}
	if ep.ReleaseDate.String != "1990-01-21" {
		t.Errorf("release_date apos re-import = %q, quero 1990-01-21", ep.ReleaseDate.String)
	}
	if ep.ExternalIDTvdb.String != "9103" {
		t.Errorf("external_id_tvdb apos re-import = %q, quero 9103 (ids refrescados)", ep.ExternalIDTvdb.String)
	}
	ns := extNS(t, ep.ExtensionDataJson.String)
	if still, _ := ns["still_url"].(string); !strings.HasSuffix(still, "/still-103.png") {
		t.Errorf("still_url apos re-import = %v, quero terminar em /still-103.png (apareceu na base existente)", ns["still_url"])
	}
	if numbering, ok := ns["numbering"].(map[string]any); !ok || numbering["official"] == nil {
		t.Errorf("numbering ausente apos re-import: %v", ns["numbering"])
	}
}

// TestReimportarSerieAtualizaTemporadaBase: a season with a stale base goes back to
// reflecting the title/year/extension_data of the provider on re-import.
func TestReimportarSerieAtualizaTemporadaBase(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	serieID := idPorTmdb(t, sqlcgen.New(database.Read), fake.SerieFixtureID)
	temp1 := filhosPorKind(t, database, serieID, relacaoTemporada)[0].ChildID

	w := sqlcgen.New(database.Write)
	if err := w.AtualizarTemporadaBase(ctx, sqlcgen.AtualizarTemporadaBaseParams{
		Title:             "TEMP VELHA",
		ReleaseDate:       nsOrNull(""),
		ExtensionDataJson: nsValid("{}"),
		ID:                temp1,
	}); err != nil {
		t.Fatalf("clobber base da temporada 1: %v", err)
	}

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("2o import (re-enrich): %v", err)
	}

	r := sqlcgen.New(database.Read)
	tp, err := r.ObterEntity(ctx, temp1)
	if err != nil {
		t.Fatalf("ObterEntity temporada 1: %v", err)
	}
	if tp.Title != "Temporada 1" {
		t.Errorf("titulo da temporada apos re-import = %q, quero Temporada 1", tp.Title)
	}
	if tp.ReleaseDate.String != "1989" {
		t.Errorf("release_date da temporada apos re-import = %q, quero 1989", tp.ReleaseDate.String)
	}
	ns := extNS(t, tp.ExtensionDataJson.String)
	if ns["season_number"] != float64(1) || ns["year"] != "1989" {
		t.Errorf("extension_data da temporada apos re-import = %v, quero season_number 1 / year 1989", ns)
	}
}

// TestReimportarPreservaOverride: the re-import refreshes the BASE (the resolution applies
// override ⟵ external ⟵ file), but it NEVER touches media_override. Here the owner writes an
// override; after the re-import the override row stays byte-identical while the base
// changes — proving that the base can be rewritten safely (the override still wins
// in the resolution; the precedence itself is covered by resolver_test).
func TestReimportarPreservaOverride(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	r := sqlcgen.New(database.Read)
	epID := idPorTmdb(t, r, "103")

	const overrideJSON = `{"title":"Titulo Escolhido Pelo Dono"}`
	w := sqlcgen.New(database.Write)
	if _, err := w.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID:     epID,
		OverrideJson: nsValid(overrideJSON),
	}); err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}
	// Ages the base to guarantee that the re-import really rewrites.
	if err := w.AtualizarEpisodioBase(ctx, sqlcgen.AtualizarEpisodioBaseParams{
		Title: "TITULO VELHO", Description: nsOrNull(""), ReleaseDate: nsOrNull(""),
		ExternalIdsJson: nsValid(`{"tmdb":"103"}`), ExtensionDataJson: nsValid("{}"), ID: epID,
	}); err != nil {
		t.Fatalf("clobber base: %v", err)
	}

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("2o import: %v", err)
	}

	ov, err := r.ObterMediaOverride(ctx, epID)
	if err != nil {
		t.Fatalf("ObterMediaOverride apos re-import: %v", err)
	}
	if ov.OverrideJson.String != overrideJSON {
		t.Errorf("override_json apos re-import = %q, quero intacto %q", ov.OverrideJson.String, overrideJSON)
	}
	if ep, _ := r.ObterEntity(ctx, epID); ep.Title != "Episode 103" {
		t.Errorf("base apos re-import = %q, quero Episode 103 (base refrescada, override intacto)", ep.Title)
	}
}

// TestReimportarNaoAlteraStatusTags: the re-import writes only the source fields;
// status and tags (curation by the owner, outside the refresh queries) stay untouched.
func TestReimportarNaoAlteraStatusTags(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	r := sqlcgen.New(database.Read)
	epID := idPorTmdb(t, r, "103")

	// Curation by the owner directly on the row (outside the source queries).
	const status, tags = "wanted", `["favorito"]`
	if _, err := database.Write.ExecContext(ctx,
		"UPDATE entity SET status = ?, tags_json = ? WHERE id = ?", status, tags, epID); err != nil {
		t.Fatalf("marcar status/tags: %v", err)
	}

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("2o import: %v", err)
	}

	ep, err := r.ObterEntity(ctx, epID)
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ep.Status != status {
		t.Errorf("status apos re-import = %q, quero %q (intocado)", ep.Status, status)
	}
	if ep.TagsJson.String != tags {
		t.Errorf("tags_json apos re-import = %q, quero %q (intocado)", ep.TagsJson.String, tags)
	}
}

// TestReimportarPosterTemporadaSoSeAusente: the season poster is downloaded on the 1st
// import; on the re-import, being already present, it is NOT re-downloaded (only-if-absent) — cheap idempotence.
func TestReimportarPosterTemporadaSoSeAusente(t *testing.T) {
	ctx := context.Background()
	database := setupDB(t)
	srv, hits := servidorImagemContado(t)
	svc := New(database, provider.NovaRegistry(fake.New(srv.URL)), t.TempDir())

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	primeiro := hits.paraSeason()
	if primeiro == 0 {
		t.Fatal("nenhum poster de temporada baixado no 1o import, quero >=1")
	}

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("2o import: %v", err)
	}
	if depois := hits.paraSeason(); depois != primeiro {
		t.Errorf("downloads de poster de temporada apos re-import = %d, quero %d (nao re-baixa presente)", depois, primeiro)
	}
}

// idPorTmdb resolves the internal id of an entity through its tmdb external id.
func idPorTmdb(t *testing.T, r *sqlcgen.Queries, tmdbID string) string {
	t.Helper()
	ent, err := r.ObterEntityPorTmdb(context.Background(), nsValid(tmdbID))
	if err != nil {
		t.Fatalf("ObterEntityPorTmdb %s: %v", tmdbID, err)
	}
	return ent.ID
}

// contadorHits counts the image requests per path category.
type contadorHits struct {
	mu     sync.Mutex
	season int
}

func (c *contadorHits) paraSeason() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.season
}

// servidorImagemContado serves fake PNGs and counts the GETs of the season poster
// (/season-*.png), to prove the only-if-absent behaviour without depending on the poster of the series node.
func servidorImagemContado(t *testing.T) (*httptest.Server, *contadorHits) {
	t.Helper()
	hits := &contadorHits{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "season-") {
			hits.mu.Lock()
			hits.season++
			hits.mu.Unlock()
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNG-FAKE"))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}
