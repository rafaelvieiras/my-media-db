package server

import (
	"net/http"
	"testing"
)

// features_0044_test covers the backend of the round artwork flow: the `still` kind
// in the upload, the still precedence (a custom `user` one beats the provider's raw
// one) in the episode detail and the fanart inheritance from the PARENT SERIES to the
// SEASON without fanart of its own. It reuses the helpers of assets_test (postAsset,
// pngPayload) and of entities_test/episodes_test (semearEntity, semearExtension,
// ligarFilhoOrdenado, detalheDe).

// TestUploadAceitaKindStill: the upload starts accepting kind=still with the same
// validations (origin=user). The 422 for a kind outside {poster,fanart,still} is already
// covered by TestUploadKindInvalido (kind=banner).
func TestUploadAceitaKindStill(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "ep1", "episode", "Ep")
	rec := postAsset(t, h, "ep1", "still", pngPayload(64))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload still status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	a := decodeAsset(t, rec)
	eq(t, "kind", a.Kind, "still")
	eq(t, "origin", a.Origin, "user")
}

// TestStillCustomVenceProvider: the still_url of the episode detail follows the
// still precedence -- without a custom asset it falls back to the provider's raw URL
// (extension_data); with a `user` still uploaded, the still_url becomes the local route
// /assets/still (which serves the binary).
func TestStillCustomVenceProvider(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e5", "episode", "Ep com still")
	semearExtension(t, d, "e5", nsStillE5) // raw still from the provider

	// Without a custom still: still_url = raw URL from the provider.
	det := detalheDe(t, h, "e5")
	if det.StillURL == nil || *det.StillURL != "https://artworks.thetvdb.com/banners/episodes/72514/5.jpg" {
		t.Fatalf("sem still custom: still_url = %v, quero a URL crua do provider", det.StillURL)
	}

	// Custom still upload (user): it starts winning -> local route.
	if rec := postAsset(t, h, "e5", "still", pngPayload(64)); rec.Code != http.StatusCreated {
		t.Fatalf("upload still: status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	det2 := detalheDe(t, h, "e5")
	eqPtr(t, "still_url (custom vence)", det2.StillURL, "/api/v1/entities/e5/assets/still")

	// The local route serves the binary of the custom still (200).
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e5/assets/still", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/still: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestTemporadaHerdaFanartDaSerie: a season WITHOUT fanart of its own inherits (display)
// the fanart of the PARENT SERIES -- backdrop_url points at the series fanart route.
func TestTemporadaHerdaFanartDaSerie(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	ligarFilhoOrdenado(t, d, "r1", "s1", "se1", "tv_season", 1)
	// The series has servable fanart; the season does not have its own.
	semearAsset(t, d, "fa1", "s1", "fanart", "/x/serie-fanart.jpg")

	det := detalheDe(t, h, "se1")
	eqPtr(t, "backdrop herdado da serie", det.BackdropURL, "/api/v1/entities/s1/assets/fanart")
}

// TestTemporadaComFanartPropriaNaoHerda: a season WITH its own fanart uses it --
// it does not inherit the series one (the inheritance only covers an empty background).
func TestTemporadaComFanartPropriaNaoHerda(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	ligarFilhoOrdenado(t, d, "r1", "s1", "se1", "tv_season", 1)
	semearAsset(t, d, "fa1", "s1", "fanart", "/x/serie-fanart.jpg")
	semearAsset(t, d, "fa2", "se1", "fanart", "/x/temporada-fanart.jpg")

	det := detalheDe(t, h, "se1")
	eqPtr(t, "backdrop proprio da temporada", det.BackdropURL, "/api/v1/entities/se1/assets/fanart")
}
