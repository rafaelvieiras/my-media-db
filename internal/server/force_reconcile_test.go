package server

// Tests of POST /entities/{id}/force-reconcile: it triggers the
// SAME orphan child reconciliation of the batch job
// (ReconciliarFilhosOrfaos, tested in reconciliar_orfaos_test.go), but for ONE entity and
// IGNORING the last_external_match_attempt_at throttle. It reuses the fakes/helpers of
// reconciliar_orfaos_test.go (same package): setupReconciliarOrfaosServer,
// semearFaixaDraftOrfa, semearSerieDraftComArquivo, fakeMusicaProviderOrfaos,
// reconciliarProberFake, mediaFileEntityID.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/provider/fake"
)

// decodeForceReconcile decodes the endpoint response.
func decodeForceReconcile(t *testing.T, body []byte) forcarReconciliacaoResultDTO {
	t.Helper()
	var out forcarReconciliacaoResultDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar forcarReconciliacaoResultDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestForceReconcileFaixaIgnoraThrottle covers the central criterion: an orphan draft
// track with a very recent last_external_match_attempt_at
// (inside the ReconciliarFilhosOrfaos throttle) is still attempted by the
// individual endpoint -- and relinks successfully, proving that the throttle is
// ignored (unlike the batch job, which would not even call the provider for this
// item -- see TestReconciliarFilhosOrfaosRespeitaThrottle).
func TestForceReconcileFaixaIgnoraThrottle(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/aha/01 - Track One.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Fake Band", Album: "Fake Album", Title: "Track One", TrackNumber: 1},
	})
	semearFaixaDraftOrfa(t, d, "fx-draft", "Track One", path)
	// Stamps a very recent attempt -- a batch throttle would block it.
	carimbarTentativa(t, d, "fx-draft", time.Now().Add(-1*time.Hour))

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/fx-draft/force-reconcile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeForceReconcile(t, rec.Body.Bytes())
	if !resultado.Linked {
		t.Errorf("linked = %v, quero true (throttle recente deveria ter sido IGNORADO)", resultado.Linked)
	}
	if musica.chamadasBuscar == 0 {
		t.Error("provider de música não foi chamado -- throttle não foi ignorado")
	}

	entityID := mediaFileEntityID(t, d, path)
	if entityID == "fx-draft" {
		t.Error("media_file ainda aponta pro draft, deveria estar repontado pra faixa resolvida")
	}
}

// TestForceReconcileSerieIgnoraThrottle covers the same criterion for a series: a
// draft series with a file whose tree only resolves via the provider (fake.New),
// stamped recently, is still retried and relinked by the individual
// endpoint.
func TestForceReconcileSerieIgnoraThrottle(t *testing.T) {
	img := servidorImagemArvoreSerie(t)
	h, d := setupSearchServer(t, WithSearchProvider(fake.New(img.URL)), WithAssetsDir(t.TempDir()))
	path := "/lib/series/springfield/Springfield Tales S01E01.mkv"
	semearSerieDraftComArquivo(t, d, "serie-draft", "Springfield Tales", path)
	carimbarTentativa(t, d, "serie-draft", time.Now().Add(-1*time.Hour))

	rec := do(t, h, http.MethodPost, "/api/v1/entities/serie-draft/force-reconcile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeForceReconcile(t, rec.Body.Bytes())
	if !resultado.Linked {
		t.Errorf("linked = %v, quero true (throttle recente deveria ter sido IGNORADO)", resultado.Linked)
	}

	entityID := mediaFileEntityID(t, d, path)
	if entityID == "serie-draft" {
		t.Error("media_file ainda aponta pro draft, deveria estar repontado pra série resolvida")
	}
}

// TestForceReconcileTipoNaoAplicavelNaoCarimba covers the "type not
// applicable" criterion: a type=movie entity responds 200/linked=false WITHOUT
// calling any reconciliation and WITHOUT stamping the attempt (unlike
// track/series, which always stamp).
func TestForceReconcileTipoNaoAplicavelNaoCarimba(t *testing.T) {
	s, d := setupReconciliarOrfaosServer(t, &fakeMusicaProviderOrfaos{}, nil)
	semearEntity(t, d, "e-movie", "movie", "Um Filme Qualquer")

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-movie/force-reconcile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeForceReconcile(t, rec.Body.Bytes())
	if resultado.Linked {
		t.Errorf("linked = %v, quero false (tipo não aplicável)", resultado.Linked)
	}

	ent, err := escrita(t, d).ObterEntity(context.Background(), "e-movie")
	if err != nil {
		t.Fatalf("ObterEntity: %v", err)
	}
	if ent.LastExternalMatchAttemptAt.Valid {
		t.Error("last_external_match_attempt_at NÃO deveria ter sido carimbado (tipo não aplicável, sem tentativa real)")
	}
}

// TestForceReconcileEntidadeInexistente404 covers the criterion: a nonexistent
// entity responds 404, same pattern as enrich-missing/auto-match.
func TestForceReconcileEntidadeInexistente404(t *testing.T) {
	s, _ := setupReconciliarOrfaosServer(t, &fakeMusicaProviderOrfaos{}, nil)
	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/naoexiste/force-reconcile", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestForceReconcileSemScannerDegradaSemErro covers the s.scanner==nil guard:
// without a scanner, it responds 200/linked=false straight away, without trying
// anything -- same guard as ReconciliarFilhosOrfaos.
func TestForceReconcileSemScannerDegradaSemErro(t *testing.T) {
	h, d := setupSearchServer(t)
	semearEntity(t, d, "fx-sem-scanner", "faixa", "Sem Scanner")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/fx-sem-scanner/force-reconcile", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	resultado := decodeForceReconcile(t, rec.Body.Bytes())
	if resultado.Linked {
		t.Errorf("linked = %v, quero false (sem scanner)", resultado.Linked)
	}
}

// TestForceReconcileErroDeInfraPropaga500 covers the criterion: a REAL
// database error (closed write pool) propagates as 500 -- it never degrades
// silently. It uses a scenario with NO candidate at all (a track whose artist/
// album do not match any provider fixture, same path as
// TestReconciliarFilhosOrfaosSerieSemMatchAindaCarimba): the reconciliation
// itself only uses the READ pool and the Prober (no write is attempted, since
// no candidate is found to relink) -- and the stamping, which is the
// ONLY write reached on this path, is what fails with the closed write
// pool, proving the "OR" of the criterion (tentarErr==nil but
// carimbarErr!=nil still becomes 500). Forcing the error INSIDE the
// reconciliation itself would require pre-seeding an already cataloged
// artist/album/track matching exactly the fake provider fixtures (so the textual match
// does not need to write anything besides the final stamp) -- not worth the extra cost
// given that the "OR" is already proven on this side.
func TestForceReconcileErroDeInfraPropaga500(t *testing.T) {
	musica := &fakeMusicaProviderOrfaos{}
	path := "/lib/musica/desconhecida/01 - Faixa Desconhecida.mp3"
	s, d := setupReconciliarOrfaosServer(t, musica, map[string]mediainfo.AudioTags{
		path: {AlbumArtist: "Banda Sem Fixture", Album: "Album Sem Fixture", Title: "Faixa Desconhecida", TrackNumber: 1},
	})
	semearFaixaDraftOrfa(t, d, "fx-draft", "Faixa Desconhecida", path)

	if err := d.Write.Close(); err != nil {
		t.Fatalf("fechar Write: %v", err)
	}

	h, err := s.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/fx-draft/force-reconcile", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, quero 500 (pool de escrita fechado, corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "internal_error")
}
