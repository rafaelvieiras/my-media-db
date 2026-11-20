package scanner

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// ── music fixtures/fakes ─────────────────────────────────────────────────────
//
// The same synthetic MBIDs of internal/enrich/musica_test.go (fictitious UUIDs, not
// real ones): 1 artist, 1 album with 2 tracks -- the 2nd one with an artist-credit
// DIVERGENT from the artist of the album (feat.), covering the same case as ADR-0027
// item 4 (already validated in the enrich layer; here we only prove that the SCANNER
// fires the right import).

const (
	mbidArtistaFixture = "aaaaaaaa-0000-0000-0000-000000000001"
	mbidAlbumFixture   = "bbbbbbbb-0000-0000-0000-000000000001"
	mbidFaixa1Fixture  = "cccccccc-0000-0000-0000-000000000001"
	mbidFaixa2Fixture  = "cccccccc-0000-0000-0000-000000000002"
	mbidFeatFixture    = "dddddddd-0000-0000-0000-000000000001"
)

// fakeMusicaProvider implements the core provider.Provider + provider.MusicaProvider
// (tree, legacy capability -- only used by the MANUAL import/dispatcher, not by the
// scanner anymore) + provider.MusicaAlbumProvider (SELECTIVE capability -- the one
// the scanner effectively uses) + provider.FaixaResolverProvider, without a real
// network. It counts the calls (buscarN/arvoreN/resolverN/albumFaixaN/albumNomeN) so
// the tests prove both the selectivity (no whole tree) and that a re-scan without
// change does NOT call the source again (incrementality).
type fakeMusicaProvider struct {
	buscarN, arvoreN, resolverN, albumFaixaN, albumNomeN int
}

func (p *fakeMusicaProvider) Nome() string { return sourceMusicBrainz }

// albumFixture is the ONLY album of the fake discography: "Fake Album", 2 tracks --
// the 2nd one (Track Two) with an artist-credit DIVERGENT from the main artist
// (feat.), covering the same case as ADR-0027 item 4 / ADR-0029 item 4. Reused by
// BuscarArtistaArvore (tree, legacy) and by the new selective capabilities
// (BuscarAlbumDaFaixa/BuscarAlbumPorNome) so as not to diverge the two paths.
func albumFixture() provider.AlbumExterno {
	return provider.AlbumExterno{
		Name: "Fake Album",
		MBID: mbidAlbumFixture,
		Year: "1999",
		Faixas: []provider.FaixaExterna{
			{
				Title:              "Track One",
				MBID:               mbidFaixa1Fixture,
				TrackNumber:        1,
				DiscNumber:         1,
				DiscTotal:          1,
				ArtistaCreditoNome: "Fake Band",
				ArtistaCreditoMBID: mbidArtistaFixture,
			},
			{
				Title:              "Track Two",
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

// Buscar answers by exact (normalized) artist NAME: "Fake Band" -> 1 single
// candidate (mbidArtistaFixture); "Artista Ambiguo" -> 2 candidates tied at the top
// (intentional ambiguity); anything else -> empty.
func (p *fakeMusicaProvider) Buscar(_ context.Context, query, tipo, _ string) ([]provider.ResultadoExterno, error) {
	p.buscarN++
	if tipo != "" && tipo != "artista" {
		return nil, nil
	}
	switch query {
	case "Fake Band":
		return []provider.ResultadoExterno{{Source: sourceMusicBrainz, SourceID: mbidArtistaFixture, Type: "artista", Title: "Fake Band"}}, nil
	case "Artista Ambiguo":
		return []provider.ResultadoExterno{
			{Source: sourceMusicBrainz, SourceID: "banda-x", Type: "artista", Title: "Artista Ambiguo"},
			{Source: sourceMusicBrainz, SourceID: "banda-y", Type: "artista", Title: "Artista Ambiguo"},
		}, nil
	default:
		return nil, nil
	}
}

func (p *fakeMusicaProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (p *fakeMusicaProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (p *fakeMusicaProvider) BuscarArtistaArvore(_ context.Context, sourceID string, _ []string) (provider.ArtistaArvore, error) {
	p.arvoreN++
	if sourceID != mbidArtistaFixture {
		return provider.ArtistaArvore{}, provider.ErrNaoEncontrado
	}
	return provider.ArtistaArvore{
		Name:   "Fake Band",
		MBID:   mbidArtistaFixture,
		Albuns: []provider.AlbumExterno{albumFixture()},
	}, nil
}

// ResolverArtistaDaFaixa only resolves mbidFaixa1Fixture -> mbidArtistaFixture
// (the case "only the track MBID is embedded, without the artist one").
func (p *fakeMusicaProvider) ResolverArtistaDaFaixa(_ context.Context, recordingMBID string) (string, error) {
	p.resolverN++
	if recordingMBID != mbidFaixa1Fixture {
		return "", provider.ErrNaoEncontrado
	}
	return mbidArtistaFixture, nil
}

// BuscarAlbumDaFaixa is the SELECTIVE capability (provider.MusicaAlbumProvider) that
// the scanner effectively uses on the path by MBID: it assembles ONLY the album that
// contains `recordingMBID`, never the whole tree.
func (p *fakeMusicaProvider) BuscarAlbumDaFaixa(_ context.Context, recordingMBID string, _ []string) (provider.AlbumExterno, bool, error) {
	p.albumFaixaN++
	alb := albumFixture()
	for _, fx := range alb.Faixas {
		if fx.MBID == recordingMBID {
			return alb, true, nil
		}
	}
	return provider.AlbumExterno{}, false, nil
}

// BuscarAlbumPorNome is the SELECTIVE capability (provider.MusicaAlbumProvider) that
// the scanner uses on the textual path: it assembles ONLY the album `nomeAlbum` of
// the artist `artistaMBID`, never the whole tree.
func (p *fakeMusicaProvider) BuscarAlbumPorNome(_ context.Context, artistaMBID, nomeAlbum string, _ provider.AlvoFaixaAlbum, _ []string) (provider.AlbumExterno, bool, error) {
	p.albumNomeN++
	alb := albumFixture()
	if artistaMBID != mbidArtistaFixture || nomeAlbum != alb.Name {
		return provider.AlbumExterno{}, false, nil
	}
	return alb, true, nil
}

// BuscarAlbumPorMbid is the SELECTIVE capability (provider.MusicaAlbumProvider, part
// 2) that the scanner does NOT call (the external album screen / single-track import
// uses it) -- it only needs to satisfy the interface so that the type-assert of
// MusicaAlbumProvider keeps succeeding in the tests of this package.
func (p *fakeMusicaProvider) BuscarAlbumPorMbid(_ context.Context, albumMBID string, _ []string) (provider.AlbumExterno, error) {
	alb := albumFixture()
	if albumMBID != alb.MBID {
		return provider.AlbumExterno{}, provider.ErrNaoEncontrado
	}
	return alb, nil
}

// proberComTags is a fake Prober: it returns a mediainfo_json whose Tags field comes
// from the `porPath` map (keyed by the file path) -- it allows exercising the matching
// pipeline without depending on a real ffmpeg/ffprobe (the same spirit as proberVazio,
// but with controllable tags). duracaoPorPath (optional) populates
// info.General.Duration per path -- a path absent from the map (or the nil map, the
// case of every test written before the duration signal) leaves Duration at the
// zero-value, the same "unknown" the real ffprobe would return for a file without that
// information.
type proberComTags struct {
	porPath        map[string]mediainfo.AudioTags
	duracaoPorPath map[string]float64
}

func (proberComTags) Disponivel() bool { return true }

func (p proberComTags) Probe(_ context.Context, path string) ([]byte, error) {
	info := mediainfo.Info{}
	if tags, ok := p.porPath[path]; ok {
		info.Tags = &tags
	}
	if dur, ok := p.duracaoPorPath[path]; ok {
		info.General.Duration = dur
	}
	return json.Marshal(info)
}

// spyMusicaImportador is a fake MusicaImportador that does NO real matching: it only
// records the LAST enrich.FaixaAlvo received by ImportarFaixaPossuida and always
// returns the same pre-configured faixaID (the corresponding entity must be pre-seeded
// by the test, since this fake creates nothing -- see preSemearFaixaSpy). It is used to
// prove, isolated from the match logic of the real enrich, that the SCANNER correctly
// propagates the duration of the local file down to the FaixaAlvo on the two points
// where it is built (importarEAcharPorMbid/matchTextualFaixa, musica.go).
type spyMusicaImportador struct {
	faixaID    string
	ultimoAlvo enrich.FaixaAlvo
	chamadas   int
	// album/faixaDoCache/doCache are the album-cache surface: the spy can return an
	// album context from the network import and answer the LOCAL import separately.
	album        *enrich.AlbumImportado
	faixaDoCache string
	doCache      int
	albumDoCache enrich.AlbumImportado
}

func (s *spyMusicaImportador) Importar(context.Context, string, string, string, []string) (string, bool, error) {
	return "", false, nil
}

func (s *spyMusicaImportador) ImportarFaixaPossuida(ctx context.Context, source, artistaMBID string, alvo enrich.FaixaAlvo, langs []string) (string, bool, error) {
	faixaID, _, criada, err := s.ImportarFaixaPossuidaComAlbum(ctx, source, artistaMBID, alvo, langs)
	return faixaID, criada, err
}

// ImportarFaixaPossuidaComAlbum is the album-aware variant: the spy returns the album
// context so the tests of the album cache can exercise the local path.
func (s *spyMusicaImportador) ImportarFaixaPossuidaComAlbum(_ context.Context, _, _ string, alvo enrich.FaixaAlvo, _ []string) (string, *enrich.AlbumImportado, bool, error) {
	s.chamadas++
	s.ultimoAlvo = alvo
	return s.faixaID, s.album, true, nil
}

// ImportarFaixaPorTituloDeAlbumComAlbum mirrors the variant above for the compilation path.
func (s *spyMusicaImportador) ImportarFaixaPorTituloDeAlbumComAlbum(ctx context.Context, source string, alvo enrich.FaixaAlvo, langs []string) (string, *enrich.AlbumImportado, bool, error) {
	faixaID, criada, err := s.ImportarFaixaPorTituloDeAlbum(ctx, source, alvo, langs)
	return faixaID, nil, criada, err
}

// ImportarFaixaDesteAlbum counts the LOCAL imports (the album cache hit) separately from
// the network ones, which is exactly what the cache tests assert.
func (s *spyMusicaImportador) ImportarFaixaDesteAlbum(_ context.Context, alb enrich.AlbumImportado, alvo enrich.FaixaAlvo, _ []string) (string, bool) {
	s.doCache++
	s.albumDoCache = alb
	s.ultimoAlvo = alvo
	return s.faixaDoCache, s.faixaDoCache != ""
}

// ImportarFaixaPorTituloDeAlbum degrades silently in this spy: it only exists for the
// tests of the path by ARTIST, and step 4b must not interfere with them.
func (s *spyMusicaImportador) ImportarFaixaPorTituloDeAlbum(context.Context, string, enrich.FaixaAlvo, []string) (string, bool, error) {
	return "", false, nil
}

// preSemearFaixaSpy creates the `faixa` entity that spyMusicaImportador will return
// (it satisfies the FK of media_file.entity_id -- spyMusicaImportador, unlike the real
// enrich, does not create the entity by itself).
func preSemearFaixaSpy(t *testing.T, d *db.DB, id, titulo string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipoFaixa, Title: titulo,
	}); err != nil {
		t.Fatalf("pre-semear faixa spy: %v", err)
	}
}

// criarSourceMusica registers a content_type='music' datasource (the field is
// already accepted directly by library.Criar, unlike the series content_type when that
// test was written).
func criarSourceMusica(t *testing.T, d *db.DB, dir string) library.Source {
	t.Helper()
	src, err := library.New(d).Criar(context.Background(), dir, "movie", "music", nil, nil)
	if err != nil {
		t.Fatalf("criar source musica: %v", err)
	}
	return src
}

// managerMusica assembles a Manager with the given tag prober and the music vertical
// configured end to end: real enrich.Servico (idempotent find-or-create by MBID) on
// top of fakeMusicaProvider (textual search + tree + reverse resolution).
func managerMusica(t *testing.T, d *db.DB, porPath map[string]mediainfo.AudioTags, prov *fakeMusicaProvider) *Manager {
	t.Helper()
	m := NewManagerComProber(d, proberComTags{porPath: porPath})
	m.agora = relogioCrescente()
	svc := enrich.New(d, provider.NovaRegistry(prov), t.TempDir())
	m.ConfigurarMusica(svc, prov)
	m.idiomas = func(context.Context) []string { return []string{"en"} }
	return m
}

// ── ACOUSTIC FINGERPRINT fixtures/fakes ───────────────────────────────────
//
// They cover step 4.5 WITHOUT real fpcalc/network, in the SAME fixture universe of
// the rest of the file (mbidFaixa1Fixture/mbidArtistaFixture, resolvable by the
// existing fakeMusicaProvider via ResolverArtistaDaFaixa/BuscarAlbumDaFaixa) -- this
// way a fingerprint test above the threshold chains into the SAME scoped import the
// tests of steps 2/3 already prove, without needing a second fixture set.

// fakeFingerprintCalculador is a fake scanner.FingerprintCalculador: it returns a
// pre-configured fingerprint/duration/error, without touching the real fpcalc.
// calcularN counts the calls -- the ORDER tests (acceptance criterion "only called
// after the textual step fails") prove ZERO calls when step 4 already resolved.
type fakeFingerprintCalculador struct {
	disponivel  bool
	fingerprint string
	duracao     float64
	err         error
	calcularN   int
}

func (f *fakeFingerprintCalculador) Disponivel() bool { return f.disponivel }

func (f *fakeFingerprintCalculador) CalcularFingerprint(_ context.Context, _ string) (string, float64, error) {
	f.calcularN++
	if f.err != nil {
		return "", 0, f.err
	}
	return f.fingerprint, f.duracao, nil
}

// fakeFingerprintProvider implements the core provider.Provider (NO-OP stubs, the same
// spirit as internal/provider/acoustid.Client) + the capability
// provider.FingerprintProvider, without a real network. identificarN counts the calls
// for the same reason as calcularN above.
type fakeFingerprintProvider struct {
	candidato    provider.RecordingCandidato
	ok           bool
	err          error
	identificarN int
}

func (p *fakeFingerprintProvider) Nome() string { return "acoustid" }

func (p *fakeFingerprintProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (p *fakeFingerprintProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (p *fakeFingerprintProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (p *fakeFingerprintProvider) IdentificarPorFingerprint(_ context.Context, _ string, _ float64) (provider.RecordingCandidato, bool, error) {
	p.identificarN++
	if p.err != nil {
		return provider.RecordingCandidato{}, false, p.err
	}
	return p.candidato, p.ok, nil
}

var (
	_ provider.Provider            = (*fakeFingerprintProvider)(nil)
	_ provider.FingerprintProvider = (*fakeFingerprintProvider)(nil)
)

// managerMusicaComFingerprint assembles the same Manager as managerMusica, but it also
// wires step 4.5 (ConfigurarFingerprint) with the given fake calculator/provider -- a
// helper for the fingerprint tests below.
func managerMusicaComFingerprint(t *testing.T, d *db.DB, porPath map[string]mediainfo.AudioTags, prov *fakeMusicaProvider, calc *fakeFingerprintCalculador, fpProv provider.Provider) *Manager {
	t.Helper()
	m := managerMusica(t, d, porPath, prov)
	m.ConfigurarFingerprint(calc, fpProv)
	return m
}

// entityDe reads type/title/status of an entity by id (assertion helper).
func entityDe(t *testing.T, d *db.DB, id string) (tipo, titulo, status string) {
	t.Helper()
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT type, title, status FROM entity WHERE id = ?", id).Scan(&tipo, &titulo, &status); err != nil {
		t.Fatalf("ler entity %s: %v", id, err)
	}
	return tipo, titulo, status
}

// mediaFileDe reads entity_id/match_confidence of the media_file of a path.
func mediaFileDe(t *testing.T, d *db.DB, path string) (entityID string, conf float64) {
	t.Helper()
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT entity_id, match_confidence FROM media_file WHERE path = ?", path).Scan(&entityID, &conf); err != nil {
		t.Fatalf("ler media_file %s: %v", path, err)
	}
	return entityID, conf
}

// ── tests ────────────────────────────────────────────────────────────────────

// TestScanMusicaMbidFaixaFastPath covers step 1 of the pipeline: the track ALREADY
// EXISTS in the catalog by MBID (e.g. another track of the same album already imported
// the tree, or the file was already cataloged before) -- a PURE lookup, without any
// call to the source. 1:1 link: media_file.entity_id points DIRECTLY to it, confidence
// 100.
func TestScanMusicaMbidFaixaFastPath(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "faixa.mp3")
	escrever(t, arq)

	// It pre-seeds the track by MBID (find-or-create already resolved before this
	// scan).
	faixaID := "faixa-preexistente"
	extIDs := fmt.Sprintf(`{"mbid":%q}`, mbidFaixa1Fixture)
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: faixaID, Type: tipoFaixa, Title: "Track One", ExternalIdsJson: nsOpt(extIDs),
	}); err != nil {
		t.Fatalf("pre-semear faixa: %v", err)
	}

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {MusicBrainzTrackID: mbidFaixa1Fixture},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	if entityID != faixaID {
		t.Errorf("media_file.entity_id = %q, quero %q (a faixa pré-existente)", entityID, faixaID)
	}
	if conf != confIDExterno {
		t.Errorf("match_confidence = %v, quero %v", conf, confIDExterno)
	}
	if prov.buscarN != 0 || prov.arvoreN != 0 || prov.resolverN != 0 {
		t.Errorf("provider chamado no fast-path por MBID: buscar=%d arvore=%d resolver=%d, quero 0/0/0", prov.buscarN, prov.arvoreN, prov.resolverN)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity"); n != 1 {
		t.Errorf("entities = %d, quero 1 (só a pré-semeada, nenhuma nova)", n)
	}
}

// TestScanMusicaMbidFaixaEArtistaImportaEscopado covers step 2 (ADR-0029): track MBID
// with NO match yet, but the tag ALSO has the artist MBID -- it fires the SCOPED
// import (artist + ALBUM OF THAT TRACK + the track itself), NEVER the whole tree. Track
// 2 of the same album (Track Two, feat.) is NOT touched just for being in the same
// release: neither it nor the feat artist (Guest Artist) become an entity.
func TestScanMusicaMbidFaixaEArtistaImportaEscopado(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "track-one.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {MusicBrainzTrackID: mbidFaixa1Fixture, MusicBrainzArtistID: mbidArtistaFixture},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" {
		t.Errorf("faixa = {type:%q title:%q}, quero {faixa, Track One}", tipo, titulo)
	}
	if status != "cataloged" {
		t.Errorf("status = %q, quero cataloged (import via MBID, não draft)", status)
	}
	if conf != confIDExterno {
		t.Errorf("match_confidence = %v, quero %v", conf, confIDExterno)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	if prov.albumFaixaN != 1 {
		t.Errorf("BuscarAlbumDaFaixa chamado %d vez(es), quero 1", prov.albumFaixaN)
	}
	// ADR-0029 items 1/3: only the artist + THE ALBUM OF THAT TRACK + the track itself
	// enter the catalog -- no other release, no other track of the SAME album (Track
	// Two) and no feat artist are auto-created.
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='artista'"); n != 1 {
		t.Errorf("artistas = %d, quero 1 (só o principal; feat NÃO auto-cria entity)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='album'"); n != 1 {
		t.Errorf("albuns = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='faixa'"); n != 1 {
		t.Errorf("faixas = %d, quero 1 (só a faixa casada, não a arvore/album inteiro)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity_relation WHERE kind = ?", musicext.KindArtistaFeat); n != 0 {
		t.Errorf("relacoes music_artista_feat = %d, quero 0 (import escopado nunca auto-cria feat)", n)
	}
}

// TestScanMusicaResolucaoReversaFaixaSemArtista covers step 3: only the track MBID is
// embedded (without the artist one) -- the reverse resolution (FaixaResolverProvider)
// finds the artist, and the rest follows the same as step 2.
func TestScanMusicaResolucaoReversaFaixaSemArtista(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "track-one.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {MusicBrainzTrackID: mbidFaixa1Fixture}, // WITHOUT MusicBrainzArtistID
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, _ := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" {
		t.Errorf("faixa = {type:%q title:%q}, quero {faixa, Track One}", tipo, titulo)
	}
	if conf != confIDExterno {
		t.Errorf("match_confidence = %v, quero %v", conf, confIDExterno)
	}
	if prov.resolverN != 1 {
		t.Errorf("ResolverArtistaDaFaixa chamado %d vez(es), quero 1", prov.resolverN)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	if prov.albumFaixaN != 1 {
		t.Errorf("BuscarAlbumDaFaixa chamado %d vez(es), quero 1", prov.albumFaixaN)
	}
}

// TestScanMusicaBuscaTextual covers step 4 (the DOMINANT case in the owner's real
// files): without ANY embedded MBID, matching by artist name (album_artist) + SCOPED
// import through the TEXTUAL path (album by name via BuscarAlbumPorNome, track by
// title inside it) -- ADR-0029, never the whole tree.
func TestScanMusicaBuscaTextual(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, _ := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" {
		t.Errorf("faixa = {type:%q title:%q}, quero {faixa, Track One}", tipo, titulo)
	}
	if conf != confMusicaTextual {
		t.Errorf("match_confidence = %v, quero %v (busca textual)", conf, confMusicaTextual)
	}
	if prov.buscarN != 1 {
		t.Errorf("Buscar chamado %d vez(es), quero 1", prov.buscarN)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	if prov.albumNomeN != 1 {
		t.Errorf("BuscarAlbumPorNome chamado %d vez(es), quero 1", prov.albumNomeN)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='faixa'"); n != 1 {
		t.Errorf("faixas = %d, quero 1 (só a faixa casada, não o album inteiro)", n)
	}
}

// TestScanMusicaBuscaTextualAmbiguaViraDraft covers ambiguity (>1 candidate tied at
// the top): it NEVER applies a "similar" match -- it falls to the draft (step 5).
func TestScanMusicaBuscaTextualAmbiguaViraDraft(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "faixa-ambigua.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Artista Ambiguo", Album: "Algum Album", Title: "Alguma Faixa"},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || status != "draft" {
		t.Errorf("faixa = {type:%q status:%q}, quero {faixa, draft}", tipo, status)
	}
	if titulo != "Alguma Faixa" {
		t.Errorf("titulo do draft = %q, quero %q (da tag title)", titulo, "Alguma Faixa")
	}
	if conf != confDraft {
		t.Errorf("match_confidence = %v, quero %v", conf, confDraft)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (ambiguo nunca importa)", prov.arvoreN)
	}
}

// TestScanMusicaSemTagsViraDraftComNomeDoArquivo covers the shallow step 5: without
// ANY readable tag (ffprobe unavailable/empty tag), the track becomes a draft with the
// FILE NAME as title (the same movie/series philosophy -- visible, not lost).
func TestScanMusicaSemTagsViraDraftComNomeDoArquivo(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "sem-tags-nenhuma.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{}, prov) // no entry = no tags
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || status != "draft" {
		t.Errorf("faixa = {type:%q status:%q}, quero {faixa, draft}", tipo, status)
	}
	if titulo != "sem-tags-nenhuma" {
		t.Errorf("titulo do draft = %q, quero %q (nome do arquivo)", titulo, "sem-tags-nenhuma")
	}
	if conf != confDraft {
		t.Errorf("match_confidence = %v, quero %v", conf, confDraft)
	}
}

// TestScanMusicaReescaneioNaoRechamaImportarNemDuplica covers the incrementality:
// re-scanning the SAME file (unchanged mtime/size) reuses the entity_id already stored
// without calling Importar/Buscar/ResolverArtistaDaFaixa again, and without duplicating
// entity or media_file.
func TestScanMusicaReescaneioNaoRechamaImportarNemDuplica(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "track-one.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {MusicBrainzTrackID: mbidFaixa1Fixture, MusicBrainzArtistID: mbidArtistaFixture},
	}, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	entityID1, _ := mediaFileDe(t, d, arq)
	albumFaixaNApos1 := prov.albumFaixaN

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2 (re-scan sem mudanca): %v", err)
	}
	entityID2, conf2 := mediaFileDe(t, d, arq)

	if entityID2 != entityID1 {
		t.Errorf("entity_id mudou no re-scan: %q -> %q", entityID1, entityID2)
	}
	if conf2 != confIDExterno {
		t.Errorf("match_confidence apos re-scan = %v, quero %v", conf2, confIDExterno)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	if prov.albumFaixaN != albumFaixaNApos1 {
		t.Errorf("BuscarAlbumDaFaixa chamado de novo no re-scan sem mudanca: antes=%d depois=%d", albumFaixaNApos1, prov.albumFaixaN)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='faixa'"); n != 1 {
		t.Errorf("faixas apos re-scan = %d, quero 1 (nao duplicou, import escopado so traz 1 faixa)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file apos re-scan = %d, quero 1 (nao duplicou)", n)
	}
	// 1:1 link: never the N:M junction of series.
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity"); n != 0 {
		t.Errorf("media_file_entity = %d, quero 0 (musica e 1:1, media_file.entity_id direto)", n)
	}
}

// ── local duration as a confirmation signal ─────────────────────────────────
//
// The three tests below use spyMusicaImportador (not the real enrich) to isolate ONE
// thing: that the SCANNER correctly propagates the duration extracted from
// mediainfo.Info (info.General.Duration) down to the enrich.FaixaAlvo, on the TWO
// points where it is built (importarEAcharPorMbid/matchTextualFaixa, musica.go) -- the
// use of that duration to confirm/disambiguate was already validated in the
// enrich/provider/matching layers.

// TestScanMusicaMbidPropagaDuracaoDoArquivo covers the path by MBID (step 2): the
// duration of the local file arrives intact at the FaixaAlvo built in
// importarEAcharPorMbid.
func TestScanMusicaMbidPropagaDuracaoDoArquivo(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "track-one.mp3")
	escrever(t, arq)

	spy := &spyMusicaImportador{faixaID: "faixa-spy-mbid"}
	preSemearFaixaSpy(t, d, spy.faixaID, "Track One")

	m := NewManagerComProber(d, proberComTags{
		porPath:        map[string]mediainfo.AudioTags{arq: {MusicBrainzTrackID: mbidFaixa1Fixture, MusicBrainzArtistID: mbidArtistaFixture}},
		duracaoPorPath: map[string]float64{arq: 214.8},
	})
	m.agora = relogioCrescente()
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})
	m.idiomas = func(context.Context) []string { return []string{"en"} }

	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if spy.chamadas != 1 {
		t.Fatalf("ImportarFaixaPossuida chamado %d vez(es), quero 1", spy.chamadas)
	}
	if spy.ultimoAlvo.DuracaoSegundos != 214.8 {
		t.Errorf("FaixaAlvo.DuracaoSegundos = %v, quero 214.8 (duração extraída do arquivo)", spy.ultimoAlvo.DuracaoSegundos)
	}
	if spy.ultimoAlvo.RecordingMBID != mbidFaixa1Fixture {
		t.Errorf("FaixaAlvo.RecordingMBID = %q, quero %q", spy.ultimoAlvo.RecordingMBID, mbidFaixa1Fixture)
	}
}

// TestScanMusicaBuscaTextualPropagaDuracaoDoArquivo covers the TEXTUAL path (step 4,
// the dominant case): the duration of the local file arrives intact at the FaixaAlvo
// built in matchTextualFaixa.
func TestScanMusicaBuscaTextualPropagaDuracaoDoArquivo(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	spy := &spyMusicaImportador{faixaID: "faixa-spy-textual"}
	preSemearFaixaSpy(t, d, spy.faixaID, "Track One")

	m := NewManagerComProber(d, proberComTags{
		porPath:        map[string]mediainfo.AudioTags{arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1}},
		duracaoPorPath: map[string]float64{arq: 187.25},
	})
	m.agora = relogioCrescente()
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})
	m.idiomas = func(context.Context) []string { return []string{"en"} }

	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if spy.chamadas != 1 {
		t.Fatalf("ImportarFaixaPossuida chamado %d vez(es), quero 1", spy.chamadas)
	}
	if spy.ultimoAlvo.DuracaoSegundos != 187.25 {
		t.Errorf("FaixaAlvo.DuracaoSegundos = %v, quero 187.25 (duração extraída do arquivo)", spy.ultimoAlvo.DuracaoSegundos)
	}
	if spy.ultimoAlvo.AlbumNome != "Fake Album" || spy.ultimoAlvo.FaixaTitulo != "Track One" {
		t.Errorf("FaixaAlvo = {album:%q titulo:%q}, quero {Fake Album, Track One}", spy.ultimoAlvo.AlbumNome, spy.ultimoAlvo.FaixaTitulo)
	}
}

// TestScanMusicaSemDuracaoNoMediainfoChegaZero covers the safe degradation: without
// info.General.Duration available (partial/degraded probe), the FaixaAlvo arrives with
// DuracaoSegundos=0 ("unknown" for the enrich/provider) -- without breaking the scoped
// import.
func TestScanMusicaSemDuracaoNoMediainfoChegaZero(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	spy := &spyMusicaImportador{faixaID: "faixa-spy-sem-duracao"}
	preSemearFaixaSpy(t, d, spy.faixaID, "Track One")

	// duracaoPorPath deliberately OMITTED (nil) -- it simulates a mediainfo without
	// General.Duration available.
	m := NewManagerComProber(d, proberComTags{
		porPath: map[string]mediainfo.AudioTags{arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1}},
	})
	m.agora = relogioCrescente()
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})
	m.idiomas = func(context.Context) []string { return []string{"en"} }

	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if spy.chamadas != 1 {
		t.Fatalf("ImportarFaixaPossuida chamado %d vez(es), quero 1", spy.chamadas)
	}
	if spy.ultimoAlvo.DuracaoSegundos != 0 {
		t.Errorf("FaixaAlvo.DuracaoSegundos = %v, quero 0 (duração desconhecida)", spy.ultimoAlvo.DuracaoSegundos)
	}
}

// ── step 4.5: acoustic fingerprint ──────────────────────────────────────────

// TestScanMusicaFingerprintNaoChamadoQuandoTextualResolve covers the central
// acceptance criterion of the fingerprint step: step 4.5 NEVER runs when step 4
// (textual) has already resolved -- zero calls to the fingerprint calculator/provider,
// even with both configured and ready to answer.
func TestScanMusicaFingerprintNaoChamadoQuandoTextualResolve(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	calc := &fakeFingerprintCalculador{disponivel: true, fingerprint: "fp-nunca-usado"}
	fpProv := &fakeFingerprintProvider{ok: true, candidato: provider.RecordingCandidato{RecordingMBID: mbidFaixa1Fixture, Confianca: 0.95}}
	m := managerMusicaComFingerprint(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	}, prov, calc, fpProv)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	_, conf := mediaFileDe(t, d, arq)
	if conf != confMusicaTextual {
		t.Errorf("match_confidence = %v, quero %v (textual resolveu antes do fingerprint)", conf, confMusicaTextual)
	}
	if calc.calcularN != 0 {
		t.Errorf("CalcularFingerprint chamado %d vez(es), quero 0 (textual ja resolveu)", calc.calcularN)
	}
	if fpProv.identificarN != 0 {
		t.Errorf("IdentificarPorFingerprint chamado %d vez(es), quero 0 (textual ja resolveu)", fpProv.identificarN)
	}
}

// TestScanMusicaFingerprintAcimaDoLimiarImportaEscopado covers the happy path of step
// 4.5: without an embedded MBID and without a textual match (tag without a usable
// artist), a fingerprint candidate ABOVE the threshold
// (matching.LimiarConfiancaFingerprint) fires the SAME SCOPED import of steps 2/3
// (ADR-0029) -- only the artist + the album of that track + the track itself, NEVER the
// whole discography.
func TestScanMusicaFingerprintAcimaDoLimiarImportaEscopado(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "faixa-desconhecida.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	calc := &fakeFingerprintCalculador{disponivel: true, fingerprint: "fp-conhecido", duracao: 214.8}
	fpProv := &fakeFingerprintProvider{ok: true, candidato: provider.RecordingCandidato{RecordingMBID: mbidFaixa1Fixture, Confianca: 0.9}}
	// Without usable tags (no MBID, no artist/album) -- steps 1-4 all fail, only step
	// 4.5 is left.
	m := managerMusicaComFingerprint(t, d, map[string]mediainfo.AudioTags{}, prov, calc, fpProv)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" {
		t.Errorf("faixa = {type:%q title:%q}, quero {faixa, Track One}", tipo, titulo)
	}
	if status != "cataloged" {
		t.Errorf("status = %q, quero cataloged (import via fingerprint, nao draft)", status)
	}
	if conf != confFingerprint {
		t.Errorf("match_confidence = %v, quero %v", conf, confFingerprint)
	}
	if calc.calcularN != 1 {
		t.Errorf("CalcularFingerprint chamado %d vez(es), quero 1", calc.calcularN)
	}
	if fpProv.identificarN != 1 {
		t.Errorf("IdentificarPorFingerprint chamado %d vez(es), quero 1", fpProv.identificarN)
	}
	if prov.resolverN != 1 {
		t.Errorf("ResolverArtistaDaFaixa chamado %d vez(es), quero 1 (fingerprint so traz o recording)", prov.resolverN)
	}
	if prov.albumFaixaN != 1 {
		t.Errorf("BuscarAlbumDaFaixa chamado %d vez(es), quero 1", prov.albumFaixaN)
	}
	if prov.arvoreN != 0 {
		t.Errorf("BuscarArtistaArvore chamado %d vez(es), quero 0 (import escopado, nunca a arvore inteira)", prov.arvoreN)
	}
	// ADR-0029: only the artist + THE ALBUM OF THAT TRACK + the track itself -- no
	// other track of the same album (Track Two) nor the feat artist.
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='artista'"); n != 1 {
		t.Errorf("artistas = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='album'"); n != 1 {
		t.Errorf("albuns = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='faixa'"); n != 1 {
		t.Errorf("faixas = %d, quero 1 (so a faixa identificada, nunca a discografia inteira)", n)
	}
}

// TestScanMusicaFingerprintIndisponivelViraDraft covers the degradation when fpcalc is
// not available (Disponivel()=false): step 4.5 neither tries to calculate nor to query
// the provider -- it falls straight to the draft (step 5), without a fatal error.
func TestScanMusicaFingerprintIndisponivelViraDraft(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "sem-fpcalc.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	calc := &fakeFingerprintCalculador{disponivel: false}
	fpProv := &fakeFingerprintProvider{ok: true, candidato: provider.RecordingCandidato{RecordingMBID: mbidFaixa1Fixture, Confianca: 0.9}}
	m := managerMusicaComFingerprint(t, d, map[string]mediainfo.AudioTags{}, prov, calc, fpProv)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	_, _, status := entityDe(t, d, entityID)
	if status != "draft" || conf != confDraft {
		t.Errorf("faixa = {status:%q conf:%v}, quero {draft, %v} (fpcalc indisponivel)", status, conf, confDraft)
	}
	if calc.calcularN != 0 {
		t.Errorf("CalcularFingerprint chamado %d vez(es), quero 0 (fpcalc indisponivel, nem tenta)", calc.calcularN)
	}
	if fpProv.identificarN != 0 {
		t.Errorf("IdentificarPorFingerprint chamado %d vez(es), quero 0 (fpcalc indisponivel, nem consulta)", fpProv.identificarN)
	}
}

// TestScanMusicaSemProviderFingerprintViraDraft covers the degradation when the
// registered provider does NOT implement provider.FingerprintProvider (e.g. AcoustID
// never configured/registered) -- even with fpcalc available, the scanner does the
// type-assert, finds no capability, and falls to the draft.
func TestScanMusicaSemProviderFingerprintViraDraft(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "sem-provider-fingerprint.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	calc := &fakeFingerprintCalculador{disponivel: true, fingerprint: "fp-qualquer"}
	// fakeMusicaProvider does NOT implement provider.FingerprintProvider -- the same
	// scenario of "active provider without the capability".
	m := managerMusicaComFingerprint(t, d, map[string]mediainfo.AudioTags{}, prov, calc, prov)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	_, _, status := entityDe(t, d, entityID)
	if status != "draft" || conf != confDraft {
		t.Errorf("faixa = {status:%q conf:%v}, quero {draft, %v} (provider sem capacidade)", status, conf, confDraft)
	}
	if calc.calcularN != 0 {
		t.Errorf("CalcularFingerprint chamado %d vez(es), quero 0 (provider sem a capacidade, nem calcula)", calc.calcularN)
	}
}

// TestScanMusicaFingerprintAbaixoDoLimiarViraDraft covers the confidence acceptance
// criterion: a candidate with Confianca BELOW matching.LimiarConfiancaFingerprint is not
// accepted -- it falls to the draft, without firing any import.
func TestScanMusicaFingerprintAbaixoDoLimiarViraDraft(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "fingerprint-fraco.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	calc := &fakeFingerprintCalculador{disponivel: true, fingerprint: "fp-fraco"}
	fpProv := &fakeFingerprintProvider{ok: true, candidato: provider.RecordingCandidato{RecordingMBID: mbidFaixa1Fixture, Confianca: matching.LimiarConfiancaFingerprint - 0.01}}
	m := managerMusicaComFingerprint(t, d, map[string]mediainfo.AudioTags{}, prov, calc, fpProv)
	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	entityID, conf := mediaFileDe(t, d, arq)
	_, _, status := entityDe(t, d, entityID)
	if status != "draft" || conf != confDraft {
		t.Errorf("faixa = {status:%q conf:%v}, quero {draft, %v} (confianca abaixo do limiar)", status, conf, confDraft)
	}
	if prov.resolverN != 0 || prov.albumFaixaN != 0 {
		t.Errorf("import escopado disparado (resolverN=%d albumFaixaN=%d), quero 0/0 -- candidato fraco nunca importa", prov.resolverN, prov.albumFaixaN)
	}
}

// ── album cache (ADR-0042) ──────────────────────────────────────────────────
//
// The expensive part of the music scan is RESOLVING the album (1-2 MusicBrainz
// calls at 1 req/s), not creating the track. Once an album is resolved, every other
// file of it is local: the track is picked from the tracklist already in memory.
// The two tests below prove the two halves of that: the first file pays the network,
// the second pays nothing -- and the second half is skipped when the tags do not say
// the same album.

// TestScanMusicaReusaAlbumResolvidoNoMesmoScan is the whole point of ADR-0042: two
// files of the SAME album, one network import and ONE local import.
func TestScanMusicaReusaAlbumResolvidoNoMesmoScan(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq1 := filepath.Join(root, "01 - Track One.mp3")
	arq2 := filepath.Join(root, "02 - Track Two.mp3")
	escrever(t, arq1)
	escrever(t, arq2)

	spy := &spyMusicaImportador{
		faixaID:      "faixa-primeira",
		faixaDoCache: "faixa-segunda",
		album: &enrich.AlbumImportado{
			AlbumID:     "album-1",
			ArtistaMBID: mbidArtistaFixture,
			Album:       albumFixture(),
		},
	}
	preSemearFaixaSpy(t, d, spy.faixaID, "Track One")
	preSemearFaixaSpy(t, d, spy.faixaDoCache, "Track Two")

	tags := mediainfo.AudioTags{AlbumArtist: "Fake Band", Album: "Fake Album"}
	t1 := tags
	t1.Title, t1.TrackNumber = "Track One", 1
	t2 := tags
	t2.Title, t2.TrackNumber = "Track Two", 2

	m := NewManagerComProber(d, proberComTags{porPath: map[string]mediainfo.AudioTags{arq1: t1, arq2: t2}})
	m.agora = relogioCrescente()
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})
	m.idiomas = func(context.Context) []string { return []string{"en"} }

	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if spy.chamadas != 1 {
		t.Errorf("import pela REDE = %d, quero 1 (so o primeiro arquivo do album resolve o album)", spy.chamadas)
	}
	if spy.doCache != 1 {
		t.Errorf("import LOCAL (cache de album) = %d, quero 1 (o segundo arquivo do album)", spy.doCache)
	}
	if spy.albumDoCache.AlbumID != "album-1" {
		t.Errorf("album reaproveitado = %q, quero album-1", spy.albumDoCache.AlbumID)
	}
	if id, _ := mediaFileDe(t, d, arq2); id != spy.faixaDoCache {
		t.Errorf("faixa do segundo arquivo = %q, quero %q (a do import local)", id, spy.faixaDoCache)
	}
}

// TestScanMusicaNaoReusaAlbumDeOutroAlbum: the cache is keyed by album artist + album,
// so a file of a DIFFERENT album still pays its own resolution.
func TestScanMusicaNaoReusaAlbumDeOutroAlbum(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq1 := filepath.Join(root, "01 - Um.mp3")
	arq2 := filepath.Join(root, "02 - Dois.mp3")
	escrever(t, arq1)
	escrever(t, arq2)

	spy := &spyMusicaImportador{faixaID: "faixa-a", faixaDoCache: "faixa-b"}
	preSemearFaixaSpy(t, d, spy.faixaID, "Um")
	preSemearFaixaSpy(t, d, spy.faixaDoCache, "Dois")

	m := NewManagerComProber(d, proberComTags{porPath: map[string]mediainfo.AudioTags{
		arq1: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Um", TrackNumber: 1},
		arq2: {AlbumArtist: "Fake Band", Album: "OUTRO Album", Title: "Dois", TrackNumber: 1},
	}})
	m.agora = relogioCrescente()
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})
	m.idiomas = func(context.Context) []string { return []string{"en"} }

	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if spy.doCache != 0 {
		t.Errorf("import LOCAL = %d, quero 0 (albuns diferentes)", spy.doCache)
	}
	if spy.chamadas != 2 {
		t.Errorf("import pela REDE = %d, quero 2 (cada album resolve o seu)", spy.chamadas)
	}
}

// ── draft throttle ─────────────────────────────────────────────────────────

// semearDraftComTentativa pre-seeds the state of a file whose previous scan ended in a
// draft track, stamped `quando` (empty = never tried).
func semearDraftComTentativa(t *testing.T, d *db.DB, path, id, quando string) {
	t.Helper()
	ctx := context.Background()
	q := sqlcgen.New(d.Write)
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID: id, Type: tipoFaixa, Title: "Track One",
	}); err != nil {
		t.Fatalf("semear faixa draft: %v", err)
	}
	if _, err := d.Write.ExecContext(ctx,
		"UPDATE entity SET status = 'draft' WHERE id = ?", id); err != nil {
		t.Fatalf("rebaixar faixa para draft: %v", err)
	}
	if quando != "" {
		if _, err := d.Write.ExecContext(ctx,
			"UPDATE entity SET last_external_match_attempt_at = ? WHERE id = ?", quando, id); err != nil {
			t.Fatalf("carimbar tentativa: %v", err)
		}
	}
	if _, err := q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID: "mf-" + id, EntityID: id, Path: path, State: "present",
		Size: sql.NullInt64{Int64: 1, Valid: true},
	}); err != nil {
		t.Fatalf("semear media_file: %v", err)
	}
}

// TestScanMusicaNaoRepeteTentativaDeDraftRecente: a file that already failed recently
// keeps the draft without a single request -- the whole point of the throttle (each
// attempt costs MusicBrainz calls at 1 req/s plus fpcalc reading the whole file).
func TestScanMusicaNaoRepeteTentativaDeDraftRecente(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	// The stored media_file has a different size, so the incremental short-circuit
	// does NOT apply: the file is "changed" and would go through the pipeline.
	recente := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	semearDraftComTentativa(t, d, arq, "faixa-draft", recente)

	spy := &spyMusicaImportador{faixaID: "faixa-nova"}
	preSemearFaixaSpy(t, d, spy.faixaID, "Track One")
	// REAL clock: the throttle compares the stamp with "now" (relogioCrescente would
	// put the fake clock before the stamp and every draft would look recent).
	m := NewManagerComProber(d, proberComTags{porPath: map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	}})
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})
	m.idiomas = func(context.Context) []string { return []string{"en"} }

	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if spy.chamadas != 0 || spy.doCache != 0 {
		t.Errorf("rede/cache chamados (%d/%d), quero 0/0 (draft tentado ha 24h)", spy.chamadas, spy.doCache)
	}
	if id, conf := mediaFileDe(t, d, arq); id != "faixa-draft" || conf != confDraft {
		t.Errorf("arquivo apontou para %q (conf %v), quero o draft existente (conf 0)", id, conf)
	}
}

// TestScanMusicaTentaDeNovoDraftForaDaJanela: past the window the scanner tries again.
func TestScanMusicaTentaDeNovoDraftForaDaJanela(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	antigo := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	semearDraftComTentativa(t, d, arq, "faixa-draft", antigo)

	spy := &spyMusicaImportador{faixaID: "faixa-resolvida"}
	preSemearFaixaSpy(t, d, spy.faixaID, "Track One")
	m := NewManagerComProber(d, proberComTags{porPath: map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	}})
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})
	m.idiomas = func(context.Context) []string { return []string{"en"} }

	src := criarSourceMusica(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if spy.chamadas != 1 {
		t.Errorf("import pela REDE = %d, quero 1 (tentativa anterior tem 30 dias)", spy.chamadas)
	}
	if id, _ := mediaFileDe(t, d, arq); id != spy.faixaID {
		t.Errorf("arquivo apontou para %q, quero %q", id, spy.faixaID)
	}
}
