package scanner

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// ── unit tests of the pure helpers ───────────────────────────────────────────
//
// The tests of the IN-MEMORY number matching (episodeIndex/casarEpisodios and
// helpers) moved to internal/tvmatch/tvmatch_test.go when that logic was extracted to
// the leaf package — same expectations, exported symbols (Index/Casar).

// TestExtrairTVNS covers the extraction of the core.tv@1 namespace and the degradation
// branches (null, empty, invalid json, absent ns) that return nil.
func TestExtrairTVNS(t *testing.T) {
	nulos := []sql.NullString{
		{},
		{Valid: true, String: ""},
		{Valid: true, String: "nao-e-json"},
		{Valid: true, String: `{"outro.ns@1":{}}`},
	}
	for i, n := range nulos {
		if got := extrairTVNS(n); got != nil {
			t.Errorf("caso %d: extrairTVNS deveria degradar para nil, got %v", i, got)
		}
	}
	got := extrairTVNS(sql.NullString{Valid: true, String: `{"core.tv@1":{"default_season_type":"official"}}`})
	if got == nil || got["default_season_type"] != "official" {
		t.Errorf("extrairTVNS = %v, quero o objeto do namespace", got)
	}
}

// TestPreferredLangDaFonte covers the resolver of the preferred language of the source
// that feeds the series auto-import via SerieImporter: a source with PreferredLanguage
// returns the value; a source without a preference (nil) returns "" (the SerieImporter
// falls back to the global language of Manager.idiomas).
func TestPreferredLangDaFonte(t *testing.T) {
	en := "en"
	comPreferencia := &scanRun{source: library.Source{PreferredLanguage: &en}}
	if got := comPreferencia.preferredLangDaFonte(); got != "en" {
		t.Errorf("preferredLangDaFonte() = %q, quero \"en\"", got)
	}

	semPreferencia := &scanRun{source: library.Source{PreferredLanguage: nil}}
	if got := semPreferencia.preferredLangDaFonte(); got != "" {
		t.Errorf("preferredLangDaFonte() = %q, quero \"\" (sem preferência)", got)
	}
}

// ── series infrastructure ────────────────────────────────────────────────────

// criarSourceSerie registers a datasource marked content_type='series' (the library
// service does not expose the field yet — the API is another step; here we write it
// directly).
func criarSourceSerie(t *testing.T, d *db.DB, dir string) library.Source {
	t.Helper()
	src, err := library.New(d).Criar(context.Background(), dir, "tv", "", nil, nil)
	if err != nil {
		t.Fatalf("criar source: %v", err)
	}
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE library_source SET content_type='series' WHERE id=?", src.ID); err != nil {
		t.Fatalf("marcar series: %v", err)
	}
	src2, err := library.New(d).Obter(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("reobter source: %v", err)
	}
	return src2
}

// managerSerieAutoImport assembles a Manager with a real auto-import: the enrich on
// top of the fake provider (canned series "Springfield Tales"/71663) and fixed
// languages.
func managerSerieAutoImport(t *testing.T, d *db.DB) *Manager {
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

// importerStub is a test SerieImporter that returns a fixed result (to exercise the
// routing to review without depending on the real provider).
type importerStub struct {
	id      string
	idPorID string
	err     error
}

func (s importerStub) ImportarSeriePorNome(context.Context, enrich.AlvoSerie, []string, string) (string, error) {
	return s.id, s.err
}

// ImportarSeriePorID keeps the stub satisfying SerieImporter. The zero value reports "not
// found", so a test that does not care about the id door keeps the old chain (import by
// name). Tests that DO care set idPorID.
func (s importerStub) ImportarSeriePorID(context.Context, string, string, []string) (string, error) {
	if s.idPorID == "" {
		return "", errors.New("id desconhecido no stub")
	}
	return s.idPorID, nil
}

// contarVinculos counts the media_file_entity rows (optionally by kind).
func contarVinculos(t *testing.T, d *db.DB) int {
	t.Helper()
	return contar(t, d, "SELECT count(*) FROM media_file_entity WHERE kind='tv_episode'")
}

// epIDPorTmdb returns the id of the episode entity by external_id_tmdb (the SourceID of
// the fixture, e.g. "101").
func epIDPorTmdb(t *testing.T, d *db.DB, tmdb string) string {
	t.Helper()
	var id string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT id FROM entity WHERE external_id_tmdb=? AND type='episode'", tmdb).Scan(&id); err != nil {
		t.Fatalf("epIDPorTmdb(%s): %v", tmdb, err)
	}
	return id
}

// ── integration tests (auto-import + match) ──────────────────────────────────

// TestScanSerieAutoImportaEVincula: a NOT imported series folder is auto-imported from
// the provider and the S01E01 file is linked to the right episode (kind='tv_episode'),
// with the media_file pointing to the SERIES (the work), not to the episode.
func TestScanSerieAutoImportaEVincula(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv'"); n != 1 {
		t.Errorf("séries = %d, quero 1 (auto-importada)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file = %d, quero 1", n)
	}
	if n := contarVinculos(t, d); n != 1 {
		t.Errorf("vínculos tv_episode = %d, quero 1", n)
	}
	// media_file.entity_id points to the SERIES.
	if n := contar(t, d, `SELECT count(*) FROM media_file mf JOIN entity e ON e.id=mf.entity_id WHERE e.type='tv'`); n != 1 {
		t.Errorf("media_file não aponta para a série: %d", n)
	}
	// the link points to episode 101.
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "101")); n != 1 {
		t.Errorf("vínculo para o ep 101 ausente")
	}
}

// TestScanSerieRangeTresVinculos: S01E01-E03 in a single file creates 3 rows in
// media_file_entity for the SAME media_file, with ordinal 0/1/2.
func TestScanSerieRangeTresVinculos(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01-E03.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file = %d, quero 1", n)
	}
	if n := contarVinculos(t, d); n != 3 {
		t.Errorf("vínculos = %d, quero 3 (range)", n)
	}
	// ordinals 0,1,2 for the same file, pointing to 101,102,103.
	for i, tmdb := range []string{"101", "102", "103"} {
		if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=? AND ordinal=?",
			epIDPorTmdb(t, d, tmdb), i); n != 1 {
			t.Errorf("ep %s deveria ter ordinal %d", tmdb, i)
		}
	}
}

// TestScanSerieAbsoluto: an anime-style file (" - 004") matches by absolute_number (the
// 71663 fixture has absolutes 1..6; 4 is ep 201).
func TestScanSerieAbsoluto(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Springfield Tales - 004.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contarVinculos(t, d); n != 1 {
		t.Errorf("vínculos = %d, quero 1 (absoluto)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "201")); n != 1 {
		t.Errorf("absoluto 004 deveria casar o ep 201")
	}
}

// TestScanSerieReScanIdempotente: re-scanning duplicates neither media_file nor links.
func TestScanSerieReScanIdempotente(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01-E03.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file após re-scan = %d, quero 1", n)
	}
	if n := contarVinculos(t, d); n != 3 {
		t.Errorf("vínculos após re-scan = %d, quero 3 (sem duplicar)", n)
	}
}

// capturingImporterStub is a test SerieImporter that records the preferredLang received
// on the LAST call and returns a fixed error -- to prove that the scanner passes along
// the PreferredLanguage of the source, without depending on a real provider.
type capturingImporterStub struct {
	err           error
	preferredLang string
}

func (s *capturingImporterStub) ImportarSeriePorNome(_ context.Context, _ enrich.AlvoSerie, _ []string, preferredLang string) (string, error) {
	s.preferredLang = preferredLang
	return "", s.err
}

func (s *capturingImporterStub) ImportarSeriePorID(context.Context, string, string, []string) (string, error) {
	return "", errors.New("id desconhecido no stub")
}

// TestScanSerieUsaPreferredLanguageDaFonteNoAutoImport covers the priority of the source
// preference: a library_source with preferred_language defined ("en") must prevail over
// the GLOBAL language injected via ConfigurarSeries ("pt-BR") in the call to
// SerieImporter.
func TestScanSerieUsaPreferredLanguageDaFonteNoAutoImport(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Mystery Show", "Season 01", "Mystery Show S01E01.mkv"))

	stub := &capturingImporterStub{err: enrich.ErrSerieAmbigua}
	m := managerFake(t, d)
	m.ConfigurarSeries(stub, func(context.Context) []string { return []string{"pt-BR"} })

	src := criarSourceSerie(t, d, root)
	en := "en"
	if _, err := d.Write.ExecContext(context.Background(),
		"UPDATE library_source SET preferred_language=? WHERE id=?", en, src.ID); err != nil {
		t.Fatalf("marcar preferred_language: %v", err)
	}
	src2, err := library.New(d).Obter(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("reobter source: %v", err)
	}
	if src2.PreferredLanguage == nil || *src2.PreferredLanguage != "en" {
		t.Fatalf("fixture: source.PreferredLanguage = %v, quero \"en\"", src2.PreferredLanguage)
	}

	if err := scanSync(t, m, src2); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stub.preferredLang != "en" {
		t.Errorf("preferredLang recebido pelo SerieImporter = %q, quero \"en\" (da fonte, não o global pt-BR)", stub.preferredLang)
	}
}

// TestScanSerieAmbiguaVaiParaRevisao: when the import is ambiguous, the file becomes a
// low-confidence media_file pointing to a DRAFT series, WITHOUT an episode link (review
// queue) — never a ghost episode.
func TestScanSerieAmbiguaVaiParaRevisao(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Mystery Show", "Season 01", "Mystery Show S01E01.mkv"))

	m := managerFake(t, d)
	m.ConfigurarSeries(importerStub{err: enrich.ErrSerieAmbigua}, nil)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	// The series has no identity yet, but the folders and the file names DO say the shape:
	// the provisional tree is built from them (season 01, episode 01) and the file is
	// linked to that episode. Before, the file hung flat on the draft series and the screen
	// had no season to open -- 785 files of 16 series measured in production.
	if n := contarVinculos(t, d); n != 1 {
		t.Errorf("vínculos = %d, quero 1 (provisório da revisão)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv' AND status='draft'"); n != 1 {
		t.Errorf("série rascunho = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='season'"); n != 1 {
		t.Errorf("temporadas = %d, quero 1 (provisória)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode' AND status='draft'"); n != 1 {
		t.Errorf("episódios rascunho = %d, quero 1 (provisório da revisão)", n)
	}
	var conf float64
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT match_confidence FROM media_file").Scan(&conf); err != nil {
		t.Fatalf("ler confiança: %v", err)
	}
	if conf >= 70 {
		t.Errorf("confiança = %v, quero < 70 (revisão)", conf)
	}
}

// TestScanSerieSemPadraoEpisodio: a file without SxxEyy/absolute in a real series stays
// pending (without a link), but the media_file exists, pointing to the series.
func TestScanSerieSemPadraoEpisodio(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "featurette bonus.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM media_file"); n != 1 {
		t.Errorf("media_file = %d, quero 1", n)
	}
	if n := contarVinculos(t, d); n != 0 {
		t.Errorf("vínculos = %d, quero 0 (sem padrão de episódio)", n)
	}
}

// TestScanSerieMaterializaAposAutoImport covers the optional automatic materialization
// on the series path: when ImportarSeriePorNome succeeds and a Materializador is
// configured, MaterializarSeAutoDownloadLigado is called once with the id of the
// auto-imported series.
func TestScanSerieMaterializaAposAutoImport(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01.mkv"))

	mat := &materializadorFake{}
	m := managerSerieAutoImport(t, d)
	m.ConfigurarMaterializacao(mat)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if mat.chamadas != 1 {
		t.Fatalf("MaterializarSeAutoDownloadLigado chamado %d vez(es), quero 1", mat.chamadas)
	}
	serieID := valorUnico(t, d, "SELECT id FROM entity WHERE type='tv'")
	if len(mat.entityIDs) != 1 || mat.entityIDs[0] != serieID {
		t.Errorf("MaterializarSeAutoDownloadLigado recebeu %v, quero [%q] (id da série auto-importada)", mat.entityIDs, serieID)
	}
}

// TestScanSerieNaoMaterializaQuandoImportFalha covers: an ambiguous/error import
// (without an auto-imported series) does NOT fire MaterializarSeAutoDownloadLigado,
// even with Materializador configured.
func TestScanSerieNaoMaterializaQuandoImportFalha(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Mystery Show", "Season 01", "Mystery Show S01E01.mkv"))

	mat := &materializadorFake{}
	m := managerFake(t, d)
	m.ConfigurarSeries(importerStub{err: enrich.ErrSerieAmbigua}, nil)
	m.ConfigurarMaterializacao(mat)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if mat.chamadas != 0 {
		t.Errorf("MaterializarSeAutoDownloadLigado chamado %d vez(es), quero 0 (import falhou)", mat.chamadas)
	}
}

// TestScanSerieSemMaterializadorNaoQuebra covers the degradation: without
// ConfigurarMaterializacao (nil, the mode prior to the auto-match), the series
// auto-import keeps working normally, without panicking.
func TestScanSerieSemMaterializadorNaoQuebra(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv'"); n != 1 {
		t.Errorf("séries = %d, quero 1", n)
	}
}

// criarSerieDraft directly creates an entity type='tv' status='draft' with the given
// title -- it simulates the result of acharOuCriarDraftSerie from a PREVIOUS scan
// (without an importer configured, or an import that failed), without depending on the
// scanner to reach that state.
func criarSerieDraft(t *testing.T, d *db.DB, id, titulo string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipoSerieTV, Title: titulo,
	}); err != nil {
		t.Fatalf("criar série draft: %v", err)
	}
	if _, err := sqlcgen.New(d.Write).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipoSerieTV, Title: titulo, Status: "draft",
	}); err != nil {
		t.Fatalf("rebaixar série draft: %v", err)
	}
}

// TestResolverSerieMissIgnoraDraftEResolveDeVerdade covers the fix of
// acharSerieResolvidaPorTitulo (a bug found while investigating the Bomfunk MC's): a
// series ALREADY EXISTING as a review draft (status='draft'), with the SAME title the
// auto-import will match, must NOT block the resolution forever -- resolverSerieMiss
// must try tentarImportarSerie again (the draft is ignored by
// acharSerieResolvidaPorTitulo, which only filters the find-by-title; the entity reused
// by the enrich ITSELF via tentarCasarComDraftLocal is a matching mechanism independent
// of this fix -- it may return the SAME draft id, and that is fine: what matters is that
// ImportarSeriePorNome WAS really called). Proof of that: WITHOUT the fix,
// acharSeriePorTitulo (without a filter) found the draft again BEFORE any import
// attempt -- carregarIndex over a draft without an episode tree (empty entity_relation)
// never matches any episode, so the file would stay forever without a link and with
// review confidence (40). WITH the fix, the real import runs, populates the episode tree
// (entity_relation) and the file matches the right episode (link + series confidence,
// 90).
func TestResolverSerieMissIgnoraDraftEResolveDeVerdade(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	arq := filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01.mkv")
	escrever(t, arq)

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)

	draftID := "serie-draft-preexistente"
	criarSerieDraft(t, d, draftID, "Springfield Tales")

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// No duplicated series -- the real import recognized/reused the same title identity,
	// it did not create a second loose entity.
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv'"); n != 1 {
		t.Errorf("séries type=tv = %d, quero 1 (sem duplicar)", n)
	}
	// The proof that the real import ran (and did not get stuck on the draft found by
	// title, without a tree): the episode matched, with a link and series confidence --
	// a draft never imported has no entity_relation at all.
	if n := contarVinculos(t, d); n != 1 {
		t.Errorf("vínculos tv_episode = %d, quero 1 (prova de import real, não só o draft achado por título)", n)
	}
	entityID, conf := mediaFileDe(t, d, arq)
	if entityID != draftID {
		t.Errorf("media_file.entity_id = %q, quero %q (mesma identidade, reaproveitada pelo import)", entityID, draftID)
	}
	if conf != confSerieEp {
		t.Errorf("match_confidence = %v, quero %v (confiança de série casada, não revisão)", conf, confSerieEp)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE id=? AND external_id_tmdb IS NOT NULL", draftID); n != 1 {
		t.Errorf("entity %q sem external_id_tmdb -- import real não persistiu identidade externa", draftID)
	}
}

// TestAcharOuCriarDraftSerieReusaMesmoDraft covers the explicit concern of the fix: the
// change to acharSerieResolvidaPorTitulo (used only by the RESOLUTION path) must NOT
// change the behavior of acharOuCriarDraftSerie (used by the draft CREATION/reuse path)
// -- called twice for the SAME title, it keeps returning the SAME id (idempotent,
// without duplicating the draft on every scan).
func TestAcharOuCriarDraftSerieReusaMesmoDraft(t *testing.T) {
	d := abrirDB(t)
	m := managerFake(t, d)
	r := &scanRun{m: m}

	id1 := r.acharOuCriarDraftSerie(context.Background(), "Série Sem Match")
	if id1 == "" {
		t.Fatalf("acharOuCriarDraftSerie devolveu vazio")
	}
	id2 := r.acharOuCriarDraftSerie(context.Background(), "Série Sem Match")
	if id2 != id1 {
		t.Errorf("acharOuCriarDraftSerie duplicou o draft: %q -> %q", id1, id2)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv' AND status='draft'"); n != 1 {
		t.Errorf("rascunhos = %d, quero 1 (reusado, não duplicado)", n)
	}
}

// valorUnico reads a string column of an expected single row (a lean assertion helper
// for the materialization tests).
func valorUnico(t *testing.T, d *db.DB, query string, args ...any) string {
	t.Helper()
	var v string
	if err := d.Read.QueryRowContext(context.Background(), query, args...).Scan(&v); err != nil {
		t.Fatalf("valorUnico (%s): %v", query, err)
	}
	return v
}

// TestScanSerieAncoraNoID: the link keeps the ID of the episode, not the number.
// Changing the default ordering of the series (re-projection) does NOT change the
// media_file_entity row.
func TestScanSerieAncoraNoID(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E01.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	ep101 := epIDPorTmdb(t, d, "101")

	var antes string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT entity_id FROM media_file_entity WHERE kind='tv_episode'").Scan(&antes); err != nil {
		t.Fatalf("ler vínculo: %v", err)
	}
	if antes != ep101 {
		t.Fatalf("vínculo = %q, quero o id do ep 101 (%q)", antes, ep101)
	}

	// It re-projects the default ordering of the series to "dvd" (it simulates an
	// ordering change). The link is anchored on the id: nothing here should rewrite
	// media_file_entity.
	if _, err := d.Write.ExecContext(context.Background(),
		`UPDATE entity SET extension_data_json=replace(extension_data_json,'"default_season_type":"official"','"default_season_type":"dvd"') WHERE type='tv'`); err != nil {
		t.Fatalf("re-projetar: %v", err)
	}
	var depois string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT entity_id FROM media_file_entity WHERE kind='tv_episode'").Scan(&depois); err != nil {
		t.Fatalf("reler vínculo: %v", err)
	}
	if depois != ep101 {
		t.Errorf("vínculo mudou após re-projeção: %q -> %q (deveria ser id-ancorado)", antes, depois)
	}
}

// ── Provisional episode ─────────────────────────────────────────────────────

// TestScanSerieEpisodioProvisorioQuandoNumeroForaDaArvore covers the real case of the
// living-room catalog: the file declares S01E99, the imported tree only has 1..3. Before,
// the file stayed without an episode (media_file hanging on the series, review
// confidence, without NFO and without a synopsis in the consuming library); now it
// becomes a provisional episode in the right season, marked as draft.
func TestScanSerieEpisodioProvisorioQuandoNumeroForaDaArvore(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 01", "Springfield Tales S01E99.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if n := contarVinculos(t, d); n != 1 {
		t.Fatalf("vínculos tv_episode = %d, quero 1 (provisório)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode' AND status='draft'"); n != 1 {
		t.Errorf("episódios draft = %d, quero 1", n)
	}
	var titulo, ext string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT title, coalesce(extension_data_json,'') FROM entity WHERE type='episode' AND status='draft'").
		Scan(&titulo, &ext); err != nil {
		t.Fatalf("ler episódio provisório: %v", err)
	}
	if titulo != "Episódio 99" {
		t.Errorf("título do provisório = %q, quero %q (mesmo padrão do enriquecimento)", titulo, "Episódio 99")
	}
	if !strings.Contains(ext, `"season":1`) || !strings.Contains(ext, `"number":99`) {
		t.Errorf("extension_data sem a numeração do arquivo: %s", ext)
	}
	if !strings.Contains(ext, `"provisorio"`) {
		t.Errorf("extension_data sem a marca de provisório: %s", ext)
	}
	// The link left the review: the media_file no longer has confidence 40.
	var conf float64
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT coalesce(match_confidence,0) FROM media_file").Scan(&conf); err != nil {
		t.Fatalf("ler match_confidence: %v", err)
	}
	if conf < confSerieEp {
		t.Errorf("match_confidence = %v, quero >= %d (casado pelo número do arquivo)", conf, confSerieEp)
	}

	// Idempotency: a re-scan does not create a second provisional episode.
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("re-scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode' AND status='draft'"); n != 1 {
		t.Errorf("episódios draft depois do re-scan = %d, quero 1", n)
	}
	if n := contarVinculos(t, d); n != 1 {
		t.Errorf("vínculos depois do re-scan = %d, quero 1", n)
	}
}

// TestScanSerieEpisodioProvisorioSemNumeroUsaPastaDaTemporada covers the other real case
// (an extra/extras folder named "Season 00"): the file name does not carry SxxEyy, but
// the folder states the season. The provisional episode is born without a number, with
// the title of the file, in a season the scanner creates when it does not exist
// either.
func TestScanSerieEpisodioProvisorioSemNumeroUsaPastaDaTemporada(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 03", "Groovies.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// The imported tree brings 2 seasons and 6 episodes; the provisional one is the
	// seventh episode, in season 3 which the scanner had to create.
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='season'"); n != 3 {
		t.Errorf("temporadas = %d, quero 3 (2 importadas + a do arquivo)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode' AND status='draft'"); n != 1 {
		t.Fatalf("episódios provisórios = %d, quero 1", n)
	}
	var titulo string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT title FROM entity WHERE type='episode' AND status='draft'").Scan(&titulo); err != nil {
		t.Fatalf("ler episódio: %v", err)
	}
	if titulo != "Groovies" {
		t.Errorf("título do provisório = %q, quero %q (nome do arquivo)", titulo, "Groovies")
	}
	// The created season must be 3 (folder "Season 03") and the provisional one must
	// hang from it.
	if n := contar(t, d, `SELECT count(*) FROM entity_relation rs
		JOIN entity_relation re ON re.parent_id=rs.child_id
		JOIN entity e ON e.id=re.child_id
		WHERE rs.kind='tv_season' AND rs.sort_index=3 AND e.status='draft'`); n != 1 {
		t.Errorf("provisório fora da temporada 3: %d", n)
	}

	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("re-scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode' AND status='draft'"); n != 1 {
		t.Errorf("episódios provisórios depois do re-scan = %d, quero 1", n)
	}
}

// TestScanSerieProvisorioSemNumeroNaoDuplicaQuandoOTituloMuda covers the idempotency of
// the provisional episode WITHOUT a number with the right key: the dedup is by the FILE
// NAME stored in the extension_data, not by the title. In the first production round the
// title rule of the provisional episode changed and the re-scan created a second episode
// for the same file ("Especial de Natal - Que Natal Que Nada" and "Especial de Natal Que
// Natal Que Nada").
func TestScanSerieProvisorioSemNumeroNaoDuplicaQuandoOTituloMuda(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 03", "Especial de Natal - Que Natal Que Nada.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	// It simulates the title stored by a previous version (old rule), keeping the
	// extension_data the way that version wrote it.
	ep, err := sqlcgen.New(d.Write).ObterEntity(context.Background(), idDoProvisorio(t, d))
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if _, err := sqlcgen.New(d.Write).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		ID:                ep.ID,
		Type:              ep.Type,
		Title:             "Especial de Natal Que Natal Que Nada",
		Status:            ep.Status,
		ExtensionDataJson: ep.ExtensionDataJson,
	}); err != nil {
		t.Fatalf("renomear provisório: %v", err)
	}

	// Re-scan with the file "new" (a different mtime forces the reprocessing).
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 03", "Especial de Natal - Que Natal Que Nada.mkv"))
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("re-scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode' AND status='draft'"); n != 1 {
		t.Errorf("provisórios depois do re-scan = %d, quero 1 (chave é o arquivo, não o título)", n)
	}
}

// idDoProvisorio returns the id of the only provisional episode of the test database.
func idDoProvisorio(t *testing.T, d *db.DB) string {
	t.Helper()
	var id string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT id FROM entity WHERE type='episode' AND status='draft'").Scan(&id); err != nil {
		t.Fatalf("idDoProvisorio: %v", err)
	}
	return id
}

// criarSerieResolvida inserts a series entity already resolved (status cataloged), so the
// scan has a real target to link the file to.
func criarSerieResolvida(t *testing.T, d *db.DB, id, titulo string) {
	t.Helper()
	if _, err := sqlcgen.New(d.Write).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipoSerieTV, Title: titulo,
	}); err != nil {
		t.Fatalf("criar série %s: %v", id, err)
	}
}

// importerPorIDStub resolves ONLY through the folder id: ImportarSeriePorNome always
// fails. It proves the scanner reaches the id door FIRST and never needs the name.
type importerPorIDStub struct {
	fonte      string
	id         string
	nomeChamou int
}

func (s *importerPorIDStub) ImportarSeriePorNome(context.Context, enrich.AlvoSerie, []string, string) (string, error) {
	s.nomeChamou++
	return "", enrich.ErrSerieNaoEncontrada
}

func (s *importerPorIDStub) ImportarSeriePorID(_ context.Context, fonte, id string, _ []string) (string, error) {
	s.fonte, s.id = fonte, id
	return "serie-do-id", nil
}

// TestScanSerieImportaPeloIDDaPasta is the crown case of the id door: a MISSPELLED folder
// name that carries the id must resolve by the id, without ever searching the name. TMDb
// answers zero results for "2 Broken Girls", so the name path cannot work -- the id can.
func TestScanSerieImportaPeloIDDaPasta(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "2 Broken Girls (2011) [tmdbid-39340]", "Season 01", "S01E01.mkv"))

	stub := &importerPorIDStub{}
	m := managerFake(t, d)
	m.ConfigurarSeries(stub, func(context.Context) []string { return []string{"en"} })
	// The import by id returns a series that already exists: the file is linked to it.
	criarSerieResolvida(t, d, "serie-do-id", "2 Broke Girls")

	if err := scanSync(t, m, criarSourceSerie(t, d, root)); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stub.fonte != "tmdb" || stub.id != "39340" {
		t.Fatalf("import por id recebeu (%q, %q), quero (tmdb, 39340)", stub.fonte, stub.id)
	}
	if stub.nomeChamou != 0 {
		t.Errorf("busca por nome rodou %d vez(es), quero 0 (o id da pasta basta)", stub.nomeChamou)
	}
}

// TestScanSerieImportaPeloTVDBDaPasta covers the other id the folder may carry: with
// [tvdbid-N] the scanner crosses the TVDB id, in the same way.
func TestScanSerieImportaPeloTVDBDaPasta(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "2 Broke Girls (2011) [tvdbid-248741]", "Season 01", "S01E01.mkv"))

	stub := &importerPorIDStub{}
	m := managerFake(t, d)
	m.ConfigurarSeries(stub, func(context.Context) []string { return []string{"en"} })
	criarSerieResolvida(t, d, "serie-do-id", "2 Broke Girls")

	if err := scanSync(t, m, criarSourceSerie(t, d, root)); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stub.fonte != "tvdb" || stub.id != "248741" {
		t.Fatalf("import por id recebeu (%q, %q), quero (tvdb, 248741)", stub.fonte, stub.id)
	}
}

// TestScanSerieSemIDNaPastaSeguiePeloNome: without an id on disk the old chain runs (import
// by name) -- the id door must not change the behavior of a normal folder.
func TestScanSerieSemIDNaPastaSeguiePeloNome(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Mystery Show", "Season 01", "Mystery Show S01E01.mkv"))

	stub := &importerPorIDStub{}
	m := managerFake(t, d)
	m.ConfigurarSeries(stub, func(context.Context) []string { return []string{"en"} })

	if err := scanSync(t, m, criarSourceSerie(t, d, root)); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stub.fonte != "" {
		t.Errorf("import por id rodou sem id na pasta (fonte=%q)", stub.fonte)
	}
	if stub.nomeChamou != 1 {
		t.Errorf("busca por nome rodou %d vez(es), quero 1", stub.nomeChamou)
	}
}

// TestScanSerieSemIdentidadeMontaArvoreProvisoria covers the shape the owner expects from
// a series that did not match any provider: the folders and the file names already state
// the tree, so the scan builds it. Season 01 from the folder, episode from SxxEyy, the
// file linked to the episode -- and the series stays draft (the identity is still missing).
func TestScanSerieSemIdentidadeMontaArvoreProvisoria(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Some Show", "Temporada 01", "Some Show - S01E01.mkv"))
	escrever(t, filepath.Join(root, "Some Show", "Temporada 01", "Some Show - S01E02.mkv"))
	escrever(t, filepath.Join(root, "Some Show", "Temporada 02", "Some Show - S02E01.mkv"))

	m := managerFake(t, d)
	m.ConfigurarSeries(importerStub{err: enrich.ErrSerieNaoEncontrada}, nil)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='season'"); n != 2 {
		t.Errorf("temporadas = %d, quero 2", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode'"); n != 3 {
		t.Errorf("episódios = %d, quero 3", n)
	}
	if n := contarVinculos(t, d); n != 3 {
		t.Errorf("vínculos arquivo-episódio = %d, quero 3", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv' AND status='draft'"); n != 1 {
		t.Errorf("série rascunho = %d, quero 1", n)
	}
	// Re-scan is idempotent: the provisional tree is found, never duplicated.
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("re-scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode'"); n != 3 {
		t.Errorf("episódios após re-scan = %d, quero 3 (sem duplicar)", n)
	}
}

// TestScanPastaEspeciaisViraTemporadaZero covers the specials folder with the word in front
// ("Temporada Specials"): it is season 0 of the series, never a series of its own. The
// files without SxxEyy still become provisional episodes of that season.
func TestScanPastaEspeciaisViraTemporadaZero(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Some Show", "Temporada Specials", "Making of.mkv"))
	escrever(t, filepath.Join(root, "Some Show", "Temporada 01", "Some Show S01E01.mkv"))

	m := managerFake(t, d)
	m.ConfigurarSeries(importerStub{err: enrich.ErrSerieNaoEncontrada}, nil)
	if err := scanSync(t, m, criarSourceSerie(t, d, root)); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv'"); n != 1 {
		t.Errorf("séries = %d, quero 1 (a pasta de especiais NÃO é série)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity_relation WHERE kind='tv_season' AND sort_index=0"); n != 1 {
		t.Errorf("temporadas 0 = %d, quero 1", n)
	}
}
