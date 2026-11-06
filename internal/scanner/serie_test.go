package scanner

import (
	"context"
	"database/sql"
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

// TestPreferredLangDaFonte cobre o resolvedor do idioma preferido da fonte
// (briefing 0084) que alimenta o auto-import de série via SerieImporter:
// fonte com PreferredLanguage devolve o valor; fonte sem preferência (nil)
// devolve "" (o SerieImporter cai no idioma global de Manager.idiomas).
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

// ── infraestrutura de série ──────────────────────────────────────────────────

// criarSourceSerie cadastra um datasource marcado content_type='series' (o serviço
// de library ainda não expõe o campo — a API é de outro passo; aqui gravamos direto).
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

func (s importerStub) ImportarSeriePorNome(context.Context, string, []string, string) (string, error) {
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

// capturingImporterStub é um SerieImporter de teste que grava o preferredLang
// recebido na ÚLTIMA chamada (briefing 0084) e devolve um erro fixo -- para
// provar que o scanner passa adiante o PreferredLanguage da fonte, sem
// depender de um provider real.
type capturingImporterStub struct {
	err           error
	preferredLang string
}

func (s *capturingImporterStub) ImportarSeriePorNome(_ context.Context, _ string, _ []string, preferredLang string) (string, error) {
	s.preferredLang = preferredLang
	return "", s.err
}

// TestScanSerieUsaPreferredLanguageDaFonteNoAutoImport cobre o item 10 do
// briefing 0084: uma library_source com preferred_language definido ("en")
// deve prevalecer sobre o idioma GLOBAL injetado via ConfigurarSeries
// ("pt-BR") na chamada ao SerieImporter.
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

// TestScanSerieMaterializaAposAutoImport cobre a materialização automática
// opcional (0089) no caminho de série: quando ImportarSeriePorNome tem
// sucesso e um Materializador está configurado,
// MaterializarSeAutoDownloadLigado é chamado uma vez com o id da série
// auto-importada.
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

// TestScanSerieNaoMaterializaQuandoImportFalha cobre: import ambíguo/erro
// (sem série auto-importada) NÃO dispara MaterializarSeAutoDownloadLigado,
// mesmo com Materializador configurado.
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

// TestScanSerieSemMaterializadorNaoQuebra cobre a degradação: sem
// ConfigurarMaterializacao (nil, modo pré-0089), o auto-import de série
// continua funcionando normalmente, sem pânico.
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

// criarSerieDraft cria diretamente uma entity type='tv' status='draft' com o
// título dado -- simula o resultado de acharOuCriarDraftSerie de um scan
// ANTERIOR (sem importer configurado, ou import que falhou), sem depender do
// scanner para chegar nesse estado.
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

// TestResolverSerieMissIgnoraDraftEResolveDeVerdade cobre o fix do briefing
// 0120 (bug achado investigando o Bomfunk MC's): uma série JÁ EXISTENTE como
// rascunho de revisão (status='draft'), com o MESMO título que o auto-import
// vai casar, NÃO deve travar a resolução para sempre -- resolverSerieMiss
// deve tentar tentarImportarSerie de novo (o draft é ignorado por
// acharSerieResolvidaPorTitulo, que só filtra o find-by-título; a entity
// reaproveitada pelo PRÓPRIO enrich via tentarCasarComDraftLocal, briefing
// 0054, é um mecanismo de casamento independente deste fix -- pode devolver o
// MESMO id do draft, e está tudo bem: o que importa é que ImportarSeriePorNome
// FOI chamado de verdade). Prova disso: SEM o fix, acharSeriePorTitulo (sem
// filtro) reencontrava o draft ANTES de qualquer tentativa de import --
// carregarIndex via um draft sem árvore de episódios (entity_relation vazia)
// nunca casa nenhum episódio, então o arquivo ficaria para sempre sem vínculo
// e com confiança de revisão (40). COM o fix, o import de verdade roda,
// popula a árvore de episódios (entity_relation) e o arquivo casa o episódio
// certo (vínculo + confiança de série, 90).
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

	// Nenhuma série duplicada -- o import real reconheceu/reaproveitou a
	// mesma identidade de título (0054), não criou uma segunda entity solta.
	if n := contar(t, d, "SELECT count(*) FROM entity WHERE type='tv'"); n != 1 {
		t.Errorf("séries type=tv = %d, quero 1 (sem duplicar)", n)
	}
	// A prova de que o import de verdade rodou (e não travou no draft achado
	// por título, sem árvore): o episódio casou, com vínculo e confiança de
	// série -- um draft nunca importado não tem entity_relation nenhuma.
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

// TestAcharOuCriarDraftSerieReusaMesmoDraft cobre a preocupação explícita do
// briefing 0120: o fix de acharSerieResolvidaPorTitulo (usada só pelo
// caminho de RESOLUÇÃO) NÃO pode mudar o comportamento de
// acharOuCriarDraftSerie (usada pelo caminho de CRIAÇÃO/reuso de rascunho) --
// chamado duas vezes pro MESMO título, continua devolvendo o MESMO id
// (idempotente, sem duplicar o draft a cada scan).
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

// valorUnico lê uma coluna string de uma linha única esperada (helper de
// asserção enxuto para os testes de materialização).
func valorUnico(t *testing.T, d *db.DB, query string, args ...any) string {
	t.Helper()
	var v string
	if err := d.Read.QueryRowContext(context.Background(), query, args...).Scan(&v); err != nil {
		t.Fatalf("valorUnico (%s): %v", query, err)
	}
	return v
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

// ── Episódio provisório (briefing 0133) ─────────────────────────────────────

// TestScanSerieEpisodioProvisorioQuandoNumeroForaDaArvore cobre o caso real do
// catálogo da sala: o arquivo declara S01E99, a árvore importada tem só o 1..3.
// Antes o arquivo ficava sem episódio (media_file pendurado na série, confiança
// de revisão, sem NFO e sem sinopse na biblioteca consumidora); agora vira
// episódio provisório na temporada certa, marcado como rascunho.
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
	if !strings.Contains(ext, `"season":1`) || !strings.Contains(ext, `"number":99`) {
		t.Errorf("extension_data sem a numeração do arquivo: %s", ext)
	}
	if !strings.Contains(ext, `"provisorio"`) {
		t.Errorf("extension_data sem a marca de provisório: %s", ext)
	}
	// O vínculo saiu da revisão: o media_file deixou de ter confiança 40.
	var conf float64
	if err := d.Read.QueryRowContext(context.Background(),
		"SELECT coalesce(match_confidence,0) FROM media_file").Scan(&conf); err != nil {
		t.Fatalf("ler match_confidence: %v", err)
	}
	if conf < confSerieEp {
		t.Errorf("match_confidence = %v, quero >= %d (casado pelo número do arquivo)", conf, confSerieEp)
	}

	// Idempotência: re-scan não cria um segundo provisório.
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

// TestScanSerieEpisodioProvisorioSemNumeroUsaPastaDaTemporada cobre o outro
// caso real (Du, Dudu e Edu/Season 00): o nome do arquivo não traz SxxEyy, mas
// a pasta diz a temporada. O provisório nasce sem número, com o título do
// arquivo, numa temporada que o scanner cria quando ela também não existe.
func TestScanSerieEpisodioProvisorioSemNumeroUsaPastaDaTemporada(t *testing.T) {
	d := abrirDB(t)
	root := t.TempDir()
	escrever(t, filepath.Join(root, "Springfield Tales", "Season 03", "Groovies.mkv"))

	m := managerSerieAutoImport(t, d)
	src := criarSourceSerie(t, d, root)
	if err := scanSync(t, m, src); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// A árvore importada traz 2 temporadas e 6 episódios; o provisório é o
	// sétimo episódio, na temporada 3 que o scanner teve de criar.
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
	// A temporada criada tem de ser a 3 (pasta "Season 03") e o provisório
	// pendurado nela.
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
