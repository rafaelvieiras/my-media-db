package enrich

// serie_reenrich_test.go cobre o briefing 0043: re-importar uma série já no catálogo
// ATUALIZA a base das temporadas e episódios existentes (numbering/still_url, título,
// sinopse, data, external_ids) — antes o reuse retornava cedo sem reescrever — SEM tocar
// status/tags nem o media_override do dono, e sem re-baixar pôster de temporada já presente.

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

// TestReimportarSerieAtualizaEpisodioBase (0043): um episódio importado ANTES do 0042
// (sem still_url no extension_data e com título/sinopse/data/ids defasados) passa a
// refletir os dados atuais do provider no re-import — em especial o still_url e o
// numbering, que só o create gravava.
func TestReimportarSerieAtualizaEpisodioBase(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	r := sqlcgen.New(database.Read)
	epID := idPorTmdb(t, r, "103")

	// Simula o estado "pre-0042": base defasada e extension_data SEM still_url/numbering.
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

// exigirEpisodio103Refrescado confere que a base do ep103 reflete os dados atuais do
// provider apos o re-import: titulo/sinopse/data/ids e o extension_data (still_url +
// numbering) que so o create gravava.
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

// TestReimportarSerieAtualizaTemporadaBase (0043): uma temporada com base defasada volta
// a refletir título/ano/extension_data do provider no re-import.
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

// TestReimportarPreservaOverride (0043): o re-import refresca a BASE (a resolução aplica
// override ⟵ externo ⟵ arquivo), mas NUNCA toca media_override. Aqui o dono grava um
// override; após o re-import a linha de override permanece byte-idêntica enquanto a base
// muda — provando que a base pode ser reescrita com segurança (o override segue vencendo
// na resolução; a precedência em si é coberta pelo resolver_test).
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
	// Defasa a base para garantir que o re-import de fato reescreve.
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

// TestReimportarNaoAlteraStatusTags (0043): o re-import escreve só os campos de fonte;
// status e tags (curadoria do dono, fora das queries de refresh) ficam intocados.
func TestReimportarNaoAlteraStatusTags(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("1o import: %v", err)
	}
	r := sqlcgen.New(database.Read)
	epID := idPorTmdb(t, r, "103")

	// Curadoria do dono direto na linha (fora das queries de fonte).
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

// TestReimportarPosterTemporadaSoSeAusente (0043): o pôster de temporada é baixado no 1o
// import; no re-import, já presente, NÃO é re-baixado (só-se-ausente) — idempotência barata.
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

// idPorTmdb resolve o id interno de uma entity pelo seu id externo tmdb.
func idPorTmdb(t *testing.T, r *sqlcgen.Queries, tmdbID string) string {
	t.Helper()
	ent, err := r.ObterEntityPorTmdb(context.Background(), nsValid(tmdbID))
	if err != nil {
		t.Fatalf("ObterEntityPorTmdb %s: %v", tmdbID, err)
	}
	return ent.ID
}

// contadorHits conta as requisições de imagem por categoria de caminho.
type contadorHits struct {
	mu     sync.Mutex
	season int
}

func (c *contadorHits) paraSeason() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.season
}

// servidorImagemContado serve PNGs fake e conta os GETs de pôster de temporada
// (/season-*.png), para provar o só-se-ausente sem depender do pôster do nó série.
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
