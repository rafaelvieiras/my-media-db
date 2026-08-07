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
// TestPatchLibraryWatchEnabled cobre o toggle do briefing 0064: PATCH liga
// watch_enabled, persiste (reflete em GET), e desliga de volta -- sem
// s.watch configurado (setupLibraryServer não usa WithWatch), a rotina de
// aplicarWatchToggle degrada em silêncio (guarda s.watch==nil), só a
// persistência é exercitada aqui.
func TestPatchLibraryWatchEnabled(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")
	if src.WatchEnabled {
		t.Fatalf("watch_enabled no cadastro = true, quero false (default)")
	}

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"watch_enabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH watch_enabled=true: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ligado := decodeSource(t, rec)
	if !ligado.WatchEnabled {
		t.Error("watch_enabled na resposta do PATCH = false, quero true")
	}

	// Reflete em GET (persistiu de verdade, não só na resposta do PATCH).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); !got.WatchEnabled {
		t.Error("watch_enabled no GET pos-PATCH = false, quero true (persistido)")
	}

	rec2 := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"watch_enabled": false})
	if rec2.Code != http.StatusOK {
		t.Fatalf("PATCH watch_enabled=false: status = %d, quero 200 (corpo=%s)", rec2.Code, rec2.Body.String())
	}
	if desligado := decodeSource(t, rec2); desligado.WatchEnabled {
		t.Error("watch_enabled apos desligar = true, quero false")
	}
}

// TestPatchLibraryCorpoVazioSoDevolveEstado cobre o caso "PATCH sem
// watch_enabled no corpo": não toca nada, só devolve o estado atual.
func TestPatchLibraryCorpoVazioSoDevolveEstado(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeSource(t, rec); got.WatchEnabled {
		t.Error("watch_enabled = true, quero false (nada deveria ter mudado)")
	}
}

// TestPatchLibraryNaoEncontrado cobre o 404 de uma fonte inexistente.
func TestPatchLibraryNaoEncontrado(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodPatch, "/api/v1/library/naoexiste", map[string]any{"watch_enabled": true})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestPatchLibraryCorpoMalformado cobre o 422 de um corpo que não é JSON válido.
func TestPatchLibraryCorpoMalformado(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, []byte("{nao-e-json"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPostLibraryComPreferredLanguage cobre o cadastro com preferred_language
// (briefing 0084): "en" está no allowed default (defaultLanguageSettings) e
// entra sem exigir PUT /settings/languages antes.
func TestPostLibraryComPreferredLanguage(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/library",
		map[string]string{"path": t.TempDir(), "type": "movie", "preferred_language": "en"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeSource(t, rec)
	if got.PreferredLanguage == nil || *got.PreferredLanguage != "en" {
		t.Errorf("preferred_language = %v, quero \"en\"", got.PreferredLanguage)
	}
}

// TestPostLibrarySemPreferredLanguageFicaNil cobre o default (campo
// omitido): preferred_language fica nil, comportamento idêntico a antes do
// briefing 0084.
func TestPostLibrarySemPreferredLanguageFicaNil(t *testing.T) {
	h := setupLibraryServer(t)
	got := criarOK(t, h, t.TempDir(), "movie")
	if got.PreferredLanguage != nil {
		t.Errorf("preferred_language = %v, quero nil (omitido no cadastro)", *got.PreferredLanguage)
	}
}

// TestPostLibraryPreferredLanguageInvalido cobre o 422 invalid_body quando o
// código não está em app_settings.languages.allowed.
func TestPostLibraryPreferredLanguageInvalido(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/library",
		map[string]string{"path": t.TempDir(), "type": "movie", "preferred_language": "xx-XX"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPatchLibraryPreferredLanguageDefineELimpa cobre o ciclo completo do
// briefing 0084: PATCH com preferred_language define; `null` explícito
// limpa; campo AUSENTE deixa inalterado.
func TestPatchLibraryPreferredLanguageDefineELimpa(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"preferred_language": "en"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH preferred_language=en: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeSource(t, rec); got.PreferredLanguage == nil || *got.PreferredLanguage != "en" {
		t.Errorf("preferred_language = %v, quero \"en\"", got.PreferredLanguage)
	}

	// Campo AUSENTE (só outro campo qualquer no corpo) não altera o valor atual.
	recInalterado := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"watch_enabled": true})
	if got := decodeSource(t, recInalterado); got.PreferredLanguage == nil || *got.PreferredLanguage != "en" {
		t.Errorf("preferred_language após PATCH sem o campo = %v, quero continuar \"en\"", got.PreferredLanguage)
	}

	// `null` explícito LIMPA a preferência.
	recNull := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, []byte(`{"preferred_language":null}`))
	if recNull.Code != http.StatusOK {
		t.Fatalf("PATCH preferred_language=null: status = %d, quero 200 (corpo=%s)", recNull.Code, recNull.Body.String())
	}
	if got := decodeSource(t, recNull); got.PreferredLanguage != nil {
		t.Errorf("preferred_language após null explícito = %v, quero nil", *got.PreferredLanguage)
	}

	// Reflete em GET (persistiu de verdade).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); got.PreferredLanguage != nil {
		t.Errorf("GET pós-PATCH: preferred_language = %v, quero nil (persistido)", *got.PreferredLanguage)
	}
}

// TestPatchLibraryPreferredLanguageInvalido cobre o 422 invalid_body no PATCH.
func TestPatchLibraryPreferredLanguageInvalido(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"preferred_language": "xx-XX"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")

	// Nada foi persistido (a validação falhou antes de tocar o banco).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); got.PreferredLanguage != nil {
		t.Errorf("preferred_language após PATCH inválido = %v, quero nil (não persistido)", *got.PreferredLanguage)
	}
}

// ── retroactive_enrichment_days (briefing 0085) ─────────────────────────────

// TestPostLibraryComRetroactiveEnrichmentDays cobre o cadastro com override
// válido (briefing 0085).
func TestPostLibraryComRetroactiveEnrichmentDays(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/library",
		map[string]any{"path": t.TempDir(), "type": "movie", "retroactive_enrichment_days": 3})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeSource(t, rec)
	if got.RetroactiveEnrichmentDays == nil || *got.RetroactiveEnrichmentDays != 3 {
		t.Errorf("retroactive_enrichment_days = %v, quero 3", got.RetroactiveEnrichmentDays)
	}
}

// TestPostLibrarySemRetroactiveEnrichmentDaysFicaNil cobre o default (campo
// omitido): retroactive_enrichment_days fica nil (herda o global).
func TestPostLibrarySemRetroactiveEnrichmentDaysFicaNil(t *testing.T) {
	h := setupLibraryServer(t)
	got := criarOK(t, h, t.TempDir(), "movie")
	if got.RetroactiveEnrichmentDays != nil {
		t.Errorf("retroactive_enrichment_days = %v, quero nil (omitido no cadastro)", *got.RetroactiveEnrichmentDays)
	}
}

// TestPostLibraryRetroactiveEnrichmentDaysInvalido cobre o 422 invalid_body
// quando o valor está fora de [1, 365].
func TestPostLibraryRetroactiveEnrichmentDaysInvalido(t *testing.T) {
	h := setupLibraryServer(t)
	for _, v := range []int{0, 366, -5} {
		rec := do(t, h, http.MethodPost, "/api/v1/library",
			map[string]any{"path": t.TempDir(), "type": "movie", "retroactive_enrichment_days": v})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("valor %d: status = %d, quero 422 (corpo=%s)", v, rec.Code, rec.Body.String())
		}
		checkErrorShape(t, rec, "invalid_body")
	}
}

// TestPatchLibraryRetroactiveEnrichmentDaysDefineELimpa cobre o ciclo
// completo do briefing 0085: PATCH com retroactive_enrichment_days define;
// `null` explícito limpa; campo AUSENTE deixa inalterado.
func TestPatchLibraryRetroactiveEnrichmentDaysDefineELimpa(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"retroactive_enrichment_days": 5})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH retroactive_enrichment_days=5: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeSource(t, rec); got.RetroactiveEnrichmentDays == nil || *got.RetroactiveEnrichmentDays != 5 {
		t.Errorf("retroactive_enrichment_days = %v, quero 5", got.RetroactiveEnrichmentDays)
	}

	// Campo AUSENTE (só outro campo qualquer no corpo) não altera o valor atual.
	recInalterado := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"watch_enabled": true})
	if got := decodeSource(t, recInalterado); got.RetroactiveEnrichmentDays == nil || *got.RetroactiveEnrichmentDays != 5 {
		t.Errorf("retroactive_enrichment_days após PATCH sem o campo = %v, quero continuar 5", got.RetroactiveEnrichmentDays)
	}

	// `null` explícito LIMPA o override.
	recNull := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, []byte(`{"retroactive_enrichment_days":null}`))
	if recNull.Code != http.StatusOK {
		t.Fatalf("PATCH retroactive_enrichment_days=null: status = %d, quero 200 (corpo=%s)", recNull.Code, recNull.Body.String())
	}
	if got := decodeSource(t, recNull); got.RetroactiveEnrichmentDays != nil {
		t.Errorf("retroactive_enrichment_days após null explícito = %v, quero nil", *got.RetroactiveEnrichmentDays)
	}

	// Reflete em GET (persistiu de verdade).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); got.RetroactiveEnrichmentDays != nil {
		t.Errorf("GET pós-PATCH: retroactive_enrichment_days = %v, quero nil (persistido)", *got.RetroactiveEnrichmentDays)
	}
}

// TestPatchLibraryRetroactiveEnrichmentDaysInvalido cobre o 422 invalid_body
// no PATCH.
func TestPatchLibraryRetroactiveEnrichmentDaysInvalido(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"retroactive_enrichment_days": 400})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")

	// Nada foi persistido (a validação falhou antes de tocar o banco).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); got.RetroactiveEnrichmentDays != nil {
		t.Errorf("retroactive_enrichment_days após PATCH inválido = %v, quero nil (não persistido)", *got.RetroactiveEnrichmentDays)
	}
}

// TestPatchLibraryPathValido cobre o núcleo do briefing 0070: PATCH com
// `path` válido reaponta a fonte (200, path novo, state online) sem re-scan.
func TestPatchLibraryPathValido(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")
	novoDir := t.TempDir()

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"path": novoDir})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH path: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeSource(t, rec)
	if got.Path != novoDir {
		t.Errorf("path = %q, quero %q", got.Path, novoDir)
	}
	if got.State != "online" {
		t.Errorf("state = %q, quero online", got.State)
	}

	// Reflete em GET (persistiu de verdade).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if gotGet := decodeSource(t, get); gotGet.Path != novoDir {
		t.Errorf("path no GET pos-PATCH = %q, quero %q (persistido)", gotGet.Path, novoDir)
	}
}

// TestPatchLibraryPathNoOp cobre o no-op (path novo IGUAL ao atual): 200, nada muda.
func TestPatchLibraryPathNoOp(t *testing.T) {
	h := setupLibraryServer(t)
	dir := t.TempDir()
	src := criarOK(t, h, dir, "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"path": dir})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeSource(t, rec); got.Path != dir {
		t.Errorf("path = %q, quero %q (no-op)", got.Path, dir)
	}
}

// TestPatchLibraryPathDuplicado cobre o 409 duplicate_path.
func TestPatchLibraryPathDuplicado(t *testing.T) {
	h := setupLibraryServer(t)
	srcA := criarOK(t, h, t.TempDir(), "movie")
	dirB := t.TempDir()
	criarOK(t, h, dirB, "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+srcA.ID, map[string]any{"path": dirB})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, quero 409 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "duplicate_path")
}

// TestPatchLibraryPathInexistente cobre o 422 invalid_path.
func TestPatchLibraryPathInexistente(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")
	inexistente := filepath.Join(t.TempDir(), "nao-existe")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"path": inexistente})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_path")
}

// TestPatchLibraryPathNaoDiretorio cobre o 422 not_a_directory.
func TestPatchLibraryPathNaoDiretorio(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")
	arquivo := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(arquivo, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"path": arquivo})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_a_directory")
}

// TestPatchLibraryPathVazio cobre o 400 invalid_body (path == "").
func TestPatchLibraryPathVazio(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"path": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPatchLibraryPathIdInexistente cobre o 404 de um id inexistente.
func TestPatchLibraryPathIdInexistente(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodPatch, "/api/v1/library/naoexiste", map[string]any{"path": t.TempDir()})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestPatchLibraryPathEWatchEnabledJuntos cobre o PATCH parcial compondo os
// dois campos no mesmo corpo (briefing 0070): path é aplicado primeiro
// (recasco), watch_enabled depois -- a resposta final reflete os dois.
func TestPatchLibraryPathEWatchEnabledJuntos(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")
	novoDir := t.TempDir()

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID,
		map[string]any{"path": novoDir, "watch_enabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeSource(t, rec)
	if got.Path != novoDir {
		t.Errorf("path = %q, quero %q", got.Path, novoDir)
	}
	if !got.WatchEnabled {
		t.Error("watch_enabled = false, quero true")
	}

	// Persistiu os dois campos.
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	gotGet := decodeSource(t, get)
	if gotGet.Path != novoDir || !gotGet.WatchEnabled {
		t.Errorf("estado pos-PATCH nao persistiu: %+v", gotGet)
	}
}

// TestBrowseLibraryPathListaSubpastas cobre o 200 do navegador de pastas
// (briefing 0071): lista só diretórios do path pedido, ordenados, com parent
// não-nil (o tempdir não é raiz).
func TestBrowseLibraryPathListaSubpastas(t *testing.T) {
	h := setupLibraryServer(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "arquivo.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/library/browse?path="+dir, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got browseResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar browseResultDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	if got.Path != dir {
		t.Errorf("path = %q, quero %q", got.Path, dir)
	}
	if got.Parent == nil || *got.Parent != filepath.Dir(dir) {
		t.Errorf("parent = %v, quero %q", got.Parent, filepath.Dir(dir))
	}
	if got.Truncated {
		t.Error("truncated = true, quero false")
	}
	if len(got.Entries) != 1 || got.Entries[0].Name != "sub" {
		t.Fatalf("entries = %+v, quero só [sub] (arquivo comum ignorado)", got.Entries)
	}
	if !got.Entries[0].Readable {
		t.Error("readable = false, quero true")
	}
}

// TestBrowseLibraryPathRaizDefault cobre path ausente/vazio -> raiz "/".
func TestBrowseLibraryPathRaizDefault(t *testing.T) {
	h := setupLibraryServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/library/browse", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got browseResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decodificar browseResultDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	if got.Path != "/" {
		t.Errorf("path = %q, quero /", got.Path)
	}
	if got.Parent != nil {
		t.Errorf("parent = %v, quero nil (raiz)", *got.Parent)
	}
}

// TestBrowseLibraryPathInexistente cobre o 422 invalid_path.
func TestBrowseLibraryPathInexistente(t *testing.T) {
	h := setupLibraryServer(t)
	inexistente := filepath.Join(t.TempDir(), "nao-existe")

	rec := do(t, h, http.MethodGet, "/api/v1/library/browse?path="+inexistente, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_path")
}

// TestBrowseLibraryPathNaoDiretorio cobre o 422 not_a_directory.
func TestBrowseLibraryPathNaoDiretorio(t *testing.T) {
	h := setupLibraryServer(t)
	arquivo := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(arquivo, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/library/browse?path="+arquivo, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_a_directory")
}

// TestBrowseLibraryPathRotaEstaticaNaoColideComWildcardID garante que
// "/browse" cai no handler dedicado, e não em getLibrary com id="browse"
// (briefing 0071: a rota estática precisa vencer o wildcard "/{id}").
func TestBrowseLibraryPathRotaEstaticaNaoColideComWildcardID(t *testing.T) {
	h := setupLibraryServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/library/browse", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (se caísse em getLibrary com id=browse, seria 404) (corpo=%s)", rec.Code, rec.Body.String())
	}
	var got browseResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("corpo não é um browseResultDTO (indício de rota errada): %v (corpo=%s)", err, rec.Body.String())
	}
}

func TestApiNotFoundShapeNovo(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/rota-inexistente", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}
