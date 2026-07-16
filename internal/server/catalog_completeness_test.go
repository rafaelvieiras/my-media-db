package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// decodeCatalogCompleteness decodifica o corpo de GET/PUT
// /settings/catalog-completeness.
func decodeCatalogCompleteness(t *testing.T, body []byte) catalogCompletenessSettings {
	t.Helper()
	var out catalogCompletenessSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar catalogCompletenessSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetCatalogCompletenessSettingsDefault cobre §5 do briefing 0057: sem
// linha em app_settings, o GET devolve o default de fábrica (description +
// media_file + artwork:[poster] para movie e tv) sem gravar nada.
func TestGetCatalogCompletenessSettingsDefault(t *testing.T) {
	h, d := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/settings/catalog-completeness", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeCatalogCompleteness(t, rec.Body.Bytes())
	for _, tipo := range []string{"movie", "tv"} {
		cfg, ok := got[tipo]
		if !ok {
			t.Fatalf("tipo %q ausente na resposta: %v", tipo, got)
		}
		if !cfg.Description || !cfg.MediaFile {
			t.Errorf("tipo %q: description=%v media_file=%v, quero ambos true", tipo, cfg.Description, cfg.MediaFile)
		}
		if cfg.Runtime || cfg.Certification || cfg.Keywords || cfg.Cast {
			t.Errorf("tipo %q: campos extras deveriam ser false, got %+v", tipo, cfg)
		}
		if len(cfg.Artwork) != 1 || cfg.Artwork[0] != "poster" {
			t.Errorf("tipo %q: artwork = %v, quero [poster]", tipo, cfg.Artwork)
		}
	}

	// Não deve ter gravado nada em app_settings.
	row, err := escrita(t, d).ObterAppSetting(t.Context(), appSettingCatalogCompleteness)
	if err == nil {
		t.Errorf("esperava sql.ErrNoRows (nada gravado), obteve linha: %+v", row)
	}
}

// TestPutCatalogCompletenessSettingsPersiste cobre §5: PUT válido grava e o
// GET seguinte reflete EXATAMENTE o que foi enviado (substituição total, não
// merge parcial com o default anterior).
func TestPutCatalogCompletenessSettingsPersiste(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{
		"movie": {
			Description: true,
			Runtime:     true,
			Cast:        true,
			Artwork:     []string{"poster", "fanart"},
		},
		"tv": {}, // objeto vazio: aceito, "nada exigido".
	}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}

	recGet := do(t, h, http.MethodGet, "/api/v1/settings/catalog-completeness", nil)
	got := decodeCatalogCompleteness(t, recGet.Body.Bytes())

	movie, ok := got["movie"]
	if !ok {
		t.Fatalf("tipo movie ausente após PUT: %v", got)
	}
	if !movie.Description || !movie.Runtime || !movie.Cast {
		t.Errorf("movie após PUT = %+v, quero description/runtime/cast=true", movie)
	}
	if movie.Certification || movie.Keywords || movie.MediaFile {
		t.Errorf("movie após PUT = %+v, quero certification/keywords/media_file=false", movie)
	}
	if len(movie.Artwork) != 2 || movie.Artwork[0] != "poster" || movie.Artwork[1] != "fanart" {
		t.Errorf("movie.artwork após PUT = %v, quero [poster fanart]", movie.Artwork)
	}

	tv, ok := got["tv"]
	if !ok {
		t.Fatalf("tipo tv ausente após PUT: %v", got)
	}
	if tv.Description || tv.MediaFile || len(tv.Artwork) != 0 {
		t.Errorf("tv após PUT (objeto vazio no corpo) = %+v, quero tudo zerado (substituição total, não merge com o default)", tv)
	}
}

// TestPutCatalogCompletenessSettingsArtworkInvalido cobre §5: um artwork fora
// de kindsValidos → 422.
func TestPutCatalogCompletenessSettingsArtworkInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{
		"movie": {Artwork: []string{"poster", "discoart"}},
	}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPutCatalogCompletenessSettingsTipoVazio cobre §5: uma chave de tipo
// vazia ("") → 422.
func TestPutCatalogCompletenessSettingsTipoVazio(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{
		"": {Description: true},
	}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPutCatalogCompletenessSettingsCorpoInvalido cobre §5: corpo que não é
// JSON válido (objeto) → 400.
func TestPutCatalogCompletenessSettingsCorpoInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", "isto não é um objeto")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPutCatalogCompletenessSettingsTipoVazioObjeto cobre §5: um tipo com
// objeto vazio ({}) é aceito (equivale a "nada exigido"), não é o mesmo caso
// de chave de tipo vazia.
func TestPutCatalogCompletenessSettingsTipoVazioObjeto(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{
		"movie": {},
		"tv":    {Description: true},
	}
	rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
}
