package enrich

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// TestEpisodioNaoReusaSerieComOMesmoID is the regression test of the production
// corruption found on 17/nov/2026: TMDB episode ids and series ids live in the SAME
// numeric space (series 13943 = "Beavis and Butt-Head", episode 13943 = "Everybody Hates
// PSATs" of series 252). The episode import used to look the external id up WITHOUT the
// type, so it found the SERIES and rewrote it as an episode: the series kept
// type='tv' plus 314 of its own files, took the episode's title and numbering, and 262
// episodes ended up hanging under it as drafts.
//
// The lookup is type-scoped now, so the episode must be created as a NEW entity and the
// series must come out untouched.
func TestEpisodioNaoReusaSerieComOMesmoID(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)

	const idCompartilhado = "13943"
	const serieID = "serie-com-o-mesmo-id"
	serieExt := `{"core.tv@1":{"default_season_type":"official","season_types":["official"]}}`
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                serieID,
		Type:              tipoSerie,
		Title:             "Beavis e Butt-Head",
		ExternalIdsJson:   sql.NullString{String: `{"tmdb":"` + idCompartilhado + `"}`, Valid: true},
		ExtensionDataJson: sql.NullString{String: serieExt, Valid: true},
	}); err != nil {
		t.Fatalf("criar serie: %v", err)
	}

	ep := provider.EpisodioExterno{
		SourceID:  idCompartilhado,
		Title:     map[string]string{"en": "Everybody Hates PSATs"},
		Numbering: map[string]provider.Numero{"official": {Season: 4, Number: 14}},
	}
	epID, err := svc.idDoEpisodio(ctx, "tmdb", ep, []string{"en"})
	if err != nil {
		t.Fatalf("idDoEpisodio: %v", err)
	}
	if epID == serieID {
		t.Fatal("o episódio reusou a série que carrega o mesmo id externo — a busca precisa ser escopada por tipo")
	}

	serie, err := sqlcgen.New(database.Read).ObterEntity(ctx, serieID)
	if err != nil {
		t.Fatalf("reler serie: %v", err)
	}
	if serie.Type != tipoSerie {
		t.Errorf("serie.type = %q, quero %q (a série não pode ser reescrita como episódio)", serie.Type, tipoSerie)
	}
	if serie.Title != "Beavis e Butt-Head" {
		t.Errorf("serie.title = %q, quero %q", serie.Title, "Beavis e Butt-Head")
	}
	if serie.ExtensionDataJson.String != serieExt {
		t.Errorf("extension_data da série foi sobrescrita: %q", serie.ExtensionDataJson.String)
	}

	novo, err := sqlcgen.New(database.Read).ObterEntity(ctx, epID)
	if err != nil {
		t.Fatalf("reler episódio: %v", err)
	}
	if novo.Type != tipoEpisodio {
		t.Errorf("episódio novo tem type = %q, quero %q", novo.Type, tipoEpisodio)
	}
	if novo.ExternalIDTmdb.String != idCompartilhado {
		t.Errorf("episódio external_id_tmdb = %q, quero %q", novo.ExternalIDTmdb.String, idCompartilhado)
	}
}

// TestEpisodioReusaEpisodioComOMesmoID is the other side of the same coin: the reuse
// that IS legitimate must keep working. An episode whose external id already belongs to
// an episode is reused, not duplicated.
func TestEpisodioReusaEpisodioComOMesmoID(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	const idCompartilhado = "13943"
	ep := provider.EpisodioExterno{
		SourceID:  idCompartilhado,
		Title:     map[string]string{"en": "Everybody Hates PSATs"},
		Numbering: map[string]provider.Numero{"official": {Season: 4, Number: 14}},
	}
	primeiro, err := svc.idDoEpisodio(ctx, "tmdb", ep, []string{"en"})
	if err != nil {
		t.Fatalf("primeiro idDoEpisodio: %v", err)
	}
	segundo, err := svc.idDoEpisodio(ctx, "tmdb", ep, []string{"en"})
	if err != nil {
		t.Fatalf("segundo idDoEpisodio: %v", err)
	}
	if primeiro != segundo {
		t.Errorf("ids diferentes (%q != %q): o episódio foi duplicado em vez de reusado", primeiro, segundo)
	}
	if _, err := sqlcgen.New(database.Read).ObterEntity(ctx, primeiro); err != nil {
		t.Fatalf("episódio reusado não existe: %v", err)
	}
}
