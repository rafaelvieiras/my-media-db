package enrich

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Fixed MBIDs used by the fakeMusicaProvider below. Synthetic UUIDs, not real ones.
const (
	mbidArtistaFixture    = "aaaaaaaa-0000-0000-0000-000000000001"
	mbidAlbumFixture      = "bbbbbbbb-0000-0000-0000-000000000001"
	mbidFaixa1Fixture     = "cccccccc-0000-0000-0000-000000000001"
	mbidFaixa2Fixture     = "cccccccc-0000-0000-0000-000000000002"
	mbidFeatFixture       = "dddddddd-0000-0000-0000-000000000001"
	mbidAlbumOutroFixture = "bbbbbbbb-0000-0000-0000-000000000002"
	mbidFaixaOutroFixture = "cccccccc-0000-0000-0000-000000000003"
)

// albumFixture/albumOutroFixture are the TWO albums of the fake discography:
// albumFixture is the only one used by BuscarArtistaArvore (the tree, legacy --
// kept as it always was so as not to change TestImportarArtistaCriaArvore/
// TestReimportarArtistaIdempotente); albumOutroFixture exists only so that
// TestImportarFaixaPossuidaCriaSoUmAlbum can prove that importing 1 track of
// albumFixture does NOT create albumOutroFixture (the discography has MORE than 1 album,
// but the scoped import only touches what was asked for).
func albumFixture() provider.AlbumExterno {
	return provider.AlbumExterno{
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
	}
}

func albumOutroFixture() provider.AlbumExterno {
	return provider.AlbumExterno{
		Name: "Other Album",
		MBID: mbidAlbumOutroFixture,
		Year: "2005",
		Faixas: []provider.FaixaExterna{
			{
				Title:              "Other Track",
				MBID:               mbidFaixaOutroFixture,
				TrackNumber:        1,
				DiscNumber:         1,
				DiscTotal:          1,
				ArtistaCreditoNome: "Fake Band",
				ArtistaCreditoMBID: mbidArtistaFixture,
			},
		},
	}
}

// fakeMusicaProvider implements the (minimal) provider.Provider core + the
// provider.MusicaProvider capability, without a real network: BuscarArtistaArvore returns a fixed
// tree with 1 album of 2 tracks — the 2nd track with an artist-credit DIVERGING (feat) from the main
// artist, covering the case of ADR-0027 item 4.
type fakeMusicaProvider struct{}

func (fakeMusicaProvider) Nome() string { return "musicbrainz" }

func (fakeMusicaProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

// BuscarDetalhe/MapearDetalhe are the MINIMAL core
// (acharOuCriarArtistaIdentidade): they return the name of the fixture artist as
// "title", enough for the SCOPED import to create the entity without touching
// release-groups. Album/track (mbidAlbumFixture/mbidFaixa1Fixture) also
// answer, imitating remapParaNome of the real provider (finding of 20/Aug/2026:
// BuscarDetalhe for those two types ignored the type/sourceID and always
// fetched an artist, breaking "Baixar Metadados" -- see
// TestEnriquecerAlbumEFaixaAtualizaTitulo). Any other sourceID returns
// an empty payload (the title falls back to the MBID, an acceptable behaviour
// for the tests that do not exercise that path).
func (fakeMusicaProvider) BuscarDetalhe(_ context.Context, sourceID, _ string, _ []string) (json.RawMessage, error) {
	switch sourceID {
	case mbidArtistaFixture:
		return json.RawMessage(`{"name":"Fake Band"}`), nil
	case mbidAlbumFixture:
		return json.RawMessage(`{"name":"Fake Album Atualizado"}`), nil
	case mbidFaixa1Fixture:
		return json.RawMessage(`{"name":"Track One Atualizada"}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func (fakeMusicaProvider) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	var d struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if d.Name != "" {
		out["title"] = d.Name
	}
	return out, nil
}

func (fakeMusicaProvider) BuscarArtistaArvore(_ context.Context, sourceID string, _ []string) (provider.ArtistaArvore, error) {
	if sourceID != mbidArtistaFixture {
		return provider.ArtistaArvore{}, provider.ErrNaoEncontrado
	}
	return provider.ArtistaArvore{
		Name:   "Fake Band",
		MBID:   mbidArtistaFixture,
		Bio:    "uma banda fake para teste",
		Albuns: []provider.AlbumExterno{albumFixture()},
	}, nil
}

// BuscarAlbumDaFaixa is the SELECTIVE capability (provider.MusicaAlbumProvider)
// used by the SCOPED import on the MBID path: it builds ONLY the
// album that contains `recordingMBID` -- it searches BOTH albums of the fake
// discography (albumFixture/albumOutroFixture) to prove that the scoped import
// does not need to "know" beforehand which album is the right one.
func (fakeMusicaProvider) BuscarAlbumDaFaixa(_ context.Context, recordingMBID string, _ []string) (provider.AlbumExterno, bool, error) {
	for _, alb := range []provider.AlbumExterno{albumFixture(), albumOutroFixture()} {
		for _, fx := range alb.Faixas {
			if fx.MBID == recordingMBID {
				return alb, true, nil
			}
		}
	}
	return provider.AlbumExterno{}, false, nil
}

// BuscarAlbumPorNome is the SELECTIVE capability (provider.MusicaAlbumProvider)
// used by the SCOPED import on the textual path: it builds ONLY the
// album `nomeAlbum` of the artist `artistaMBID`.
func (fakeMusicaProvider) BuscarAlbumPorNome(_ context.Context, artistaMBID, nomeAlbum string, _ provider.AlvoFaixaAlbum, _ []string) (provider.AlbumExterno, bool, error) {
	if artistaMBID != mbidArtistaFixture {
		return provider.AlbumExterno{}, false, nil
	}
	for _, alb := range []provider.AlbumExterno{albumFixture(), albumOutroFixture()} {
		if alb.Name == nomeAlbum {
			return alb, true, nil
		}
	}
	return provider.AlbumExterno{}, false, nil
}

// BuscarAlbumPorMbid is the SELECTIVE capability (provider.MusicaAlbumProvider)
// used by the external album screen and by
// ImportarFaixaAvulsa: it builds ONLY the album `albumMBID`, with no prior
// resolution step -- ErrNaoEncontrado when none of the fixture albums matches.
func (fakeMusicaProvider) BuscarAlbumPorMbid(_ context.Context, albumMBID string, _ []string) (provider.AlbumExterno, error) {
	for _, alb := range []provider.AlbumExterno{albumFixture(), albumOutroFixture()} {
		if alb.MBID == albumMBID {
			return alb, nil
		}
	}
	return provider.AlbumExterno{}, provider.ErrNaoEncontrado
}

// svcMusicaFake builds the Servico with the fakeMusicaProvider registered under "musicbrainz".
func svcMusicaFake(t *testing.T) (*Servico, *db.DB) {
	t.Helper()
	database := setupDB(t)
	svc := New(database, provider.NovaRegistry(fakeMusicaProvider{}), t.TempDir())
	return svc, database
}

// TestImportarArtistaCriaArvore: importing tipo=artista creates the artist + 1 album + 2
// tracks, linked through entity_relation (music_artista_principal/music_album), and the
// track with a diverging artist-credit ALSO gets music_artista_feat, without replacing the
// music_artista_principal link of the album.
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
	// Track 2 must NOT get a music_artista_feat link with the main artist
	// itself (the divergence test is track 2 → GUEST artist, below).
	featDoArtistaPrincipal := filhosPorKind(t, database, artistaID, musicext.KindArtistaFeat)
	if len(featDoArtistaPrincipal) != 0 {
		t.Errorf("artista principal com vinculo feat = %d, quero 0 (feat e' de OUTRO artista)", len(featDoArtistaPrincipal))
	}

	// The GUEST (feat) artist was created and linked to track 2 through music_artista_feat.
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

// TestReimportarArtistaIdempotente: re-importing the same artist MBID reuses the
// artist, the album, the tracks and the feat artist — without duplicating any entity or
// entity_relation (ADR-0027, idempotence acceptance criterion).
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

// TestImportarFaixaPossuidaCriaSoUmAlbum is the CENTRAL acceptance criterion of the
// scoped import (ADR-0029 items 1/3): matching 1 track through ImportarFaixaPossuida creates ONLY the
// artist + THE ALBUM OF THAT TRACK + the track itself -- the fake discography has a
// SECOND album (albumOutroFixture) that is NEVER touched, proving that the scoped
// import does not enumerate the whole discography (unlike importarArtista,
// TestImportarArtistaCriaArvore above, which keeps bringing everything).
func TestImportarFaixaPossuidaCriaSoUmAlbum(t *testing.T) {
	svc, database := svcMusicaFake(t)
	ctx := context.Background()

	faixaID, criada, err := svc.ImportarFaixaPossuida(ctx, "musicbrainz", mbidArtistaFixture,
		FaixaAlvo{RecordingMBID: mbidFaixa1Fixture}, []string{"en"})
	if err != nil {
		t.Fatalf("ImportarFaixaPossuida: %v", err)
	}
	if !criada {
		t.Error("criada = false, quero true (faixa nova)")
	}

	r := sqlcgen.New(database.Read)
	faixa, err := r.ObterEntity(ctx, faixaID)
	if err != nil {
		t.Fatalf("ObterEntity faixa: %v", err)
	}
	if faixa.Type != tipoFaixa || faixa.Title != "Track One" {
		t.Errorf("faixa = {type:%q title:%q}, quero {faixa, Track One}", faixa.Type, faixa.Title)
	}

	artista, err := r.ObterEntityPorMbid(ctx, nsValid(mbidArtistaFixture))
	if err != nil {
		t.Fatalf("ObterEntityPorMbid artista: %v", err)
	}
	if artista.Title != "Fake Band" {
		t.Errorf("artista title = %q, quero Fake Band (via BuscarDetalhe/MapearDetalhe, sem release-groups)", artista.Title)
	}

	if n := contarComFiltro(t, database, "SELECT count(*) FROM entity WHERE type = ?", tipoArtista); n != 1 {
		t.Errorf("artistas = %d, quero 1", n)
	}
	if n := contarComFiltro(t, database, "SELECT count(*) FROM entity WHERE type = ?", tipoAlbum); n != 1 {
		t.Errorf("albuns = %d, quero 1 (SO Fake Album -- Other Album NAO tocado)", n)
	}
	if n := contarComFiltro(t, database, "SELECT count(*) FROM entity WHERE type = ?", tipoFaixa); n != 1 {
		t.Errorf("faixas = %d, quero 1 (SO Track One -- Track Two/Other Track NAO tocadas)", n)
	}
	if _, err := r.ObterEntityPorMbid(ctx, nsValid(mbidAlbumOutroFixture)); !isNoRows(err) {
		t.Errorf("Other Album foi criado (err=%v), quero NoRows (nunca tocado)", err)
	}

	albuns := filhosPorKind(t, database, artista.ID, musicext.KindArtistaPrincipal)
	if len(albuns) != 1 {
		t.Fatalf("albuns do artista = %d, quero 1", len(albuns))
	}
	faixas := filhosPorKind(t, database, albuns[0].ChildID, musicext.KindAlbum)
	if len(faixas) != 1 || faixas[0].ChildID != faixaID {
		t.Errorf("faixas do album = %+v, quero 1 ligada a %s", faixas, faixaID)
	}
}

// TestImportarFaixaPossuidaFeatGravaMetadadoSemEntity is the 2nd central acceptance
// criterion (ADR-0029 item 4): the track with a DIVERGING artist-credit (Track Two,
// feat. Guest Artist) does NOT create an entity nor an entity_relation(kind=music_artista_feat)
// automatically -- it only writes the raw name/MBID into extension_data.
func TestImportarFaixaPossuidaFeatGravaMetadadoSemEntity(t *testing.T) {
	svc, database := svcMusicaFake(t)
	ctx := context.Background()

	faixaID, _, err := svc.ImportarFaixaPossuida(ctx, "musicbrainz", mbidArtistaFixture,
		FaixaAlvo{RecordingMBID: mbidFaixa2Fixture}, []string{"en"})
	if err != nil {
		t.Fatalf("ImportarFaixaPossuida: %v", err)
	}

	r := sqlcgen.New(database.Read)
	faixa, err := r.ObterEntity(ctx, faixaID)
	if err != nil {
		t.Fatalf("ObterEntity faixa: %v", err)
	}
	ns := extNSMusica(t, faixa.ExtensionDataJson.String)
	if ns[musicext.FeatArtistNameKey] != "Guest Artist" {
		t.Errorf("feat_artist_name = %v, quero 'Guest Artist'", ns[musicext.FeatArtistNameKey])
	}
	if ns[musicext.FeatArtistMbidKey] != mbidFeatFixture {
		t.Errorf("feat_artist_mbid = %v, quero %s", ns[musicext.FeatArtistMbidKey], mbidFeatFixture)
	}

	if _, err := r.ObterEntityPorMbid(ctx, nsValid(mbidFeatFixture)); !isNoRows(err) {
		t.Errorf("artista feat foi criado (err=%v), quero NoRows -- import escopado NUNCA auto-cria entity/relation de feat", err)
	}
	if n := contarComFiltro(t, database, "SELECT count(*) FROM entity_relation WHERE kind = ?", musicext.KindArtistaFeat); n != 0 {
		t.Errorf("relacoes music_artista_feat = %d, quero 0", n)
	}
	if n := contarComFiltro(t, database, "SELECT count(*) FROM entity WHERE type = ?", tipoArtista); n != 1 {
		t.Errorf("artistas = %d, quero 1 (so o principal)", n)
	}
}

// TestImportarFaixaPossuidaViaNomeTextual covers the TEXTUAL path (with no embedded
// recording MBID): it resolves the album by name (MusicaAlbumProvider.
// BuscarAlbumPorNome) and the track by title inside it -- the same selectivity as the
// MBID path (only 1 album/1 track, Other Album stays untouched).
func TestImportarFaixaPossuidaViaNomeTextual(t *testing.T) {
	svc, database := svcMusicaFake(t)
	ctx := context.Background()

	faixaID, criada, err := svc.ImportarFaixaPossuida(ctx, "musicbrainz", mbidArtistaFixture,
		FaixaAlvo{AlbumNome: "Fake Album", FaixaTitulo: "Track One"}, []string{"en"})
	if err != nil {
		t.Fatalf("ImportarFaixaPossuida: %v", err)
	}
	if !criada {
		t.Error("criada = false, quero true")
	}

	r := sqlcgen.New(database.Read)
	faixa, err := r.ObterEntity(ctx, faixaID)
	if err != nil {
		t.Fatalf("ObterEntity faixa: %v", err)
	}
	if faixa.Title != "Track One" {
		t.Errorf("faixa title = %q, quero Track One", faixa.Title)
	}
	if n := contarComFiltro(t, database, "SELECT count(*) FROM entity WHERE type = ?", tipoAlbum); n != 1 {
		t.Errorf("albuns = %d, quero 1", n)
	}
	if n := contarComFiltro(t, database, "SELECT count(*) FROM entity WHERE type = ?", tipoFaixa); n != 1 {
		t.Errorf("faixas = %d, quero 1", n)
	}
}

// TestImportarFaixaPossuidaSemSuporte: a source that does NOT implement
// MusicaAlbumProvider (the SELECTIVE capability) returns an error -- the scanner degrades
// (logs and falls back to the next step/draft).
func TestImportarFaixaPossuidaSemSuporte(t *testing.T) {
	database := setupDB(t)
	fakeProv := &fakeProvider{raw: json.RawMessage(`{}`), campos: map[string]any{}}
	svc, _ := novoServico(t, database, fakeProv)
	if _, _, err := svc.ImportarFaixaPossuida(context.Background(), "tmdb", mbidArtistaFixture,
		FaixaAlvo{RecordingMBID: mbidFaixa1Fixture}, []string{"en"}); err == nil {
		t.Fatal("quero erro: provider sem suporte a album seletivo de musica")
	}
}

// TestImportarArtistaSemSuporte: a source that does NOT implement MusicaProvider returns
// an error (the handler translates it to 502).
func TestImportarArtistaSemSuporte(t *testing.T) {
	database := setupDB(t)
	fakeProv := &fakeProvider{raw: json.RawMessage(`{}`), campos: map[string]any{}}
	svc, _ := novoServico(t, database, fakeProv)
	if _, _, err := svc.Importar(context.Background(), "tmdb", mbidArtistaFixture, tipoArtista, []string{"en"}); err == nil {
		t.Fatal("quero erro: provider sem suporte a musica")
	}
}

// extNSMusica decodes extension_data_json and returns the object of the
// core.music@1 namespace.
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

// contarComFiltro runs a parameterized SELECT count(*) (simple WHERE) against the
// test database -- a variant of contarLinhas/contarEntities (creditos_test.go/
// matchlocal_test.go) that accepts a filter, used by the selectivity tests of the
// scoped import.
func contarComFiltro(t *testing.T, database *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.Read.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("contarComFiltro (%q, %v): %v", query, args, err)
	}
	return n
}

// TestEnriquecerAlbumEFaixaAtualizaTitulo covers the bug found in the production validation
// of 20/Aug/2026: "Baixar Metadados" (Enriquecer force=true) for tipo=album/faixa
// hit a 404/502 on the real MusicBrainz because BuscarDetalhe ignored the type and always
// fetched /artist/{sourceID} -- the sourceID for those two types is the MBID of the
// release-group/recording, not of an artist. fakeMusicaProvider.BuscarDetalhe now
// imitates remapParaNome of the real provider (the title comes in "name") for mbidAlbumFixture/
// mbidFaixa1Fixture; the entity is born with Title==sourceID (pre-fix fallback, the same template
// as TestEnriquecerCuraTituloNumerico in enrich_test.go) to prove that Enriquecer
// updates the title instead of failing.
func TestEnriquecerAlbumEFaixaAtualizaTitulo(t *testing.T) {
	svc, database := svcMusicaFake(t)
	ctx := context.Background()
	w := sqlcgen.New(database.Write)

	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "al1", Type: tipoAlbum, Title: mbidAlbumFixture}); err != nil {
		t.Fatalf("CriarEntity album: %v", err)
	}
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "fx1", Type: tipoFaixa, Title: mbidFaixa1Fixture}); err != nil {
		t.Fatalf("CriarEntity faixa: %v", err)
	}

	if err := svc.Enriquecer(ctx, "al1", "musicbrainz", mbidAlbumFixture, tipoAlbum, []string{"en"}, true); err != nil {
		t.Fatalf("Enriquecer album: %v", err)
	}
	if err := svc.Enriquecer(ctx, "fx1", "musicbrainz", mbidFaixa1Fixture, tipoFaixa, []string{"en"}, true); err != nil {
		t.Fatalf("Enriquecer faixa: %v", err)
	}

	album, err := sqlcgen.New(database.Read).ObterEntity(ctx, "al1")
	if err != nil {
		t.Fatalf("ObterEntity album: %v", err)
	}
	if album.Title != "Fake Album Atualizado" {
		t.Errorf("titulo do album = %q, esperava \"Fake Album Atualizado\"", album.Title)
	}

	faixa, err := sqlcgen.New(database.Read).ObterEntity(ctx, "fx1")
	if err != nil {
		t.Fatalf("ObterEntity faixa: %v", err)
	}
	if faixa.Title != "Track One Atualizada" {
		t.Errorf("titulo da faixa = %q, esperava \"Track One Atualizada\"", faixa.Title)
	}
}

// TestMelhorFaixaPorTituloGateDuracaoRejeitaDivergencia: the title matches (score >=
// LimiarAutoMatch, no tie), but the duration of the local file differs from the one of the
// candidate beyond matching.ToleranciaDuracaoSegundos, with BOTH sides
// known — the CONFIRMATION gate rejects even with a perfect title.
func TestMelhorFaixaPorTituloGateDuracaoRejeitaDivergencia(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Track One", MBID: mbidFaixa1Fixture, DuracaoMs: 200000}, // 200s
	}
	// target with a 10s difference (> ToleranciaDuracaoSegundos, 3s) from the candidate.
	duracaoAlvoSegundos := 200 + matching.ToleranciaDuracaoSegundos + 7
	if _, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Track One", DuracaoSegundos: duracaoAlvoSegundos}, faixas); ok {
		t.Error("esperava rejeicao pelo gate de duracao (divergencia > tolerancia), mas aceitou")
	}
}

// TestMelhorFaixaPorTituloGateDuracaoAceitaDentroDaTolerancia: the same scenario as
// above, but the divergence stays within matching.ToleranciaDuracaoSegundos —
// the gate confirms the candidate chosen by title.
func TestMelhorFaixaPorTituloGateDuracaoAceitaDentroDaTolerancia(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Track One", MBID: mbidFaixa1Fixture, DuracaoMs: 200000}, // 200s
	}
	duracaoAlvoSegundos := 200 + matching.ToleranciaDuracaoSegundos - 1
	fx, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Track One", DuracaoSegundos: duracaoAlvoSegundos}, faixas)
	if !ok {
		t.Fatal("esperava aceitar (divergencia dentro da tolerancia), mas rejeitou")
	}
	if fx.MBID != mbidFaixa1Fixture {
		t.Errorf("MBID = %q, esperava %q", fx.MBID, mbidFaixa1Fixture)
	}
}

// TestMelhorFaixaPorTituloGateDuracaoPuladoQuandoAlvoDesconhecida: when
// duracaoAlvoSegundos is 0 (local file with no known duration), the gate is
// skipped and the candidate chosen by title is accepted even with a candidate duration
// quite different from what any "real" track would have — it preserves the
// behaviour from before the duration gate existed.
func TestMelhorFaixaPorTituloGateDuracaoPuladoQuandoAlvoDesconhecida(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Track One", MBID: mbidFaixa1Fixture, DuracaoMs: 200000}, // 200s
	}
	fx, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Track One", DuracaoSegundos: 0}, faixas)
	if !ok {
		t.Fatal("esperava aceitar (gate pulado com duracaoAlvoSegundos=0), mas rejeitou")
	}
	if fx.MBID != mbidFaixa1Fixture {
		t.Errorf("MBID = %q, esperava %q", fx.MBID, mbidFaixa1Fixture)
	}
}

// TestMelhorFaixaPorTituloGateDuracaoPuladoQuandoCandidatoDesconhecida: when
// the candidate does not report DuracaoMs (0, the source brought none), the gate is also
// skipped even with a known duracaoAlvoSegundos — it only compares when BOTH
// sides have the data.
func TestMelhorFaixaPorTituloGateDuracaoPuladoQuandoCandidatoDesconhecida(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Track One", MBID: mbidFaixa1Fixture}, // DuracaoMs=0
	}
	fx, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Track One", DuracaoSegundos: 200}, faixas)
	if !ok {
		t.Fatal("esperava aceitar (gate pulado com DuracaoMs do candidato=0), mas rejeitou")
	}
	if fx.MBID != mbidFaixa1Fixture {
		t.Errorf("MBID = %q, esperava %q", fx.MBID, mbidFaixa1Fixture)
	}
}

// TestMelhorFaixaPorTituloDesempataPorDuracaoQuandoTituloEmpata covers the
// real case (Bomfunk MC's/Freestyler): two tracks with the SAME
// title tie at the highest score -- one with a duration compatible with the target,
// the other not. The duration must break the tie and return the compatible track, instead
// of rejecting (zero,false) just because a tie exists.
func TestMelhorFaixaPorTituloDesempataPorDuracaoQuandoTituloEmpata(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Freestyler", MBID: mbidFaixa1Fixture, DuracaoMs: 247_000},
		{Title: "Freestyler", MBID: mbidFaixa2Fixture, DuracaoMs: 306_000},
	}
	duracaoAlvoSegundos := 306.364 // diff = 364ms << 3s of tolerance

	fx, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Freestyler", DuracaoSegundos: duracaoAlvoSegundos}, faixas)
	if !ok {
		t.Fatal("esperava aceitar (duracao deveria desempatar as duas faixas 'Freestyler'), mas rejeitou")
	}
	if fx.MBID != mbidFaixa2Fixture {
		t.Errorf("MBID = %q, esperava %q (a faixa de duracao compativel)", fx.MBID, mbidFaixa2Fixture)
	}
}

// TestMelhorFaixaPorTituloNaoDesempataQuandoDuracaoDoAlvoDesconhecida covers the
// same title tie of the previous test, but with duracaoAlvoSegundos == 0
// (local file with no known duration) -- with no signal to decide, it rejects
// (safe behaviour, no regression).
func TestMelhorFaixaPorTituloNaoDesempataQuandoDuracaoDoAlvoDesconhecida(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Freestyler", MBID: mbidFaixa1Fixture, DuracaoMs: 247_000},
		{Title: "Freestyler", MBID: mbidFaixa2Fixture, DuracaoMs: 306_000},
	}

	if _, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Freestyler", DuracaoSegundos: 0}, faixas); ok {
		t.Fatal("esperava rejeicao (duracao do alvo desconhecida nao pode desempatar titulo duplicado), mas aceitou")
	}
}

// TestMelhorFaixaPorTituloNaoDesempataQuandoAmbasCompativeis covers the title
// tie with BOTH tracks within the duration tolerance -- it stays
// ambiguous (a tie in the duration as well), it never chooses arbitrarily.
func TestMelhorFaixaPorTituloNaoDesempataQuandoAmbasCompativeis(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Freestyler", MBID: mbidFaixa1Fixture, DuracaoMs: 306_100},
		{Title: "Freestyler", MBID: mbidFaixa2Fixture, DuracaoMs: 306_900},
	}
	duracaoAlvoSegundos := 306.5

	if _, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Freestyler", DuracaoSegundos: duracaoAlvoSegundos}, faixas); ok {
		t.Fatal("esperava rejeicao (ambas as faixas empatadas ficam dentro da tolerancia, continua ambiguo), mas aceitou")
	}
}

// TestMelhorFaixaPorTituloDesempataPorDiscoEFaixa covers the new tiebreaker:
// a reissue with the SAME track on disc 1 and on disc 2 (identical titles,
// identical durations). The (disc, track) position of the file is the only
// signal that says which of the two it is -- without it the case stayed ambiguous and fell
// into a draft.
func TestMelhorFaixaPorTituloDesempataPorDiscoEFaixa(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Pale Shelter", MBID: "rec-d1", TrackNumber: 2, DiscNumber: 1, DiscTotal: 2, DuracaoMs: 261000},
		{Title: "Pale Shelter", MBID: "rec-d2", TrackNumber: 1, DiscNumber: 2, DiscTotal: 2, DuracaoMs: 261000},
	}
	fx, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Pale Shelter", FaixaDisco: 2, FaixaNumero: 1, DuracaoSegundos: 261}, faixas)
	if !ok {
		t.Fatal("esperava resolver por (disco, faixa), mas rejeitou")
	}
	if fx.MBID != "rec-d2" {
		t.Errorf("MBID = %q, esperava rec-d2 (disco 2, faixa 1)", fx.MBID)
	}

	// Without the position, the tie stays ambiguous: it rejects (it never applies a "similar" one).
	if _, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Pale Shelter", DuracaoSegundos: 261}, faixas); ok {
		t.Error("aceitou sem posicao: empate de titulo e duracao deveria rejeitar")
	}
}

// TestMelhorFaixaPorTituloDiscoErradoNaoCasa confirms that the position is not a free
// pass: if the disc/track of the file does not exist in the release, the tie becomes
// ambiguous again and the track is rejected (no applying the one from the wrong disc).
func TestMelhorFaixaPorTituloDiscoErradoNaoCasa(t *testing.T) {
	faixas := []provider.FaixaExterna{
		{Title: "Pale Shelter", MBID: "rec-d1", TrackNumber: 2, DiscNumber: 1, DuracaoMs: 261000},
		{Title: "Pale Shelter", MBID: "rec-d2", TrackNumber: 1, DiscNumber: 2, DuracaoMs: 261000},
	}
	if _, ok := melhorFaixaPorTitulo(FaixaAlvo{FaixaTitulo: "Pale Shelter", FaixaDisco: 3, FaixaNumero: 1, DuracaoSegundos: 261}, faixas); ok {
		t.Error("aceitou com disco 3 (inexistente): deveria rejeitar por ambiguidade")
	}
}
