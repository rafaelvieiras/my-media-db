package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// decodeCatalogCompleteness decodes the body of GET/PUT
// /settings/catalog-completeness.
func decodeCatalogCompleteness(t *testing.T, body []byte) catalogCompletenessSettings {
	t.Helper()
	var out catalogCompletenessSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar catalogCompletenessSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// TestGetCatalogCompletenessSettingsDefault covers: without a row
// in app_settings, the GET returns the factory default (description + media_file +
// artwork:[poster] for movie and tv) without writing anything.
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

	// It must not have written anything to app_settings.
	row, err := escrita(t, d).ObterAppSetting(t.Context(), appSettingCatalogCompleteness)
	if err == nil {
		t.Errorf("esperava sql.ErrNoRows (nada gravado), obteve linha: %+v", row)
	}
}

// TestPutCatalogCompletenessSettingsPersiste covers: a valid PUT writes it and the
// following GET reflects EXACTLY what was sent (total replacement, not
// partial merge with the previous default).
func TestPutCatalogCompletenessSettingsPersiste(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{
		"movie": {
			Description: true,
			Runtime:     true,
			Cast:        true,
			Artwork:     []string{"poster", "fanart"},
		},
		"tv": {}, // empty object: accepted, "nothing required".
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

// TestPutCatalogCompletenessSettingsArtworkInvalido covers: an artwork outside
// kindsValidos → 422.
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

// TestPutCatalogCompletenessSettingsTipoVazio covers: an empty type
// key ("") → 422.
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

// TestPutCatalogCompletenessSettingsCorpoInvalido covers: a body that is not
// valid JSON (object) → 400.
func TestPutCatalogCompletenessSettingsCorpoInvalido(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", "isto não é um objeto")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "invalid_body")
}

// TestPutCatalogCompletenessSettingsTipoVazioObjeto covers: a type with an
// empty object ({}) is accepted (equivalent to "nothing required"), it is not the same
// case as an empty type key.
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
