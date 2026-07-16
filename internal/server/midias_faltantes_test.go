package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
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
