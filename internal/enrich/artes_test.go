package enrich

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// fakeArteProvider implementa o core provider.Provider NO-OP (Nome "fanarttv", como a
// fanart.tv real) MAIS a capacidade provider.ArteProvider, devolvendo artes canned. Grava
// os ids/tipo recebidos para as assercoes.
type fakeArteProvider struct {
	artes   []provider.Arte
	recebeu provider.ExternalIDs
	tipo    string
}

func (f *fakeArteProvider) Nome() string { return "fanarttv" }

func (f *fakeArteProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (f *fakeArteProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (f *fakeArteProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (f *fakeArteProvider) BuscarArtes(_ context.Context, ids provider.ExternalIDs, tipo string, _ []string) ([]provider.Arte, error) {
	f.recebeu = ids
	f.tipo = tipo
	return f.artes, nil
}

// lerInventario le o envelope de inventario da fanart.tv (media_external source=fanarttv)
// de uma entity e devolve o mapa kind->[]ArteInventario. Falha o teste se ausente.
func lerInventario(t *testing.T, database *sqlcgen.Queries, entityID string) map[string][]provider.ArteInventario {
	t.Helper()
	row, err := database.ObterMediaExternalPorFonte(context.Background(), sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: entityID,
		Source:   "fanarttv",
	})
	if err != nil {
		t.Fatalf("inventario fanarttv ausente para %s: %v", entityID, err)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(row.PayloadJson.String), &env); err != nil {
		t.Fatalf("envelope invalido: %v", err)
	}
	var inv map[string][]provider.ArteInventario
	if err := json.Unmarshal(env[provider.ChaveEnvelopeInventarioArtes], &inv); err != nil {
		t.Fatalf("inventario invalido: %v", err)
	}
	return inv
}

// TestImportarFilmePersisteInventarioSemBaixar: importar um filme com um ArteProvider
// registrado persiste o inventario como media_external source=fanarttv (envelope por
// kind, com lang/likes), SEM materializar assets das artes de arte (nenhum binario disc/
// logo baixado). O poster do TMDB (fluxo primario) segue como asset origin=external.
func TestImportarFilmePersisteInventarioSemBaixar(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER-TMDB"))

	tmdb := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Matrix"}`),
		campos: map[string]any{"title": "Matrix", "poster_url": img.URL + "/p.jpg"},
	}
	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/p1.jpg", Lang: "en", Likes: 10},
		{Kind: "logo", URL: "https://assets.fanart.tv/l1.png", Lang: "en", Likes: 5},
		{Kind: "disc", URL: "https://assets.fanart.tv/d1.png", Disc: 1, DiscType: "bluray"},
	}}
	svc := New(database, provider.NovaRegistry(tmdb, arte), t.TempDir())

	id, _, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"en"})
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}

	// o ArteProvider recebeu o id externo do item e o tipo movie.
	if arte.recebeu.Tmdb != "603" || arte.tipo != "movie" {
		t.Errorf("BuscarArtes recebeu ids=%+v tipo=%q, quero Tmdb=603 tipo=movie", arte.recebeu, arte.tipo)
	}

	r := sqlcgen.New(database.Read)
	inv := lerInventario(t, r, id)
	assertInventarioFilme(t, inv)

	// LAZY: nenhum asset de logo/disc baixado (so o poster do TMDB vira asset).
	assets, err := r.ListarAssetsPorEntity(ctx, id)
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	assertNenhumAssetDeArteLazy(t, assets)
}

// assertInventarioFilme confere o envelope de inventario (poster/logo/disc) do filme.
func assertInventarioFilme(t *testing.T, inv map[string][]provider.ArteInventario) {
	t.Helper()
	if len(inv["poster"]) != 1 || inv["poster"][0].Lang != "en" || inv["poster"][0].Likes != 10 {
		t.Errorf("poster inventario = %+v, quero 1 en likes=10", inv["poster"])
	}
	if len(inv["logo"]) != 1 {
		t.Errorf("logo inventario = %d, quero 1", len(inv["logo"]))
	}
	if d := inv["disc"]; len(d) != 1 || d[0].Disc != 1 || d[0].DiscType != "bluray" {
		t.Errorf("disc inventario = %+v, quero 1 disc=1 bluray", d)
	}
}

// assertNenhumAssetDeArteLazy prova que nenhuma arte lazy (logo/disc/clearart) virou
// asset materializado — só o poster do fluxo primario (TMDB) baixa.
func assertNenhumAssetDeArteLazy(t *testing.T, assets []sqlcgen.Asset) {
	t.Helper()
	for _, a := range assets {
		if a.Kind == "logo" || a.Kind == "disc" || a.Kind == "clearart" {
			t.Errorf("asset %q materializado, mas o inventario deve ser LAZY", a.Kind)
		}
	}
}

// TestImportarSerieRoteiaArtesDeTemporada: importar uma serie com um ArteProvider que
// devolve artes de temporada roteia cada arte a entity ALVO: Season vazio/"all" -> serie;
// Season "1" -> temporada 1; Season inexistente -> descartada.
func TestImportarSerieRoteiaArtesDeTemporada(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/png", []byte("PNG"))

	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/tv.jpg", Lang: "en"},     // serie
		{Kind: "poster", URL: "https://assets.fanart.tv/all.jpg", Season: "all"}, // serie
		{Kind: "poster", URL: "https://assets.fanart.tv/s1.jpg", Season: "1"},    // temporada 1
		{Kind: "poster", URL: "https://assets.fanart.tv/s99.jpg", Season: "99"},  // inexistente -> drop
	}}
	svc := New(database, provider.NovaRegistry(fake.New(img.URL), arte), t.TempDir())

	serieID, _, err := svc.Importar(ctx, "tmdb", fake.SerieFixtureID, "tv", []string{"en"})
	if err != nil {
		t.Fatalf("Importar serie: %v", err)
	}
	if arte.tipo != "tv" {
		t.Errorf("BuscarArtes tipo = %q, quero tv", arte.tipo)
	}

	r := sqlcgen.New(database.Read)
	// serie: tv.jpg + all.jpg = 2 posters.
	invSerie := lerInventario(t, r, serieID)
	if len(invSerie["poster"]) != 2 {
		t.Errorf("poster da serie = %d, quero 2 (tv + all)", len(invSerie["poster"]))
	}

	// temporada 1: s1.jpg = 1 poster.
	temp1 := temporadaComSortIndex(t, r, serieID, 1)
	invTemp := lerInventario(t, r, temp1)
	if len(invTemp["poster"]) != 1 || invTemp["poster"][0].Season != "1" {
		t.Errorf("poster da temporada 1 = %+v, quero 1 com season=1", invTemp["poster"])
	}
}

// temporadaComSortIndex acha o id da entity temporada de sort_index dado sob a serie.
func temporadaComSortIndex(t *testing.T, r *sqlcgen.Queries, serieID string, idx int) string {
	t.Helper()
	rels, err := r.ListarRelacoesFilhas(context.Background(), serieID)
	if err != nil {
		t.Fatalf("ListarRelacoesFilhas: %v", err)
	}
	for _, rel := range rels {
		if rel.Kind == relacaoTemporada && rel.SortIndex.Valid && int(rel.SortIndex.Int64) == idx {
			return rel.ChildID
		}
	}
	t.Fatalf("temporada sort_index=%d nao encontrada", idx)
	return ""
}
