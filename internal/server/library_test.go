package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
)

// setupLibraryServer sobe um Server com banco temporário migrado e devolve o
// handler raiz (rotas montadas) para exercitar /api/v1/library ponta a ponta.
func setupLibraryServer(t *testing.T) http.Handler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "library.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	h, err := New("v-teste", database).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h
}

// do executa uma requisição contra o handler e devolve o recorder.
func do(t *testing.T, h http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if raw, ok := body.([]byte); ok {
		rdr = bytes.NewReader(raw)
	} else if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal corpo: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, rdr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// criarOK faz POST /api/v1/library esperando 201 e devolve o registro criado.
func criarOK(t *testing.T, h http.Handler, path, tipo string) sourceDTO {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": path, "type": tipo})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %q status = %d, quero 201 (corpo=%s)", path, rec.Code, rec.Body.String())
	}
	return decodeSource(t, rec)
}

// decodeSource decodifica um sourceDTO do corpo.
func decodeSource(t *testing.T, rec *httptest.ResponseRecorder) sourceDTO {
	t.Helper()
	var s sourceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("decodificar sourceDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return s
}

// checkErrorShape valida que o corpo segue {error:{code,message}} e que o code
// bate com o esperado.
func checkErrorShape(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, quero application/json", ct)
	}
	var env errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decodificar envelope de erro: %v (corpo=%s)", err, rec.Body.String())
	}
	if env.Error.Code != wantCode {
		t.Errorf("error.code = %q, quero %q", env.Error.Code, wantCode)
	}
	if env.Error.Message == "" {
		t.Error("error.message vazio, quero mensagem clara")
	}
}

func TestPostLibraryDiretorioValido(t *testing.T) {
	h := setupLibraryServer(t)
	dir := t.TempDir()

	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": dir, "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeSource(t, rec)
	if got.ID == "" {
		t.Error("id vazio no registro criado")
	}
	if got.Path != dir {
		t.Errorf("path = %q, quero %q (normalizado absoluto)", got.Path, dir)
	}
	if got.Type != "movie" {
		t.Errorf("type = %q, quero movie", got.Type)
	}
	if got.State != "online" {
		t.Errorf("state = %q, quero online", got.State)
	}
	if got.LastScan != nil {
		t.Errorf("last_scan = %v, quero null", *got.LastScan)
	}
}

func TestPostLibraryPathInexistente(t *testing.T) {
	h := setupLibraryServer(t)
	inexistente := filepath.Join(t.TempDir(), "nao-existe")

	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": inexistente, "type": "movie"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_path")
}

func TestPostLibraryArquivoNaoDiretorio(t *testing.T) {
	h := setupLibraryServer(t)
	arquivo := filepath.Join(t.TempDir(), "um-arquivo.txt")
	if err := os.WriteFile(arquivo, []byte("oi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": arquivo, "type": "tv"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
	checkErrorShape(t, rec, "not_a_directory")
}

func TestPostLibrarySemPermissao(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("rodando como root: a checagem de permissão (chmod 0000) não vale; demais casos cobrem a validação")
	}
	h := setupLibraryServer(t)
	dir := filepath.Join(t.TempDir(), "sem-permissao")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod 0000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // restaura p/ o TempDir limpar

	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": dir, "type": "movie"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
	checkErrorShape(t, rec, "path_unreadable")
}

func TestPostLibraryDuplicado(t *testing.T) {
	h := setupLibraryServer(t)
	dir := t.TempDir()

	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": dir, "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("primeiro POST status = %d, quero 201", rec.Code)
	}

	// Mesmo caminho por uma forma NÃO-limpa (/dir/.. -> pai; /pai/base -> dir):
	// após normalização vira o mesmo path e deve colidir.
	naoLimpo := filepath.Join(dir, "..", filepath.Base(dir))
	rec2 := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": naoLimpo, "type": "movie"})
	if rec2.Code != http.StatusConflict {
		t.Fatalf("POST duplicado status = %d, quero 409 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	checkErrorShape(t, rec2, "duplicate_path")
}

func TestPostLibraryTipoInvalido(t *testing.T) {
	h := setupLibraryServer(t)
	dir := t.TempDir()

	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": dir, "type": "musica"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_type")
}

func TestPostLibraryCorpoInvalido(t *testing.T) {
	h := setupLibraryServer(t)

	// path ausente.
	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"type": "movie"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("path ausente: status = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_body")

	// JSON malformado.
	rec2 := do(t, h, http.MethodPost, "/api/v1/library", []byte("{isto nao e json"))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("json malformado: status = %d, quero 400", rec2.Code)
	}
	checkErrorShape(t, rec2, "invalid_body")
}

func TestGetLibraryListaComStateReavaliado(t *testing.T) {
	h := setupLibraryServer(t)
	// dir que vamos remover para virar offline.
	dirRemovido := filepath.Join(t.TempDir(), "some")
	if err := os.Mkdir(dirRemovido, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	dirVivo := t.TempDir()

	criarOK(t, h, dirRemovido, "movie")
	criarOK(t, h, dirVivo, "tv")

	// Remove o dir do disco: na leitura seu state deve virar offline.
	if err := os.Remove(dirRemovido); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/library", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET list status = %d, quero 200", rec.Code)
	}
	var out listDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decodificar listDTO: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items = %d, quero 2", len(out.Items))
	}
	byPath := map[string]string{}
	for _, it := range out.Items {
		byPath[it.Path] = it.State
	}
	if byPath[dirRemovido] != "offline" {
		t.Errorf("state do dir removido = %q, quero offline", byPath[dirRemovido])
	}
	if byPath[dirVivo] != "online" {
		t.Errorf("state do dir vivo = %q, quero online", byPath[dirVivo])
	}
}

func TestGetLibraryPorID(t *testing.T) {
	h := setupLibraryServer(t)
	dir := t.TempDir()

	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": dir, "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, quero 201", rec.Code)
	}
	criado := decodeSource(t, rec)

	// 200 no id existente.
	recGet := do(t, h, http.MethodGet, "/api/v1/library/"+criado.ID, nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /{id} status = %d, quero 200", recGet.Code)
	}
	got := decodeSource(t, recGet)
	if got.ID != criado.ID || got.Path != dir {
		t.Errorf("registro divergente: %+v", got)
	}

	// 404 em id inexistente.
	rec404 := do(t, h, http.MethodGet, "/api/v1/library/nao-existe", nil)
	if rec404.Code != http.StatusNotFound {
		t.Fatalf("GET /{id} inexistente status = %d, quero 404", rec404.Code)
	}
	checkErrorShape(t, rec404, "not_found")
}

func TestDeleteLibrary(t *testing.T) {
	h := setupLibraryServer(t)
	dir := t.TempDir()

	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": dir, "type": "movie"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, quero 201", rec.Code)
	}
	criado := decodeSource(t, rec)

	// DELETE existente -> 204 sem corpo.
	recDel := do(t, h, http.MethodDelete, "/api/v1/library/"+criado.ID, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, quero 204", recDel.Code)
	}
	if recDel.Body.Len() != 0 {
		t.Errorf("corpo do 204 não vazio: %q", recDel.Body.String())
	}

	// Sumiu: GET agora 404.
	if recGet := do(t, h, http.MethodGet, "/api/v1/library/"+criado.ID, nil); recGet.Code != http.StatusNotFound {
		t.Errorf("GET após DELETE status = %d, quero 404", recGet.Code)
	}

	// DELETE inexistente -> 404.
	recDel404 := do(t, h, http.MethodDelete, "/api/v1/library/nao-existe", nil)
	if recDel404.Code != http.StatusNotFound {
		t.Fatalf("DELETE inexistente status = %d, quero 404", recDel404.Code)
	}
	checkErrorShape(t, recDel404, "not_found")
}

// TestApiNotFoundShapeNovo garante que o 404 genérico de /api migrou para o
// shape padrão {error:{code,message}} (antes era {"error":"not found"}).
func TestApiNotFoundShapeNovo(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/rota-inexistente", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}
