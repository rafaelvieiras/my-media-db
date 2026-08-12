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

// decodeLangs decodifica o corpo de GET/PUT /settings/languages.
func decodeLangs(t *testing.T, body []byte) languageSettings {
	t.Helper()
	var out languageSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar languageSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetLanguageSettingsDefault cobre §5: sem linha em app_settings, o GET devolve
// o default seguro (pt-BR / en / [pt-BR, en]).
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

// TestPutLanguageSettingsPersiste cobre §5: PUT válido grava e o GET seguinte
// reflete (cache invalidado).
func TestPutLanguageSettingsPersiste(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := languageSettings{Primary: "pt-BR", Fallback: "en", Allowed: []string{"pt-BR", "en", "es"}}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/languages", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	// GET reflete o novo allowed (persistiu + cache invalidado).
	recGet := do(t, h, http.MethodGet, "/api/v1/settings/languages", nil)
	got := decodeLangs(t, recGet.Body.Bytes())
	if len(got.Allowed) != 3 || got.Allowed[2] != "es" {
		t.Errorf("allowed após PUT = %v, quero [pt-BR en es]", got.Allowed)
	}
}

// TestPutLanguageSettingsInvalida cobre §5: validação (principal/fallback ⊆
// permitidos; códigos não-vazios) responde 422 no envelope padrão.
func TestPutLanguageSettingsInvalida(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	casos := []languageSettings{
		{Primary: "fr", Fallback: "en", Allowed: []string{"pt-BR", "en"}},    // principal fora
		{Primary: "pt-BR", Fallback: "fr", Allowed: []string{"pt-BR", "en"}}, // fallback fora
		{Primary: "pt-BR", Fallback: "en", Allowed: []string{}},              // sem permitidos
		{Primary: "", Fallback: "en", Allowed: []string{"pt-BR", "en", ""}},  // código vazio
	}
	for i, c := range casos {
		rec := do(t, h, http.MethodPut, "/api/v1/settings/languages", c)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("caso %d: status = %d, quero 422 (corpo=%s)", i, rec.Code, rec.Body.String())
		}
		checkErrorShape(t, rec, "invalid_body")
	}
}

// ── Enriquecimento retroativo (briefing 0085) ───────────────────────────────

// decodeRetro decodifica o corpo de GET/PUT /settings/enrichment-retroativo.
func decodeRetro(t *testing.T, body []byte) retroactiveEnrichmentSettings {
	t.Helper()
	var out retroactiveEnrichmentSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar retroactiveEnrichmentSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetEnrichmentRetroativoSettingsDefault cobre o critério §5: sem linha
// em app_settings, o GET devolve o default seguro {retry_days: 15}.
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

// TestPutEnrichmentRetroativoSettingsPersiste cobre o critério §5: PUT válido
// grava e o GET seguinte reflete (cache invalidado, sem reiniciar o servidor).
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

// TestPutEnrichmentRetroativoSettingsInvalida cobre o critério §5: retry_days
// fora de [1, 365] → 422 invalid_body, valor anterior preservado.
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

	// Valor anterior (default 15) preservado -- nenhum PUT inválido acima
	// deveria ter gravado nada.
	recGet := do(t, h, http.MethodGet, "/api/v1/settings/enrichment-retroativo", nil)
	got := decodeRetro(t, recGet.Body.Bytes())
	if got.RetryDays != 15 {
		t.Errorf("retry_days após PUTs inválidos = %d, quero 15 (default preservado)", got.RetryDays)
	}
}

// TestPutEnrichmentRetroativoSettingsCorpoMalformado cobre o critério §5:
// corpo JSON inválido → 400 invalid_body.
func TestPutEnrichmentRetroativoSettingsCorpoMalformado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/settings/enrichment-retroativo", []byte(`{`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// ── Materialização automática de artes (briefing 0089) ──────────────────────

// decodeAutoMaterialize decodifica o corpo de GET/PUT
// /settings/auto-materialize-artwork.
func decodeAutoMaterialize(t *testing.T, body []byte) autoMaterializeArtworkSettings {
	t.Helper()
	var out autoMaterializeArtworkSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar autoMaterializeArtworkSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetAutoMaterializeArtworkSettingsDefault cobre o critério §5: sem linha
// em app_settings, o GET devolve o default seguro {enabled:false}.
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

// TestPutAutoMaterializeArtworkSettingsPersiste cobre o critério §5: PUT
// grava e o GET seguinte reflete (cache invalidado).
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

// TestPutAutoMaterializeArtworkSettingsCorpoMalformado cobre o critério §5:
// corpo JSON inválido → 400 invalid_body.
func TestPutAutoMaterializeArtworkSettingsCorpoMalformado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/settings/auto-materialize-artwork", []byte(`{`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestGetEntityLangInvalido cobre §5: ?lang= fora dos permitidos → 400 invalid_query.
func TestGetEntityLangInvalido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Filme")
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e?lang=fr", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_query")
}

// TestListEntitiesLangInvalido cobre §5: ?lang= inválido na lista → 400.
func TestListEntitiesLangInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/entities?lang=fr", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_query")
}

// TestGetEntityResolvedLanguages cobre §5: o detalhe traz resolved_languages com o
// idioma efetivo por campo e o shape segue plano (title é string, não objeto).
func TestGetEntityResolvedLanguages(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Filme")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/e?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	// Shape plano: title é string.
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
	// title cai na base arquivo (neutra) → proveniência "".
	if prov, ok := ent.ResolvedLanguages["title"]; !ok || prov != "" {
		t.Errorf("resolved_languages[title] = %q (ok=%v), quero \"\"", prov, ok)
	}
}

// TestGetEntityFallbackNoBanco cobre a fiação ponta a ponta: com settings pt-BR/en e
// external em en, o pedido pt-BR cai no fallback en (proveniência en).
func TestGetEntityFallbackNoBanco(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e", "movie", "Titulo Arquivo")
	// Override com título só em inglês (formato por idioma); pt-BR ausente → cai no en.
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

// ── yt-dlp (briefing 0101, ADR-0025) ────────────────────────────────────────

// escreverScriptExecutavel grava um arquivo executável mínimo em `path` --
// os testes de settings.go só precisam que o arquivo EXISTA (Status faz
// os.Stat no path customizado, sem rodar --version); a validação de conteúdo
// já é coberta pelos testes do próprio pacote internal/ytdlp.
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

// setupYtdlpServer sobe um Server com banco temporário migrado e um
// ytdlp.Gerenciador injetado (envPath já resolvido, como main.go faria com
// MYMEDIADB_YTDLP_PATH), para exercitar GET/PUT /settings/ytdlp ponta a
// ponta.
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

// decodeYtdlp decodifica o corpo de GET/PUT /settings/ytdlp.
func decodeYtdlp(t *testing.T, body []byte) ytdlpStatusResponse {
	t.Helper()
	var out ytdlpStatusResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar ytdlpStatusResponse: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetYtdlpSettingsSemGerenciador cobre o caso defensivo: sem
// ytdlp.Gerenciador injetado (setupEntitiesServer não injeta), GET
// /settings/ytdlp degrada para indisponível/"nenhum" em vez de panicar --
// nenhum outro endpoint do produto quebra por causa disso.
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

// TestGetYtdlpSettingsOrigemEnv cobre o critério §5: com MYMEDIADB_YTDLP_PATH
// setado (env, aqui já resolvido e passado ao Gerenciador em WithYtdlp),
// GET /settings/ytdlp reporta origem="custom" usando esse path, sem
// custom_binary_path (banco vazio).
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

// TestPutYtdlpSettingsBancoVenceSobreEnv cobre o critério §5: com
// custom_binary_path gravado via PUT /settings/ytdlp (banco), esse path vence
// sobre a env var -- aqui provado apontando a env var para um arquivo
// INEXISTENTE (indisponível antes do PUT) e o banco para um arquivo real
// (disponível depois do PUT), com efeito IMEDIATO (mesma execução do
// processo, sem reiniciar).
func TestPutYtdlpSettingsBancoVenceSobreEnv(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "nao-existe-env")
	h, _ := setupYtdlpServer(t, envPath)

	// Antes do PUT: só a env var (inexistente) está configurada -> indisponível.
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

	// GET seguinte reflete o mesmo estado (persistiu + cache invalidado).
	recGet := do(t, h, http.MethodGet, "/api/v1/settings/ytdlp", nil)
	gotGet := decodeYtdlp(t, recGet.Body.Bytes())
	if !gotGet.Disponivel || gotGet.CustomBinaryPath == nil || *gotGet.CustomBinaryPath != dbPath {
		t.Errorf("GET apos PUT = %+v, quero disponivel=true custom_binary_path=%q", gotGet, dbPath)
	}
}

// TestPutYtdlpSettingsLimpaVoltaParaEnv cobre o critério §5 (limpeza do
// override): PUT com custom_binary_path=null apaga o path do banco e devolve
// o controle para a env var -- aqui provado voltando a ficar indisponível
// (env aponta pra um arquivo inexistente).
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

// TestPutYtdlpSettingsStringVaziaLimpaComoNull cobre a normalização do PUT:
// string vazia tem o MESMO efeito de null (limpa o override), conforme o
// contrato do briefing 0101 (§4).
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

// TestPutYtdlpSettingsCorpoMalformado cobre o critério §5: corpo JSON
// inválido → 400 invalid_body, mesmo contrato de erro dos outros
// /settings/*.
func TestPutYtdlpSettingsCorpoMalformado(t *testing.T) {
	h, _ := setupYtdlpServer(t, "")
	rec := do(t, h, http.MethodPut, "/api/v1/settings/ytdlp", []byte(`{`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}
