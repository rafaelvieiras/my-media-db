package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// setupMidiasFaltantesServer starts a Server (keeping the reference, not only the HTTP
// handler) with a temporary assetsDir. internal/job/midiasfaltantes only talks to this code
// through the small Materializador interface (mocked in the job package test) -- the REAL
// implementation is only exercised by calling the *Server methods directly, the way the job
// does.
func setupMidiasFaltantesServer(t *testing.T) (*Server, http.Handler, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "midias-faltantes.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	srv := New("v-teste", database, WithAssetsDir(t.TempDir()))
	h, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return srv, h, database
}

// TestListarEntitiesComMediaFilePresente covers the eligible universe of the midias_faltantes
// job: only entities with a 'present' media_file show up, catalog-only entities (without a
// file) are left out.
func TestListarEntitiesComMediaFilePresente(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "comArquivo", "movie", "Com Arquivo")
	libDirComVideo(t, d, "comArquivo", "mf1", "Com.Arquivo.mkv")
	semearEntity(t, d, "semArquivo", "movie", "Sem Arquivo")

	ids, err := srv.ListarEntitiesComMediaFilePresente(context.Background())
	if err != nil {
		t.Fatalf("ListarEntitiesComMediaFilePresente: %v", err)
	}
	if len(ids) != 1 || ids[0] != "comArquivo" {
		t.Fatalf("ids = %v, quero [comArquivo] (só entities com media_file present)", ids)
	}
}

// TestMaterializarMidiasFaltantesBaixaPosterEGravaNFO covers the body of the midias_faltantes
// job: an entity with a present media_file and ONE poster already selected (local asset, not
// materialized yet) has the poster written into the real folder and the NFO written, in the
// SAME call -- exactly what the periodic job does for items already resolved but missing on
// disk.
func TestMaterializarMidiasFaltantesBaixaPosterEGravaNFO(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	// A poster already exists as an asset (e.g. selected from the gallery before), but still
	// only in the cache -- without a local_path.
	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	if a.LocalPath != nil {
		t.Fatalf("local_path recem-criado = %v, quero nil", a.LocalPath)
	}

	artes, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	if len(artes) != 1 || artes[0] != "poster" {
		t.Errorf("artesBaixadas = %v, quero [poster]", artes)
	}
	if !nfoGravado {
		t.Error("nfoGravado = false, quero true (entity sem nfo_path ainda)")
	}

	posterPath := filepath.Join(libDir, "poster.png")
	if _, err := os.Stat(posterPath); err != nil {
		t.Errorf("poster não materializado em disco (%s): %v", posterPath, err)
	}
	nfoPath := filepath.Join(libDir, "Matrix.1999.nfo")
	if _, err := os.Stat(nfoPath); err != nil {
		t.Errorf("nfo não gravado em disco (%s): %v", nfoPath, err)
	}

	// Idempotent: calling again does not report the same artwork as "downloaded" (it already
	// had a local_path) nor tries to rewrite the NFO (it already had nfo_path).
	artes2, nfoGravado2, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("2a chamada: %v", err)
	}
	if len(artes2) != 0 {
		t.Errorf("2a chamada artesBaixadas = %v, quero [] (poster já materializado)", artes2)
	}
	if nfoGravado2 {
		t.Error("2a chamada nfoGravado = true, quero false (nfo_path já preenchido)")
	}
}

// semearArvoreSerie builds a series with 2 seasons/3 episodes, files PRESENT in real
// subfolders "Season 01"/"Season 02" (the normal structure of any real library): mf1/mf2 in
// Season 01, mf3 in Season 02, all with media_file.entity_id=SERIES (the scanner convention)
// and linked to the episodes only through the media_file_entity join (kind=tv_episode). It
// returns the library root directory (libRoot).
func semearArvoreSerie(t *testing.T, d *db.DB) (libRoot string) {
	t.Helper()
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "se2", "season", "Temporada 2")
	semearEntity(t, d, "e1", "episode", "Episodio 1")
	semearEntity(t, d, "e2", "episode", "Episodio 2")
	semearEntity(t, d, "e3", "episode", "Episodio 3")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-se2", "s1", "se2", "tv_season", 2)
	ligarFilhoOrdenado(t, d, "r-e1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-e2", "se1", "e2", "tv_episode", 2)
	ligarFilhoOrdenado(t, d, "r-e3", "se2", "e3", "tv_episode", 1)

	libRoot = t.TempDir()
	season1 := filepath.Join(libRoot, "Season 01")
	season2 := filepath.Join(libRoot, "Season 02")
	semearFile(t, d, "mf1", "s1", filepath.Join(season1, "Show.S01E01.mkv"), "present", 0.9, "")
	semearFile(t, d, "mf2", "s1", filepath.Join(season1, "Show.S01E02.mkv"), "present", 0.9, "")
	semearFile(t, d, "mf3", "s1", filepath.Join(season2, "Show.S02E01.mkv"), "present", 0.9, "")
	for mfID, epID := range map[string]string{"mf1": "e1", "mf2": "e2", "mf3": "e3"} {
		if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: mfID, EntityID: epID, Kind: "tv_episode", Ordinal: 0,
		}); err != nil {
			t.Fatalf("CriarVinculoEntidade(%s->%s): %v", mfID, epID, err)
		}
	}
	return libRoot
}

// TestListarEntitiesComMediaFilePresenteArvoreDeSerie covers cause 1 of the fix: season
// (present descendant episode) and episode (media_file through the join) now show up in the
// eligible universe of the job -- before, only the series (media_file.entity_id direct)
// showed up.
func TestListarEntitiesComMediaFilePresenteArvoreDeSerie(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearArvoreSerie(t, d)

	ids, err := srv.ListarEntitiesComMediaFilePresente(context.Background())
	if err != nil {
		t.Fatalf("ListarEntitiesComMediaFilePresente: %v", err)
	}
	achados := map[string]bool{}
	for _, id := range ids {
		achados[id] = true
	}
	for _, quero := range []string{"s1", "se1", "se2", "e1", "e2", "e3"} {
		if !achados[quero] {
			t.Errorf("id %q ausente de ListarEntitiesComMediaFilePresente (%v)", quero, ids)
		}
	}
}

// TestMaterializarMidiasFaltantesArvoreDeSerieGravaNaRaiz covers cause 2 of the fix (wrong
// root): in a series with per-season subfolders, the SERIES poster materializes in the ROOT
// (libRoot), NEVER in "Season 01/"; the SEASON poster materializes as season01-poster.jpg
// ALSO in the root; the EPISODE NFO is written (that kind is now reached); an episode does
// NOT get an automatic still; a season does NOT get an NFO of its own (guard preserved).
func TestMaterializarMidiasFaltantesArvoreDeSerieGravaNaRaiz(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	libRoot := semearArvoreSerie(t, d)
	ctx := context.Background()

	// SERIES poster: selected as an asset (still only in the cache).
	postAsset(t, h, "s1", "poster", pngPayload(64))
	artesSerie, nfoSerie, err := srv.MaterializarMidiasFaltantes(ctx, "s1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(s1): %v", err)
	}
	if len(artesSerie) != 1 || artesSerie[0] != "poster" {
		t.Errorf("artesSerie = %v, quero [poster]", artesSerie)
	}
	if !nfoSerie {
		t.Error("nfoSerie = false, quero true (tvshow.nfo)")
	}
	posterSerie := filepath.Join(libRoot, "poster.png")
	if _, err := os.Stat(posterSerie); err != nil {
		t.Errorf("poster da serie nao esta na RAIZ (%s): %v", posterSerie, err)
	}
	if _, err := os.Stat(filepath.Join(libRoot, "Season 01", "poster.png")); err == nil {
		t.Error("poster da serie foi gravado DENTRO de Season 01 -- bug da raiz errada")
	}
	if _, err := os.Stat(filepath.Join(libRoot, "tvshow.nfo")); err != nil {
		t.Errorf("tvshow.nfo ausente na raiz: %v", err)
	}

	// SEASON poster (se1 = season 1): selected as an asset of the season itself.
	postAsset(t, h, "se1", "poster", pngPayload(64))
	artesSeason, nfoSeason, err := srv.MaterializarMidiasFaltantes(ctx, "se1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(se1): %v", err)
	}
	if len(artesSeason) != 1 || artesSeason[0] != "poster" {
		t.Errorf("artesSeason = %v, quero [poster]", artesSeason)
	}
	if nfoSeason {
		t.Error("nfoSeason = true, quero false (temporada nao tem NFO proprio)")
	}
	seasonPoster := filepath.Join(libRoot, "season01-poster.png")
	if _, err := os.Stat(seasonPoster); err != nil {
		t.Errorf("season01-poster.png ausente na raiz (%s): %v", seasonPoster, err)
	}
	if _, err := os.Stat(filepath.Join(libRoot, "tvshow.nfo.season")); err == nil {
		t.Error("nao deveria existir NFO de temporada")
	}

	// EPISODE NFO (e1): no still, but with the NFO written in the real episode folder
	// (Season 01, not in the root).
	artesEp, nfoEp, err := srv.MaterializarMidiasFaltantes(ctx, "e1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(e1): %v", err)
	}
	if len(artesEp) != 0 {
		t.Errorf("artesEp = %v, quero [] (still de episodio continua lazy/manual, 0044)", artesEp)
	}
	if !nfoEp {
		t.Error("nfoEp = false, quero true (episodio ganha NFO, causa 1 do 0081)")
	}
	nfoEpPath := filepath.Join(libRoot, "Season 01", "Show.S01E01.nfo")
	if _, err := os.Stat(nfoEpPath); err != nil {
		t.Errorf("nfo do episodio ausente em %s: %v", nfoEpPath, err)
	}
}

// TestMaterializarMidiasFaltantesSemCandidatoNaoFalha covers the "nothing resolved yet" case:
// an entity with a present media_file but NO asset at all -- there is no winning candidate of
// any kind, so no artwork is downloaded, but the NFO (which only depends on the metadata
// already resolved by the import, not on artwork) is still written, and the call returns no
// error (best-effort: it explicitly does NOT auto-select anything when there is no
// candidate).
func TestMaterializarMidiasFaltantesSemCandidatoNaoFalha(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Arte")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Sem.Arte.mkv")

	artes, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	if len(artes) != 0 {
		t.Errorf("artesBaixadas = %v, quero [] (nenhum candidato)", artes)
	}
	if !nfoGravado {
		t.Error("nfoGravado = false, quero true (NFO não depende de arte)")
	}
	if _, err := os.Stat(filepath.Join(libDir, "Sem.Arte.nfo")); err != nil {
		t.Errorf("nfo não gravado em disco: %v", err)
	}
}

// ── Self-heal of materialized artwork/NFO ────────────────────────────────

// TestMaterializarMidiasFaltantesArquivoSumidoRegravaMesmoPath covers the acceptance
// criterion of the self-heal: a poster already materialized (local_path filled in) whose
// physical file vanished from disk (remote mount, external deletion) -- running
// MaterializarMidiasFaltantes again rewrites the binary at the SAME local_path, without
// rotating, and reports the self-heal in artesBaixadas.
func TestMaterializarMidiasFaltantesArquivoSumidoRegravaMesmoPath(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	postAsset(t, h, "e1", "poster", pngPayload(64))
	artes, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	if len(artes) != 1 || artes[0] != "poster" {
		t.Fatalf("1a chamada artesBaixadas = %v, quero [poster]", artes)
	}
	posterPath := filepath.Join(libDir, "poster.png")
	if _, err := os.Stat(posterPath); err != nil {
		t.Fatalf("poster não materializado em %s: %v", posterPath, err)
	}

	// Simulates the file physically vanishing (remote mount, external deletion).
	if err := os.Remove(posterPath); err != nil {
		t.Fatalf("os.Remove (simular sumiço): %v", err)
	}

	artes2, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("2a chamada (self-heal): %v", err)
	}
	achouPoster := false
	for _, k := range artes2 {
		if k == "poster" {
			achouPoster = true
		}
	}
	if !achouPoster {
		t.Errorf("2a chamada artesBaixadas = %v, quero conter poster (autocura reportada)", artes2)
	}
	if _, err := os.Stat(posterPath); err != nil {
		t.Errorf("poster não regravado em disco após self-heal (%s): %v", posterPath, err)
	}
	if _, err := os.Stat(filepath.Join(libDir, "poster1.png")); err == nil {
		t.Error("arquivo rotacionado (poster1.png) encontrado -- self-heal deveria regravar no MESMO nome bare")
	}
}

// TestMaterializarMidiasFaltantesArquivoPresenteNaoRebaixaNaoRegrava covers the counterpart:
// when the materialized file is present on disk, the 2nd call does not report the artwork as
// "downloaded" again (the same idempotent behavior as before the self-heal -- the self-heal
// must not become an unconditional rewrite on every job run).
func TestMaterializarMidiasFaltantesArquivoPresenteNaoRebaixaNaoRegrava(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	postAsset(t, h, "e1", "poster", pngPayload(64))
	if _, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1"); err != nil {
		t.Fatalf("1a chamada: %v", err)
	}

	artes2, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("2a chamada: %v", err)
	}
	if len(artes2) != 0 {
		t.Errorf("2a chamada artesBaixadas = %v, quero [] (arquivo presente, nada a fazer)", artes2)
	}
}

// TestMaterializarMidiasFaltantesNfoSumidoRegravaDoZero covers the acceptance criterion of
// the self-heal for the NFO: entity.nfo_path filled in whose .nfo file vanished from disk --
// running MaterializarMidiasFaltantes again rewrites the .nfo from scratch (gravarNFO is
// already idempotent by content, without the rotation trap an asset has).
func TestMaterializarMidiasFaltantesNfoSumidoRegravaDoZero(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	if !nfoGravado {
		t.Fatal("1a chamada nfoGravado = false, quero true")
	}
	nfoPath := filepath.Join(libDir, "Matrix.1999.nfo")
	if _, err := os.Stat(nfoPath); err != nil {
		t.Fatalf("nfo não gravado em %s: %v", nfoPath, err)
	}

	if err := os.Remove(nfoPath); err != nil {
		t.Fatalf("os.Remove (simular sumiço do nfo): %v", err)
	}

	_, nfoGravado2, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("2a chamada (self-heal): %v", err)
	}
	if !nfoGravado2 {
		t.Error("2a chamada nfoGravado = false, quero true (self-heal: nfo sumiu, precisa regravar)")
	}
	if _, err := os.Stat(nfoPath); err != nil {
		t.Errorf("nfo não regravado após self-heal (%s): %v", nfoPath, err)
	}
}

// ── Convergence of the legacy naming (case B) ────────────────────────────

// semearAssetPrincipalEMoverParaLocalLegado materializes a poster (which becomes the principal
// of the kind) and then simulates the legacy naming (pre-fix): it moves the binary into
// "Season 01" with an episode name and updates local_path in the database -- the same real
// state observed in production on a Men in Black library entry. It returns the asset (already
// with the legacy local_path), the expected canonical path (root of libDir) and the legacy
// path.
func semearAssetPrincipalEMoverParaLocalLegado(t *testing.T, srv *Server, h http.Handler, d *db.DB, libDir string) (asset sqlcgen.Asset, canonico, legadoPath string) {
	t.Helper()
	postAsset(t, h, "e1", "poster", pngPayload(64))
	if _, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1"); err != nil {
		t.Fatalf("1a materializacao: %v", err)
	}
	assets := decodeAssetList(t, srv, "e1")
	if len(assets) != 1 || assets[0].IsPrincipal == 0 {
		t.Fatalf("asset nao materializado como principal: %+v", assets)
	}
	asset = assets[0]
	canonico = filepath.Join(libDir, "poster.png")
	if asset.LocalPath.String != canonico {
		t.Fatalf("local_path = %q, quero %q", asset.LocalPath.String, canonico)
	}

	legadoDir := filepath.Join(libDir, "Season 01")
	if err := os.MkdirAll(legadoDir, 0o755); err != nil {
		t.Fatalf("mkdir legado: %v", err)
	}
	legadoPath = filepath.Join(legadoDir, "Matrix.1999.S01E01-poster.png")
	if err := os.Rename(canonico, legadoPath); err != nil {
		t.Fatalf("mover para local legado: %v", err)
	}
	if err := escrita(t, d).AtualizarAssetLocalPath(context.Background(), sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: nsOf(legadoPath), ID: asset.ID,
	}); err != nil {
		t.Fatalf("AtualizarAssetLocalPath (simular legado): %v", err)
	}
	asset.LocalPath = nsOf(legadoPath)
	return asset, canonico, legadoPath
}

// TestMaterializarMidiasFaltantesAssetLegadoPrincipalConvergeParaNomeCanonico covers the
// acceptance criterion of the naming convergence: an ALREADY PRINCIPAL asset whose local_path
// points at an EPISODE name/folder (legacy naming, written before DiretorioDestino was fixed)
// -- running MaterializarMidiasFaltantes again RENAMES (does not recopy) the file back to the
// canonical bare name in the root, and persists the new local_path.
func TestMaterializarMidiasFaltantesAssetLegadoPrincipalConvergeParaNomeCanonico(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")
	_, canonico, legadoPath := semearAssetPrincipalEMoverParaLocalLegado(t, srv, h, d, libDir)

	artes, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("2a materializacao (convergencia): %v", err)
	}
	achouPoster := false
	for _, k := range artes {
		if k == "poster" {
			achouPoster = true
		}
	}
	if !achouPoster {
		t.Errorf("artesBaixadas = %v, quero conter poster (convergencia reportada)", artes)
	}
	if _, err := os.Stat(canonico); err != nil {
		t.Errorf("poster nao convergiu de volta pro nome canonico (%s): %v", canonico, err)
	}
	if _, err := os.Stat(legadoPath); err == nil {
		t.Error("arquivo legado ainda existe apos convergencia -- deveria ter sido RENOMEADO (movido), nao recopiado")
	}
	assetsDepois := decodeAssetList(t, srv, "e1")
	if len(assetsDepois) != 1 || assetsDepois[0].LocalPath.String != canonico {
		t.Errorf("local_path no banco = %+v, quero %q", assetsDepois, canonico)
	}
}

// TestMaterializarMidiasFaltantesAssetLegadoNaoPrincipalNaoConverge covers the safety
// counterpart: a local asset under the legacy naming that is NOT is_principal (an edge case --
// no asset of the kind is principal) does NOT get its name recomputed -- it only preserves the
// existing self-heal behavior (rewrite in place if the file vanished). Recomputing the bare
// name for a non-principal one would collide with the name the real principal should
// occupy.
func TestMaterializarMidiasFaltantesAssetLegadoNaoPrincipalNaoConverge(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")
	_, _, legadoPath := semearAssetPrincipalEMoverParaLocalLegado(t, srv, h, d, libDir)

	if err := escrita(t, d).DesmarcarAssetsPrincipalPorKind(context.Background(), sqlcgen.DesmarcarAssetsPrincipalPorKindParams{
		EntityID: "e1", Kind: "poster",
	}); err != nil {
		t.Fatalf("DesmarcarAssetsPrincipalPorKind: %v", err)
	}

	if _, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1"); err != nil {
		t.Fatalf("2a materializacao: %v", err)
	}

	if _, err := os.Stat(legadoPath); err != nil {
		t.Errorf("arquivo legado deveria continuar no lugar (asset nao-principal nao converge): %v", err)
	}
	assetsDepois := decodeAssetList(t, srv, "e1")
	if assetsDepois[0].LocalPath.String != legadoPath {
		t.Errorf("local_path mudou = %q, quero continuar %q (nao-principal nao converge)", assetsDepois[0].LocalPath.String, legadoPath)
	}
}

// semearNfoNaRaizEMoverParaLocalLegado writes tvshow.nfo correctly in the root (post-fix) and
// then simulates the legacy naming (pre-fix): it moves the .nfo into "Season 01" and updates
// entity.nfo_path to there -- the same real state observed in production on a Men in Black
// library entry. It returns the expected canonical path (root) and the legacy path.
func semearNfoNaRaizEMoverParaLocalLegado(t *testing.T, srv *Server, d *db.DB, libRoot string) (nfoCanonico, legadoPath string) {
	t.Helper()
	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "s1")
	if err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	if !nfoGravado {
		t.Fatal("1a chamada nfoGravado = false, quero true")
	}
	nfoCanonico = filepath.Join(libRoot, "tvshow.nfo")
	if _, err := os.Stat(nfoCanonico); err != nil {
		t.Fatalf("tvshow.nfo ausente na raiz: %v", err)
	}

	legadoDir := filepath.Join(libRoot, "Season 01")
	if err := os.MkdirAll(legadoDir, 0o755); err != nil {
		t.Fatalf("mkdir legado: %v", err)
	}
	legadoPath = filepath.Join(legadoDir, "tvshow.nfo")
	if err := os.Rename(nfoCanonico, legadoPath); err != nil {
		t.Fatalf("mover nfo para local legado: %v", err)
	}
	if err := escrita(t, d).AtualizarEntityNfo(context.Background(), sqlcgen.AtualizarEntityNfoParams{
		NfoPath: nsOf(legadoPath), NfoWrittenAt: nsOf("2026-07-23T00:00:00Z"), ID: "s1",
	}); err != nil {
		t.Fatalf("AtualizarEntityNfo (simular legado): %v", err)
	}
	return nfoCanonico, legadoPath
}

// TestMaterializarMidiasFaltantesNfoLegadoConvergeERemoveOrfao covers the acceptance criterion
// of the naming convergence for the NFO: a legacy entity.nfo_path (written before
// DiretorioDestino was fixed, pointing inside "Season 01") -- running
// MaterializarMidiasFaltantes again rewrites the .nfo in the correct root and removes the old
// orphan file.
func TestMaterializarMidiasFaltantesNfoLegadoConvergeERemoveOrfao(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	libRoot := semearArvoreSerie(t, d)
	nfoCanonico, legadoPath := semearNfoNaRaizEMoverParaLocalLegado(t, srv, d, libRoot)

	_, nfoGravado2, err := srv.MaterializarMidiasFaltantes(context.Background(), "s1")
	if err != nil {
		t.Fatalf("2a chamada (convergencia): %v", err)
	}
	if !nfoGravado2 {
		t.Error("2a chamada nfoGravado = false, quero true (convergencia reportada)")
	}
	if _, err := os.Stat(nfoCanonico); err != nil {
		t.Errorf("tvshow.nfo nao convergiu de volta pra raiz (%s): %v", nfoCanonico, err)
	}
	if _, err := os.Stat(legadoPath); err == nil {
		t.Error("nfo legado orfao ainda existe apos convergencia -- deveria ter sido removido")
	}
}

// ── Music: album/artist enter the job universe and get a "poster" ────────

// semearArvoreMusica builds artist->album->track (entity_relation) with ONE present
// media_file on the track (the only way music links a media_file, ADR-0027) and returns the
// ids (artistaID, albumID).
// semearArvoreMusica creates artist -> album -> track inside a REAL temporary folder and
// returns the ids plus the album folder (the artist folder is its parent). The folder has to
// exist on disk because the destination of a music entity is derived from the track path and
// the job now refuses to download when there is nowhere to write (issue #79).
func semearArvoreMusica(t *testing.T, d *db.DB) (artistaID, albumID, albumDir string) {
	t.Helper()
	albumDir = filepath.Join(t.TempDir(), "Artists", "Duran Duran", "Rio (1982)")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", albumDir, err)
	}
	semearEntity(t, d, "art1", "artista", "Duran Duran")
	semearEntity(t, d, "alb1", "album", "Rio")
	semearEntity(t, d, "fx1", "faixa", "Hungry Like the Wolf")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", filepath.Join(albumDir, "01 - Hungry Like the Wolf.flac"), "present", 1, "")
	return "art1", "alb1", albumDir
}

// TestListarEntitiesComMediaFilePresenteMusica covers the extension of the eligible universe
// of the midias_faltantes job to music: artist/album WITHOUT a DIRECT media_file, but with an
// owned descendant track, now show up -- before, only the direct media_file.entity_id (which
// music never has on artist/album) was considered.
func TestListarEntitiesComMediaFilePresenteMusica(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, albumID, _ := semearArvoreMusica(t, d)
	semearEntity(t, d, "art2", "artista", "Sem Posse")

	ids, err := srv.ListarEntitiesComMediaFilePresente(context.Background())
	if err != nil {
		t.Fatalf("ListarEntitiesComMediaFilePresente: %v", err)
	}
	achados := map[string]bool{}
	for _, id := range ids {
		achados[id] = true
	}
	if !achados[artistaID] {
		t.Errorf("artista %q ausente de ListarEntitiesComMediaFilePresente (%v)", artistaID, ids)
	}
	if !achados[albumID] {
		t.Errorf("album %q ausente de ListarEntitiesComMediaFilePresente (%v)", albumID, ids)
	}
	if achados["art2"] {
		t.Errorf("art2 (sem faixa possuida nenhuma) presente em ListarEntitiesComMediaFilePresente (%v)", ids)
	}
}

// semearInventarioArte writes the artwork inventory envelope (ChaveEnvelopeInventarioArtes)
// into media_external, in the format read by montarGruposArtwork -- the same shape used by the
// real enrich (fanart.tv/musicbrainz).
func semearInventarioArte(t *testing.T, d *db.DB, meID, entityID, source, kind, url string) {
	t.Helper()
	inv, err := json.Marshal(map[string]any{
		provider.ChaveEnvelopeInventarioArtes: map[string]any{
			kind: []provider.ArteInventario{{URL: url}},
		},
	})
	if err != nil {
		t.Fatalf("marshal inventario: %v", err)
	}
	semearMediaExternal(t, d, meID, entityID, source, string(inv))
}

// TestMaterializarMidiasFaltantesAlbumBaixaCapaDoMusicbrainz covers the ALBUM case:
// fanart.tv/Cover Art Archive already writes the cover under kind='poster' (the aggregator's
// own vocabulary), so the GENERIC loop of kindsMidiasFaltantes already materializes it with no
// special case -- it only needed the album to enter the eligible universe (test above) and the
// destination helper to treat the absence of a real folder gracefully
// (materializarAssetOuCache).
func TestMaterializarMidiasFaltantesAlbumBaixaCapaDoMusicbrainz(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	_, albumID, albumDir := semearArvoreMusica(t, d)
	imgSrv := servidorImagem(t, nil)
	semearInventarioArte(t, d, "me-mb", albumID, "musicbrainz", "poster", imgSrv.URL+"/img")

	artes, _, err := srv.MaterializarMidiasFaltantes(context.Background(), albumID)
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(%s): %v", albumID, err)
	}
	achouPoster := false
	for _, k := range artes {
		if k == "poster" {
			achouPoster = true
		}
	}
	if !achouPoster {
		t.Fatalf("artesBaixadas = %v, quero conter poster", artes)
	}

	assets := decodeAssetList(t, srv, albumID)
	if len(assets) != 1 || assets[0].Kind != "poster" {
		t.Errorf("assets do album = %+v, quero 1 asset kind=poster", assets)
	}
	// issue #79: the cover has to LAND in the album folder, as folder.<ext> -- before, it was
	// downloaded every run and written nowhere (local_path stayed empty).
	quero := filepath.Join(albumDir, "folder.png")
	if !assets[0].LocalPath.Valid || assets[0].LocalPath.String != quero {
		t.Errorf("local_path = %v, quero %q", assets[0].LocalPath, quero)
	}
	if _, err := os.Stat(quero); err != nil {
		t.Errorf("arquivo fisico ausente em %s: %v", quero, err)
	}
	if assets[0].IsPrincipal != 1 {
		t.Errorf("is_principal = %d, quero 1 (primeira arte do kind)", assets[0].IsPrincipal)
	}
}

// TestMaterializarMidiasFaltantesArtistaBaixaPosterViaThumb covers the ARTIST case: fanart.tv
// does not model a dedicated artist poster kind (only thumb/fanart/logo/banner, kindsArtista in
// internal/provider/fanarttv) -- materializarPosterDeArtista uses the winner of the `thumb`
// kind as the poster, otherwise the artist card would NEVER have has_poster=true.
func TestMaterializarMidiasFaltantesArtistaBaixaPosterViaThumb(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, _, albumDir := semearArvoreMusica(t, d)
	imgSrv := servidorImagem(t, nil)
	semearInventarioArte(t, d, "me-fa", artistaID, "fanarttv", "thumb", imgSrv.URL+"/img")

	artes, _, err := srv.MaterializarMidiasFaltantes(context.Background(), artistaID)
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(%s): %v", artistaID, err)
	}
	achouPoster := false
	for _, k := range artes {
		if k == "poster" {
			achouPoster = true
		}
	}
	if !achouPoster {
		t.Fatalf("artesBaixadas = %v, quero conter poster (via alias de thumb)", artes)
	}

	assets := decodeAssetList(t, srv, artistaID)
	temPoster := false
	for _, a := range assets {
		if a.Kind == "poster" {
			temPoster = true
		}
		if a.Kind == "thumb" && a.LocalPath.Valid {
			t.Errorf("thumb materializado em %q -- o artista só recebe o poster (o thumb É a mesma imagem)", a.LocalPath.String)
		}
	}
	if !temPoster {
		t.Errorf("assets do artista = %+v, quero um asset kind=poster (derivado do thumb)", assets)
	}
	// issue #79: the artist picture lands in the ARTIST folder (one level above the album),
	// as folder.<ext> -- the name the music library already uses.
	quero := filepath.Join(filepath.Dir(albumDir), "folder.png")
	if _, err := os.Stat(quero); err != nil {
		t.Errorf("arquivo fisico ausente em %s: %v", quero, err)
	}
}

// TestMaterializarPosterDeArtistaIdempotente covers that a 2nd call does not re-download nor
// duplicate when the artist already has a poster (even if only in the cache, without a real
// folder).
func TestMaterializarPosterDeArtistaIdempotente(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, _, _ := semearArvoreMusica(t, d)
	imgSrv := servidorImagem(t, nil)
	semearInventarioArte(t, d, "me-fa", artistaID, "fanarttv", "thumb", imgSrv.URL+"/img")

	if _, _, err := srv.MaterializarMidiasFaltantes(context.Background(), artistaID); err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	antes := decodeAssetList(t, srv, artistaID)

	artes2, _, err := srv.MaterializarMidiasFaltantes(context.Background(), artistaID)
	if err != nil {
		t.Fatalf("2a chamada: %v", err)
	}
	for _, k := range artes2 {
		if k == "poster" {
			t.Errorf("2a chamada artesBaixadas = %v, nao deveria rebaixar poster (ja existe)", artes2)
		}
	}
	depois := decodeAssetList(t, srv, artistaID)
	if len(depois) != len(antes) {
		t.Errorf("assets do artista mudaram de %d para %d entre chamadas -- duplicou", len(antes), len(depois))
	}
}

// TestMaterializarPosterDeArtistaSemInventarioNaoFazNada covers the "nothing to do" case: an
// eligible artist (owned track) but WITHOUT any cached artwork inventory -- no thumb candidate,
// so no poster is created, with no error.
func TestMaterializarPosterDeArtistaSemInventarioNaoFazNada(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, _, _ := semearArvoreMusica(t, d)

	artes, _, err := srv.MaterializarMidiasFaltantes(context.Background(), artistaID)
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	for _, k := range artes {
		if k == "poster" {
			t.Errorf("artesBaixadas = %v, nao deveria conter poster (sem inventario nenhum)", artes)
		}
	}
	if assets := decodeAssetList(t, srv, artistaID); len(assets) != 0 {
		t.Errorf("assets do artista = %+v, quero nenhum", assets)
	}
}

// TestMaterializarPosterDeArtistaFalhaDownloadNaoPropaga covers that a DOWNLOAD failure (the
// content is not an image) degrades silently (best-effort, the same spirit as
// materializarKindVencedor) instead of propagating an error -- with no asset created.
func TestMaterializarPosterDeArtistaFalhaDownloadNaoPropaga(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, _, _ := semearArvoreMusica(t, d)
	imgSrv := servidorImagem(t, nil)
	semearInventarioArte(t, d, "me-fa", artistaID, "fanarttv", "thumb", imgSrv.URL+"/texto")

	artes, _, err := srv.MaterializarMidiasFaltantes(context.Background(), artistaID)
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	for _, k := range artes {
		if k == "poster" {
			t.Errorf("artesBaixadas = %v, nao deveria conter poster (download falhou -- nao e imagem)", artes)
		}
	}
	if assets := decodeAssetList(t, srv, artistaID); len(assets) != 0 {
		t.Errorf("assets do artista = %+v, quero nenhum (download falhou)", assets)
	}
}

// decodeAssetList lists the entity assets straight from the database (simpler than decoding
// the by-kind HTTP response of /artwork).
func decodeAssetList(t *testing.T, srv *Server, entityID string) []sqlcgen.Asset {
	t.Helper()
	assets, err := srv.leitura().ListarAssetsPorEntity(context.Background(), entityID)
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity(%s): %v", entityID, err)
	}
	return assets
}

// ── MaterializarSeAutoDownloadLigado (adapter of scanner.Materializador) ──

// TestMaterializarSeAutoDownloadLigadoToggleDesligadoNaoMaterializaNada covers the criterion:
// with the toggle off (factory default), the call is a no-op -- no artwork is materialized, no
// error.
func TestMaterializarSeAutoDownloadLigadoToggleDesligadoNaoMaterializaNada(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")
	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	if a.LocalPath != nil {
		t.Fatalf("local_path recem-criado = %v, quero nil", a.LocalPath)
	}

	if err := srv.MaterializarSeAutoDownloadLigado(context.Background(), "e1"); err != nil {
		t.Fatalf("MaterializarSeAutoDownloadLigado (toggle desligado): %v", err)
	}

	posterPath := filepath.Join(libDir, "poster.png")
	if _, err := os.Stat(posterPath); err == nil {
		t.Errorf("poster materializado em disco (%s) com o toggle desligado -- quero nada", posterPath)
	}
}

// TestMaterializarSeAutoDownloadLigadoToggleLigadoMaterializa covers the criterion: with the
// toggle on, the call really materializes the winning artwork into the real folder, reusing
// MaterializarMidiasFaltantes.
func TestMaterializarSeAutoDownloadLigadoToggleLigadoMaterializa(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")
	a := decodeAsset(t, postAsset(t, h, "e1", "poster", pngPayload(64)))
	if a.LocalPath != nil {
		t.Fatalf("local_path recem-criado = %v, quero nil", a.LocalPath)
	}

	rec := do(t, h, http.MethodPut, "/api/v1/settings/auto-materialize-artwork",
		autoMaterializeArtworkSettings{Enabled: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT toggle status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	if err := srv.MaterializarSeAutoDownloadLigado(context.Background(), "e1"); err != nil {
		t.Fatalf("MaterializarSeAutoDownloadLigado (toggle ligado): %v", err)
	}

	posterPath := filepath.Join(libDir, "poster.png")
	if _, err := os.Stat(posterPath); err != nil {
		t.Errorf("poster não materializado em disco (%s) com o toggle ligado: %v", posterPath, err)
	}
}

// ── Re-convergence by NFO content ────────────────────────────────────────

// lerNFO returns the content of the .nfo at the entity's canonical destination.
func lerNFO(t *testing.T, libDir, nomeBase string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(libDir, nomeBase+".nfo"))
	if err != nil {
		t.Fatalf("ler nfo: %v", err)
	}
	return string(b)
}

// TestMaterializarMidiasFaltantesNfoVazioRegrava covers the extreme case: the .nfo exists at
// the right destination, but is empty (0 bytes). The job used to look only at path+presence and
// never touch the file again, so an empty .nfo stayed empty forever -- the consuming library
// read an episode with nothing inside.
func TestMaterializarMidiasFaltantesNfoVazioRegrava(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	if _, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1"); err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	nfoPath := filepath.Join(libDir, "Matrix.1999.nfo")
	if err := os.WriteFile(nfoPath, nil, 0o644); err != nil {
		t.Fatalf("truncar nfo: %v", err)
	}

	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("2a chamada: %v", err)
	}
	if !nfoGravado {
		t.Error("2a chamada nfoGravado = false, quero true (nfo vazio precisa ser regravado)")
	}
	if corpo := lerNFO(t, libDir, "Matrix.1999"); len(corpo) == 0 {
		t.Error("nfo continua vazio depois da 2a chamada")
	}
}

// TestMaterializarMidiasFaltantesNfoDesatualizadoRegrava covers the normal case: the database
// gained a synopsis AFTER the first write. The .nfo on disk has to be rewritten with the new
// data, even with nfo_path filled in and the file present.
func TestMaterializarMidiasFaltantesNfoDesatualizadoRegrava(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	if _, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1"); err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	if corpo := lerNFO(t, libDir, "Matrix.1999"); strings.Contains(corpo, "sinopse nova") {
		t.Fatal("nfo já tinha a sinopse nova antes de o banco mudar (teste inválido)")
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if _, err := escrita(t, d).AtualizarEntity(context.Background(), sqlcgen.AtualizarEntityParams{
		Type:              ent.Type,
		Title:             ent.Title,
		OriginalTitle:     ent.OriginalTitle,
		Description:       nsOf("Uma sinopse nova, escrita depois da primeira gravação."),
		ReleaseDate:       ent.ReleaseDate,
		Status:            ent.Status,
		TagsJson:          ent.TagsJson,
		ExternalIdsJson:   ent.ExternalIdsJson,
		ExtensionDataJson: ent.ExtensionDataJson,
		ID:                ent.ID,
	}); err != nil {
		t.Fatalf("AtualizarEntity: %v", err)
	}

	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("2a chamada: %v", err)
	}
	if !nfoGravado {
		t.Error("2a chamada nfoGravado = false, quero true (banco mudou, nfo precisa acompanhar)")
	}
	if corpo := lerNFO(t, libDir, "Matrix.1999"); !strings.Contains(corpo, "sinopse nova") {
		t.Errorf("nfo não foi reescrito com a sinopse nova; conteúdo: %q", corpo)
	}
}

// TestMaterializarMidiasFaltantesNfoIgualNaoRegrava is the opposite side: database and disk
// up to date, nothing to do. The file must not be touched (not even its mtime), otherwise the
// job becomes write churn on the remote mount and the mtime stops signaling a real change.
func TestMaterializarMidiasFaltantesNfoIgualNaoRegrava(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	if _, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1"); err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	nfoPath := filepath.Join(libDir, "Matrix.1999.nfo")
	antes, err := os.Stat(nfoPath)
	if err != nil {
		t.Fatalf("stat nfo: %v", err)
	}
	corpoAntes := lerNFO(t, libDir, "Matrix.1999")

	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("2a chamada: %v", err)
	}
	if nfoGravado {
		t.Error("2a chamada nfoGravado = true, quero false (conteúdo já convergente)")
	}
	depois, err := os.Stat(nfoPath)
	if err != nil {
		t.Fatalf("stat nfo (2): %v", err)
	}
	if !depois.ModTime().Equal(antes.ModTime()) {
		t.Errorf("mtime do nfo mudou sem conteúdo novo (%v -> %v)", antes.ModTime(), depois.ModTime())
	}
	if corpo := lerNFO(t, libDir, "Matrix.1999"); corpo != corpoAntes {
		t.Error("conteúdo do nfo mudou sem o banco mudar")
	}
}

// TestMaterializarMidiasFaltantesMusicaSemPastaNaoBaixa covers issue #79 from the other side:
// when the destination folder does not exist on disk (the remote mount is offline, the album
// folder was renamed outside the app), the job must NOT download the artwork -- it used to
// fetch it, write nothing, and report the kind as materialized, every single run.
func TestMaterializarMidiasFaltantesMusicaSemPastaNaoBaixa(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	// the seeded track points inside a folder that is created and then removed: the database
	// still says "present", the disk does not have it.
	_, albumID, albumDir := semearArvoreMusica(t, d)
	if err := os.RemoveAll(albumDir); err != nil {
		t.Fatalf("RemoveAll(%s): %v", albumDir, err)
	}
	hits := 0
	imgSrv := servidorImagem(t, &hits)
	semearInventarioArte(t, d, "me-mb", albumID, "musicbrainz", "poster", imgSrv.URL+"/img")

	artes, nfo, err := srv.MaterializarMidiasFaltantes(context.Background(), albumID)
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(%s): %v", albumID, err)
	}
	if len(artes) != 0 {
		t.Errorf("artesBaixadas = %v, quero nenhuma (sem pasta de destino)", artes)
	}
	if nfo {
		t.Error("nfoGravado = true, quero false (musica nao tem NFO)")
	}
	if hits != 0 {
		t.Errorf("downloads = %d, quero 0 (nao baixar sem destino)", hits)
	}
	if assets := decodeAssetList(t, srv, albumID); len(assets) != 0 {
		t.Errorf("assets do album = %+v, quero nenhum", assets)
	}
}

// TestMaterializarMidiasFaltantesArtistaNaRaizNaoBaixa covers the guard of pastaDeMusica: when
// the tracks are NOT inside an album subfolder (the album folder IS the library root), going up
// one level to find the artist folder would drop the picture in the root of the library -- worse
// than not writing it, so the job skips the artist.
func TestMaterializarMidiasFaltantesArtistaNaRaizNaoBaixa(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	raiz := t.TempDir()
	semearSource(t, d, "srcMusica", raiz)
	semearEntity(t, d, "artRaiz", "artista", "Sem Album")
	semearEntity(t, d, "albRaiz", "album", "Solto")
	semearEntity(t, d, "fxRaiz", "faixa", "Faixa Solta")
	ligarFilhoOrdenado(t, d, "r1", "artRaiz", "albRaiz", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "albRaiz", "fxRaiz", "music_album", 1)
	semearFile(t, d, "f1", "fxRaiz", filepath.Join(raiz, "01 - Faixa Solta.flac"), "present", 1, "")

	// the ALBUM still has a destination: its folder IS the root, and a cover there is the
	// cover of that album (nothing is overwritten, the name does not collide).
	imgSrv := servidorImagem(t, nil)
	semearInventarioArte(t, d, "me-fa", "artRaiz", "fanarttv", "thumb", imgSrv.URL+"/img")

	artes, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "artRaiz")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	if len(artes) != 0 {
		t.Errorf("artesBaixadas = %v, quero nenhuma (nao escrever na raiz da biblioteca)", artes)
	}
	if _, err := os.Stat(filepath.Join(raiz, "folder.png")); !os.IsNotExist(err) {
		t.Errorf("folder.png existe na raiz da biblioteca (stat err=%v), quero ausente", err)
	}
}

// TestMaterializarMidiasFaltantesAlbumConvergeNomeParaFolder covers the legacy case: an album
// whose cover is already materialized in the album folder under the VIDEO name (poster.jpg, the
// name an older version or another manager could have written). The job converges it on the
// music name -- folder.jpg -- renaming the same bytes, so the folder ends up with ONE name per
// artwork (ADR-0043).
func TestMaterializarMidiasFaltantesAlbumConvergeNomeParaFolder(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	_, albumID, albumDir := semearArvoreMusica(t, d)
	legado := filepath.Join(albumDir, "poster.jpg")
	if err := os.WriteFile(legado, []byte("jpeg-fake"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", legado, err)
	}
	ctx := context.Background()
	esc := escrita(t, d)
	if _, err := esc.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID: "aLegado", EntityID: albumID, Kind: "poster", Origin: "local", PathOrUri: legado,
	}); err != nil {
		t.Fatalf("CriarAsset: %v", err)
	}
	if err := esc.AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
		LocalPath: nsOf(legado), ID: "aLegado",
	}); err != nil {
		t.Fatalf("AtualizarAssetLocalPath: %v", err)
	}
	if err := esc.MarcarAssetPrincipal(ctx, "aLegado"); err != nil {
		t.Fatalf("MarcarAssetPrincipal: %v", err)
	}

	artes, _, err := srv.MaterializarMidiasFaltantes(ctx, albumID)
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	canonico := filepath.Join(albumDir, "folder.jpg")
	if _, err := os.Stat(canonico); err != nil {
		t.Errorf("arquivo ausente em %s: %v", canonico, err)
	}
	if _, err := os.Stat(legado); !os.IsNotExist(err) {
		t.Errorf("poster.jpg ainda existe (stat err=%v), quero renomeado para folder.jpg", err)
	}
	assets := decodeAssetList(t, srv, albumID)
	if len(assets) != 1 || !assets[0].LocalPath.Valid || assets[0].LocalPath.String != canonico {
		t.Errorf("assets = %+v, quero 1 com local_path %q", assets, canonico)
	}
	achou := false
	for _, k := range artes {
		if k == "poster" {
			achou = true
		}
	}
	if !achou {
		t.Errorf("artesBaixadas = %v, quero conter poster (a convergencia de nome e trabalho)", artes)
	}
}

// TestMaterializarMidiasFaltantesAlbumMultiDiscoGravaNoAlbum covers a multi-disc release: the
// tracks live in "CD 1"/"Digital Media 01" subfolders, but the cover belongs to the ALBUM --
// it lands on the album folder, not duplicated inside each disc folder.
func TestMaterializarMidiasFaltantesAlbumMultiDiscoGravaNoAlbum(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	albumDir := filepath.Join(t.TempDir(), "Artists", "Duran Duran", "Rio (1982)")
	discoDir := filepath.Join(albumDir, "Digital Media 01")
	if err := os.MkdirAll(discoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	semearEntity(t, d, "art1", "artista", "Duran Duran")
	semearEntity(t, d, "alb1", "album", "Rio")
	semearEntity(t, d, "fx1", "faixa", "Hungry Like the Wolf")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", filepath.Join(discoDir, "01 - Hungry Like the Wolf.flac"), "present", 1, "")
	imgSrv := servidorImagem(t, nil)
	semearInventarioArte(t, d, "me-mb", "alb1", "musicbrainz", "poster", imgSrv.URL+"/img")

	if _, _, err := srv.MaterializarMidiasFaltantes(context.Background(), "alb1"); err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(albumDir, "folder.png")); err != nil {
		t.Errorf("capa do album ausente em %s: %v", albumDir, err)
	}
	if _, err := os.Stat(filepath.Join(discoDir, "folder.png")); err == nil {
		t.Error("capa gravada DENTRO da pasta do disco -- deve ficar no album")
	}
}

// semearArvoreSerieMultiEpisodio seeds a series whose TWO episodes live in ONE video file
// (Show.S01E01E02.mkv, the multi-episode convention of Kodi/Jellyfin), returning the library
// root and the path of the shared video.
func semearArvoreSerieMultiEpisodio(t *testing.T, d *db.DB) (libRoot, video string) {
	t.Helper()
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	semearEntity(t, d, "e1", "episode", "Primeiro Episodio")
	semearEntity(t, d, "e2", "episode", "Segundo Episodio")
	ligarFilhoOrdenado(t, d, "r-se1", "s1", "se1", "tv_season", 1)
	ligarFilhoOrdenado(t, d, "r-e1", "se1", "e1", "tv_episode", 1)
	ligarFilhoOrdenado(t, d, "r-e2", "se1", "e2", "tv_episode", 2)

	libRoot = t.TempDir()
	video = filepath.Join(libRoot, "Season 01", "Show.S01E01E02.mkv")
	semearFile(t, d, "mf1", "s1", video, "present", 0.9, "")
	for ordinal, epID := range map[int64]string{0: "e1", 1: "e2"} {
		if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: "mf1", EntityID: epID, Kind: "tv_episode", Ordinal: ordinal,
		}); err != nil {
			t.Fatalf("CriarVinculoEntidade(%s): %v", epID, err)
		}
	}
	return libRoot, video
}

// TestMaterializarMidiasFaltantesUmNFOPorArquivo covers issue #80: two episodes in ONE video
// file share ONE .nfo, whose document carries one <episodedetails> block per episode -- before,
// each entity wrote its own single-block document over the other one and the file ended up with
// a mix (one episode's title, another one's number), rewritten on every run.
func TestMaterializarMidiasFaltantesUmNFOPorArquivo(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	_, video := semearArvoreSerieMultiEpisodio(t, d)
	nfoPath := localmedia.CaminhoNFODeVideo(video)

	artes, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(e1): %v", err)
	}
	if len(artes) != 0 {
		t.Errorf("artes = %v, quero nenhuma", artes)
	}
	if !nfoGravado {
		t.Fatal("nfoGravado = false, quero true (primeira gravacao)")
	}
	corpo, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("ler %s: %v", nfoPath, err)
	}
	texto := string(corpo)
	for _, quero := range []string{"Primeiro Episodio", "Segundo Episodio", "<season>1</season>", "<episode>1</episode>", "<episode>2</episode>"} {
		if !strings.Contains(texto, quero) {
			t.Errorf("NFO nao contem %q:\n%s", quero, texto)
		}
	}
	if blocos := strings.Count(texto, "<episodedetails>"); blocos != 2 {
		t.Errorf("blocos <episodedetails> = %d, quero 2", blocos)
	}
	// BOTH episodes point at the one .nfo (it describes the file, not the entity).
	for _, id := range []string{"e1", "e2"} {
		ent, err := srv.leitura().ObterEntity(context.Background(), id)
		if err != nil {
			t.Fatalf("ObterEntity(%s): %v", id, err)
		}
		if !ent.NfoPath.Valid || ent.NfoPath.String != nfoPath {
			t.Errorf("%s.nfo_path = %v, quero %q", id, ent.NfoPath, nfoPath)
		}
	}

	// 2nd episode: already convergent, nothing is rewritten.
	artes2, nfo2, err := srv.MaterializarMidiasFaltantes(context.Background(), "e2")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(e2): %v", err)
	}
	if len(artes2) != 0 || nfo2 {
		t.Errorf("2a passada (e2): artes=%v nfo=%v, quero nada (documento do arquivo ja converge)", artes2, nfo2)
	}
	depois, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("reler %s: %v", nfoPath, err)
	}
	if string(depois) != texto {
		t.Error("o NFO mudou na 2a passada -- a convergencia do documento compartilhado falhou")
	}
}

// TestMaterializarMidiasFaltantesNFOCompartilhadoCorrigeLegado covers the retroactive part: a
// file whose .nfo was written by the old per-entity logic (ONE block, with the title of one
// episode and the number the file name gave) is rewritten as the document of the FILE, with one
// block per episode and the numbering of the tree.
func TestMaterializarMidiasFaltantesNFOCompartilhadoCorrigeLegado(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	_, video := semearArvoreSerieMultiEpisodio(t, d)
	nfoPath := localmedia.CaminhoNFODeVideo(video)

	legado := `<?xml version="1.0" encoding="UTF-8"?>
<episodedetails>
  <title>Segundo Episodio</title>
  <season>3</season>
  <episode>1</episode>
  <watched>true</watched>
  <playcount>4</playcount>
</episodedetails>
`
	if err := localmedia.EscreverArquivoAtomico(nfoPath, []byte(legado)); err != nil {
		t.Fatalf("escrever legado: %v", err)
	}

	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes(e1): %v", err)
	}
	if !nfoGravado {
		t.Fatal("nfoGravado = false, quero true (o legado nao converge)")
	}
	corpo, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("ler %s: %v", nfoPath, err)
	}
	texto := string(corpo)
	if blocos := strings.Count(texto, "<episodedetails>"); blocos != 2 {
		t.Fatalf("blocos = %d, quero 2 -- o legado nao virou documento do arquivo:\n%s", blocos, texto)
	}
	if strings.Contains(texto, "<season>3</season>") {
		t.Errorf("o <season>3</season> do legado sobreviveu (deve vir da arvore):\n%s", texto)
	}
	// the watched state of the block that ALREADY existed is preserved on the episode it
	// belonged to (the file declared "Segundo Episodio", the 2nd block).
	if !strings.Contains(texto, "<playcount>4</playcount>") {
		t.Errorf("playcount preservado ausente:\n%s", texto)
	}
}

// TestPastaDeMusicaSemFaixaNaoTemDestino covers the "nothing to do" arm of pastaDeMusica: an
// album/artist without an owned descendant track has no folder (ADR-0027: they never have a
// media_file of their own), which is a no-op, never an error.
func TestPastaDeMusicaSemFaixaNaoTemDestino(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "albVazio", "album", "Sem Faixa")
	semearEntity(t, d, "artVazio", "artista", "Sem Album")
	ctx := context.Background()

	for _, id := range []string{"albVazio", "artVazio"} {
		ent, err := srv.leitura().ObterEntity(ctx, id)
		if err != nil {
			t.Fatalf("ObterEntity(%s): %v", id, err)
		}
		if dir, faixa, ok := srv.pastaDeMusica(ctx, ent); ok {
			t.Errorf("pastaDeMusica(%s) = (%q, %q, true), quero ok=false", id, dir, faixa)
		}
	}
	// e a entity fora da vertical de musica tambem nao tem pasta por aqui
	ent, err := srv.leitura().ObterEntity(ctx, "albVazio")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	ent.Type = "movie"
	if _, _, ok := srv.pastaDeMusica(ctx, ent); ok {
		t.Error("pastaDeMusica(filme) = ok, quero false (so album/artista)")
	}
}

// TestMaterializarMidiasFaltantesEpisodioSemArquivoNaoGravaNFO covers the guard: an episode
// entity without a 'present' media_file has no file to name a .nfo after -- no write, no error.
func TestMaterializarMidiasFaltantesEpisodioSemArquivoNaoGravaNFO(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "eSolto", "episode", "Episodio Sem Arquivo")

	artes, nfo, err := srv.MaterializarMidiasFaltantes(context.Background(), "eSolto")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	if len(artes) != 0 || nfo {
		t.Errorf("artes=%v nfo=%v, quero nada (sem arquivo nao ha destino)", artes, nfo)
	}
}

// TestMaterializarMidiasFaltantesNFOCompartilhadoPreservaCamposPorEpisodio covers the per-block
// preservation: the local fields (watched/rating/play count) of EACH episode of the file survive
// a rewrite, matched by <season>/<episode> -- not the old "one block for everybody". The file is
// hand-edited to hold a single block with a watch count, which is what makes it diverge from the
// document of the file.
func TestMaterializarMidiasFaltantesNFOCompartilhadoPreservaCamposPorEpisodio(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	_, video := semearArvoreSerieMultiEpisodio(t, d)
	nfoPath := localmedia.CaminhoNFODeVideo(video)
	ctx := context.Background()

	if _, _, err := srv.MaterializarMidiasFaltantes(ctx, "e1"); err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	corpo, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("ler %s: %v", nfoPath, err)
	}
	partes := strings.SplitN(string(corpo), "<episodedetails>", 3)
	if len(partes) != 3 {
		t.Fatalf("esperava 2 blocos, achei %d:\n%s", len(partes)-1, corpo)
	}
	// O dono editou o NFO a mao: sobrou so o bloco do SEGUNDO episodio, com um playcount.
	soSegundo := partes[0] + "<episodedetails>" + partes[2]
	soSegundo = strings.Replace(soSegundo, "</episodedetails>", "  <playcount>7</playcount>\n</episodedetails>", 1)
	if err := localmedia.EscreverArquivoAtomico(nfoPath, []byte(soSegundo)); err != nil {
		t.Fatalf("escrever editado: %v", err)
	}

	// O documento do ARQUIVO tem 2 blocos: o editado nao converge e e reescrito, preservando o
	// playcount no episodio a que ele pertencia.
	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(ctx, "e1")
	if err != nil {
		t.Fatalf("2a chamada: %v", err)
	}
	if !nfoGravado {
		t.Fatal("nfoGravado = false, quero true (o arquivo nao tem mais os 2 blocos)")
	}
	final, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("reler %s: %v", nfoPath, err)
	}
	partes = strings.SplitN(string(final), "<episodedetails>", 3)
	if len(partes) != 3 {
		t.Fatalf("esperava 2 blocos no final, achei %d:\n%s", len(partes)-1, final)
	}
	if strings.Contains(partes[1], "<playcount>") {
		t.Errorf("o 1o bloco ganhou playcount que era do 2o:\n%s", partes[1])
	}
	if !strings.Contains(partes[2], "<playcount>7</playcount>") {
		t.Errorf("o 2o bloco perdeu o playcount preservado:\n%s", partes[2])
	}
}

// TestMaterializarAssetsMusicaRotacionaNomesBare covers the music rotation: the first artwork of
// a kind takes the bare name (folder.png), the second one the numeric suffix (folder1.png) -- and
// neither ever becomes poster.png, the video name no music folder should receive (ADR-0043).
func TestMaterializarAssetsMusicaRotacionaNomesBare(t *testing.T) {
	_, h, d := setupMidiasFaltantesServer(t)
	_, albumID, albumDir := semearArvoreMusica(t, d)

	primeiro := decodeAsset(t, postAsset(t, h, albumID, "poster", pngPayload(64)))
	rec := do(t, h, http.MethodPost, "/api/v1/entities/"+albumID+"/assets/"+primeiro.ID+"/materialize", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("materialize do 1o: status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	segundo := decodeAsset(t, postAsset(t, h, albumID, "poster", pngPayload(64)))
	rec = do(t, h, http.MethodPost, "/api/v1/entities/"+albumID+"/assets/"+segundo.ID+"/materialize", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("materialize do 2o: status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}

	for _, quero := range []string{"folder.png", "folder1.png"} {
		if _, err := os.Stat(filepath.Join(albumDir, quero)); err != nil {
			t.Errorf("arquivo ausente %s: %v", quero, err)
		}
	}
	for _, naoQuero := range []string{"poster.png", "poster1.png"} {
		if _, err := os.Stat(filepath.Join(albumDir, naoQuero)); err == nil {
			t.Errorf("%s existe -- nome de video vazou para a pasta de musica", naoQuero)
		}
	}
}

// TestWriteEntityNfoEpisodioEscreveDocumentoDoArquivo covers the HTTP path: POST
// /entities/{id}/nfo of ONE episode of a multi-episode file writes the document of the FILE (both
// blocks), the same as the job -- the endpoint is idempotent-but-correct, never per entity.
func TestWriteEntityNfoEpisodioEscreveDocumentoDoArquivo(t *testing.T) {
	_, h, d := setupMidiasFaltantesServer(t)
	_, video := semearArvoreSerieMultiEpisodio(t, d)

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/nfo", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	corpo, err := os.ReadFile(localmedia.CaminhoNFODeVideo(video))
	if err != nil {
		t.Fatalf("ler NFO: %v", err)
	}
	texto := string(corpo)
	for _, quero := range []string{"Primeiro Episodio", "Segundo Episodio", "<episode>2</episode>"} {
		if !strings.Contains(texto, quero) {
			t.Errorf("NFO do arquivo nao contem %q:\n%s", quero, texto)
		}
	}
}

// TestWriteEntityNfoMusicaResponde404 covers the decision that music has no NFO (yet): the album
// folder resolves, but there is no music NFO convention -- the endpoint answers 404 (no
// destination) instead of writing a nameless ".nfo" next to the tracks.
func TestWriteEntityNfoMusicaResponde404(t *testing.T) {
	_, h, d := setupMidiasFaltantesServer(t)
	_, albumID, albumDir := semearArvoreMusica(t, d)

	rec := do(t, h, http.MethodPost, "/api/v1/entities/"+albumID+"/nfo", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	entradas, err := os.ReadDir(albumDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", albumDir, err)
	}
	for _, entrada := range entradas {
		if strings.HasSuffix(entrada.Name(), ".nfo") {
			t.Errorf("arquivo %q escrito na pasta de musica, quero nenhum NFO", entrada.Name())
		}
	}
}

// TestDescobrirArteLocalMusicaSemScanner covers the no-op arm: without a scanner there is
// nothing to recheck on disk, and the batch says so instead of failing.
func TestDescobrirArteLocalMusicaSemScanner(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearArvoreMusica(t, d)

	achados, err := srv.DescobrirArteLocalMusica(context.Background(), nil)
	if err != nil {
		t.Fatalf("DescobrirArteLocalMusica: %v", err)
	}
	if achados != 0 {
		t.Errorf("achados = %d, quero 0 (sem scanner nao ha o que rechecar)", achados)
	}
}

// TestMaterializarMidiasFaltantesNFOCompartilhadoRemoveOrfao covers the cleanup of a sibling's
// legacy .nfo: when an episode of the file had its NFO registered at an OLD path (legacy naming
// or a moved file), the write must stamp the new destination on every episode of the file and
// remove the orphan, so Kodi does not find two documents for one episode.
func TestMaterializarMidiasFaltantesNFOCompartilhadoRemoveOrfao(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	_, video := semearArvoreSerieMultiEpisodio(t, d)
	nfoPath := localmedia.CaminhoNFODeVideo(video)
	ctx := context.Background()

	orfao := filepath.Join(filepath.Dir(video), "legado-antigo.nfo")
	if err := localmedia.EscreverArquivoAtomico(orfao, []byte("<episodedetails><title>velho</title></episodedetails>\n")); err != nil {
		t.Fatalf("escrever orfao: %v", err)
	}
	if err := escrita(t, d).AtualizarEntityNfo(ctx, sqlcgen.AtualizarEntityNfoParams{
		NfoPath:      nsOf(orfao),
		NfoWrittenAt: nsOf("2026-01-01T00:00:00.000Z"),
		ID:           "e2",
	}); err != nil {
		t.Fatalf("AtualizarEntityNfo: %v", err)
	}

	if _, nfoGravado, err := srv.MaterializarMidiasFaltantes(ctx, "e1"); err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	} else if !nfoGravado {
		t.Fatal("nfoGravado = false, quero true")
	}
	for _, id := range []string{"e1", "e2"} {
		ent, err := srv.leitura().ObterEntity(ctx, id)
		if err != nil {
			t.Fatalf("ObterEntity(%s): %v", id, err)
		}
		if !ent.NfoPath.Valid || ent.NfoPath.String != nfoPath {
			t.Errorf("%s.nfo_path = %v, quero %q", id, ent.NfoPath, nfoPath)
		}
	}
	if _, err := os.Stat(orfao); !os.IsNotExist(err) {
		t.Errorf("NFO legado %s continua no disco (stat err=%v), quero removido", orfao, err)
	}
}

// TestGravarNFOEpisodiosErroDeEscrita covers the failure arm: when the .nfo cannot be written
// (a directory squatting on the path), the error propagates instead of being reported as a
// written document.
func TestGravarNFOEpisodiosErroDeEscrita(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	_, video := semearArvoreSerieMultiEpisodio(t, d)
	nfoPath := localmedia.CaminhoNFODeVideo(video)
	if err := os.MkdirAll(nfoPath, 0o755); err != nil {
		t.Fatalf("criar diretorio no lugar do nfo: %v", err)
	}
	ent, err := srv.leitura().ObterEntity(context.Background(), "e1")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if _, err := srv.gravarNFOEpisodios(context.Background(), ent, map[string][]artworkItem{}); err == nil {
		t.Error("gravarNFOEpisodios sem erro, quero o erro de escrita propagado")
	}
}

// semearEpisodioSemNumero seeds a series and ONE episode that the tree cannot number (no season
// link, so both season and episode come back as 0) -- 430 of those exist in the owner's library,
// written by whichever manager indexed them.
func semearEpisodioSemNumero(t *testing.T, d *db.DB) (video string) {
	t.Helper()
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "e1", "episode", "Especial")
	ligarFilhoOrdenado(t, d, "r-e1", "s1", "e1", "tv_episode", 0)
	libRoot := t.TempDir()
	video = filepath.Join(libRoot, "Season 01", "Serie - Especial.mkv")
	semearFile(t, d, "mf1", "s1", video, "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "e1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}
	return video
}

// TestMaterializarMidiasFaltantesEpisodioSemNumeroMantemONumeroDoArquivo cobre a regra de
// numeracao do bloco: quando a arvore NAO sabe a temporada/episodio (ambos 0), vale o numero que
// o arquivo ja declara. O contrario -- omitir a tag porque o modelo nao sabe -- faz o arquivo
// divergir do documento em TODA execucao: o job reescreveria 360 arquivos por run para nao dizer
// nada de novo, e o numero que o Kodi le desapareceria.
// TestMaterializarMidiasFaltantesEpisodioComNumeroParcialCompletaPeloNome cobre o caso que
// aparece na biblioteca: o NFO danificado ainda declara <season>, mas perdeu <episode>. Uma
// numeracao PARCIAL nao pode ser autoritativa (era exatamente essa a forma do dano da v0.15.4),
// e completar metade com o arquivo e metade com o nome daria um par que nunca existiu: o par
// inteiro vem do nome.
func TestMaterializarMidiasFaltantesEpisodioComNumeroParcialCompletaPeloNome(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "e1", "episode", "Episodio 7")
	ligarFilhoOrdenado(t, d, "r-e1", "s1", "e1", "tv_episode", 0)
	libRoot := t.TempDir()
	video := filepath.Join(libRoot, "Season 01", "Serie.S01E07.mkv")
	semearFile(t, d, "mf1", "s1", video, "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "e1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}
	nfoPath := localmedia.CaminhoNFODeVideo(video)
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Dano real medido: sobrou <season>4</season> e o <episode> sumiu.
	danificado := `<?xml version="1.0" encoding="UTF-8"?>
<episodedetails>
  <title>Episodio 7</title>
  <season>4</season>
</episodedetails>
`
	if err := localmedia.EscreverArquivoAtomico(nfoPath, []byte(danificado)); err != nil {
		t.Fatalf("escrever danificado: %v", err)
	}

	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	if !nfoGravado {
		t.Fatal("nfoGravado = false, quero true (numeracao parcial tem de ser completada)")
	}
	final, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("reler %s: %v", nfoPath, err)
	}
	if !strings.Contains(string(final), "<season>1</season>") {
		t.Errorf("quero a temporada do NOME (1), nao a parcial do arquivo (4):\n%s", final)
	}
	if !strings.Contains(string(final), "<episode>7</episode>") {
		t.Errorf("o episodio do nome nao entrou:\n%s", final)
	}
}

// TestMaterializarMidiasFaltantesEpisodioSemNumeroRecuperaDoNomeDoArquivo cobre a TERCEIRA
// camada da numeracao: a arvore nao sabe (0/0) e o arquivo (dano de uma execucao anterior) nao
// declara nada, mas o NOME do video diz SxxEyy. Sem essa camada o documento e o arquivo danificado
// concordam em "sem numero", o arquivo converge e o dano fica permanente.
func TestMaterializarMidiasFaltantesEpisodioSemNumeroRecuperaDoNomeDoArquivo(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "e1", "episode", "Episodio 7")
	ligarFilhoOrdenado(t, d, "r-e1", "s1", "e1", "tv_episode", 0)
	libRoot := t.TempDir()
	video := filepath.Join(libRoot, "Season 01", "Serie.S01E07.mkv")
	semearFile(t, d, "mf1", "s1", video, "present", 0.9, "")
	if err := escrita(t, d).CriarVinculoEntidade(context.Background(), sqlcgen.CriarVinculoEntidadeParams{
		MediaFileID: "mf1", EntityID: "e1", Kind: "tv_episode", Ordinal: 0,
	}); err != nil {
		t.Fatalf("CriarVinculoEntidade: %v", err)
	}
	nfoPath := localmedia.CaminhoNFODeVideo(video)

	// O dano: um NFO escrito pela versao que omitia a numeracao que o modelo nao tinha.
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	danificado := `<?xml version="1.0" encoding="UTF-8"?>
<episodedetails>
  <title>Episodio 7</title>
</episodedetails>
`
	if err := localmedia.EscreverArquivoAtomico(nfoPath, []byte(danificado)); err != nil {
		t.Fatalf("escrever danificado: %v", err)
	}

	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(context.Background(), "e1")
	if err != nil {
		t.Fatalf("MaterializarMidiasFaltantes: %v", err)
	}
	if !nfoGravado {
		t.Fatal("nfoGravado = false, quero true (o arquivo danificado tem de ser reescrito)")
	}
	final, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("reler %s: %v", nfoPath, err)
	}
	if !strings.Contains(string(final), "<season>1</season>") || !strings.Contains(string(final), "<episode>7</episode>") {
		t.Errorf("numeracao do nome do arquivo nao foi recuperada:\n%s", final)
	}
}

func TestMaterializarMidiasFaltantesEpisodioSemNumeroMantemONumeroDoArquivo(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	video := semearEpisodioSemNumero(t, d)
	nfoPath := localmedia.CaminhoNFODeVideo(video)
	ctx := context.Background()

	if _, _, err := srv.MaterializarMidiasFaltantes(ctx, "e1"); err != nil {
		t.Fatalf("1a chamada: %v", err)
	}
	corpo, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("ler %s: %v", nfoPath, err)
	}
	if strings.Contains(string(corpo), "<episode>") {
		t.Fatalf("sem numero na arvore e sem arquivo previo, nao quero <episode> no documento:\n%s", corpo)
	}
	// O arquivo ganha a numeracao que o gerenciador anterior tinha (o caso real da biblioteca).
	editado := strings.Replace(string(corpo), "</episodedetails>",
		"  <season>4</season>\n  <episode>11</episode>\n</episodedetails>", 1)
	if err := localmedia.EscreverArquivoAtomico(nfoPath, []byte(editado)); err != nil {
		t.Fatalf("escrever editado: %v", err)
	}

	_, nfoGravado, err := srv.MaterializarMidiasFaltantes(ctx, "e1")
	if err != nil {
		t.Fatalf("2a chamada: %v", err)
	}
	if nfoGravado {
		t.Error("nfoGravado = true, quero false (o arquivo ja diz o que o documento diria)")
	}
	final, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("reler %s: %v", nfoPath, err)
	}
	if !strings.Contains(string(final), "<episode>11</episode>") {
		t.Errorf("o numero do arquivo foi perdido:\n%s", final)
	}
	if !strings.Contains(string(final), "<season>4</season>") {
		t.Errorf("a temporada do arquivo foi perdida:\n%s", final)
	}
}
