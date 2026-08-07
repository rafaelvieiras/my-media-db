package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
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
