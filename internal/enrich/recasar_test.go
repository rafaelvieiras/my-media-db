package enrich

// recasar_test.go cobre o briefing 0069: arquivos que ja estavam anexados
// diretamente na entity da SERIE (media_file.entity_id = serieID), sem vinculo
// tv_episode -- o caso do scanner encontrando a serie ANTES dela ter arvore --
// ganham o vinculo certo assim que Enriquecer sincroniza a arvore (0067), SEM
// precisar de um novo scan.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// criarMediaFileSolto cria um media_file "preso" direto na serie (sem vinculo de
// episodio), como o scanner grava quando a serie ainda esta em revisao/sem arvore
// (upsertMediaFileSerie, internal/scanner/serie.go).
func criarMediaFileSolto(t *testing.T, database *db.DB, id, serieID, path string) {
	t.Helper()
	w := sqlcgen.New(database.Write)
	if _, err := w.CriarMediaFile(context.Background(), sqlcgen.CriarMediaFileParams{
		ID:              id,
		EntityID:        serieID,
		Path:            path,
		State:           "present",
		MatchConfidence: sql.NullFloat64{Float64: 40, Valid: true},
	}); err != nil {
		t.Fatalf("CriarMediaFile(%s): %v", id, err)
	}
}

// vinculosDoArquivo lista os vinculos media_file_entity (kind=tv_episode) de um
// media_file, na ordem do ordinal.
func vinculosDoArquivo(t *testing.T, database *db.DB, mediaFileID string) []sqlcgen.MediaFileEntity {
	t.Helper()
	vs, err := sqlcgen.New(database.Read).ListarVinculosPorArquivo(context.Background(), mediaFileID)
	if err != nil {
		t.Fatalf("ListarVinculosPorArquivo(%s): %v", mediaFileID, err)
	}
	var out []sqlcgen.MediaFileEntity
	for _, v := range vs {
		if v.Kind == relacaoEpisodio {
			out = append(out, v)
		}
	}
	return out
}

// idDoEpisodioPorTmdb devolve o id da entity episodio pelo external_id_tmdb (mesma
// fixture do fake usada em serie_test.go: T1 = 101/102/103).
func idDoEpisodioPorTmdb(t *testing.T, database *db.DB, tmdb string) string {
	t.Helper()
	ent, err := sqlcgen.New(database.Read).ObterEntityPorTmdb(context.Background(), nsValid(tmdb))
	if err != nil {
		t.Fatalf("ObterEntityPorTmdb(%s): %v", tmdb, err)
	}
	return ent.ID
}

// TestRecasarArquivosPendentesAoSincronizarArvore reproduz o cenario do §5 do 0069:
// uma serie draft com 2 arquivos (S01E01/S01E02) ja anexados a ela SEM vinculo de
// episodio (simula um scan que rodou ANTES da arvore existir) + Enriquecer (o mesmo
// caminho que Corrigir/auto-match usam, 0067) -- os dois arquivos devem ganhar o
// vinculo tv_episode certo, sem nenhum novo scan.
func TestRecasarArquivosPendentesAoSincronizarArvore(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	criarMediaFileSolto(t, database, "mf1", "s1", "/lib/Springfield Tales/Season 01/Springfield Tales S01E01.mkv")
	criarMediaFileSolto(t, database, "mf2", "s1", "/lib/Springfield Tales/Season 01/Springfield Tales S01E02.mkv")

	if err := svc.Enriquecer(ctx, "s1", "tmdb", fake.SerieFixtureID, tipoSerie, []string{"en"}); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	ep101 := idDoEpisodioPorTmdb(t, database, "101")
	ep102 := idDoEpisodioPorTmdb(t, database, "102")

	v1 := vinculosDoArquivo(t, database, "mf1")
	if len(v1) != 1 || v1[0].EntityID != ep101 {
		t.Fatalf("vinculos de mf1 = %+v, quero 1 vinculo apontando pro ep101 (%s)", v1, ep101)
	}
	v2 := vinculosDoArquivo(t, database, "mf2")
	if len(v2) != 1 || v2[0].EntityID != ep102 {
		t.Fatalf("vinculos de mf2 = %+v, quero 1 vinculo apontando pro ep102 (%s)", v2, ep102)
	}
}

// TestRecasarArquivosPendentesSemPadraoOuSemMatchFicaPendente cobre os dois ramos de
// "nao casa, sem forcar": um arquivo sem padrao de episodio reconhecivel e outro com
// numero que nao existe na arvore sincronizada -- os DOIS continuam sem vinculo.
func TestRecasarArquivosPendentesSemPadraoOuSemMatchFicaPendente(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	criarMediaFileSolto(t, database, "mf1", "s1", "/lib/Springfield Tales/featurette bonus.mkv")
	criarMediaFileSolto(t, database, "mf2", "s1", "/lib/Springfield Tales/Season 05/Springfield Tales S05E99.mkv")

	if err := svc.Enriquecer(ctx, "s1", "tmdb", fake.SerieFixtureID, tipoSerie, []string{"en"}); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	if v := vinculosDoArquivo(t, database, "mf1"); len(v) != 0 {
		t.Errorf("vinculos de mf1 (sem padrao) = %+v, quero 0", v)
	}
	if v := vinculosDoArquivo(t, database, "mf2"); len(v) != 0 {
		t.Errorf("vinculos de mf2 (S05E99 inexistente) = %+v, quero 0", v)
	}
}

// TestRecasarArquivosPendentesNaoReprocessaVinculoExistente: um arquivo que JA tinha
// vinculo tv_episode (de um scan anterior) nao e' reprocessado nem duplicado -- o
// vinculo (inclusive um ordinal "estranho", de proposito) permanece intocado.
func TestRecasarArquivosPendentesNaoReprocessaVinculoExistente(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	criarMediaFileSolto(t, database, "mf1", "s1", "/lib/Springfield Tales/Season 01/Springfield Tales S01E01.mkv")

	// O vinculo aponta pra uma entity SINTETICA (nao a arvore real, que so nasce dentro
	// do Enriquecer abaixo), com um ordinal que o recasamento normal jamais produziria
	// (7) -- exatamente pra provar que o vinculo PRE-EXISTENTE nao e mexido.
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "ep-preexistente", Type: tipoEpisodio, Title: "Ep pre-existente"}); err != nil {
		t.Fatalf("CriarEntity (ep pre-existente): %v", err)
	}
	if err := w.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "ep-preexistente", Kind: relacaoEpisodio, Ordinal: 7,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade (pre-existente): %v", err)
	}

	if err := svc.Enriquecer(ctx, "s1", "tmdb", fake.SerieFixtureID, tipoSerie, []string{"en"}); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	v := vinculosDoArquivo(t, database, "mf1")
	if len(v) != 1 {
		t.Fatalf("vinculos de mf1 = %+v, quero exatamente 1 (sem duplicar)", v)
	}
	if v[0].EntityID != "ep-preexistente" || v[0].Ordinal != 7 {
		t.Errorf("vinculo de mf1 = %+v, quero intocado (ep-preexistente, ordinal 7)", v[0])
	}
}

// TestRecasarArquivosPendentesFalhaDegradaSemPanic: uma falha ao LISTAR os arquivos
// pendentes (conexao de leitura fechada) nao pode derrubar o chamador -- a funcao e
// void e so loga, exatamente como o resto de aplicarArvoreSerie (0067).
func TestRecasarArquivosPendentesFalhaDegradaSemPanic(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	if err := database.Read.Close(); err != nil {
		t.Fatalf("fechar conexao de leitura: %v", err)
	}

	// Nao deve panicar; sem I/O de leitura disponivel, so degrada (loga e retorna).
	svc.recasarArquivosPendentes(ctx, "s1")
}
