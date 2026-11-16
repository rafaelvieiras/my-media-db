package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
	"github.com/rafaelvieiras/my-media-db/internal/ytdlp"
)

// decodeLangs decodes the body of GET/PUT /settings/languages.
func decodeLangs(t *testing.T, body []byte) languageSettings {
	t.Helper()
	var out languageSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar languageSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetLanguageSettingsDefault covers: without a row in app_settings, the GET returns
// the safe default (pt-BR / en / [pt-BR, en]).
func TestGetLanguageSettingsDefault(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/settings/languages", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeLangs(t, rec.Body.Bytes())
	eq(t, "primary", got.Primary, "pt-BR")
	eq(t, "fallback", got.Fallback, "en")
	if len(got.Allowed) != 2 || got.Allowed[0] != "pt-BR" || got.Allowed[1] != "en" {
		t.Errorf("allowed = %v, quero [pt-BR en]", got.Allowed)
	}
}

// TestPutLanguageSettingsPersiste covers: a valid PUT writes it and the following GET
// reflects it (cache invalidated).
func TestPutLanguageSettingsPersiste(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := languageSettings{Primary: "pt-BR", Fallback: "en", Allowed: []string{"pt-BR", "en", "es"}}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/languages", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	// GET reflects the new allowed (persisted + cache invalidated).
	recGet := do(t, h, http.MethodGet, "/api/v1/settings/languages", nil)
	got := decodeLangs(t, recGet.Body.Bytes())
	if len(got.Allowed) != 3 || got.Allowed[2] != "es" {
		t.Errorf("allowed após PUT = %v, quero [pt-BR en es]", got.Allowed)
	}
}

// TestPutLanguageSettingsInvalida covers: validation (primary/fallback ⊆
// allowed; non-empty codes) responds 422 in the standard envelope.
func TestPutLanguageSettingsInvalida(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	casos := []languageSettings{
		{Primary: "fr", Fallback: "en", Allowed: []string{"pt-BR", "en"}},    // primary outside
		{Primary: "pt-BR", Fallback: "fr", Allowed: []string{"pt-BR", "en"}}, // fallback outside
		{Primary: "pt-BR", Fallback: "en", Allowed: []string{}},              // no allowed
		{Primary: "", Fallback: "en", Allowed: []string{"pt-BR", "en", ""}},  // empty code
	}
	for i, c := range casos {
		rec := do(t, h, http.MethodPut, "/api/v1/settings/languages", c)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("caso %d: status = %d, quero 422 (corpo=%s)", i, rec.Code, rec.Body.String())
		}
		checkErrorShape(t, rec, "invalid_body")
	}
}

// ── Retroactive enrichment ───────────────────────────────

// decodeRetro decodes the body of GET/PUT /settings/enrichment-retroativo.
func decodeRetro(t *testing.T, body []byte) retroactiveEnrichmentSettings {
	t.Helper()
	var out retroactiveEnrichmentSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar retroactiveEnrichmentSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetEnrichmentRetroativoSettingsDefault covers the criterion: without a row
// in app_settings, the GET returns the safe default {retry_days: 15}.
func TestGetEnrichmentRetroativoSettingsDefault(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/settings/enrichment-retroativo", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeRetro(t, rec.Body.Bytes())
	if got.RetryDays != 15 {
		t.Errorf("retry_days = %d, quero 15", got.RetryDays)
	}
}

// TestPutEnrichmentRetroativoSettingsPersiste covers the criterion: a valid PUT
// writes it and the following GET reflects it (cache invalidated, without restarting the
// server).
func TestPutEnrichmentRetroativoSettingsPersiste(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := retroactiveEnrichmentSettings{RetryDays: 7}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/enrichment-retroativo", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	recGet := do(t, h, http.MethodGet, "/api/v1/settings/enrichment-retroativo", nil)
	got := decodeRetro(t, recGet.Body.Bytes())
	if got.RetryDays != 7 {
		t.Errorf("retry_days após PUT = %d, quero 7", got.RetryDays)
	}
}

// TestPutEnrichmentRetroativoSettingsInvalida covers the criterion: retry_days
// outside [1, 365] → 422 invalid_body, previous value preserved.
func TestPutEnrichmentRetroativoSettingsInvalida(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	casos := []retroactiveEnrichmentSettings{
		{RetryDays: 0},
		{RetryDays: 400},
		{RetryDays: -1},
	}
	for i, c := range casos {
		rec := do(t, h, http.MethodPut, "/api/v1/settings/enrichment-retroativo", c)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("caso %d: status = %d, quero 422 (corpo=%s)", i, rec.Code, rec.Body.String())
		}
		checkErrorShape(t, rec, "invalid_body")
	}

	// Previous value (default 15) preserved -- no invalid PUT above
	// should have written anything.
	recGet := do(t, h, http.MethodGet, "/api/v1/settings/enrichment-retroativo", nil)
	got := decodeRetro(t, recGet.Body.Bytes())
	if got.RetryDays != 15 {
		t.Errorf("retry_days após PUTs inválidos = %d, quero 15 (default preservado)", got.RetryDays)
	}
}

// TestPutEnrichmentRetroativoSettingsCorpoMalformado covers the criterion:
// invalid JSON body → 400 invalid_body.
func TestPutEnrichmentRetroativoSettingsCorpoMalformado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/settings/enrichment-retroativo", []byte(`{`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// ── Automatic artwork materialization ──────────────────────

// decodeAutoMaterialize decodes the body of GET/PUT
// /settings/auto-materialize-artwork.
func decodeAutoMaterialize(t *testing.T, body []byte) autoMaterializeArtworkSettings {
	t.Helper()
	var out autoMaterializeArtworkSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar autoMaterializeArtworkSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetAutoMaterializeArtworkSettingsDefault covers the criterion: without a row
// in app_settings, the GET returns the safe default {enabled:false}.
func TestGetAutoMaterializeArtworkSettingsDefault(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/settings/auto-materialize-artwork", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMaterialize(t, rec.Body.Bytes())
	if got.Enabled {
		t.Error("enabled = true, quero false (default de fábrica)")
	}
}

// TestPutAutoMaterializeArtworkSettingsPersiste covers the criterion: the PUT
// writes it and the following GET reflects it (cache invalidated).
func TestPutAutoMaterializeArtworkSettingsPersiste(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := autoMaterializeArtworkSettings{Enabled: true}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/auto-materialize-artwork", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMaterialize(t, rec.Body.Bytes())
	if !got.Enabled {
		t.Error("resposta do PUT: enabled = false, quero true")
	}

	recGet := do(t, h, http.MethodGet, "/api/v1/settings/auto-materialize-artwork", nil)
	gotGet := decodeAutoMaterialize(t, recGet.Body.Bytes())
	if !gotGet.Enabled {
		t.Error("GET após PUT: enabled = false, quero true (cache invalidado)")
	}
}

// TestPutAutoMaterializeArtworkSettingsCorpoMalformado covers the criterion:
// invalid JSON body → 400 invalid_body.
func TestPutAutoMaterializeArtworkSettingsCorpoMalformado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/settings/auto-materialize-artwork", []byte(`{`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestGetEntityLangInvalido covers: ?lang= outside the allowed ones → 400
// invalid_query.
func TestGetEntityLangInvalido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Filme")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e?lang=fr", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_query")
}

// TestListEntitiesLangInvalido covers: invalid ?lang= in the list → 400.
func TestListEntitiesLangInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities?lang=fr", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_query")
}

// TestGetEntityResolvedLanguages covers: the detail brings resolved_languages with the
// effective language per field and the shape stays flat (title is a string, not an
// object).
func TestGetEntityResolvedLanguages(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Filme")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	// Flat shape: title is a string.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	var titulo string
	if err := json.Unmarshal(raw["title"], &titulo); err != nil {
		t.Errorf("title não é string plana: %s", raw["title"])
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar Entidade: %v", err)
	}
	eq(t, "lang eco", ent.Lang, "pt-BR")
	if ent.ResolvedLanguages == nil {
		t.Fatal("resolved_languages ausente, quero presente")
	}
	// title falls back to the file base (neutral) → provenance "".
	if prov, ok := ent.ResolvedLanguages["title"]; !ok || prov != "" {
		t.Errorf("resolved_languages[title] = %q (ok=%v), quero \"\"", prov, ok)
	}
}

// TestGetEntityFallbackNoBanco covers the end-to-end wiring: with pt-BR/en settings and
// the external one in en, the pt-BR request falls back to en (en provenance).
func TestGetEntityFallbackNoBanco(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Titulo Arquivo")
	// Override with a title only in english (per-language format); pt-BR absent → falls
	// back to en.
	_, err := escrita(t, d).UpsertMediaOverride(context.Background(), sqlcgen.UpsertMediaOverrideParams{
		EntityID:     "e",
		OverrideJson: nsOf(`{"title":{"en":"English Title"}}`),
	})
	if err != nil {
		t.Fatalf("UpsertMediaOverride: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var ent resolver.Entidade
	if err := json.Unmarshal(rec.Body.Bytes(), &ent); err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	eq(t, "title(fallback en)", ent.Title, "English Title")
	eq(t, "prov title", ent.ResolvedLanguages["title"], "en")
}

// ── yt-dlp ────────────────────────────────────────

// escreverScriptExecutavel writes a minimal executable file at `path` --
// the settings.go tests only need the file to EXIST (Status performs
// os.Stat on the custom path, without running --version); content validation
// is already covered by the tests of the internal/ytdlp package itself.
func escreverScriptExecutavel(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// setupYtdlpServer starts a Server with a temporary migrated database and a
// ytdlp.Gerenciador injected (envPath already resolved, as main.go would do with
// MYMEDIADB_YTDLP_PATH), to exercise GET/PUT /settings/ytdlp end to
// end.
func setupYtdlpServer(t *testing.T, envPath string) (http.Handler, *db.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ytdlp.sqlite")
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	g := ytdlp.Novo(t.TempDir(), envPath)
	h, err := New("v-teste", database, WithYtdlp(g)).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h, database
}

// decodeYtdlp decodes the body of GET/PUT /settings/ytdlp.
func decodeYtdlp(t *testing.T, body []byte) ytdlpStatusResponse {
	t.Helper()
	var out ytdlpStatusResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar ytdlpStatusResponse: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetYtdlpSettingsSemGerenciador covers the defensive case: without a
// ytdlp.Gerenciador injected (setupEntitiesServer does not inject one), GET
// /settings/ytdlp degrades to unavailable/"none" instead of panicking --
// no other endpoint of the product breaks because of it.
func TestGetYtdlpSettingsSemGerenciador(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/settings/ytdlp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeYtdlp(t, rec.Body.Bytes())
	if got.Disponivel {
		t.Error("disponivel = true, quero false (sem Gerenciador)")
	}
	if got.Origem != "nenhum" {
		t.Errorf("origem = %q, quero nenhum", got.Origem)
	}
	if got.Detalhe == nil || *got.Detalhe == "" {
		t.Error("detalhe ausente, quero o motivo")
	}
}

// TestGetYtdlpSettingsOrigemEnv covers the criterion: with MYMEDIADB_YTDLP_PATH
// set (env, here already resolved and passed to the Gerenciador in WithYtdlp),
// GET /settings/ytdlp reports origem="custom" using that path, without
// custom_binary_path (empty database).
func TestGetYtdlpSettingsOrigemEnv(t *testing.T) {
	envPath := escreverScriptExecutavel(t, filepath.Join(t.TempDir(), "env-yt-dlp"))
	h, _ := setupYtdlpServer(t, envPath)

	rec := do(t, h, http.MethodGet, "/api/v1/settings/ytdlp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeYtdlp(t, rec.Body.Bytes())
	if !got.Disponivel || got.Origem != "custom" {
		t.Errorf("(disponivel, origem) = (%v, %q), quero (true, custom)", got.Disponivel, got.Origem)
	}
	if got.CustomBinaryPath != nil {
		t.Errorf("custom_binary_path = %v, quero nil (nada gravado no banco ainda)", *got.CustomBinaryPath)
	}
}

// TestPutYtdlpSettingsBancoVenceSobreEnv covers the criterion: with
// custom_binary_path written via PUT /settings/ytdlp (database), that path wins
// over the env var -- proven here by pointing the env var at a NONEXISTENT file
// (unavailable before the PUT) and the database at a real file
// (available after the PUT), with IMMEDIATE effect (same execution of the
// process, without restarting).
func TestPutYtdlpSettingsBancoVenceSobreEnv(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "nao-existe-env")
	h, _ := setupYtdlpServer(t, envPath)

	// Before the PUT: only the env var (nonexistent) is configured -> unavailable.
	recAntes := do(t, h, http.MethodGet, "/api/v1/settings/ytdlp", nil)
	gotAntes := decodeYtdlp(t, recAntes.Body.Bytes())
	if gotAntes.Disponivel {
		t.Fatal("disponivel = true antes do PUT, quero false (env aponta pra arquivo inexistente)")
	}

	dbPath := escreverScriptExecutavel(t, filepath.Join(t.TempDir(), "db-yt-dlp"))
	rec := do(t, h, http.MethodPut, "/api/v1/settings/ytdlp", ytdlpPathSettings{CustomBinaryPath: &dbPath})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeYtdlp(t, rec.Body.Bytes())
	if !got.Disponivel || got.Origem != "custom" {
		t.Errorf("resposta do PUT: (disponivel, origem) = (%v, %q), quero (true, custom)", got.Disponivel, got.Origem)
	}
	if got.CustomBinaryPath == nil || *got.CustomBinaryPath != dbPath {
		t.Errorf("custom_binary_path = %v, quero %q", got.CustomBinaryPath, dbPath)
	}

	// The following GET reflects the same state (persisted + cache invalidated).
	recGet := do(t, h, http.MethodGet, "/api/v1/settings/ytdlp", nil)
	gotGet := decodeYtdlp(t, recGet.Body.Bytes())
	if !gotGet.Disponivel || gotGet.CustomBinaryPath == nil || *gotGet.CustomBinaryPath != dbPath {
		t.Errorf("GET apos PUT = %+v, quero disponivel=true custom_binary_path=%q", gotGet, dbPath)
	}
}

// TestPutYtdlpSettingsLimpaVoltaParaEnv covers the criterion (clearing the
// override): PUT with custom_binary_path=null deletes the path from the database and
// gives control back to the env var -- proven here by becoming unavailable again
// (the env points at a nonexistent file).
func TestPutYtdlpSettingsLimpaVoltaParaEnv(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "nao-existe-env")
	h, _ := setupYtdlpServer(t, envPath)

	dbPath := escreverScriptExecutavel(t, filepath.Join(t.TempDir(), "db-yt-dlp"))
	do(t, h, http.MethodPut, "/api/v1/settings/ytdlp", ytdlpPathSettings{CustomBinaryPath: &dbPath})

	rec := do(t, h, http.MethodPut, "/api/v1/settings/ytdlp", ytdlpPathSettings{CustomBinaryPath: nil})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT (limpar) status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeYtdlp(t, rec.Body.Bytes())
	if got.CustomBinaryPath != nil {
		t.Errorf("custom_binary_path = %v, quero nil (limpo)", *got.CustomBinaryPath)
	}
	if got.Disponivel {
		t.Error("disponivel = true, quero false (voltou pra env, que aponta pra arquivo inexistente)")
	}
}

// TestPutYtdlpSettingsStringVaziaLimpaComoNull covers the PUT normalization:
// an empty string has the SAME effect as null (it clears the override), according to the
// contract.
func TestPutYtdlpSettingsStringVaziaLimpaComoNull(t *testing.T) {
	envPath := escreverScriptExecutavel(t, filepath.Join(t.TempDir(), "env-yt-dlp"))
	h, _ := setupYtdlpServer(t, envPath)

	vazio := ""
	rec := do(t, h, http.MethodPut, "/api/v1/settings/ytdlp", ytdlpPathSettings{CustomBinaryPath: &vazio})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeYtdlp(t, rec.Body.Bytes())
	if got.CustomBinaryPath != nil {
		t.Errorf("custom_binary_path = %v, quero nil (string vazia normaliza p/ null)", *got.CustomBinaryPath)
	}
}

// TestPutYtdlpSettingsCorpoMalformado covers the criterion: invalid JSON
// body → 400 invalid_body, same error contract as the other
// /settings/*.
func TestPutYtdlpSettingsCorpoMalformado(t *testing.T) {
	h, _ := setupYtdlpServer(t, "")
	rec := do(t, h, http.MethodPut, "/api/v1/settings/ytdlp", []byte(`{`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}
