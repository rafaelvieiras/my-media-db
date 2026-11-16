package enrich

// serie_tvdb_cruzado_test.go covers cause 2: when the tree of a
// series comes ONLY from TMDB (no TVDB configured, the provider-independence scenario),
// the tvdb cross-id from the external_ids append (which TMDB already fetches in the rich detail)
// must be captured and merged into entity.external_ids_json -- which unlocks the
// fanart.tv ArteProvider on TV (it requires tvdb_id exclusively). See mesclarTvdbCruzado in
// serie.go.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// tmdbBaseComTvdbFake is the base source of the tree with Nome()="tmdb" (the scenario with no TVDB
// registered, where TMDB is the SerieProvider) that ALSO implements provider.TvdbCruzadoProvider
// -- BuscarDetalhe returns a payload with external_ids.tvdb_id, in the SAME format as the real
// TMDB append (external_ids: {tvdb_id: "..."}), and TvdbIDDoDetalheSerie extracts it
// with NO network, mirroring the real method of the tmdb client.
type tmdbBaseComTvdbFake struct {
	imgURL string
	tvdbID string
}

func (tmdbBaseComTvdbFake) Nome() string { return "tmdb" }
func (tmdbBaseComTvdbFake) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}
func (f tmdbBaseComTvdbFake) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	payload, _ := json.Marshal(map[string]any{
		"external_ids": map[string]any{"tvdb_id": f.tvdbID},
	})
	return json.RawMessage(payload), nil
}
func (f tmdbBaseComTvdbFake) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{
		"title":             "Serie Sem TVDB Configurado",
		"original_language": "en",
		"poster_url":        f.imgURL + "/poster.jpg",
	}, nil
}
func (tmdbBaseComTvdbFake) BuscarSerieArvore(context.Context, string, []string) (provider.SerieArvore, error) {
	return provider.SerieArvore{DefaultSeasonType: "official", SeasonTypes: []string{"official"}}, nil
}
func (f tmdbBaseComTvdbFake) TvdbIDDoDetalheSerie(raw json.RawMessage) (string, bool) {
	var d struct {
		ExternalIds struct {
			TvdbID string `json:"tvdb_id"`
		} `json:"external_ids"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", false
	}
	return d.ExternalIds.TvdbID, d.ExternalIds.TvdbID != ""
}

// TestEnriquecerSerieSemTvdbCapturaCrossId: a series whose tree comes ONLY from TMDB
// (no TVDB configured) gains "tvdb" in external_ids_json after the enrichment, with the
// value matching the tvdb_id of the TMDB external_ids append -- and the fanart.tv
// inventory starts to be written (the ArteProvider receives the freshly captured tvdb, unlocking
// rotaETabela case "tv").
func TestEnriquecerSerieSemTvdbCapturaCrossId(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))
	base := tmdbBaseComTvdbFake{imgURL: img.URL, tvdbID: "81189"}
	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/tv.jpg"},
	}}
	svc := New(database, provider.NovaRegistry(base, arte), t.TempDir())

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Serie"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	if err := svc.Enriquecer(ctx, "s1", "tmdb", "9999", tipoSerie, []string{"en"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTvdb.String != "81189" {
		t.Errorf("external_id_tvdb = %q, quero 81189 (capturado do append external_ids do TMDB)", ent.ExternalIDTvdb.String)
	}
	// The ArteProvider received the freshly captured tvdb IN THE SAME pass (idsExternosDe read the
	// external_ids_json ALREADY merged before enriquecerArtes of the series ran).
	if arte.recebeu.Tvdb != "81189" {
		t.Errorf("BuscarArtes recebeu Tvdb=%q, quero 81189", arte.recebeu.Tvdb)
	}
	// fanart.tv inventory written (before the fix, rotaETabela degraded silently for
	// lack of tvdb_id -- ObterMediaExternalPorFonte is :one, it would fail if absent/duplicated).
	lerInventario(t, r, "s1")
}

// TestEnriquecerSerieSemTvdbIdempotente: repeating the enrichment neither duplicates nor breaks
// -- the merge is always an idempotent ADDITION (mesclarExternalIdSeAusente).
func TestEnriquecerSerieSemTvdbIdempotente(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))
	base := tmdbBaseComTvdbFake{imgURL: img.URL, tvdbID: "81189"}
	svc := New(database, provider.NovaRegistry(base), t.TempDir())

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Serie"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := svc.Enriquecer(ctx, "s1", "tmdb", "9999", tipoSerie, []string{"en"}, false); err != nil {
			t.Fatalf("Enriquecer #%d: %v", i, err)
		}
	}
	ent, err := sqlcgen.New(database.Read).ObterEntity(ctx, "s1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTvdb.String != "81189" {
		t.Errorf("external_id_tvdb = %q, quero 81189 (estavel apos re-enriquecer)", ent.ExternalIDTvdb.String)
	}
}

// TestSerieComTvdbConfiguradoNaoRegride (non-regression criterion): when the
// base source of the tree IS ALREADY TVDB (the tvdb client does not implement TvdbCruzadoProvider),
// mesclarTvdbCruzado is a no-op -- the tvdb keeps coming, as it always came, from
// mesclarExternalIds(source="tvdb"). The fanart.tv inventory is still written normally.
func TestSerieComTvdbConfiguradoNaoRegride(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-BYTES"))
	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/tv.jpg"},
	}}
	svc := New(database, provider.NovaRegistry(tvdbArvoreFake{imgURL: img.URL}, arte), t.TempDir())

	serieID, criada, err := svc.Importar(ctx, "tvdb", "71663", "tv", []string{"en"})
	if err != nil || !criada {
		t.Fatalf("Importar serie: id=%s criada=%v err=%v", serieID, criada, err)
	}

	r := sqlcgen.New(database.Read)
	ent, err := r.ObterEntity(ctx, serieID)
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.ExternalIDTvdb.String != "71663" {
		t.Errorf("external_id_tvdb = %q, quero 71663 (fonte-base, sem alteracao)", ent.ExternalIDTvdb.String)
	}
	if arte.recebeu.Tvdb != "71663" {
		t.Errorf("BuscarArtes recebeu Tvdb=%q, quero 71663 (sem regressao)", arte.recebeu.Tvdb)
	}
	lerInventario(t, r, serieID)
}
