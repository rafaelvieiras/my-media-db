package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// ── ReconciliarFaixaDraft ─────────────────────────────────────────────────

// criarFaixaDraftOrfa creates an entity type='faixa' status='draft' and,
// optionally, a 'present' media_file pointing to it -- it simulates the result of
// step 5 of the music pipeline (musica.go, criarFaixaDraft) without depending on the
// scanner to reach that state.
func criarFaixaDraftOrfa(t *testing.T, d *db.DB, id, titulo, path string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipoFaixa, Title: titulo,
	}); err != nil {
		t.Fatalf("criar faixa draft: %v", err)
	}
	if _, err := sqlcgen.New(d.Write).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipoFaixa, Title: titulo, Status: "draft",
	}); err != nil {
		t.Fatalf("rebaixar faixa draft: %v", err)
	}
	if path == "" {
		return
	}
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: "mf-" + id, EntityID: id, Path: path, State: "present",
	}); err != nil {
		t.Fatalf("criar media_file da faixa draft: %v", err)
	}
}

// TestReconciliarFaixaDraftReligaComSucesso covers the happy case: the orphan draft
// track has a real file on disk; the provider resolves it by textual search (the same
// path of matchTextualFaixa/musica.go) -- the media_file leaves the draft track and
// starts pointing to the newly resolved track, with the textual search
// match_confidence.
func TestReconciliarFaixaDraftReligaComSucesso(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{
		arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	}, prov)

	draftID := "faixa-draft-orfa"
	criarFaixaDraftOrfa(t, d, draftID, "Track One", arq)

	linked, err := m.ReconciliarFaixaDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarFaixaDraft: %v", err)
	}
	if !linked {
		t.Fatalf("linked = false, quero true")
	}

	entityID, conf := mediaFileDe(t, d, arq)
	if entityID == draftID {
		t.Errorf("media_file ainda aponta pro draft %q, deveria estar repontado pra faixa resolvida", draftID)
	}
	if conf != confMusicaTextual {
		t.Errorf("match_confidence = %v, quero %v (busca textual)", conf, confMusicaTextual)
	}
	tipo, titulo, status := entityDe(t, d, entityID)
	if tipo != tipoFaixa || titulo != "Track One" || status != "cataloged" {
		t.Errorf("faixa resolvida = {%q %q %q}, quero {faixa Track One cataloged}", tipo, titulo, status)
	}
	// The old draft track is PURGED. It was born from the matcher itself (title
	// derived from the file, no external id, no override), and the non-destructive
	// principle protects what the OWNER wrote -- not an empty shell. Without the
	// purge, it stayed in the review queue forever as "sem arquivo" (114 cases
	// measured in production).
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE id=?", draftID); n != 0 {
		t.Errorf("faixa draft substituída = %d, quero 0 (purgada)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file WHERE entity_id=?", draftID); n != 0 {
		t.Errorf("media_file ainda apontando pro draft antigo = %d, quero 0", n)
	}
}

// TestReconciliarFaixaDraftSemMediaFileNaoLiga covers the degradation: a draft track
// without ANY media_file (an orphan in the shallowest sense: the entity exists, but
// nothing on disk points to it) cannot be reconciled -- linked=false without an
// error.
func TestReconciliarFaixaDraftSemMediaFileNaoLiga(t *testing.T) {
	d := abrirDB(t)
	prov := &fakeMusicaProvider{}
	m := managerMusica(t, d, map[string]mediainfo.AudioTags{}, prov)

	draftID := "faixa-draft-sem-arquivo"
	criarFaixaDraftOrfa(t, d, draftID, "Faixa Sem Arquivo", "")

	linked, err := m.ReconciliarFaixaDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarFaixaDraft: %v", err)
	}
	if linked {
		t.Errorf("linked = true, quero false (sem media_file nenhum)")
	}
}

// TestReconciliarFaixaDraftPropagaDuracaoDoArquivo covers the duration signal in the
// reconciliation: the duration of the local file extracted by probarTags arrives
// intact at the enrich.FaixaAlvo built in matchTextualFaixa -- the SAME path the
// scan uses (musica.go). It uses spyMusicaImportador (defined in musica_test.go)
// instead of the real enrich to observe the FaixaAlvo directly.
func TestReconciliarFaixaDraftPropagaDuracaoDoArquivo(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	spy := &spyMusicaImportador{faixaID: "faixa-spy-reconciliar"}
	preSemearFaixaSpy(t, d, spy.faixaID, "Track One")

	m := NewManagerComProber(d, proberComTags{
		porPath:        map[string]mediainfo.AudioTags{arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1}},
		duracaoPorPath: map[string]float64{arq: 199.4},
	})
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})

	draftID := "faixa-draft-orfa-duracao"
	criarFaixaDraftOrfa(t, d, draftID, "Track One", arq)

	linked, err := m.ReconciliarFaixaDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarFaixaDraft: %v", err)
	}
	if !linked {
		t.Fatalf("linked = false, quero true")
	}
	if spy.chamadas != 1 {
		t.Fatalf("ImportarFaixaPossuida chamado %d vez(es), quero 1", spy.chamadas)
	}
	if spy.ultimoAlvo.DuracaoSegundos != 199.4 {
		t.Errorf("FaixaAlvo.DuracaoSegundos = %v, quero 199.4 (duração extraída do arquivo)", spy.ultimoAlvo.DuracaoSegundos)
	}
}

// TestReconciliarFaixaDraftSemDuracaoChegaZero covers the safe degradation: without
// info.General.Duration available in the tag re-extraction, the FaixaAlvo arrives with
// DuracaoSegundos=0 -- without breaking the reconciliation.
func TestReconciliarFaixaDraftSemDuracaoChegaZero(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "01 - Track One.mp3")
	escrever(t, arq)

	spy := &spyMusicaImportador{faixaID: "faixa-spy-reconciliar-sem-duracao"}
	preSemearFaixaSpy(t, d, spy.faixaID, "Track One")

	// duracaoPorPath deliberately OMITTED (nil).
	m := NewManagerComProber(d, proberComTags{
		porPath: map[string]mediainfo.AudioTags{arq: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1}},
	})
	m.ConfigurarMusica(spy, &fakeMusicaProvider{})

	draftID := "faixa-draft-orfa-sem-duracao"
	criarFaixaDraftOrfa(t, d, draftID, "Track One", arq)

	linked, err := m.ReconciliarFaixaDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarFaixaDraft: %v", err)
	}
	if !linked {
		t.Fatalf("linked = false, quero true")
	}
	if spy.ultimoAlvo.DuracaoSegundos != 0 {
		t.Errorf("FaixaAlvo.DuracaoSegundos = %v, quero 0 (duração desconhecida)", spy.ultimoAlvo.DuracaoSegundos)
	}
}

// ── ReconciliarSerieDraft ─────────────────────────────────────────────────

// managerSerieAutoImportReconciliar assembles a Manager with ConfigurarSeries only
// (without depending on a scanRun) -- the same fake provider of
// managerSerieAutoImport, but without requiring a real scan to test
// ReconciliarSerieDraft directly.
func managerSerieAutoImportReconciliar(t *testing.T, d *db.DB) *Manager {
	t.Helper()
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNG"))
	}))
	t.Cleanup(img.Close)
	svc := enrich.New(d, provider.NovaRegistry(fake.New(img.URL)), t.TempDir())
	m := managerFake(t, d)
	m.ConfigurarSeries(svc, func(context.Context) []string { return []string{"en"} })
	return m
}

// TestReconciliarSerieDraftReligaEpisodios covers the happy case: a draft series with
// 1+ media_file is reimported successfully
// (tentarImportarSerie/ImportarSeriePorNome against the fake provider) and each
// media_file is repointed + gains the right episode link.
func TestReconciliarSerieDraftReligaEpisodios(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "Springfield Tales S01E01.mkv")
	escrever(t, arq)

	m := managerSerieAutoImportReconciliar(t, d)

	draftID := "serie-draft-orfa"
	criarSerieDraft(t, d, draftID, "Springfield Tales")
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: "mf-serie-draft", EntityID: draftID, Path: arq, State: "present",
	}); err != nil {
		t.Fatalf("criar media_file da série draft: %v", err)
	}

	linked, err := m.ReconciliarSerieDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarSerieDraft: %v", err)
	}
	if !linked {
		t.Fatalf("linked = false, quero true")
	}

	if n := contarVinculos(t, d); n != 1 {
		t.Errorf("vínculos tv_episode = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "101")); n != 1 {
		t.Errorf("vínculo para o ep 101 ausente")
	}
	entityID, conf := mediaFileDe(t, d, arq)
	if conf != confSerieEp {
		t.Errorf("match_confidence = %v, quero %v", conf, confSerieEp)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE id=? AND external_id_tmdb IS NOT NULL", entityID); n != 1 {
		t.Errorf("entity resolvida %q sem external_id_tmdb -- import real não persistiu identidade externa", entityID)
	}
}

// TestReconciliarSerieDraftSemImporterNaoLiga covers the explicit degradation:
// without SerieImporter configured (nil, ConfigurarSeries never called),
// ReconciliarSerieDraft never tries anything -- linked=false without an error
// (tentarImportarSerie already degrades that way, see serie.go).
func TestReconciliarSerieDraftSemImporterNaoLiga(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "Some Show S01E01.mkv")
	escrever(t, arq)

	m := managerFake(t, d) // no ConfigurarSeries -- serieImporter stays nil

	draftID := "serie-draft-sem-importer"
	criarSerieDraft(t, d, draftID, "Some Show")
	if _, err := sqlcgen.New(d.Write).CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID: "mf-sem-importer", EntityID: draftID, Path: arq, State: "present",
	}); err != nil {
		t.Fatalf("criar media_file: %v", err)
	}

	linked, err := m.ReconciliarSerieDraft(context.Background(), draftID, []string{"en"})
	if err != nil {
		t.Fatalf("ReconciliarSerieDraft: %v", err)
	}
	if linked {
		t.Errorf("linked = true, quero false (sem SerieImporter configurado)")
	}
	// Nothing was repointed -- the media_file stays on the original draft series.
	entityID := valorUnico(t, d, "SELECT entity_id FROM media_file WHERE path = ?", arq)
	if entityID != draftID {
		t.Errorf("media_file.entity_id mudou pra %q sem importer configurado", entityID)
	}
}
