package enrich

import (
	"context"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// TestExtensaoEpisodioIncluiStill (0042): extensaoEpisodio grava `still_url` no envelope
// core.tv@1 QUANDO o provider trouxe um still (StillURL != ""), ao lado do numbering/
// absolute_number — o still é artwork NEUTRO de idioma, só a URL crua (nada baixado).
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

// TestExtensaoEpisodioSemStill (0042): sem still (StillURL vazio) a chave `still_url`
// NAO entra no envelope (omitida, nao vazia) — a UI cai no placeholder. O numbering
// segue presente (o still e opcional, nao substitui o resto do envelope).
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

// TestImportarSerieGravaStillDoEpisodio (0042): importar a arvore grava o still_url cru
// do provider no extension_data core.tv@1 de cada episodio com still — SEM baixar a
// imagem (so a URL). O fake serve /still-<id>.png; conferimos o ep 103.
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
