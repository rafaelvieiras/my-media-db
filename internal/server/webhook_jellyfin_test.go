package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// decodeJellyfinToken decodifica o corpo de GET/POST .../webhooks/jellyfin/token.
func decodeJellyfinToken(t *testing.T, body []byte) jellyfinWebhookTokenSettings {
	t.Helper()
	var out jellyfinWebhookTokenSettings
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar jellyfinWebhookTokenSettings: %v (corpo=%s)", err, body)
	}
	return out
}

// gerarTokenTeste chama POST .../webhooks/jellyfin/token e devolve o token
// gerado.
func gerarTokenTeste(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin/token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("gerar token: status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	return decodeJellyfinToken(t, rec.Body.Bytes()).Token
}

// ── Token de configuração (briefing 0093) ───────────────────────────────────

// TestGetJellyfinWebhookTokenSemToken cobre §5: GET antes de qualquer POST
// devolve 404 (nenhum token gerado ainda).
func TestGetJellyfinWebhookTokenSemToken(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodGet, "/api/v1/webhooks/jellyfin/token", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestRegenerateJellyfinWebhookToken cobre §5: POST gera um token; um
// segundo POST gera um token DIFERENTE e o antigo deixa de funcionar no
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

	// O token ANTIGO não bate mais no webhook -> 401.
	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+tokenAntigo,
		map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Movie"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("token antigo: status = %d, quero 401 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// ── Autenticação do webhook (briefing 0093 §5) ──────────────────────────────

// TestReceiveJellyfinWebhookTokenAusenteOuInvalido cobre §5: sem token ou
// com token errado -> 401, nenhum evento criado.
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

	// Sem token.
	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin", payload)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token: status = %d, quero 401 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "unauthorized")

	// Token errado.
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

// TestReceiveJellyfinWebhookSemTokenConfigurado cobre a robustez do handler
// quando NENHUM token foi gerado ainda -- 401 (não 500), já que não há
// token válido possível.
func TestReceiveJellyfinWebhookSemTokenConfigurado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token=qualquer",
		map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Movie"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, quero 401 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "unauthorized")
}

// ── Casamento por id externo (briefing 0093 §5) ─────────────────────────────

// TestReceiveJellyfinWebhookCasaPorTmdb cobre §5: Provider_tmdb casando uma
// entity existente gera um historico_evento correto.
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

// TestReceiveJellyfinWebhookCasaPorImdb cobre §5: mesmo teste usando
// Provider_imdb isoladamente (sem tmdb no payload).
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

// TestReceiveJellyfinWebhookCasaPorTvdb cobre §5: mesmo teste usando
// Provider_tvdb isoladamente (sem tmdb/imdb no payload).
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

// TestReceiveJellyfinWebhookSemMatch cobre §5: payload sem nenhum id
// batendo -> 204, nenhum historico_evento criado.
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

// TestReceiveJellyfinWebhookPlaybackStopLimiar cobre §5: PlaybackStop com
// PlaybackPercentage >= 80 grava status=completed; < 80 grava status=stopped.
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

// TestReceiveJellyfinWebhookPayloadMalformado cobre §5: payload com corpo
// não-JSON não derruba o handler (sem 500) -- degrada para 204/descartado.
func TestReceiveJellyfinWebhookPayloadMalformado(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	token := gerarTokenTeste(t, h)

	rec := do(t, h, http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token, []byte("isto nao e json"))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, quero 204 (corpo=%s)", rec.Code, rec.Body.String())
	}
}

// TestReceiveJellyfinWebhookCamposAusentes cobre §5: payload com campos
// ausentes (só NotificationType/ItemType, sem nenhum Provider_*) não
// derruba o handler -- 204, descartado por falta de match.
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

// TestReceiveJellyfinWebhookItemTypeNaoSuportado cobre §3: ItemType fora de
// Movie/Episode é ignorado (204, nenhum evento criado), mesmo com id
// batendo.
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

// criarEntityComExternalIds cria uma entity com external_ids_json (para o
// casamento por id externo do webhook), reaproveitando CriarEntity direto.
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
