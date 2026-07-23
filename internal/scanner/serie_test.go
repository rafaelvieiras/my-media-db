package scanner

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/library"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// ── testes de unidade dos helpers puros ──────────────────────────────────────
//
// Os testes do casamento por número EM MEMÓRIA (episodeIndex/casarEpisodios e
// helpers) migraram para internal/tvmatch/tvmatch_test.go quando essa lógica foi
// extraída para o pacote leaf (briefing 0069) — mesmas expectativas, símbolos
// exportados (Index/Casar).

// TestExtrairTVNS cobre a extracao do namespace core.tv@1 e os ramos de degradacao
// (null, vazio, json invalido, ns ausente) que devolvem nil.
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

// TestEhPastaTemporada cobre os reconhecimentos de pasta de temporada (incluindo os
// specials/temporada 0) e a rejeicao de nomes que sao a propria serie.
func TestEhPastaTemporada(t *testing.T) {
	for _, s := range []string{"Season 01", "Temporada 1", "S01", "specials", "Especiais"} {
		if !ehPastaTemporada(s) {
			t.Errorf("%q deveria ser pasta de temporada", s)
		}
	}
	for _, s := range []string{"Springfield Tales", "extras", ""} {
		if ehPastaTemporada(s) {
			t.Errorf("%q NAO deveria ser pasta de temporada", s)
		}
	}
}

// ── infraestrutura de série ──────────────────────────────────────────────────

// criarSourceSerie cadastra um datasource marcado content_type='series' (o serviço
// de library ainda não expõe o campo — a API é de outro passo; aqui gravamos direto).
func criarSourceSerie(t *testing.T, d *db.DB, dir string) library.Source {
	t.Helper()
	src, err := library.New(d).Criar(context.Background(), dir, "tv", "")
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

// managerSerieAutoImport monta um Manager com auto-import real: o enrich sobre o
// provider fake (série canned "Springfield Tales"/71663) e idiomas fixos.
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

// importerStub é um SerieImporter de teste que devolve um resultado fixo (para
// exercitar o roteamento a revisão sem depender do provider real).
type importerStub struct {
	id  string
	err error
}

func (s importerStub) ImportarSeriePorNome(context.Context, string, []string) (string, error) {
	return s.id, s.err
}

// contarVinculos conta as linhas de media_file_entity (opcionalmente por kind).
func contarVinculos(t *testing.T, d *db.DB) int {
	t.Helper()
	return contar(t, d, "SELECT count(*) FROM media_file_entity WHERE kind='tv_episode'")
}

// epIDPorTmdb devolve o id da entity episódio pelo external_id_tmdb (o SourceID da
// fixture, ex.: "101").
func epIDPorTmdb(t *testing.T, d *db.DB, tmdb string) string {
	t.Helper()
	var id string
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT id FROM entity WHERE external_id_tmdb=? AND type='episode'", tmdb).Scan(&id); err != nil {
		t.Fatalf("epIDPorTmdb(%s): %v", tmdb, err)
	}
	return id
}

// ── testes de integração (auto-import + match) ───────────────────────────────

// TestScanSerieAutoImportaEVincula: uma pasta de série NÃO importada é auto-importada
// do provider e o arquivo S01E01 é vinculado ao episódio certo (kind='tv_episode'),
// com o media_file apontando para a SÉRIE (a obra), não para o episódio.
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
	// media_file.entity_id aponta para a SÉRIE.
	if n := contar(t, d, `SELECT count(*) FROM media_file mf JOIN entity e ON e.id=mf.entity_id WHERE e.type='tv'`); n != 1 {
		t.Errorf("media_file não aponta para a série: %d", n)
	}
	// o vínculo aponta para o episódio 101.
	if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=?", epIDPorTmdb(t, d, "101")); n != 1 {
		t.Errorf("vínculo para o ep 101 ausente")
	}
}

// TestScanSerieRangeTresVinculos: S01E01-E03 num único arquivo cria 3 linhas em
// media_file_entity para o MESMO media_file, com ordinal 0/1/2.
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
	// ordinais 0,1,2 para o mesmo arquivo, apontando 101,102,103.
	for i, tmdb := range []string{"101", "102", "103"} {
		if n := contar(t, d, "SELECT count(*) FROM media_file_entity WHERE entity_id=? AND ordinal=?",
			epIDPorTmdb(t, d, tmdb), i); n != 1 {
			t.Errorf("ep %s deveria ter ordinal %d", tmdb, i)
		}
	}
}

// TestScanSerieAbsoluto: um arquivo estilo anime (" - 004") casa por absolute_number
// (a fixture 71663 tem absolutos 1..6; o 4 é o ep 201).
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

// TestScanSerieReScanIdempotente: reescanear não duplica media_file nem vínculos.
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

// TestScanSerieAmbiguaVaiParaRevisao: quando o import é ambíguo, o arquivo vira um
// media_file de baixa confiança apontando para uma série RASCUNHO, SEM vínculo de
// episódio (fila de revisão) — nunca um episódio fantasma.
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
	if n := contarVinculos(t, d); n != 0 {
		t.Errorf("vínculos = %d, quero 0 (revisão)", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv' AND status='draft'"); n != 1 {
		t.Errorf("série rascunho = %d, quero 1", n)
	}
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='episode'"); n != 0 {
		t.Errorf("episódios = %d, quero 0 (sem fantasma)", n)
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

// TestScanSerieSemPadraoEpisodio: um arquivo sem SxxEyy/absoluto numa série real fica
// pendente (sem vínculo), mas o media_file existe apontando para a série.
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

// TestScanSerieAncoraNoID: o vínculo guarda o ID do episódio, não o número. Trocar a
// ordenação default da série (re-projeção) NÃO altera a linha de media_file_entity.
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

	// Re-projeta a ordenação default da série para "dvd" (simula troca de ordem). O
	// vínculo é ancorado no id: nada aqui deve reescrever media_file_entity.
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
