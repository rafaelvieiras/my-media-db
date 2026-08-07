package server

// Testes de POST /entities/{id}/materialize-missing-media (briefing 0088,
// menu Gerenciar > "Baixar Artes Faltantes"): prova que o handler (1) força
// uma NOVA consulta ao ArteProvider registrado (atualizando/gravando o
// inventário media_external(source='fanarttv')) e (2) materializa na pasta
// real via a MESMA MaterializarMidiasFaltantes do job 0064 -- mesmo padrão
// de fake provider de internal/enrich/artes_test.go, mesmo padrão de
// allowlist/assetsDir de artwork_missing_test.go/midias_faltantes_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// fakeArteProviderServer implementa o core provider.Provider NO-OP (Nome
// "fanarttv") MAIS a capacidade provider.ArteProvider, devolvendo artes
// canned -- espelha fakeArteProvider de internal/enrich/artes_test.go (não
// exportado de lá, então precisa da própria cópia aqui).
type fakeArteProviderServer struct {
	artes    []provider.Arte
	chamadas int
}

func (f *fakeArteProviderServer) Nome() string { return "fanarttv" }

func (f *fakeArteProviderServer) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (f *fakeArteProviderServer) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (f *fakeArteProviderServer) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (f *fakeArteProviderServer) BuscarArtes(_ context.Context, _ provider.ExternalIDs, _ string, _ []string) ([]provider.Arte, error) {
	f.chamadas++
	return f.artes, nil
}

// setupMaterializeMissingMediaServer sobe um Server com assetsDir temporário,
// o fakeArteProviderServer registrado (liga s.enrich) e a allowlist do proxy
// restrita ao host informado.
func setupMaterializeMissingMediaServer(t *testing.T, hostPermitido string, arte *fakeArteProviderServer) (http.Handler, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "materialize-missing.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database, WithAssetsDir(dir), WithSearchProvider(arte))
	s.hostsImagem = map[string]bool{hostPermitido: true}
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// semearEntityComTmdb cria uma entity com external_ids_json={"tmdb":sourceID}
// -- a âncora que AtualizarInventarioArtes/idsExternosDe precisa para cruzar
// com o ArteProvider (sem nenhum id externo, BuscarArtes nem é chamado).
func semearEntityComTmdb(t *testing.T, d *db.DB, id, tipo, titulo, sourceID string) {
	t.Helper()
	extIDs, err := json.Marshal(map[string]string{"tmdb": sourceID})
	if err != nil {
		t.Fatalf("marshal external_ids: %v", err)
	}
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: id, Type: tipo, Title: titulo, ExternalIdsJson: nsOf(string(extIDs)),
	}); err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}

// decodeMaterializeMissingMediaResult decodifica a resposta do endpoint.
func decodeMaterializeMissingMediaResult(t *testing.T, body []byte) materializeMissingMediaResultDTO {
	t.Helper()
	var out materializeMissingMediaResultDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar materializeMissingMediaResultDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestMaterializeMissingMediaAtualizaInventarioEMaterializa cobre o critério
// central do briefing 0088: o inventário fanart.tv da entity é escrito por
// UMA nova consulta ao ArteProvider (nunca reusa um inventário pré-existente
// só passivamente) E a arte vencedora resultante é materializada de verdade
// na pasta real da biblioteca, junto com o .nfo.
func TestMaterializeMissingMediaAtualizaInventarioEMaterializa(t *testing.T) {
	srv := servidorImagem(t, nil)
	arte := &fakeArteProviderServer{artes: []provider.Arte{
		{Kind: kindPoster, URL: srv.URL + "/img", Likes: 5},
	}}
	h, d := setupMaterializeMissingMediaServer(t, hostDe(t, srv.URL), arte)
	semearEntityComTmdb(t, d, "e1", "movie", "Matrix", "603")
	libDir := libDirComVideo(t, d, "e1", "mf1", "Matrix.1999.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/materialize-missing-media", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if arte.chamadas != 1 {
		t.Errorf("BuscarArtes chamado %d vezes, quero 1 (nova consulta forcada)", arte.chamadas)
	}

	resultado := decodeMaterializeMissingMediaResult(t, rec.Body.Bytes())
	if len(resultado.ArtesBaixadas) != 1 || resultado.ArtesBaixadas[0] != kindPoster {
		t.Errorf("artes_baixadas = %v, quero [poster]", resultado.ArtesBaixadas)
	}
	if !resultado.NfoGravado {
		t.Error("nfo_gravado = false, quero true (entity sem nfo_path ainda)")
	}

	// O inventario fanart.tv foi gravado (media_external source=fanarttv).
	row, err := escrita(t, d).ObterMediaExternalPorFonte(context.Background(), sqlcgen.ObterMediaExternalPorFonteParams{
		EntityID: "e1", Source: "fanarttv",
	})
	if err != nil {
		t.Fatalf("inventario fanarttv ausente: %v", err)
	}
	if row.PayloadJson.String == "" {
		t.Error("payload do inventario fanarttv vazio")
	}

	// A arte foi materializada de verdade na pasta real (poster.png -- content-type
	// da imagem de teste, servidorImagem serve PNG).
	posterPath := filepath.Join(libDir, "poster.png")
	if _, err := os.Stat(posterPath); err != nil {
		t.Errorf("poster nao materializado em disco (%s): %v", posterPath, err)
	}
	nfoPath := filepath.Join(libDir, "Matrix.1999.nfo")
	if _, err := os.Stat(nfoPath); err != nil {
		t.Errorf("nfo nao gravado em disco (%s): %v", nfoPath, err)
	}
}

// TestMaterializeMissingMediaSemEnrichDegradaESoMaterializa cobre a
// degradação silenciosa (AGENTS.md §6): sem NENHUM provider de busca externa
// registrado, s.enrich fica nil -- o handler pula a etapa de atualizar o
// inventário (sem 5xx) e segue direto para MaterializarMidiasFaltantes sobre
// o inventário que já existir (aqui, nenhum -- então nada a materializar,
// mas o .nfo ainda é gravado, best-effort de sempre).
func TestMaterializeMissingMediaSemEnrichDegradaESoMaterializa(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "sem-enrich.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := New("v-teste", database, WithAssetsDir(dir)) // sem WithSearchProvider: s.enrich fica nil
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	semearEntity(t, database, "e1", "movie", "Sem Provider")
	libDir := libDirComVideo(t, database, "e1", "mf1", "Sem.Provider.mkv")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e1/materialize-missing-media", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeMaterializeMissingMediaResult(t, rec.Body.Bytes())
	if len(resultado.ArtesBaixadas) != 0 {
		t.Errorf("artes_baixadas = %v, quero [] (sem inventario, nada a materializar)", resultado.ArtesBaixadas)
	}
	if !resultado.NfoGravado {
		t.Error("nfo_gravado = false, quero true (nfo nao depende de arte)")
	}
	if _, err := os.Stat(filepath.Join(libDir, "Sem.Provider.nfo")); err != nil {
		t.Errorf("nfo nao gravado em disco: %v", err)
	}
}

// TestMaterializeMissingMediaEntidadeInexistente: 404 para entity inexistente.
func TestMaterializeMissingMediaEntidadeInexistente(t *testing.T) {
	arte := &fakeArteProviderServer{}
	h, _ := setupMaterializeMissingMediaServer(t, "example.com", arte)
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/materialize-missing-media", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
}
