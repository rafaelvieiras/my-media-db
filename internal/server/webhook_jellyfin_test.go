package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// decodeJellyfinToken decodes the body of GET/POST .../webhooks/jellyfin/token.
func decodeJellyfinToken(t *testing.T, body []byte) jellyfinWebhookTokenSettings {
	t.Helper()
	var out jellyfinWebhookTokenSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar jellyfinWebhookTokenSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// gerarTokenTeste calls POST .../webhooks/jellyfin/token and returns the generated
// token.
func gerarTokenTeste(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin/token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("gerar token: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeJellyfinToken(t, rec.Body.Bytes()).Token
}

// ── Configuration token ───────────────────────────────────

// TestGetJellyfinWebhookTokenSemToken covers: GET before any POST
// returns 404 (no token generated yet).
func TestGetJellyfinWebhookTokenSemToken(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/webhooks/jellyfin/token", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestRegenerateJellyfinWebhookToken covers: POST generates a token; a
// second POST generates a DIFFERENT token and the old one stops working in the
// webhook.
func TestRegenerateJellyfinWebhookToken(t *testing.T) {
	h, _ := setupEntitiesServer(t)

	tokenAntigo := gerarTokenTeste(t, h)
	if tokenAntigo == "" {
		t.Fatal("token gerado vazio")
	}

	recGet := do(t, h, http.MethodGet, "/api/v1/webhooks/jellyfin/token", nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET status = %d, quero 200 (corpo=%s)", recGet.Code, recGet.Body.String())
	}
	if got := decodeJellyfinToken(t, recGet.Body.Bytes()).Token; got != tokenAntigo {
		t.Errorf("GET token = %q, quero %q", got, tokenAntigo)
	}

	tokenNovo := gerarTokenTeste(t, h)
	if tokenNovo == tokenAntigo {
		t.Fatal("token novo igual ao antigo")
	}

	// The OLD token no longer matches the webhook -> 401.
	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+tokenAntigo,
		map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Movie"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("token antigo: status = %d, quero 401 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// ── Webhook authentication ──────────────────────────────

// TestReceiveJellyfinWebhookTokenAusenteOuInvalido covers: without a token or
// with a wrong token -> 401, no event created.
func TestReceiveJellyfinWebhookTokenAusenteOuInvalido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	entID := "e1"
	criarEntityComExternalIds(t, d, entID, "movie", "Filme 1", `{"tmdb":"603"}`)
	_ = gerarTokenTeste(t, h)

	payload := map[string]any{
		"NotificationType": "PlaybackStart",
		"ItemType":         "Movie",
		"Provider_tmdb":    "603",
	}

	// Without a token.
	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin", payload)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "unauthorized")

	// Wrong token.
	rec = do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token=errado", payload)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("token errado: status = %d, quero 401 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "unauthorized")

	recHist := do(t, h, http.MethodGet, "/api/v1/entities/"+entID+"/historico", nil)
	list := decodeHistoricoList(t, recHist.Body.Bytes())
	if len(list.Eventos) != 0 {
		t.Errorf("eventos criados apesar do 401 = %d, quero 0", len(list.Eventos))
	}
}

// TestReceiveJellyfinWebhookSemTokenConfigurado covers the robustness of the handler
// when NO token has been generated yet -- 401 (not 500), since there is no
// possible valid token.
func TestReceiveJellyfinWebhookSemTokenConfigurado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token=qualquer",
		map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Movie"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, quero 401 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "unauthorized")
}

// ── Matching by external id ─────────────────────────────

// TestReceiveJellyfinWebhookCasaPorTmdb covers: Provider_tmdb matching an
// existing entity generates a correct historico_evento.
func TestReceiveJellyfinWebhookCasaPorTmdb(t *testing.T) {
	h, d := setupEntitiesServer(t)
	entID := "e1"
	criarEntityComExternalIds(t, d, entID, "movie", "Filme 1", `{"tmdb":"603"}`)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, map[string]any{
		"NotificationType":      "PlaybackStart",
		"ItemType":              "Movie",
		"Provider_tmdb":         "603",
		"PlaybackPositionTicks": 0,
		"RunTimeTicks":          100000000,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}

	recHist := do(t, h, http.MethodGet, "/api/v1/entities/"+entID+"/historico", nil)
	list := decodeHistoricoList(t, recHist.Body.Bytes())
	if len(list.Eventos) != 1 {
		t.Fatalf("len(eventos) = %d, quero 1", len(list.Eventos))
	}
	ev := list.Eventos[0]
	eq(t, "kind", ev.Kind, "assistir")
	eq(t, "status", ev.Status, "started")
	eq(t, "source", ev.Source, "jellyfin-webhook")
}

// TestReceiveJellyfinWebhookCasaPorImdb covers: same test using
// Provider_imdb alone (without tmdb in the payload).
func TestReceiveJellyfinWebhookCasaPorImdb(t *testing.T) {
	h, d := setupEntitiesServer(t)
	entID := "e1"
	criarEntityComExternalIds(t, d, entID, "movie", "Filme 1", `{"imdb":"tt0133093"}`)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, map[string]any{
		"NotificationType": "PlaybackStart",
		"ItemType":         "Movie",
		"Provider_imdb":    "tt0133093",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}
	recHist := do(t, h, http.MethodGet, "/api/v1/entities/"+entID+"/historico", nil)
	list := decodeHistoricoList(t, recHist.Body.Bytes())
	if len(list.Eventos) != 1 {
		t.Fatalf("len(eventos) = %d, quero 1", len(list.Eventos))
	}
}

// TestReceiveJellyfinWebhookCasaPorTvdb covers: same test using
// Provider_tvdb alone (without tmdb/imdb in the payload).
func TestReceiveJellyfinWebhookCasaPorTvdb(t *testing.T) {
	h, d := setupEntitiesServer(t)
	entID := "e1"
	criarEntityComExternalIds(t, d, entID, "tv", "Serie 1", `{"tvdb":"9001"}`)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, map[string]any{
		"NotificationType": "PlaybackStart",
		"ItemType":         "Episode",
		"Provider_tvdb":    "9001",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}
	recHist := do(t, h, http.MethodGet, "/api/v1/entities/"+entID+"/historico", nil)
	list := decodeHistoricoList(t, recHist.Body.Bytes())
	if len(list.Eventos) != 1 {
		t.Fatalf("len(eventos) = %d, quero 1", len(list.Eventos))
	}
}

// TestReceiveJellyfinWebhookSemMatch covers: a payload without any matching
// id -> 204, no historico_evento created IN THE ENTITY (the newer behavior changes the
// destination to orphan, tested in TestReceiveJellyfinWebhookSemMatchGravaOrfao).
func TestReceiveJellyfinWebhookSemMatch(t *testing.T) {
	h, d := setupEntitiesServer(t)
	entID := "e1"
	criarEntityComExternalIds(t, d, entID, "movie", "Filme 1", `{"tmdb":"603"}`)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, map[string]any{
		"NotificationType": "PlaybackStart",
		"ItemType":         "Movie",
		"Provider_tmdb":    "999999",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}
	recHist := do(t, h, http.MethodGet, "/api/v1/entities/"+entID+"/historico", nil)
	list := decodeHistoricoList(t, recHist.Body.Bytes())
	if len(list.Eventos) != 0 {
		t.Errorf("len(eventos) = %d, quero 0 (sem match)", len(list.Eventos))
	}
}

// TestReceiveJellyfinWebhookSemMatchGravaOrfao covers the orphan behavior: an
// event without a match is NO longer discarded silently -- it becomes a
// historico_evento row with entity_id NULL and the raw IDs/title/year of the payload,
// visible in GET /historico/orfaos.
func TestReceiveJellyfinWebhookSemMatchGravaOrfao(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, map[string]any{
		"NotificationType": "PlaybackStart",
		"ItemType":         "Movie",
		"Provider_tmdb":    "999999",
		"Provider_imdb":    "tt9999999",
		"Name":             "Filme Desconhecido",
		"Year":             2020,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}

	recOrfaos := do(t, h, http.MethodGet, "/api/v1/historico/orfaos", nil)
	if recOrfaos.Code != http.StatusOK {
		t.Fatalf("GET orfaos: status = %d, quero 200 (corpo=%s)", recOrfaos.Code, recOrfaos.Body.String())
	}
	list := decodeHistoricoOrfaoList(t, recOrfaos.Body.Bytes())
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("orfaos = %+v, quero 1 item", list)
	}
	item := list.Items[0]
	eq(t, "kind", item.Kind, "assistir")
	eq(t, "status", item.Status, "started")
	eq(t, "source", item.Source, "jellyfin-webhook")
	if item.RawTitle == nil || *item.RawTitle != "Filme Desconhecido" {
		t.Errorf("raw_title = %v, quero 'Filme Desconhecido'", item.RawTitle)
	}
	if item.RawYear == nil || *item.RawYear != "2020" {
		t.Errorf("raw_year = %v, quero '2020'", item.RawYear)
	}
	if item.RawExternalIDs.Tmdb != "999999" {
		t.Errorf("raw_external_ids.tmdb = %q, quero 999999", item.RawExternalIDs.Tmdb)
	}
	if item.RawExternalIDs.Imdb != "tt9999999" {
		t.Errorf("raw_external_ids.imdb = %q, quero tt9999999", item.RawExternalIDs.Imdb)
	}
}

// TestReceiveJellyfinWebhookPlaybackStopLimiar covers: PlaybackStop with
// PlaybackPercentage >= 80 writes status=completed; < 80 writes status=stopped.
func TestReceiveJellyfinWebhookPlaybackStopLimiar(t *testing.T) {
	h, d := setupEntitiesServer(t)
	token := gerarTokenTeste(t, h)

	casos := []struct {
		nome  string
		pct   float64
		quer  string
		entID string
	}{
		{"abaixo do limiar", 42.0, "stopped", "e1"},
		{"acima do limiar", 95.0, "completed", "e2"},
	}
	for _, c := range casos {
		criarEntityComExternalIds(t, d, c.entID, "movie", "Filme "+c.entID, `{"tmdb":"tmdb-`+c.entID+`"}`)
		rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, map[string]any{
			"NotificationType":   "PlaybackStop",
			"ItemType":           "Movie",
			"Provider_tmdb":      "tmdb-" + c.entID,
			"PlaybackPercentage": c.pct,
		})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d, quero 204 (corpo=%s)", c.nome, rec.Code, rec.Body.String())
		}
		recHist := do(t, h, http.MethodGet, "/api/v1/entities/"+c.entID+"/historico", nil)
		list := decodeHistoricoList(t, recHist.Body.Bytes())
		if len(list.Eventos) != 1 {
			t.Fatalf("%s: len(eventos) = %d, quero 1", c.nome, len(list.Eventos))
		}
		if list.Eventos[0].Status != c.quer {
			t.Errorf("%s: status = %q, quero %q", c.nome, list.Eventos[0].Status, c.quer)
		}
	}
}

// TestReceiveJellyfinWebhookPayloadMalformado covers: a payload with a
// non-JSON body does not take the handler down (no 500) -- it degrades to
// 204/discarded.
func TestReceiveJellyfinWebhookPayloadMalformado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, []byte("isto nao e json"))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestReceiveJellyfinWebhookCamposAusentes covers: a payload with missing
// fields (only NotificationType/ItemType, without any Provider_*) does not
// take the handler down -- 204, discarded for lack of a match.
func TestReceiveJellyfinWebhookCamposAusentes(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, map[string]any{
		"NotificationType": "PlaybackStart",
		"ItemType":         "Movie",
	})
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestReceiveJellyfinWebhookItemTypeNaoSuportado covers: an ItemType outside
// Movie/Episode is ignored (204, no event created), even with a matching
// id.
func TestReceiveJellyfinWebhookItemTypeNaoSuportado(t *testing.T) {
	h, d := setupEntitiesServer(t)
	entID := "e1"
	criarEntityComExternalIds(t, d, entID, "movie", "Filme 1", `{"tmdb":"603"}`)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, map[string]any{
		"NotificationType": "PlaybackStart",
		"ItemType":         "Audio",
		"Provider_tmdb":    "603",
	})
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}
	recHist := do(t, h, http.MethodGet, "/api/v1/entities/"+entID+"/historico", nil)
	list := decodeHistoricoList(t, recHist.Body.Bytes())
	if len(list.Eventos) != 0 {
		t.Errorf("len(eventos) = %d, quero 0 (ItemType nao suportado)", len(list.Eventos))
	}
}

// criarEntityComExternalIds creates an entity with external_ids_json (for the
// matching by external id of the webhook), reusing CriarEntity directly.
func criarEntityComExternalIds(t *testing.T, d *db.DB, id, tipo, titulo, externalIdsJSON string) {
	t.Helper()
	_, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID:              id,
		Type:            tipo,
		Title:           titulo,
		ExternalIdsJson: nsOf(externalIdsJSON),
	})
	if err != nil {
		t.Fatalf("CriarEntity(%s): %v", id, err)
	}
}
