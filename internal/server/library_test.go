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

// setupLibraryServer starts a Server with a temporary migrated database and returns the
// root handler (routes mounted) to exercise /api/v1/library end to end.
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

// do executes a request against the handler and returns the recorder.
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

// criarOK performs POST /api/v1/library expecting 201 and returns the created
// record.
func criarOK(t *testing.T, h http.Handler, path, tipo string) sourceDTO {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"path": path, "type": tipo})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %q status = %d, quero 201 (corpo=%s)", path, rec.Code, rec.Body.String())
	}
	return decodeSource(t, rec)
}

// decodeSource decodes a sourceDTO from the body.
func decodeSource(t *testing.T, rec *httptest.ResponseRecorder) sourceDTO {
	t.Helper()
	var s sourceDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("decodificar sourceDTO: %v (corpo=%s)", err, rec.Body.String())
	}
	return s
}

// checkErrorShape validates that the body follows {error:{code,message}} and that the code
// matches the expected one.
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
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // restore so TempDir can clean up

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

	// Same path through a NON-clean form (/dir/.. -> parent; /parent/base -> dir):
	// after normalization it becomes the same path and must collide.
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

	// absent path.
	rec := do(t, h, http.MethodPost, "/api/v1/library", map[string]string{"type": "movie"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("path ausente: status = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_body")

	// Malformed JSON.
	rec2 := do(t, h, http.MethodPost, "/api/v1/library", []byte("{isto nao e json"))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("json malformado: status = %d, quero 400", rec2.Code)
	}
	checkErrorShape(t, rec2, "invalid_body")
}

func TestGetLibraryListaComStateReavaliado(t *testing.T) {
	h := setupLibraryServer(t)
	// dir that we will remove to become offline.
	dirRemovido := filepath.Join(t.TempDir(), "some")
	if err := os.Mkdir(dirRemovido, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	dirVivo := t.TempDir()

	criarOK(t, h, dirRemovido, "movie")
	criarOK(t, h, dirVivo, "tv")

	// Removes the dir from disk: on read its state must become offline.
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

	// 200 on an existing id.
	recGet := do(t, h, http.MethodGet, "/api/v1/library/"+criado.ID, nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /{id} status = %d, quero 200", recGet.Code)
	}
	got := decodeSource(t, recGet)
	if got.ID != criado.ID || got.Path != dir {
		t.Errorf("registro divergente: %+v", got)
	}

	// 404 on a nonexistent id.
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

	// DELETE existing -> 204 without body.
	recDel := do(t, h, http.MethodDelete, "/api/v1/library/"+criado.ID, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, quero 204", recDel.Code)
	}
	if recDel.Body.Len() != 0 {
		t.Errorf("corpo do 204 não vazio: %q", recDel.Body.String())
	}

	// It disappeared: GET now 404.
	if recGet := do(t, h, http.MethodGet, "/api/v1/library/"+criado.ID, nil); recGet.Code != http.StatusNotFound {
		t.Errorf("GET após DELETE status = %d, quero 404", recGet.Code)
	}

	// DELETE nonexistent -> 404.
	recDel404 := do(t, h, http.MethodDelete, "/api/v1/library/nao-existe", nil)
	if recDel404.Code != http.StatusNotFound {
		t.Fatalf("DELETE inexistente status = %d, quero 404", recDel404.Code)
	}
	checkErrorShape(t, recDel404, "not_found")
}

// TestApiNotFoundShapeNovo guarantees that the generic /api 404 migrated to the
// standard shape {error:{code,message}} (it used to be {"error":"not found"}).
// TestPatchLibraryWatchEnabled covers the toggle: PATCH turns watch_enabled on, persists
// (it reflects in GET), and turns it off again -- without s.watch configured
// (setupLibraryServer does not use WithWatch), the aplicarWatchToggle routine
// degrades silently (s.watch==nil guard), only the persistence is exercised
// here.
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

	// Reflects in GET (it persisted for real, not only in the PATCH response).
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

// TestPatchLibraryCorpoVazioSoDevolveEstado covers the case "PATCH without
// watch_enabled in the body": it touches nothing, it only returns the current
// state.
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

// TestPatchLibraryNaoEncontrado covers the 404 of a nonexistent source.
func TestPatchLibraryNaoEncontrado(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodPatch, "/api/v1/library/naoexiste", map[string]any{"watch_enabled": true})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestPatchLibraryCorpoMalformado covers the 422 of a body that is not valid
// JSON.
func TestPatchLibraryCorpoMalformado(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, []byte("{nao-e-json"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPostLibraryComPreferredLanguage covers the registration with preferred_language:
// "en" is in the default allowed list (defaultLanguageSettings) and
// goes in without requiring PUT /settings/languages first.
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

// TestPostLibrarySemPreferredLanguageFicaNil covers the default (omitted field):
// preferred_language stays nil, behavior identical to before.
func TestPostLibrarySemPreferredLanguageFicaNil(t *testing.T) {
	h := setupLibraryServer(t)
	got := criarOK(t, h, t.TempDir(), "movie")
	if got.PreferredLanguage != nil {
		t.Errorf("preferred_language = %v, quero nil (omitido no cadastro)", *got.PreferredLanguage)
	}
}

// TestPostLibraryPreferredLanguageInvalido covers the 422 invalid_body when the
// code is not in app_settings.languages.allowed.
func TestPostLibraryPreferredLanguageInvalido(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/library",
		map[string]string{"path": t.TempDir(), "type": "movie", "preferred_language": "xx-XX"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPatchLibraryPreferredLanguageDefineELimpa covers the complete cycle:
// PATCH with preferred_language defines it; an explicit `null`
// clears it; an ABSENT field leaves it unchanged.
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

	// An ABSENT field (only some other field in the body) does not change the current
	// value.
	recInalterado := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"watch_enabled": true})
	if got := decodeSource(t, recInalterado); got.PreferredLanguage == nil || *got.PreferredLanguage != "en" {
		t.Errorf("preferred_language após PATCH sem o campo = %v, quero continuar \"en\"", got.PreferredLanguage)
	}

	// An explicit `null` CLEARS the preference.
	recNull := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, []byte(`{"preferred_language":null}`))
	if recNull.Code != http.StatusOK {
		t.Fatalf("PATCH preferred_language=null: status = %d, quero 200 (corpo=%s)", recNull.Code, recNull.Body.String())
	}
	if got := decodeSource(t, recNull); got.PreferredLanguage != nil {
		t.Errorf("preferred_language após null explícito = %v, quero nil", *got.PreferredLanguage)
	}

	// Reflects in GET (it persisted for real).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); got.PreferredLanguage != nil {
		t.Errorf("GET pós-PATCH: preferred_language = %v, quero nil (persistido)", *got.PreferredLanguage)
	}
}

// TestPatchLibraryPreferredLanguageInvalido covers the 422 invalid_body on
// PATCH.
func TestPatchLibraryPreferredLanguageInvalido(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"preferred_language": "xx-XX"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")

	// Nothing was persisted (the validation failed before touching the database).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); got.PreferredLanguage != nil {
		t.Errorf("preferred_language após PATCH inválido = %v, quero nil (não persistido)", *got.PreferredLanguage)
	}
}

// ── retroactive_enrichment_days ─────────────────────────────

// TestPostLibraryComRetroactiveEnrichmentDays covers the registration with a valid
// override.
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

// TestPostLibrarySemRetroactiveEnrichmentDaysFicaNil covers the default (omitted field):
// retroactive_enrichment_days stays nil (it inherits the global one).
func TestPostLibrarySemRetroactiveEnrichmentDaysFicaNil(t *testing.T) {
	h := setupLibraryServer(t)
	got := criarOK(t, h, t.TempDir(), "movie")
	if got.RetroactiveEnrichmentDays != nil {
		t.Errorf("retroactive_enrichment_days = %v, quero nil (omitido no cadastro)", *got.RetroactiveEnrichmentDays)
	}
}

// TestPostLibraryRetroactiveEnrichmentDaysInvalido covers the 422 invalid_body
// when the value is outside [1, 365].
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

// TestPatchLibraryRetroactiveEnrichmentDaysDefineELimpa covers the complete
// cycle: PATCH with retroactive_enrichment_days defines it; an explicit `null`
// clears it; an ABSENT field leaves it unchanged.
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

	// An ABSENT field (only some other field in the body) does not change the current
	// value.
	recInalterado := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"watch_enabled": true})
	if got := decodeSource(t, recInalterado); got.RetroactiveEnrichmentDays == nil || *got.RetroactiveEnrichmentDays != 5 {
		t.Errorf("retroactive_enrichment_days após PATCH sem o campo = %v, quero continuar 5", got.RetroactiveEnrichmentDays)
	}

	// An explicit `null` CLEARS the override.
	recNull := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, []byte(`{"retroactive_enrichment_days":null}`))
	if recNull.Code != http.StatusOK {
		t.Fatalf("PATCH retroactive_enrichment_days=null: status = %d, quero 200 (corpo=%s)", recNull.Code, recNull.Body.String())
	}
	if got := decodeSource(t, recNull); got.RetroactiveEnrichmentDays != nil {
		t.Errorf("retroactive_enrichment_days após null explícito = %v, quero nil", *got.RetroactiveEnrichmentDays)
	}

	// Reflects in GET (it persisted for real).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); got.RetroactiveEnrichmentDays != nil {
		t.Errorf("GET pós-PATCH: retroactive_enrichment_days = %v, quero nil (persistido)", *got.RetroactiveEnrichmentDays)
	}
}

// TestPatchLibraryRetroactiveEnrichmentDaysInvalido covers the 422 invalid_body on
// PATCH.
func TestPatchLibraryRetroactiveEnrichmentDaysInvalido(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"retroactive_enrichment_days": 400})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")

	// Nothing was persisted (the validation failed before touching the database).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if got := decodeSource(t, get); got.RetroactiveEnrichmentDays != nil {
		t.Errorf("retroactive_enrichment_days após PATCH inválido = %v, quero nil (não persistido)", *got.RetroactiveEnrichmentDays)
	}
}

// TestPatchLibraryPathValido covers the core: PATCH with a valid `path`
// re-points the source (200, new path, online state) without a re-scan.
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

	// Reflects in GET (it persisted for real).
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	if gotGet := decodeSource(t, get); gotGet.Path != novoDir {
		t.Errorf("path no GET pos-PATCH = %q, quero %q (persistido)", gotGet.Path, novoDir)
	}
}

// TestPatchLibraryPathNoOp covers the no-op (new path EQUAL to the current one): 200,
// nothing changes.
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

// TestPatchLibraryPathDuplicado covers the 409 duplicate_path.
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

// TestPatchLibraryPathInexistente covers the 422 invalid_path.
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

// TestPatchLibraryPathNaoDiretorio covers the 422 not_a_directory.
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

// TestPatchLibraryPathVazio covers the 400 invalid_body (path == "").
func TestPatchLibraryPathVazio(t *testing.T) {
	h := setupLibraryServer(t)
	src := criarOK(t, h, t.TempDir(), "movie")

	rec := do(t, h, http.MethodPatch, "/api/v1/library/"+src.ID, map[string]any{"path": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPatchLibraryPathIdInexistente covers the 404 of a nonexistent id.
func TestPatchLibraryPathIdInexistente(t *testing.T) {
	h := setupLibraryServer(t)
	rec := do(t, h, http.MethodPatch, "/api/v1/library/naoexiste", map[string]any{"path": t.TempDir()})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestPatchLibraryPathEWatchEnabledJuntos covers the partial PATCH composing the
// two fields in the same body: path is applied first (re-root), watch_enabled
// afterwards -- the final response reflects both.
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

	// Persisted both fields.
	get := do(t, h, http.MethodGet, "/api/v1/library/"+src.ID, nil)
	gotGet := decodeSource(t, get)
	if gotGet.Path != novoDir || !gotGet.WatchEnabled {
		t.Errorf("estado pos-PATCH nao persistiu: %+v", gotGet)
	}
}

// TestBrowseLibraryPathListaSubpastas covers the 200 of the folder browser:
// it lists only directories of the requested path, sorted, with a non-nil parent
// (the tempdir is not a root).
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

// TestBrowseLibraryPathRaizDefault covers an absent/empty path -> root "/".
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

// TestBrowseLibraryPathInexistente covers the 422 invalid_path.
func TestBrowseLibraryPathInexistente(t *testing.T) {
	h := setupLibraryServer(t)
	inexistente := filepath.Join(t.TempDir(), "nao-existe")

	rec := do(t, h, http.MethodGet, "/api/v1/library/browse?path="+inexistente, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_path")
}

// TestBrowseLibraryPathNaoDiretorio covers the 422 not_a_directory.
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

// TestBrowseLibraryPathRotaEstaticaNaoColideComWildcardID guarantees that
// "/browse" falls into the dedicated handler, and not into getLibrary with id="browse"
// (the static route has to beat the "/{id}" wildcard).
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
