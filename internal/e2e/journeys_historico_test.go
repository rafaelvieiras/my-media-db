package e2e

// TestJornadaHistoricoConsumo cobre a jornada ponta-a-ponta do épico de
// histórico de consumo (briefings 0092/0093/0094, ADR-0022): registrar um
// evento manual, gerar o token do webhook do Jellyfin e simular uma
// notificação de reprodução chegando por ele, conferir que os dois caminhos
// convergem para o MESMO `historico_evento` (mesma entity, `kind="assistir"`)
// e que o estado derivado (GET .../historico/estado) reflete sempre o evento
// mais recente — e por fim apagar um evento pontual (correção do dono).
import (
	"net/http"
	"testing"
)

func TestJornadaHistoricoConsumo(t *testing.T) {
	c := novoHarness(t)

	// (0) Importa o filme (entity com external_ids.tmdb=603) — mesmo alvo das
	// demais jornadas deste pacote.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// (1) Evento MANUAL (API própria, briefing 0092): sem `source`, cai no
	// default "manual"; `occurred_at` fixo no passado.
	manual := c.obj(http.MethodPost, "/api/v1/entities/"+id+"/historico",
		map[string]any{
			"kind":        "assistir",
			"status":      "completed",
			"occurred_at": "2026-01-05T20:00:00Z",
		}, http.StatusCreated)
	if got := str(t, manual, "source"); got != "manual" {
		t.Errorf("source do evento manual = %q, quero \"manual\" (default)", got)
	}
	manualID := str(t, manual, "id")

	// GET .../historico lista o evento recém-criado.
	lista1 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico", nil, http.StatusOK)
	eventos1, _ := lista1["eventos"].([]any)
	if len(eventos1) != 1 {
		t.Fatalf("eventos apos o POST manual = %d, quero 1 (%v)", len(eventos1), eventos1)
	}

	// GET .../historico/estado deriva "completed" (único evento até aqui).
	estado1 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico/estado", nil, http.StatusOK)
	if got := estadoStatus(t, estado1, "assistir"); got != "completed" {
		t.Errorf("estado.assistir.status apos evento manual = %q, quero \"completed\"", got)
	}

	// (2) Webhook do Jellyfin (briefing 0093): gera o token (upsert em
	// app_settings) e confirma que o GET devolve o MESMO valor.
	tok := c.obj(http.MethodPost, "/api/v1/webhooks/jellyfin/token", nil, http.StatusOK)
	token := str(t, tok, "token")
	if token == "" {
		t.Fatal("token gerado veio vazio")
	}
	tokLido := c.obj(http.MethodGet, "/api/v1/webhooks/jellyfin/token", nil, http.StatusOK)
	if str(t, tokLido, "token") != token {
		t.Errorf("GET .../token = %q, quero o mesmo token gerado (%q)", str(t, tokLido, "token"), token)
	}

	// Token errado -> 401, não grava evento nenhum.
	if status, _ := c.bruto(http.MethodPost, "/api/v1/webhooks/jellyfin?token=errado",
		map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Movie", "Provider_tmdb": "603"}); status != http.StatusUnauthorized {
		t.Errorf("webhook com token errado: status = %d, quero 401", status)
	}

	// ItemType não suportado (Audio): 204 mas SEM gravar historico_evento
	// (descarte silencioso, briefing 0093 §3).
	if status, _ := c.bruto(http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token,
		map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Audio", "Provider_tmdb": "603"}); status != http.StatusNoContent {
		t.Errorf("webhook com ItemType=Audio: status = %d, quero 204 (descartado)", status)
	}

	// PlaybackStart de um filme já catalogado (casa por Provider_tmdb) grava
	// um 2° evento `assistir`/started, com `source="jellyfin-webhook"`.
	if status, _ := c.bruto(http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token,
		map[string]any{
			"NotificationType":      "PlaybackStart",
			"ItemType":              "Movie",
			"Provider_tmdb":         "603",
			"PlaybackPositionTicks": 0,
			"RunTimeTicks":          100_000_000,
		}); status != http.StatusNoContent {
		t.Errorf("webhook PlaybackStart: status = %d, quero 204", status)
	}

	lista2 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico?kind=assistir", nil, http.StatusOK)
	eventos2, _ := lista2["eventos"].([]any)
	if len(eventos2) != 2 {
		t.Fatalf("eventos apos o webhook = %d, quero 2 (manual + webhook, %v)", len(eventos2), eventos2)
	}
	maisRecente, _ := eventos2[0].(map[string]any)
	if got, _ := maisRecente["source"].(string); got != "jellyfin-webhook" {
		t.Errorf("source do evento mais recente = %q, quero \"jellyfin-webhook\" (occurred_at do webhook é agora, vence o manual de 2026-01-05)", got)
	}
	if got, _ := maisRecente["status"].(string); got != "started" {
		t.Errorf("status do evento do webhook = %q, quero \"started\" (PlaybackStart)", got)
	}

	// O estado derivado agora reflete o evento MAIS RECENTE (o do webhook,
	// started) — não fica preso ao primeiro evento gravado.
	estado2 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico/estado", nil, http.StatusOK)
	if got := estadoStatus(t, estado2, "assistir"); got != "started" {
		t.Errorf("estado.assistir.status apos o webhook = %q, quero \"started\"", got)
	}

	// (3) Apagar o evento manual (correção pontual do dono, não é purga em
	// massa) — o estado derivado passa a refletir só o evento do webhook.
	c.obj(http.MethodDelete, "/api/v1/entities/"+id+"/historico/"+manualID, nil, http.StatusNoContent)
	lista3 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico", nil, http.StatusOK)
	eventos3, _ := lista3["eventos"].([]any)
	if len(eventos3) != 1 {
		t.Errorf("eventos apos apagar o manual = %d, quero 1 (só o do webhook restante)", len(eventos3))
	}

	// Apagar de novo o mesmo id -> 404 (já não existe).
	if status, _ := c.bruto(http.MethodDelete, "/api/v1/entities/"+id+"/historico/"+manualID, nil); status != http.StatusNotFound {
		t.Errorf("2a DELETE do mesmo evento: status = %d, quero 404", status)
	}
}

// estadoStatus lê estado[kind].status de uma resposta de GET .../historico/estado.
func estadoStatus(t *testing.T, m map[string]any, kind string) string {
	t.Helper()
	estado, ok := m["estado"].(map[string]any)
	if !ok {
		t.Fatalf("estado ausente/tipo errado em %v", m)
	}
	item, ok := estado[kind].(map[string]any)
	if !ok {
		t.Fatalf("estado[%q] ausente/tipo errado em %v", kind, estado)
	}
	s, _ := item["status"].(string)
	return s
}
