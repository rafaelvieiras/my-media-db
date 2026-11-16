package enrich

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// tvdbArvoreFake is the series TREE source (source="tvdb"): it implements SerieProvider.
// MapearDetalhe returns the identity (title/description/original_language) + poster, which
// the rich enrichment must NOT clobber. Minimal tree (no seasons) — the focus is the series node.
type tvdbArvoreFake struct{ imgURL string }

func (tvdbArvoreFake) Nome() string { return "tvdb" }
func (tvdbArvoreFake) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (tvdbArvoreFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{"tvdb":true}`), nil
}
func (f tvdbArvoreFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{
		"title":             "The Simpsons",
		"description":       "Springfield family.",
		"original_language": "en",
		"poster_url":        f.imgURL + "/poster.jpg",
	}, nil
}
func (tvdbArvoreFake) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{DefaultSeasonType: "official", SeasonTypes: []string{"official"}}, nil
}

// tmdbRicoFake is the "tmdb" provider that exposes ONLY the series rich capability: it is NOT
// the source of the tree. It returns the rich canonical (only the rich keys) for the expected tvdbID.
type tmdbRicoFake struct {
	backdropURL string
	chamadas    int
	ultimoTvdb  string
}

func (tmdbRicoFake) Nome() string { return "tmdb" }
func (tmdbRicoFake) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (tmdbRicoFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (tmdbRicoFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *tmdbRicoFake) BuscarSerieDetalheRico(_ context.Context, tvdbID string, _ []string) (map[string]any, bool, error) {
	f.chamadas++
	f.ultimoTvdb = tvdbID
	if tvdbID != "71663" {
		return nil, false, nil
	}
	return map[string]any{
		"tagline":                 map[string]any{"en": "Slogan"},
		"keywords":                map[string]any{"en": []string{"satire"}},
		"certification_by_region": map[string]any{"US": "TV-PG"},
		"tags":                    map[string]any{"en": []string{"Animation"}},
		"similar":                 []any{map[string]any{"source_id": "1434", "type": "tv", "title": "Family Guy"}},
		"backdrop_url":            f.backdropURL + "/bg.jpg",
		"airing_status":           "Returning Series",
	}, true, nil
}

// svcSerieRico builds the Servico with the TVDB tree + the TMDB rich enricher and an image server
// (the TVDB poster and the TMDB fanart are downloaded from it).
func svcSerieRico(t *testing.T) (*Servico, *db.DB, *tmdbRicoFake) {
	t.Helper()
	database := setupDB(t)
	img := servidorImagem(t, "image/jpeg", []byte("ART-BYTES"))
	rico := &tmdbRicoFake{backdropURL: img.URL}
	svc := New(database, provider.NovaRegistry(tvdbArvoreFake{imgURL: img.URL}, rico), t.TempDir())
	return svc, database, rico
}

// TestImportarSerieMesclaRicoTMDB: importing the series from TVDB writes, BESIDES the
// TVDB layer, the media_external layer "tmdb" with the rich canonical (tagline/keywords/similar/
// rating/tags/status/backdrop) WITHOUT clobbering the TVDB identity, and downloads the fanart.
func TestImportarSerieMesclaRicoTMDB(t *testing.T) {
	svc, database, rico := svcSerieRico(t)
	ctx := context.Background()

	serieID, criada, err := svc.Importar(ctx, "tvdb", "71663", "tv", []string{"en"})
	if err != nil || !criada {
		t.Fatalf("Importar serie: id=%s criada=%v err=%v", serieID, criada, err)
	}
	if rico.chamadas != 1 || rico.ultimoTvdb != "71663" {
		t.Errorf("BuscarSerieDetalheRico chamadas=%d tvdb=%q, quero 1 chamada com 71663", rico.chamadas, rico.ultimoTvdb)
	}

	r := sqlcgen.New(database.Read)
	// TVDB tree identity intact (not clobbered by the rich layer).
	if serie, err := r.ObterEntity(ctx, serieID); err != nil || serie.Title != "The Simpsons" {
		t.Fatalf("serie.title = %q err=%v, quero The Simpsons (TVDB)", serie.Title, err)
	}

	exigirCamadaRica(t, serieRicoPersistido(t, database, serieID))
	exigirArtesSerie(t, r, serieID)

	// Idempotent: re-importing rewrites the SAME row (without duplicating).
	if _, _, err := svc.Importar(ctx, "tvdb", "71663", "tv", []string{"en"}); err != nil {
		t.Fatalf("reimport: %v", err)
	}
	_ = serieRicoPersistido(t, database, serieID) // ObterMediaExternalPorFonte is :one — it would fail if it duplicated
}

// arvoreGenericaFake is a tree base source with an ARBITRARY name (not "tvdb"): it proves
// that the selection of the base and of the rich source is name-agnostic. MapearDetalhe
// gives the identity (not clobberable). Minimal tree (no seasons).
type arvoreGenericaFake struct{ nome string }

func (f arvoreGenericaFake) Nome() string { return f.nome }
func (arvoreGenericaFake) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (arvoreGenericaFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{"base":true}`), nil
}
func (arvoreGenericaFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{"title": "Base Show", "original_language": "en"}, nil
}
func (arvoreGenericaFake) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{DefaultSeasonType: "official", SeasonTypes: []string{"official"}}, nil
}

// ricoGenericoFake is a rich enricher with an ARBITRARY name (not "tmdb"). It captures the
// external id it received — it must be the sourceID of the base source, proving that idExternoDaSerie
// is generic. It returns NO backdrop (no artwork download in the test).
type ricoGenericoFake struct {
	nome     string
	chamadas int
	ultimoID string
}

func (f ricoGenericoFake) Nome() string { return f.nome }
func (ricoGenericoFake) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (ricoGenericoFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (ricoGenericoFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f *ricoGenericoFake) BuscarSerieDetalheRico(_ context.Context, id string, _ []string) (map[string]any, bool, error) {
	f.chamadas++
	f.ultimoID = id
	return map[string]any{"tagline": map[string]any{"en": "Slogan generico"}, "airing_status": "Ended"}, true, nil
}

// TestEnriquecerSerieRicoAgnostico: with sources of ARBITRARY names, the tree comes
// from the base source and the OTHER provider (the rich one) enriches the series node, crossing the
// sourceID of the base source. It proves that there is no hardcode of "tmdb"/"tvdb": the rich layer
// is written under the name of the rich source and the rich one received the id of the base source.
func TestEnriquecerSerieRicoAgnostico(t *testing.T) {
	database := setupDB(t)
	rico := &ricoGenericoFake{nome: "beta"}
	svc := New(database, provider.NovaRegistry(arvoreGenericaFake{nome: "alfa"}, rico), t.TempDir())
	ctx := context.Background()

	serieID, _, err := svc.Importar(ctx, "alfa", "555", "tv", []string{"en"})
	if err != nil {
		t.Fatalf("Importar serie (fonte-base alfa): %v", err)
	}
	if rico.chamadas != 1 || rico.ultimoID != "555" {
		t.Fatalf("rico chamadas=%d id=%q, quero 1 chamada com o sourceID da fonte-base (555)", rico.chamadas, rico.ultimoID)
	}
	// The rich layer was written under the NAME of the rich source ("beta"), not "tmdb".
	row, err := sqlcgen.New(database.Read).ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: serieID,
		Source:   "beta",
	})
	if err != nil {
		t.Fatalf("camada media_external da fonte rica 'beta' ausente: %v", err)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(row.PayloadJson.String), &env); err != nil {
		t.Fatalf("payload rico invalido: %v", err)
	}
	if _, ok := env[provider.ChaveEnvelopeSerieRico]; !ok {
		t.Errorf("payload da fonte rica sem envelope %q: %q", provider.ChaveEnvelopeSerieRico, row.PayloadJson.String)
	}
}

// TestImportarSerieSoTMDBSemRico: with ONLY the base source registered (no other rich
// provider), the tree imports normally and NO rich layer is written — no panic, no
// error (the series stays with the tree only). It covers the "single-provider" path.
func TestImportarSerieSoTMDBSemRico(t *testing.T) {
	database := setupDB(t)
	svc := New(database, provider.NovaRegistry(arvoreGenericaFake{nome: "alfa"}), t.TempDir())
	ctx := context.Background()

	serieID, criada, err := svc.Importar(ctx, "alfa", "555", "tv", []string{"en"})
	if err != nil || !criada {
		t.Fatalf("Importar serie so-fonte-base: id=%s criada=%v err=%v", serieID, criada, err)
	}
	// There is no OTHER rich provider: no media_external layer of cross enrichment.
	if _, err := sqlcgen.New(database.Read).ObterMediaExternalPorFonte(ctx, sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: serieID,
		Source:   "beta",
	}); err == nil {
		t.Error("nao deveria haver camada rica sem um segundo provider")
	}
}

// exigirCamadaRica checks that the persisted rich canonical carries THE contract keys
// and the raw status, and does NOT carry title/description/original_language (it would clobber the tree).
func exigirCamadaRica(t *testing.T, rico map[string]any) {
	t.Helper()
	for _, chave := range []string{"tagline", "keywords", "certification_by_region", "tags", "similar", "backdrop_url", "airing_status"} {
		if _, ok := rico[chave]; !ok {
			t.Errorf("canonico rico persistido sem %q: %v", chave, rico)
		}
	}
	if rico["airing_status"] != "Returning Series" {
		t.Errorf("airing_status = %v, quero Returning Series", rico["airing_status"])
	}
	for _, proibido := range []string{"title", "description", "original_language"} {
		if _, existe := rico[proibido]; existe {
			t.Errorf("camada rica NAO deveria carregar %q (clobbaria o TVDB)", proibido)
		}
	}
}

// exigirArtesSerie checks that the FANART was downloaded from backdrop_url (closing the
// gap) and that the TVDB POSTER coexists (the two artworks live together).
func exigirArtesSerie(t *testing.T, r *sqlcgen.Queries, serieID string) {
	t.Helper()
	ctx := context.Background()
	fan, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: serieID, Kind: "fanart", Origin: "external"})
	if err != nil {
		t.Fatalf("fanart da serie ausente: %v", err)
	}
	if b, _ := os.ReadFile(fan.PathOrUri); string(b) != "ART-BYTES" {
		t.Errorf("binario da fanart diverge do baixado")
	}
	if _, err := r.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{EntityID: serieID, Kind: "poster", Origin: "external"}); err != nil {
		t.Errorf("poster do TVDB deveria coexistir com a fanart: %v", err)
	}
}

// serieRicoPersistido reads the media_external layer "tmdb" of the series node and unwraps the
// rich canonical from the ChaveEnvelopeSerieRico envelope.
func serieRicoPersistido(t *testing.T, database *db.DB, serieID string) map[string]any {
	t.Helper()
	row, err := sqlcgen.New(database.Read).ObterMediaExternalPorFonte(context.Background(), sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: serieID,
		Source:   "tmdb",
	})
	if err != nil {
		t.Fatalf("media_external tmdb do no serie: %v", err)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(row.PayloadJson.String), &env); err != nil {
		t.Fatalf("payload rico invalido (%q): %v", row.PayloadJson.String, err)
	}
	blob, ok := env[provider.ChaveEnvelopeSerieRico]
	if !ok {
		t.Fatalf("payload sem chave %q: %q", provider.ChaveEnvelopeSerieRico, row.PayloadJson.String)
	}
	var campos map[string]any
	if err := json.Unmarshal(blob, &campos); err != nil {
		t.Fatalf("canonico rico invalido: %v", err)
	}
	return campos
}
