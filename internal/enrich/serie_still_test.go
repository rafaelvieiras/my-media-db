package enrich

import (
	"context"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// TestExtensaoEpisodioIncluiStill: extensaoEpisodio writes `still_url` into the
// core.tv@1 envelope WHEN the provider brought a still (StillURL != ""), alongside the numbering/
// absolute_number — the still is language-NEUTRAL artwork, only the raw URL (nothing downloaded).
func TestExtensaoEpisodioIncluiStill(t *testing.T) {
	ep := provider.EpisodioExterno{
		SourceID:  "1",
		Numbering: map[string]provider.Numero{"official": {Season: 1, Number: 1}},
		StillURL:  "https://artworks.thetvdb.com/banners/episodes/72514/1.jpg",
	}
	ns := extNS(t, extensaoEpisodio(ep))
	if ns["still_url"] != ep.StillURL {
		t.Errorf("still_url = %v, quero %q", ns["still_url"], ep.StillURL)
	}
}

// TestExtensaoEpisodioSemStill: with no still (empty StillURL) the `still_url` key
// does NOT enter the envelope (omitted, not empty) — the UI falls back to the placeholder. The numbering
// stays present (the still is optional, it does not replace the rest of the envelope).
func TestExtensaoEpisodioSemStill(t *testing.T) {
	ep := provider.EpisodioExterno{
		SourceID:  "1",
		Numbering: map[string]provider.Numero{"official": {Season: 1, Number: 1}},
	}
	ns := extNS(t, extensaoEpisodio(ep))
	if _, existe := ns["still_url"]; existe {
		t.Errorf("still_url presente sem still: %v", ns["still_url"])
	}
	if _, ok := ns["numbering"].(map[string]any); !ok {
		t.Error("numbering ausente do envelope do episodio sem still")
	}
}

// TestImportarSerieGravaStillDoEpisodio: importing the tree writes the raw still_url
// of the provider into the core.tv@1 extension_data of each episode with a still — WITHOUT downloading the
// image (only the URL). The fake serves /still-<id>.png; we check ep 103.
func TestImportarSerieGravaStillDoEpisodio(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	if _, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"}); err != nil {
		t.Fatalf("Importar serie: %v", err)
	}
	r := sqlcgen.New(database.Read)
	ep, err := r.ObterEntityPorTmdb(ctx, nsValid("103"))
	if err != nil {
		t.Fatalf("episodio 103 nao encontrado: %v", err)
	}
	ns := extNS(t, ep.ExtensionDataJson.String)
	still, ok := ns["still_url"].(string)
	if !ok || !strings.HasSuffix(still, "/still-103.png") {
		t.Errorf("still_url ep103 = %v, quero terminar em /still-103.png", ns["still_url"])
	}
}
