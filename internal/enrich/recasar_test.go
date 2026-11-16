package enrich

// recasar_test.go covers the re-matching of pending files: files that were already attached
// directly to the SERIES entity (media_file.entity_id = serieID), without a
// tv_episode link -- the case of the scanner finding the series BEFORE it had a tree --
// get the right link as soon as Enriquecer syncs the tree, with NO
// need for a new scan.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// criarMediaFileSolto creates a media_file "stuck" directly on the series (with no episode
// link), the way the scanner writes it when the series is still under review/without a tree
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

// vinculosDoArquivo lists the media_file_entity links (kind=tv_episode) of a
// media_file, in ordinal order.
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

// idDoEpisodioPorTmdb returns the id of the episode entity by external_id_tmdb (the same
// fixture of the fake used in serie_test.go: S1 = 101/102/103).
func idDoEpisodioPorTmdb(t *testing.T, database *db.DB, tmdb string) string {
	t.Helper()
	ent, err := sqlcgen.New(database.Read).ObterEntityPorTmdb(context.Background(), nsValid(tmdb))
	if err != nil {
		t.Fatalf("ObterEntityPorTmdb(%s): %v", tmdb, err)
	}
	return ent.ID
}

// TestRecasarArquivosPendentesAoSincronizarArvore reproduces the §5 scenario:
// a draft series with 2 files (S01E01/S01E02) already attached to it WITHOUT an episode
// link (simulating a scan that ran BEFORE the tree existed) + Enriquecer (the same
// path that Corrigir/auto-match use) -- both files must get the right tv_episode
// link, without any new scan.
func TestRecasarArquivosPendentesAoSincronizarArvore(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	criarMediaFileSolto(t, database, "mf1", "s1", "/lib/Springfield Tales/Season 01/Springfield Tales S01E01.mkv")
	criarMediaFileSolto(t, database, "mf2", "s1", "/lib/Springfield Tales/Season 01/Springfield Tales S01E02.mkv")

	if err := svc.Enriquecer(ctx, "s1", "tmdb", fake.SerieFixtureID, tipoSerie, []string{"en"}, false); err != nil {
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

// TestRecasarArquivosPendentesSemPadraoOuSemMatchFicaPendente covers the two branches of
// "no match, no forcing": one file with no recognizable episode pattern and another with a
// number that does not exist in the synced tree -- BOTH stay without a link.
func TestRecasarArquivosPendentesSemPadraoOuSemMatchFicaPendente(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	criarMediaFileSolto(t, database, "mf1", "s1", "/lib/Springfield Tales/featurette bonus.mkv")
	criarMediaFileSolto(t, database, "mf2", "s1", "/lib/Springfield Tales/Season 05/Springfield Tales S05E99.mkv")

	if err := svc.Enriquecer(ctx, "s1", "tmdb", fake.SerieFixtureID, tipoSerie, []string{"en"}, false); err != nil {
		t.Fatalf("Enriquecer: %v", err)
	}

	if v := vinculosDoArquivo(t, database, "mf1"); len(v) != 0 {
		t.Errorf("vinculos de mf1 (sem padrao) = %+v, quero 0", v)
	}
	if v := vinculosDoArquivo(t, database, "mf2"); len(v) != 0 {
		t.Errorf("vinculos de mf2 (S05E99 inexistente) = %+v, quero 0", v)
	}
}

// TestRecasarArquivosPendentesNaoReprocessaVinculoExistente: a file that ALREADY had a
// tv_episode link (from a previous scan) is neither reprocessed nor duplicated -- the
// link (including a deliberately "odd" ordinal) remains untouched.
func TestRecasarArquivosPendentesNaoReprocessaVinculoExistente(t *testing.T) {
	svc, database := svcSerieFake(t)
	ctx := context.Background()

	w := sqlcgen.New(database.Write)
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "s1", Type: tipoSerie, Title: "Springfield Tales"}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}
	criarMediaFileSolto(t, database, "mf1", "s1", "/lib/Springfield Tales/Season 01/Springfield Tales S01E01.mkv")

	// The link points at a SYNTHETIC entity (not the real tree, which is only born inside
	// the Enriquecer below), with an ordinal that the normal re-matching would never produce
	// (7) -- exactly to prove that the PRE-EXISTING link is not touched.
	if _, err := w.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: "ep-preexistente", Type: tipoEpisodio, Title: "Ep pre-existente"}); err != nil {
		t.Fatalf("CriarEntity (ep pre-existente): %v", err)
	}
	if err := w.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "ep-preexistente", Kind: relacaoEpisodio, Ordinal: 7,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade (pre-existente): %v", err)
	}

	if err := svc.Enriquecer(ctx, "s1", "tmdb", fake.SerieFixtureID, tipoSerie, []string{"en"}, false); err != nil {
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

// TestRecasarArquivosPendentesFalhaDegradaSemPanic: a failure to LIST the pending files
// (closed read connection) cannot bring down the caller -- the function is
// void and only logs, exactly like the rest of aplicarArvoreSerie.
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

	// It must not panic; with no read I/O available, it only degrades (logs and returns).
	svc.recasarArquivosPendentes(ctx, "s1")
}
