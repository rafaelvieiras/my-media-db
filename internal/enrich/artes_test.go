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
	artes    []provider.Arte
	recebeu  provider.ExternalIDs
	tipo     string
	chamadas int // conta as invocacoes de BuscarArtes (0087: prova ausencia de dupla-chamada)
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

// TestEnriquecerFilmeForaDoImportPersisteInventarioArtes (0087, causa 1): um filme
// enriquecido via Enriquecer (nao Importar -- o caminho de auto-match/retroativo/
// Corrigir/Confirmar candidato/POST /entities/{id}/enrich) passa a ter o inventario
// fanart.tv gravado, igual ao import manual. Repetir a chamada nao duplica a linha
// (UPSERT por entity_id+source; ObterMediaExternalPorFonte e :one -- erraria se
// duplicasse).
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
	inv := lerInventario(t, r, "e1") // :one -- so passa se nao duplicou
	if len(inv["poster"]) != 1 {
		t.Errorf("poster inventario = %d, quero 1", len(inv["poster"]))
	}
	if arte.recebeu.Tmdb != "603" || arte.tipo != "movie" {
		t.Errorf("BuscarArtes recebeu ids=%+v tipo=%q, quero Tmdb=603 tipo=movie", arte.recebeu, arte.tipo)
	}
}

// TestImportarFilmeNaoDuplicaChamadaDeArtes (0087): o fix da causa 1 reusa o parametro
// sincronizarArvore para NAO disparar enriquecerArtes de dentro de enriquecerBase quando a
// chamada vem de importarItemBase (sincronizarArvore=false) -- quem dispara e' o proprio
// importarItem, explicitamente, uma unica vez por chamada de Importar (seja criando, seja
// reusando um item existente). Sem essa guarda, o import de um filme JA EXISTENTE
// chamaria BuscarArtes DUAS vezes (uma da enriquecerBase reusada, outra do gatilho de
// importarItem).
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

	// #1: cria o filme -- BuscarArtes deve ser chamado exatamente 1 vez (nao 0, nao 2).
	id1, criada1, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"en"})
	if err != nil || !criada1 {
		t.Fatalf("Importar #1: id=%s criada=%v err=%v", id1, criada1, err)
	}
	if arte.chamadas != 1 {
		t.Errorf("chamadas apos import #1 = %d, quero 1", arte.chamadas)
	}

	// #2: reusa o MESMO filme (dedup por external_id_tmdb) -- passa pelo branch de
	// reuse de importarItemBase (enriquecerBase com sincronizarArvore=false). Ainda assim
	// deve somar so +1 chamada (feita pelo gatilho explicito de importarItem), nao +2.
	id2, criada2, err := svc.Importar(ctx, "tmdb", "603", "movie", []string{"en"})
	if err != nil || criada2 || id2 != id1 {
		t.Fatalf("Importar #2: id=%s criada=%v err=%v, quero reuso de %s", id2, criada2, err, id1)
	}
	if arte.chamadas != 2 {
		t.Errorf("chamadas apos import #2 = %d, quero 2 (1 por chamada de Importar, sem duplicar)", arte.chamadas)
	}
}

// TestEnriquecerArtistaForaDoImportPersisteInventarioArtesEChaveMbid (briefing 0119):
// espelha TestEnriquecerFilmeForaDoImportPersisteInventarioArtes para a vertical de
// musica -- um artista LEGADO (sem external_ids, criado antes do MusicBrainz existir)
// que ganha a identidade via Enriquecer (o caminho generico de auto-match/retroativo/
// "Corrigir"/"Confirmar candidato", nunca importarArtista/ImportarFaixaPossuida) passa
// a ter o inventario fanart.tv gravado, igual ao import de arvore. Tambem prova que a
// identidade fica sob a chave "mbid" (musicext.ChaveExternalIDMbid) -- nao sob o nome
// literal da fonte "musicbrainz" -- que e a chave que idsExternosDe/ObterEntityPorMbid
// realmente leem. Repetir a chamada nao duplica o inventario (mesma prova de
// TestEnriquecerFilmeForaDoImportPersisteInventarioArtes, ObterMediaExternalPorFonte e
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
	inv := lerInventario(t, r, "a1") // :one -- so passa se nao duplicou
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
