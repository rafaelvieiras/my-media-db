package server

// Tests of ReconciliarFilhosOrfaos: tracks/series that got stuck in draft because the match
// failed at scan time are retried against the identity tree already resolved (or that came to
// be resolved later). It reuses Manager.ReconciliarFaixaDraft/ReconciliarSerieDraft (already
// implemented/tested in internal/scanner) behind a real *scanner.Manager with a fake Prober --
// the same pattern as scan_test.go/TestPostScanEmAndamento409 (s.scanner replaced directly,
// rewiring music/series by hand because swapping the instance drops the config montarServicos
// applied to the original).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
	"github.com/rafaelvieiras/my-media-db/internal/scanner"
)

// ── fakes ────────────────────────────────────────────────────────────────

// reconciliarProberFake serves audio tags pre-configured per path, without touching
// ffprobe/disk for real -- the same role as proberComTags (scanner/musica_test.go),
// reimplemented here because it is a non-exported test type of the other package.
type reconciliarProberFake struct {
	porPath map[string]mediainfo.AudioTags
}

func (reconciliarProberFake) Disponivel() bool { return true }

func (p reconciliarProberFake) Probe(_ context.Context, path string) ([]byte, error) {
	info := mediainfo.Info{}
	if tags, ok := p.porPath[path]; ok {
		info.Tags = &tags
	}
	return json.Marshal(info)
}

// fakeMusicaProviderOrfaos implements the core provider.Provider +
// provider.MusicaAlbumProvider (the minimum matchTextualFaixa/enrich.ImportarFaixaPossuida
// require for the TEXTUAL path), under Nome()="musicbrainz" -- it registers a single fake
// artist/album/track ("Fake Band"/"Fake Album"/"Track One") to prove the happy path of the
// track reconciliation. chamadasBuscar counts the artist searches, so the tests can prove that
// the throttle really skipped the provider call.
type fakeMusicaProviderOrfaos struct {
	chamadasBuscar int
}

func (p *fakeMusicaProviderOrfaos) Nome() string { return "musicbrainz" }

func (p *fakeMusicaProviderOrfaos) Buscar(_ context.Context, query, tipo, _ string) ([]provider.ResultadoExterno, error) {
	p.chamadasBuscar++
	if tipo != "artista" || !strings.EqualFold(query, "Fake Band") {
		return nil, nil
	}
	return []provider.ResultadoExterno{{Source: "musicbrainz", SourceID: "mbid-artista-1", Type: "artista", Title: "Fake Band"}}, nil
}

func (p *fakeMusicaProviderOrfaos) BuscarDetalhe(_ context.Context, sourceID, _ string, _ []string) (json.RawMessage, error) {
	if sourceID != "mbid-artista-1" {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(`{"title":"Fake Band"}`), nil
}

func (p *fakeMusicaProviderOrfaos) MapearDetalhe(raw json.RawMessage) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *fakeMusicaProviderOrfaos) BuscarAlbumDaFaixa(_ context.Context, _ string, _ []string) (provider.AlbumExterno, bool, error) {
	return provider.AlbumExterno{}, false, nil
}

func (p *fakeMusicaProviderOrfaos) BuscarAlbumPorNome(_ context.Context, artistaMBID, nomeAlbum string, _ provider.AlvoFaixaAlbum, _ []string) (provider.AlbumExterno, bool, error) {
	if artistaMBID != "mbid-artista-1" || !strings.EqualFold(nomeAlbum, "Fake Album") {
		return provider.AlbumExterno{}, false, nil
	}
	return provider.AlbumExterno{
		Name: "Fake Album",
		MBID: "mbid-album-1",
		Faixas: []provider.FaixaExterna{
			{Title: "Track One", MBID: "mbid-faixa-1", TrackNumber: 1},
		},
	}, true, nil
}

func (p *fakeMusicaProviderOrfaos) BuscarAlbumPorMbid(_ context.Context, _ string, _ []string) (provider.AlbumExterno, error) {
	return provider.AlbumExterno{}, provider.ErrNaoEncontrado
}

// setupReconciliarOrfaosServer starts a Server with the fake music/series providers registered
// (which enables s.enrich) and replaces s.scanner with one that has a controllable Prober --
// rewiring ConfigurarMusica/ConfigurarSeries by hand (swapping the instance drops the config
// montarServicos applied to the original, the same pattern as scan_test.go).
func setupReconciliarOrfaosServer(t *testing.T, musica *fakeMusicaProviderOrfaos, tags map[string]mediainfo.AudioTags) (*Server, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "reconciliar-orfaos.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	s := New("v-teste", database, WithSearchProvider(musica, fake.New("")), WithAssetsDir(t.TempDir()))
	s.scanner = scanner.NewManagerComProber(database, reconciliarProberFake{porPath: tags})
	s.scanner.ConfigurarMusica(s.enrich, musica)
	s.scanner.ConfigurarSeries(s.enrich, s.idiomasScanner)
	return s, database
}

// rebaixarParaDraft marks an already created entity (semearEntity) as status='draft' --
// CriarEntity does not accept a status at creation (default 'cataloged'), so it needs an extra
// AtualizarEntity, the same pattern as criarFaixaDraftOrfa/criarSerieDraft
// (internal/scanner/reconciliar_test.go/serie_test.go).
func rebaixarParaDraft(t *testing.T, d *db.DB, id, tipo, titulo string) {
	t.Helper()
	if _, err := escrita(t, d).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipo, Title: titulo, Status: "draft",
	}); err != nil {
		t.Fatalf("rebaixar %s para draft: %v", id, err)
	}
}

// semearFaixaDraftOrfa creates a type=faixa status=draft entity with a 'present' media_file
// pointing at it -- it simulates the outcome of step 5 of the music pipeline (criarFaixaDraft)
// without depending on a real scan.
func semearFaixaDraftOrfa(t *testing.T, d *db.DB, id, titulo, path string) {
	t.Helper()
	semearEntity(t, d, id, tipoEntFaixa, titulo)
	rebaixarParaDraft(t, d, id, tipoEntFaixa, titulo)
	semearFile(t, d, "mf-"+id, id, path, "present", 0.4, "")
}

// semearSerieDraftComArquivo creates a type=tv status=draft entity with a 'present' media_file
// pointing straight at it (gravarArquivoSerie always writes it like that, even in review).
func semearSerieDraftComArquivo(t *testing.T, d *db.DB, id, titulo, path string) {
	t.Helper()
	semearEntity(t, d, id, tipoEntSerie, titulo)
	rebaixarParaDraft(t, d, id, tipoEntSerie, titulo)
	semearFile(t, d, "mf-"+id, id, path, "present", 0.4, "")
}

// mediaFileEntityID reads the entity_id of the media_file of a path (a local assertion helper --
// valorUnico/contar of internal/scanner are not exported/reusable here).
func mediaFileEntityID(t *testing.T, d *db.DB, path string) string {
	t.Helper()
	var id string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT entity_id FROM media_file WHERE path = ?", path).Scan(&id); err != nil {
		t.Fatalf("ler media_file %q: %v", path, err)
	}
	return id
}

// contarMediaFilePorEntity counts how many media_file rows point at an entity.
func contarMediaFilePorEntity(t *testing.T, d *db.DB, entityID string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT count(*) FROM media_file WHERE entity_id = ?", entityID).Scan(&n); err != nil {
		t.Fatalf("contar media_file de %q: %v", entityID, err)
	}
	return n
}

// TestReconciliarFilhosOrfaosFaixaReligaComSucesso covers the central criterion for music: an
// orphan draft track with a present file, when the artist/album resolve through a textual
// search, is re-linked (it leaves the review, the media_file starts pointing at the resolved
// track) and the attempt is stamped.
func TestReconciliarFilhosOrfaosFaixaReligaComSucesso(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/aha/01 - Track One.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	})

	semearFaixaDraftOrfa(t, d, "fx-draft", "Track One", path)

	reconciliados, err := s.ReconciliarFilhosOrfaos(context.Background())
	if err != nil {
		t.Fatalf("ReconciliarFilhosOrfaos: %v", err)
	}
	if reconciliados != 1 {
		t.Fatalf("reconciliados = %d, quero 1", reconciliados)
	}

	// The draft that lost its file is PURGED (it used to stay in the review queue forever as
	// "sem arquivo"). The attempt stamp lives on it, so it goes away with it -- what matters is
	// the new link.
	if _, err := escrita(t, d).ObterEntity(context.Background(), "fx-draft"); err == nil {
		t.Error("faixa draft substituída deveria ter sido purgada")
	}

	entityID := mediaFileEntityID(t, d, path)
	if entityID == "fx-draft" {
		t.Error("media_file ainda aponta pro draft, deveria estar repontado pra faixa resolvida")
	}
	if n := contarMediaFilePorEntity(t, d, "fx-draft"); n != 0 {
		t.Errorf("media_file ainda apontando pro draft antigo = %d, quero 0", n)
	}
}

// TestReconciliarFilhosOrfaosSerieSemMatchAindaCarimba covers the throttle criterion even on
// failure: a draft series with a file whose title matches no provider fixture
// (ImportarSeriePorNome finds no candidate) is not re-linked (linked=false, no error) -- but the
// attempt is stamped anyway, so the job does not hammer the same item every cycle.
func TestReconciliarFilhosOrfaosSerieSemMatchAindaCarimba(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/series/desconhecida/Desconhecida S01E01.mkv"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{})

	semearSerieDraftComArquivo(t, d, "serie-draft", "Série Sem Fixture Nenhuma", path)

	reconciliados, err := s.ReconciliarFilhosOrfaos(context.Background())
	if err != nil {
		t.Fatalf("ReconciliarFilhosOrfaos: %v", err)
	}
	if reconciliados != 0 {
		t.Errorf("reconciliados = %d, quero 0 (sem candidato na fonte)", reconciliados)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "serie-draft")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if !ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at deveria ter sido carimbado mesmo sem religar")
	}
	entityID := mediaFileEntityID(t, d, path)
	if entityID != "serie-draft" {
		t.Errorf("media_file.entity_id mudou pra %q sem match nenhum", entityID)
	}
}

// TestReconciliarFilhosOrfaosRespeitaThrottle covers the throttle: an item verified recently
// (last_external_match_attempt_at stamped in this same test run, within the retryDays window)
// does not show up in the eligible list -- the music provider is not even called again.
func TestReconciliarFilhosOrfaosRespeitaThrottle(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/aha/01 - Track One.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	})

	semearFaixaDraftOrfa(t, d, "fx-draft", "Track One", path)
	if err := s.carimbarTentativaExterna(context.Background(), "fx-draft"); err != nil {
		t.Fatalf("carimbarTentativaExterna: %v", err)
	}

	reconciliados, err := s.ReconciliarFilhosOrfaos(context.Background())
	if err != nil {
		t.Fatalf("ReconciliarFilhosOrfaos: %v", err)
	}
	if reconciliados != 0 {
		t.Errorf("reconciliados = %d, quero 0 (dentro do throttle)", reconciliados)
	}
	if musica.chamadasBuscar != 0 {
		t.Errorf("provider de música chamado %d vez(es), quero 0 (dentro do throttle)", musica.chamadasBuscar)
	}
}

// TestReconciliarFilhosOrfaosSemScannerDegradaSemErro covers the s.scanner==nil guard: without
// a scanner, the reconciliation methods would not even exist to call -- it degrades to (0, nil)
// without touching the database.
func TestReconciliarFilhosOrfaosSemScannerDegradaSemErro(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reconciliar-orfaos-sem-scanner.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database)
	s.scanner = nil

	reconciliados, err := s.ReconciliarFilhosOrfaos(context.Background())
	if err != nil {
		t.Fatalf("ReconciliarFilhosOrfaos: %v", err)
	}
	if reconciliados != 0 {
		t.Errorf("reconciliados = %d, quero 0", reconciliados)
	}
}
