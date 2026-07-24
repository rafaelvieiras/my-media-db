package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// setupMidiasFaltantesServer sobe um Server (guardando a referência, não só
// o handler HTTP) com assetsDir temporário. internal/job/midiasfaltantes só
// fala com este código através da pequena interface Materializador
// (mockada no teste do pacote de job) -- a implementação REAL só é
// exercitada chamando os métodos do *Server diretamente, como o job faz.
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

// TestListarEntitiesComMediaFilePresente cobre o universo elegível do job
// midias_faltantes (briefing 0064): só entities com media_file 'present'
// aparecem, entities só-catálogo (sem arquivo) ficam de fora.
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

// TestMaterializarMidiasFaltantesBaixaPosterEGravaNFO cobre o corpo do job
// midias_faltantes (briefing 0064 §3): uma entity com media_file present e
// UM poster já selecionado (asset local, ainda não materializado) tem o
// poster gravado na pasta real e o NFO escrito, na MESMA chamada -- exatamente
// o que o job periódico faz para itens já resolvidos mas faltando no disco.
func TestMaterializarMidiasFaltantesBaixaPosterEGravaNFO(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	semearEntity(t, d, "e1", "movie", "Matrix")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	// Um poster já existe como asset (ex.: selecionado da galeria antes),
	// mas ainda só no cache -- sem local_path.
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

	// Idempotente: chamar de novo não reporta a mesma arte como "baixada"
	// (já tinha local_path) nem tenta regravar o NFO (já tinha nfo_path).
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

// semearArvoreSerie monta uma série com 2 temporadas/3 episódios, arquivos
// PRESENTES em subpastas reais "Season 01"/"Season 02" (a estrutura normal de
// qualquer biblioteca real -- briefing 0081): mf1/mf2 em Season 01, mf3 em
// Season 02, todos com media_file.entity_id=SÉRIE (convenção real do
// scanner) e vinculados aos episódios só pela junção media_file_entity
// (kind=tv_episode). Devolve o diretório-raiz da biblioteca (libRoot).
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

// TestListarEntitiesComMediaFilePresenteArvoreDeSerie cobre a causa 1 do
// briefing 0081: temporada (episódio descendente presente) e episódio
// (media_file via junção) passam a aparecer no universo elegível do job --
// antes, só a série (media_file.entity_id direto) aparecia.
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

// TestMaterializarMidiasFaltantesArvoreDeSerieGravaNaRaiz cobre a causa 2 do
// briefing 0081 (raiz errada): numa série com subpastas por temporada, o
// pôster da SÉRIE materializa na RAIZ (libRoot), NUNCA em "Season 01/"; o
// pôster de TEMPORADA materializa como season01-poster.jpg TAMBÉM na raiz; o
// NFO do EPISÓDIO é gravado (kind agora alcançado); episódio NÃO ganha still
// automático; temporada NÃO ganha NFO próprio (guarda preservada).
func TestMaterializarMidiasFaltantesArvoreDeSerieGravaNaRaiz(t *testing.T) {
	srv, h, d := setupMidiasFaltantesServer(t)
	libRoot := semearArvoreSerie(t, d)
	ctx := context.Background()

	// Pôster da SÉRIE: selecionado como asset (ainda só no cache).
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

	// Pôster de TEMPORADA (se1 = temporada 1): selecionado como asset da
	// própria season.
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

	// NFO do EPISÓDIO (e1): sem still, mas com NFO gravado na pasta real do
	// episódio (Season 01, nao na raiz).
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

// TestMaterializarMidiasFaltantesSemCandidatoNaoFalha cobre o caso "nada
// resolvido ainda": entity com media_file present mas SEM nenhum asset --
// não há candidato vencedor de kind nenhum, então nenhuma arte é baixada, mas
// o NFO (que só depende do metadado já resolvido pelo import, não de arte)
// ainda é gravado, e a chamada não retorna erro (best-effort, briefing 0064 §3
// explicitamente NÃO auto-seleciona nada quando não há candidato).
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
