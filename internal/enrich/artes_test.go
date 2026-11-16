package enrich

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// fakeArteProvider implements the provider.Provider core as a NO-OP (Nome "fanarttv", like
// the real fanart.tv) PLUS the provider.ArteProvider capability, returning canned artworks. It records
// the ids/type received for the assertions.
type fakeArteProvider struct {
	artes    []provider.Arte
	recebeu  provider.ExternalIDs
	tipo     string
	chamadas int // counts the BuscarArtes invocations (proves the absence of a double call)
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
	f.chamadas++
	f.recebeu = ids
	f.tipo = tipo
	return f.artes, nil
}

// lerInventario reads the inventory envelope of fanart.tv (media_external source=fanarttv)
// of an entity and returns the kind->[]ArteInventario map. It fails the test when absent.
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

// TestImportarFilmePersisteInventarioSemBaixar: importing a movie with an ArteProvider
// registered persists the inventory as media_external source=fanarttv (envelope by
// kind, with lang/likes), WITHOUT materializing assets of the artworks (no disc/
// logo binary downloaded). The TMDB poster (primary flow) stays as an asset with origin=external.
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

	// the ArteProvider received the external id of the item and the movie type.
	if arte.recebeu.Tmdb != "603" || arte.tipo != "movie" {
		t.Errorf("BuscarArtes recebeu ids=%+v tipo=%q, quero Tmdb=603 tipo=movie", arte.recebeu, arte.tipo)
	}

	r := sqlcgen.New(database.Read)
	inv := lerInventario(t, r, id)
	assertInventarioFilme(t, inv)

	// LAZY: no logo/disc asset downloaded (only the TMDB poster becomes an asset).
	assets, err := r.ListarAssetsPorEntity(ctx, id)
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity: %v", err)
	}
	assertNenhumAssetDeArteLazy(t, assets)
}

// assertInventarioFilme checks the inventory envelope (poster/logo/disc) of the movie.
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

// assertNenhumAssetDeArteLazy proves that no lazy artwork (logo/disc/clearart) became a
// materialized asset — only the poster of the primary flow (TMDB) is downloaded.
func assertNenhumAssetDeArteLazy(t *testing.T, assets []sqlcgen.Asset) {
	t.Helper()
	for _, a := range assets {
		if a.Kind == "logo" || a.Kind == "disc" || a.Kind == "clearart" {
			t.Errorf("asset %q materializado, mas o inventario deve ser LAZY", a.Kind)
		}
	}
}

// TestImportarSerieRoteiaArtesDeTemporada: importing a series with an ArteProvider that
// returns season artworks routes each artwork to the TARGET entity: Season empty/"all" -> series;
// Season "1" -> season 1; Season that does not exist -> discarded.
func TestImportarSerieRoteiaArtesDeTemporada(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/png", []byte("PNG"))

	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/tv.jpg", Lang: "en"},     // series
		{Kind: "poster", URL: "https://assets.fanart.tv/all.jpg", Season: "all"}, // series
		{Kind: "poster", URL: "https://assets.fanart.tv/s1.jpg", Season: "1"},    // season 1
		{Kind: "poster", URL: "https://assets.fanart.tv/s99.jpg", Season: "99"},  // missing -> drop
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
	// series: tv.jpg + all.jpg = 2 posters.
	invSerie := lerInventario(t, r, serieID)
	if len(invSerie["poster"]) != 2 {
		t.Errorf("poster da serie = %d, quero 2 (tv + all)", len(invSerie["poster"]))
	}

	// season 1: s1.jpg = 1 poster.
	temp1 := temporadaComSortIndex(t, r, serieID, 1)
	invTemp := lerInventario(t, r, temp1)
	if len(invTemp["poster"]) != 1 || invTemp["poster"][0].Season != "1" {
		t.Errorf("poster da temporada 1 = %+v, quero 1 com season=1", invTemp["poster"])
	}
}

// TestEnriquecerFilmeForaDoImportPersisteInventarioArtes (cause 1): a movie
// enriched through Enriquecer (not Importar -- the auto-match/retroactive/
// Corrigir/Confirmar candidato/POST /entities/{id}/enrich path) now has the fanart.tv
// inventory written, just like the manual import. Repeating the call does not duplicate the
// row (UPSERT by entity_id+source; ObterMediaExternalPorFonte is :one -- it would fail if it
// duplicated).
func TestEnriquecerFilmeForaDoImportPersisteInventarioArtes(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/jpeg", []byte("POSTER"))

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "e1", Type: "movie", Title: "arquivo.mkv"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	tmdb := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Matrix"}`),
		campos: map[string]any{"title": "Matrix", "poster_url": img.URL + "/p.jpg"},
	}
	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/p1.jpg", Lang: "en"},
	}}
	svc := New(database, provider.NovaRegistry(tmdb, arte), t.TempDir())

	for i := 0; i < 2; i++ {
		if err := svc.Enriquecer(ctx, "e1", "tmdb", "603", "movie", []string{"en"}, false); err != nil {
			t.Fatalf("Enriquecer #%d: %v", i, err)
		}
	}

	r := sqlcgen.New(database.Read)
	inv := lerInventario(t, r, "e1") // :one -- it only passes when it did not duplicate
	if len(inv["poster"]) != 1 {
		t.Errorf("poster inventario = %d, quero 1", len(inv["poster"]))
	}
	if arte.recebeu.Tmdb != "603" || arte.tipo != "movie" {
		t.Errorf("BuscarArtes recebeu ids=%+v tipo=%q, quero Tmdb=603 tipo=movie", arte.recebeu, arte.tipo)
	}
}

// TestImportarFilmeNaoDuplicaChamadaDeArtes: the fix reuses the sincronizarArvore parameter
// so as NOT to fire enriquecerArtes from inside enriquecerBase when the
// call comes from importarItemBase (sincronizarArvore=false) -- the one that fires is
// importarItem itself, explicitly, a single time per Importar call (either creating or
// reusing an existing item). Without that guard, importing an ALREADY EXISTING movie
// would call BuscarArtes TWICE (one from the reused enriquecerBase, one from the
// importarItem trigger).
func TestImportarFilmeNaoDuplicaChamadaDeArtes(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()
	img := servidorImagem(t, "image/png", []byte("PNG"))

	tmdb := &fakeProvider{
		raw:    json.RawMessage(`{"title":"Matrix"}`),
		campos: map[string]any{"title": "Matrix", "poster_url": img.URL + "/p.png"},
	}
	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "poster", URL: "https://assets.fanart.tv/p1.jpg"},
	}}
	svc := New(database, provider.NovaRegistry(tmdb, arte), t.TempDir())

	// #1: create the movie -- BuscarArtes must be called exactly 1 time (not 0, not 2).
	id1, criada1, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"en"})
	if err != nil || !criada1 {
		t.Fatalf("Importar #1: id=%s criada=%v err=%v", id1, criada1, err)
	}
	if arte.chamadas != 1 {
		t.Errorf("chamadas apos import #1 = %d, quero 1", arte.chamadas)
	}

	// #2: reuse the SAME movie (dedup by external_id_tmdb) -- it goes through the
	// reuse branch of importarItemBase (enriquecerBase with sincronizarArvore=false). Even so
	// it must add only +1 call (made by the explicit trigger of importarItem), not +2.
	id2, criada2, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"en"})
	if err != nil || criada2 || id2 != id1 {
		t.Fatalf("Importar #2: id=%s criada=%v err=%v, quero reuso de %s", id2, criada2, err, id1)
	}
	if arte.chamadas != 2 {
		t.Errorf("chamadas apos import #2 = %d, quero 2 (1 por chamada de Importar, sem duplicar)", arte.chamadas)
	}
}

// TestEnriquecerArtistaForaDoImportPersisteInventarioArtesEChaveMbid:
// it mirrors TestEnriquecerFilmeForaDoImportPersisteInventarioArtes for the music
// vertical -- a LEGACY artist (without external_ids, created before MusicBrainz existed)
// that gains the identity through Enriquecer (the generic path of auto-match/retroactive/
// "Corrigir"/"Confirmar candidato", never importarArtista/ImportarFaixaPossuida) now
// has the fanart.tv inventory written, just like the tree import. It also proves that the
// identity stays under the "mbid" key (musicext.ChaveExternalIDMbid) -- not under the
// literal source name "musicbrainz" -- which is the key that idsExternosDe/ObterEntityPorMbid
// really read. Repeating the call does not duplicate the inventory (same proof as
// TestEnriquecerFilmeForaDoImportPersisteInventarioArtes, ObterMediaExternalPorFonte is
// :one).
func TestEnriquecerArtistaForaDoImportPersisteInventarioArtesEChaveMbid(t *testing.T) {
	database := setupDB(t)
	ctx := context.Background()

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "a1", Type: tipoArtista, Title: "Nome Legado"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	arte := &fakeArteProvider{artes: []provider.Arte{
		{Kind: "artistthumb", URL: "https://assets.fanart.tv/thumb.jpg"},
	}}
	svc := New(database, provider.NovaRegistry(fakeMusicaProvider{}, arte), t.TempDir())

	for i := 0; i < 2; i++ {
		if err := svc.Enriquecer(ctx, "a1", "musicbrainz", mbidArtistaFixture, tipoArtista, []string{"en"}, false); err != nil {
			t.Fatalf("Enriquecer #%d: %v", i, err)
		}
	}

	if arte.recebeu.Mbid != mbidArtistaFixture || arte.tipo != tipoArtista {
		t.Errorf("BuscarArtes recebeu ids=%+v tipo=%q, quero Mbid=%s tipo=%s", arte.recebeu, arte.tipo, mbidArtistaFixture, tipoArtista)
	}

	r := sqlcgen.New(database.Read)
	inv := lerInventario(t, r, "a1") // :one -- it only passes when it did not duplicate
	if len(inv["artistthumb"]) != 1 {
		t.Errorf("artistthumb inventario = %d, quero 1", len(inv["artistthumb"]))
	}

	ent, err := r.ObterEntity(ctx, "a1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	var ids map[string]string
	if err := json.Unmarshal([]byte(ent.ExternalIdsJson.String), &ids); err != nil {
		t.Fatalf("external_ids invalido: %v", err)
	}
	if ids["mbid"] != mbidArtistaFixture {
		t.Errorf(`external_ids["mbid"] = %q, quero %q`, ids["mbid"], mbidArtistaFixture)
	}
	if _, ok := ids["musicbrainz"]; ok {
		t.Errorf(`external_ids nao deveria ter a chave "musicbrainz" (so "mbid")`)
	}
}

// temporadaComSortIndex finds the id of the season entity with the given sort_index under the series.
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
