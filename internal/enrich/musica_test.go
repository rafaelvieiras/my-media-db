package enrich

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// MBIDs fixos usados pelo fakeMusicaProvider abaixo. UUIDs sintéticos, não reais.
const (
	mbidArtistaFixture = "aaaaaaaa-0000-0000-0000-000000000001"
	mbidAlbumFixture   = "bbbbbbbb-0000-0000-0000-000000000001"
	mbidFaixa1Fixture  = "cccccccc-0000-0000-0000-000000000001"
	mbidFaixa2Fixture  = "cccccccc-0000-0000-0000-000000000002"
	mbidFeatFixture    = "dddddddd-0000-0000-0000-000000000001"
)

// fakeMusicaProvider implementa o core provider.Provider (mínimo) + a capacidade
// provider.MusicaProvider, sem rede real: BuscarArtistaArvore devolve uma árvore fixa
// com 1 álbum de 2 faixas — a 2ª faixa com artist-credit DIVERGENTE (feat) do artista
// principal, cobrindo o caso do ADR-0027 item 4.
type fakeMusicaProvider struct{}

func (fakeMusicaProvider) Nome() string { return "musicbrainz" }

func (fakeMusicaProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (fakeMusicaProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (fakeMusicaProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (fakeMusicaProvider) BuscarArtistaArvore(_ context.Context, sourceID string, _ []string) (provider.ArtistaArvore, error) {
	if sourceID != mbidArtistaFixture {
		return provider.ArtistaArvore{}, provider.ErrNaoEncontrado
	}
	return provider.ArtistaArvore{
		Name: "Fake Band",
		MBID: mbidArtistaFixture,
		Bio:  "uma banda fake para teste",
		Albuns: []provider.AlbumExterno{
			{
				Name:  "Fake Album",
				MBID:  mbidAlbumFixture,
				Year:  "1999",
				Genre: "rock",
				Faixas: []provider.FaixaExterna{
					{
						Title:              "Track One",
						MBID:               mbidFaixa1Fixture,
						TrackNumber:        1,
						DiscNumber:         1,
						DiscTotal:          1,
						ISRC:               "US1234500001",
						ArtistaCreditoNome: "Fake Band",
						ArtistaCreditoMBID: mbidArtistaFixture,
					},
					{
						Title:              "Track Two (feat. Guest)",
						MBID:               mbidFaixa2Fixture,
						TrackNumber:        2,
						DiscNumber:         1,
						DiscTotal:          1,
						ArtistaCreditoNome: "Guest Artist",
						ArtistaCreditoMBID: mbidFeatFixture,
					},
				},
			},
		},
	}, nil
}

// svcMusicaFake monta o Servico com o fakeMusicaProvider registrado sob "musicbrainz".
func svcMusicaFake(t *testing.T) (*Servico, *db.DB) {
	t.Helper()
	database := setupDB(t)
	svc := New(database, provider.NovaRegistry(fakeMusicaProvider{}), t.TempDir())
	return svc, database
}

// TestImportarArtistaCriaArvore: importar tipo=artista cria o artista + 1 álbum + 2
// faixas, ligados por entity_relation (music_artista_principal/music_album), e a
// faixa com artist-credit divergente ganha TAMBÉM music_artista_feat, sem substituir o
// vínculo music_artista_principal do álbum.
func TestImportarArtistaCriaArvore(t *testing.T) {
	svc, database := svcMusicaFake(t)
	ctx := context.Background()

	artistaID, criada, err := svc.Importar(ctx, "musicbrainz", mbidArtistaFixture, tipoArtista, []string{"en"})
	if err != nil {
		t.Fatalf("Importar artista: %v", err)
	}
	if !criada {
		t.Error("criada = false, quero true (artista novo)")
	}

	r := sqlcgen.New(database.Read)
	artista, err := r.ObterEntity(ctx, artistaID)
	if err != nil {
		t.Fatalf("ObterEntity artista: %v", err)
	}
	if artista.Type != tipoArtista || artista.Title != "Fake Band" {
		t.Errorf("artista = {type:%q title:%q}, quero {artista, Fake Band}", artista.Type, artista.Title)
	}
	if artista.ExternalIDMbid.String != mbidArtistaFixture {
		t.Errorf("artista external_id_mbid = %q, quero %s", artista.ExternalIDMbid.String, mbidArtistaFixture)
	}

	albuns := filhosPorKind(t, database, artistaID, musicext.KindArtistaPrincipal)
	if len(albuns) != 1 {
		t.Fatalf("albuns do artista = %d, quero 1", len(albuns))
	}
	albumID := albuns[0].ChildID
	album, err := r.ObterEntity(ctx, albumID)
	if err != nil {
		t.Fatalf("ObterEntity album: %v", err)
	}
	if album.Type != tipoAlbum || album.Title != "Fake Album" || album.ReleaseDate.String != "1999" {
		t.Errorf("album = {type:%q title:%q year:%q}, quero {album, Fake Album, 1999}", album.Type, album.Title, album.ReleaseDate.String)
	}
	nsAlbum := extNSMusica(t, album.ExtensionDataJson.String)
	if nsAlbum["genre"] != "rock" {
		t.Errorf("album genre = %v, quero rock", nsAlbum["genre"])
	}

	faixas := filhosPorKind(t, database, albumID, musicext.KindAlbum)
	if len(faixas) != 2 {
		t.Fatalf("faixas do album = %d, quero 2", len(faixas))
	}
	if faixas[0].SortIndex.Int64 != 1 || faixas[1].SortIndex.Int64 != 2 {
		t.Errorf("sort_index das faixas = %d,%d, quero 1,2", faixas[0].SortIndex.Int64, faixas[1].SortIndex.Int64)
	}

	faixa1, err := r.ObterEntity(ctx, faixas[0].ChildID)
	if err != nil {
		t.Fatalf("ObterEntity faixa1: %v", err)
	}
	nsF1 := extNSMusica(t, faixa1.ExtensionDataJson.String)
	if nsF1["track_number"] != float64(1) || nsF1["isrc"] != "US1234500001" {
		t.Errorf("faixa1 extension = %v, quero track_number 1 / isrc US1234500001", nsF1)
	}

	faixa2ID := faixas[1].ChildID
	// A faixa 2 NÃO deve ganhar vínculo music_artista_feat com o próprio artista
	// principal (o teste de divergência é a faixa 2 → artista GUEST, abaixo).
	featDoArtistaPrincipal := filhosPorKind(t, database, artistaID, musicext.KindArtistaFeat)
	if len(featDoArtistaPrincipal) != 0 {
		t.Errorf("artista principal com vinculo feat = %d, quero 0 (feat e' de OUTRO artista)", len(featDoArtistaPrincipal))
	}

	// O artista GUEST (feat) foi criado e ligado à faixa 2 por music_artista_feat.
	guest, err := r.ObterEntityPorMbid(ctx, nsValid(mbidFeatFixture))
	if err != nil {
		t.Fatalf("ObterEntityPorMbid guest: %v", err)
	}
	if guest.Type != tipoArtista || guest.Title != "Guest Artist" {
		t.Errorf("guest = {type:%q title:%q}, quero {artista, Guest Artist}", guest.Type, guest.Title)
	}
	featDoGuest := filhosPorKind(t, database, guest.ID, musicext.KindArtistaFeat)
	if len(featDoGuest) != 1 || featDoGuest[0].ChildID != faixa2ID {
		t.Fatalf("vinculo feat do guest = %+v, quero 1 ligado a faixa2 %s", featDoGuest, faixa2ID)
	}
}

// TestReimportarArtistaIdempotente: re-importar o mesmo MBID de artista reusa o
// artista, o álbum, as faixas e o artista feat — sem duplicar nenhuma entity nem
// entity_relation (ADR-0027/briefing 0106, critério de aceite de idempotência).
func TestReimportarArtistaIdempotente(t *testing.T) {
	svc, database := svcMusicaFake(t)
	ctx := context.Background()

	id1, criada1, err := svc.Importar(ctx, "musicbrainz", mbidArtistaFixture, tipoArtista, []string{"en"})
	if err != nil || !criada1 {
		t.Fatalf("1o import: id=%s criada=%v err=%v", id1, criada1, err)
	}
	id2, criada2, err := svc.Importar(ctx, "musicbrainz", mbidArtistaFixture, tipoArtista, []string{"en"})
	if err != nil {
		t.Fatalf("2o import: %v", err)
	}
	if id2 != id1 {
		t.Errorf("reimport criou novo artista: %s != %s", id2, id1)
	}
	if criada2 {
		t.Error("criada = true no reimport, quero false")
	}

	r := sqlcgen.New(database.Read)
	albuns := filhosPorKind(t, database, id1, musicext.KindArtistaPrincipal)
	if len(albuns) != 1 {
		t.Errorf("albuns apos reimport = %d, quero 1 (sem duplicar)", len(albuns))
	}
	faixas := filhosPorKind(t, database, albuns[0].ChildID, musicext.KindAlbum)
	if len(faixas) != 2 {
		t.Errorf("faixas apos reimport = %d, quero 2 (sem duplicar)", len(faixas))
	}
	guest, err := r.ObterEntityPorMbid(ctx, nsValid(mbidFeatFixture))
	if err != nil {
		t.Fatalf("ObterEntityPorMbid guest apos reimport: %v", err)
	}
	featDoGuest := filhosPorKind(t, database, guest.ID, musicext.KindArtistaFeat)
	if len(featDoGuest) != 1 {
		t.Errorf("vinculo feat do guest apos reimport = %d, quero 1 (sem duplicar)", len(featDoGuest))
	}
}

// TestImportarArtistaSemSuporte: uma fonte que NAO implementa MusicaProvider devolve
// erro (o handler traduz para 502).
func TestImportarArtistaSemSuporte(t *testing.T) {
	database := setupDB(t)
	fakeProv := &fakeProvider{raw: json.RawMessage(`{}`), campos: map[string]any{}}
	svc, _ := novoServico(t, database, fakeProv)
	if _, _, err := svc.Importar(context.Background(), "tmdb", mbidArtistaFixture, tipoArtista, []string{"en"}); err == nil {
		t.Fatal("quero erro: provider sem suporte a musica")
	}
}

// extNSMusica decodifica o extension_data_json e devolve o objeto do namespace
// core.music@1.
func extNSMusica(t *testing.T, raw string) map[string]any {
	t.Helper()
	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("extension_data invalido (%q): %v", raw, err)
	}
	ns, ok := envelope[musicext.Namespace]
	if !ok {
		t.Fatalf("extension_data sem namespace %s: %q", musicext.Namespace, raw)
	}
	return ns
}
