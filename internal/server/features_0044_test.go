package server

import (
	"net/http"
	"testing"
)

// features_0044_test cobre o backend do fluxo de artes redondo (0044): o kind `still`
// no upload, a precedencia do still (custom `user` vence o cru do provider) no detalhe
// do episodio e a heranca de fanart da SERIE PAI para a TEMPORADA sem fanart propria.
// Reusa os helpers de assets_test (postAsset, pngPayload) e de entities_test/
// episodes_test (semearEntity, semearExtension, ligarFilhoOrdenado, detalheDe).

// TestUploadAceitaKindStill: o upload passa a aceitar kind=still (0044) com as mesmas
// validacoes (origin=user). O 422 para kind fora de {poster,fanart,still} ja e coberto
// por TestUploadKindInvalido (kind=banner).
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

// TestStillCustomVenceProvider: o still_url do detalhe do episodio segue a precedencia
// do 0044 -- sem asset custom cai na URL crua do provider (extension_data, 0042); com um
// still `user` uploadado, o still_url vira a rota local /assets/still (que serve o binario).
func TestStillCustomVenceProvider(t *testing.T) {
	h, d, _ := setupAssetsServer(t)
	semearEntity(t, d, "e5", "episode", "Ep com still")
	semearExtension(t, d, "e5", nsStillE5) // still cru do provider (0042)

	// Sem still custom: still_url = URL crua do provider.
	det := detalheDe(t, h, "e5")
	if det.StillURL == nil || *det.StillURL != "https://artworks.thetvdb.com/banners/episodes/72514/5.jpg" {
		t.Fatalf("sem still custom: still_url = %v, quero a URL crua do provider", det.StillURL)
	}

	// Upload de still custom (user): passa a vencer -> rota local.
	if rec := postAsset(t, h, "e5", "still", pngPayload(64)); rec.Code != http.StatusCreated {
		t.Fatalf("upload still: status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	det2 := detalheDe(t, h, "e5")
	eqPtr(t, "still_url (custom vence)", det2.StillURL, "/api/v1/entities/e5/assets/still")

	// A rota local serve o binario do still custom (200).
	rec := do(t, h, http.MethodGet, "/api/v1/entities/e5/assets/still", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/still: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestTemporadaHerdaFanartDaSerie: uma temporada SEM fanart propria herda (display) a
// fanart da SERIE PAI -- backdrop_url aponta para a rota de fanart da serie (0044).
func TestTemporadaHerdaFanartDaSerie(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "s1", "tv", "Serie")
	semearEntity(t, d, "se1", "season", "Temporada 1")
	ligarFilhoOrdenado(t, d, "r1", "s1", "se1", "tv_season", 1)
	// Serie tem fanart servivel; a temporada nao tem a sua.
	semearAsset(t, d, "fa1", "s1", "fanart", "/x/serie-fanart.jpg")

	det := detalheDe(t, h, "se1")
	eqPtr(t, "backdrop herdado da serie", det.BackdropURL, "/api/v1/entities/s1/assets/fanart")
}

// TestTemporadaComFanartPropriaNaoHerda: uma temporada COM fanart propria usa a sua --
// nao herda a da serie (a heranca so cobre o fundo vazio).
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
