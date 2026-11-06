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
	"github.com/rafaelvieiras/my-media-db/internal/provider"
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

// ── Self-heal de arte/NFO materializados (briefing 0127) ─────────────────

// TestMaterializarMidiasFaltantesArquivoSumidoRegravaMesmoPath cobre o
// critério de aceite do briefing 0127: um poster já materializado
// (local_path preenchido) cujo arquivo físico sumiu do disco (mount remoto,
// exclusão externa) -- rodar MaterializarMidiasFaltantes de novo regrava o
// binário no MESMO local_path, sem rotacionar, e reporta a autocura em
// artesBaixadas.
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

	// Simula o arquivo sumindo fisicamente (mount remoto, exclusão externa).
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

// TestMaterializarMidiasFaltantesArquivoPresenteNaoRebaixaNaoRegrava cobre a
// contrapartida: quando o arquivo já materializado está presente no disco, a
// 2a chamada não reporta a arte como "baixada" de novo (mesmo comportamento
// idempotente de antes do briefing 0127 -- self-heal não deve virar
// reescrita incondicional a cada rodada do job).
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

// TestMaterializarMidiasFaltantesNfoSumidoRegravaDoZero cobre o critério de
// aceite do briefing 0127 para o NFO: entity.nfo_path preenchido cujo
// arquivo .nfo sumiu do disco -- rodar MaterializarMidiasFaltantes de novo
// regrava o .nfo do zero (gravarNFO já é idempotente por conteúdo, sem a
// armadilha de rotação que asset tem).
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

// ── Convergência de naming legado (briefing 0128, Caso B) ────────────────

// semearAssetPrincipalEMoverParaLocalLegado materializa um poster (que vira
// o principal do kind) e depois simula naming legado (pre-briefing-0081):
// move o binário pra dentro de "Season 01" com nome de episódio e atualiza
// local_path no banco -- mesmo estado real observado no MIB Homens de Preto
// em produção. Devolve o asset (já com local_path legado), o caminho
// canônico esperado (raiz de libDir) e o caminho legado.
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

// TestMaterializarMidiasFaltantesAssetLegadoPrincipalConvergeParaNomeCanonico
// cobre o critério de aceite do briefing 0128: um asset JÁ PRINCIPAL cujo
// local_path aponta pra um nome/pasta de EPISÓDIO (naming legado, gravado
// antes do briefing 0081 corrigir DiretorioDestino) -- rodar
// MaterializarMidiasFaltantes de novo RENOMEIA (não recopia) o arquivo de
// volta pro nome bare canônico na raiz, e persiste o novo local_path.
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

// TestMaterializarMidiasFaltantesAssetLegadoNaoPrincipalNaoConverge cobre a
// contrapartida de segurança (briefing 0128 §6): um asset local em naming
// legado que NÃO é is_principal (edge case -- nenhum asset do kind é
// principal) NÃO tem o nome recalculado -- só preserva o comportamento
// existente do briefing 0127 (regravar in-place se o arquivo tiver sumido).
// Recalcular o nome bare para um não-principal colidiria com o nome que o
// verdadeiro principal deveria ocupar.
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

// semearNfoNaRaizEMoverParaLocalLegado grava o tvshow.nfo corretamente na
// raiz (pós-briefing-0081) e depois simula naming legado (pré-0081): move o
// .nfo pra dentro de "Season 01" e atualiza entity.nfo_path pra lá -- mesmo
// estado real observado no MIB Homens de Preto em produção. Devolve o
// caminho canônico esperado (raiz) e o caminho legado.
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

// TestMaterializarMidiasFaltantesNfoLegadoConvergeERemoveOrfao cobre o
// critério de aceite do briefing 0128 para o NFO: um entity.nfo_path legado
// (gravado antes do briefing 0081, apontando pra dentro de "Season 01") --
// rodar MaterializarMidiasFaltantes de novo regrava o .nfo na raiz correta
// e remove o arquivo antigo órfão.
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

// ── Música (briefing 0116): álbum/artista entram no universo do job e ganham "poster" ──

// semearArvoreMusica monta artista->album->faixa (entity_relation) com UM media_file
// present na faixa (o único jeito de música ligar a media_file, ADR-0027) e devolve os
// ids (artistaID, albumID).
func semearArvoreMusica(t *testing.T, d *db.DB) (artistaID, albumID string) {
	t.Helper()
	semearEntity(t, d, "art1", "artista", "Duran Duran")
	semearEntity(t, d, "alb1", "album", "Rio")
	semearEntity(t, d, "fx1", "faixa", "Hungry Like the Wolf")
	ligarFilhoOrdenado(t, d, "r1", "art1", "alb1", "music_artista_principal", 0)
	ligarFilhoOrdenado(t, d, "r2", "alb1", "fx1", "music_album", 1)
	semearFile(t, d, "f1", "fx1", "/lib/musica/rio/01.flac", "present", 1, "")
	return "art1", "alb1"
}

// TestListarEntitiesComMediaFilePresenteMusica cobre a extensão do universo elegível do
// job midias_faltantes pra música (briefing 0116): artista/album SEM media_file DIRETO,
// mas com uma faixa descendente possuída, agora aparecem -- antes, só media_file.entity_id
// direto (que música nunca tem em artista/album) era considerado.
func TestListarEntitiesComMediaFilePresenteMusica(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, albumID := semearArvoreMusica(t, d)
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

// semearInventarioArte grava o envelope de inventário de arte (ChaveEnvelopeInventarioArtes)
// em media_external, no formato lido por montarGruposArtwork -- mesmo shape usado pelo
// enrich real (fanart.tv/musicbrainz).
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

// TestMaterializarMidiasFaltantesAlbumBaixaCapaDoMusicbrainz cobre o caso ÁLBUM: a
// fanart.tv/Cover Art Archive já grava a capa sob kind='poster' (mesmo vocabulário do
// agregador), então o loop GENÉRICO de kindsMidiasFaltantes já materializa sem nenhum
// caso especial -- só precisava que álbum entrasse no universo elegível (teste acima) e
// que o helper de destino tratasse a ausência de pasta real graciosamente
// (materializarAssetOuCache).
func TestMaterializarMidiasFaltantesAlbumBaixaCapaDoMusicbrainz(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	_, albumID := semearArvoreMusica(t, d)
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
}

// TestMaterializarMidiasFaltantesArtistaBaixaPosterViaThumb cobre o caso ARTISTA: a
// fanart.tv não modela um kind próprio de pôster de artista (só thumb/fanart/logo/banner,
// kindsArtista em internal/provider/fanarttv) -- materializarPosterDeArtista usa o
// vencedor do kind `thumb` como poster (briefing 0116), senão o card de artista NUNCA
// teria has_poster=true.
func TestMaterializarMidiasFaltantesArtistaBaixaPosterViaThumb(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, _ := semearArvoreMusica(t, d)
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
	}
	if !temPoster {
		t.Errorf("assets do artista = %+v, quero um asset kind=poster (derivado do thumb)", assets)
	}
}

// TestMaterializarPosterDeArtistaIdempotente cobre que uma 2a chamada não rebaixa/duplica
// quando o artista já tem um poster (mesmo que só em cache, sem pasta real).
func TestMaterializarPosterDeArtistaIdempotente(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, _ := semearArvoreMusica(t, d)
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

// TestMaterializarPosterDeArtistaSemInventarioNaoFaz nada cobre "nada a fazer" (briefing
// 0116): artista elegível (faixa possuída) mas SEM nenhum inventário de arte cacheado --
// nenhum candidato de thumb, então nenhum poster é criado, sem erro.
func TestMaterializarPosterDeArtistaSemInventarioNaoFazNada(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, _ := semearArvoreMusica(t, d)

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

// TestMaterializarPosterDeArtistaFalhaDownloadNaoPropaga cobre que uma falha de DOWNLOAD
// (conteúdo não é imagem) degrada em silêncio (best-effort, mesmo espírito de
// materializarKindVencedor) em vez de propagar erro -- sem asset criado.
func TestMaterializarPosterDeArtistaFalhaDownloadNaoPropaga(t *testing.T) {
	srv, _, d := setupMidiasFaltantesServer(t)
	artistaID, _ := semearArvoreMusica(t, d)
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

// decodeAssetList lista os assets da entity direto do banco (mais simples que decodificar
// a resposta HTTP agrupada por kind de /artwork).
func decodeAssetList(t *testing.T, srv *Server, entityID string) []sqlcgen.Asset {
	t.Helper()
	assets, err := srv.leitura().ListarAssetsPorEntity(context.Background(), entityID)
	if err != nil {
		t.Fatalf("ListarAssetsPorEntity(%s): %v", entityID, err)
	}
	return assets
}

// ── MaterializarSeAutoDownloadLigado (briefing 0089, adaptador de scanner.Materializador) ──

// TestMaterializarSeAutoDownloadLigadoToggleDesligadoNaoMaterializaNada cobre
// o critério §5: com o toggle desligado (default de fábrica), a chamada é
// no-op -- nenhuma arte é materializada, nenhum erro.
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

// TestMaterializarSeAutoDownloadLigadoToggleLigadoMaterializa cobre o
// critério §5: com o toggle ligado, a chamada materializa de fato a arte
// vencedora na pasta real, reusando MaterializarMidiasFaltantes.
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

// ── Reconvergência por conteúdo do NFO (briefing 0134) ───────────────────

// lerNFO devolve o conteúdo do .nfo no destino canônico da entity.
func lerNFO(t *testing.T, libDir, nomeBase string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(libDir, nomeBase+".nfo"))
	if err != nil {
		t.Fatalf("ler nfo: %v", err)
	}
	return string(b)
}

// TestMaterializarMidiasFaltantesNfoVazioRegrava cobre o caso extremo do
// briefing 0134: o .nfo existe no destino certo, mas está vazio (0 byte). Até
// o 0133 o job só olhava caminho+presença e nunca mais tocava no arquivo, então
// um .nfo vazio ficava vazio para sempre -- a biblioteca consumidora lia um
// episódio sem nada dentro.
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

// TestMaterializarMidiasFaltantesNfoDesatualizadoRegrava cobre o caso normal
// do 0134: o banco ganhou sinopse DEPOIS da primeira gravação. O .nfo do disco
// tem que ser reescrito com o dado novo, mesmo com nfo_path preenchido e o
// arquivo presente.
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

// TestMaterializarMidiasFaltantesNfoIgualNaoRegrava é o lado oposto: banco e
// disco em dia, nada a fazer. O arquivo não pode ser tocado (nem o mtime muda),
// senão o job vira churn de escrita no mount remoto e o mtime deixa de sinalizar
// mudança real.
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
