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

// TestJornadaHistoricoOrfaosBackfill cobre a jornada ponta-a-ponta do
// briefing 0099 (eventos órfãos + backfill retroativo): um webhook do
// Jellyfin cujo id externo não casa com nenhuma entity local vira um evento
// ÓRFÃO (em vez de ser descartado), aparece em GET /historico/orfaos, e
// quando o dono corrige o `external_ids` da entity certa (PUT
// .../external-ids) para incluir aquele mesmo id, o backfill automático
// vincula o evento -- ele some da lista de órfãos e passa a aparecer em GET
// .../entities/{id}/historico.
func TestJornadaHistoricoOrfaosBackfill(t *testing.T) {
	c := novoHarness(t)

	// Importa o filme (mesmo alvo das demais jornadas deste pacote), mas SEM
	// o imdb que o webhook vai reportar -- só tmdb=603 vem do fixture fake.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	tok := c.obj(http.MethodPost, "/api/v1/webhooks/jellyfin/token", nil, http.StatusOK)
	token := str(t, tok, "token")

	// (1) Webhook de um item cujo tmdb NÃO casa com nenhuma entity local:
	// antes do 0099 seria descartado (204 sem gravar nada); agora vira um
	// evento ÓRFÃO (ainda 204 -- contrato HTTP do webhook não muda).
	if status, _ := c.bruto(http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token,
		map[string]any{
			"NotificationType": "PlaybackStart",
			"ItemType":         "Movie",
			"Provider_tmdb":    "999999",
			"Name":             "Filme Desconhecido",
			"Year":             2020,
		}); status != http.StatusNoContent {
		t.Errorf("webhook sem match: status = %d, quero 204", status)
	}

	// (2) GET /historico/orfaos lista o evento recém-gravado.
	orfaos1 := c.obj(http.MethodGet, "/api/v1/historico/orfaos", nil, http.StatusOK)
	if got := orfaos1["total"]; got != float64(1) {
		t.Fatalf("historico/orfaos total = %v, quero 1", got)
	}
	itens1, _ := orfaos1["items"].([]any)
	if len(itens1) != 1 {
		t.Fatalf("historico/orfaos items = %d, quero 1", len(itens1))
	}
	orfao1, _ := itens1[0].(map[string]any)
	if got := str(t, orfao1, "raw_title"); got != "Filme Desconhecido" {
		t.Errorf("orfao.raw_title = %q, quero \"Filme Desconhecido\"", got)
	}
	rawIDs1, _ := orfao1["raw_external_ids"].(map[string]any)
	if got, _ := rawIDs1["tmdb"].(string); got != "999999" {
		t.Errorf("orfao.raw_external_ids.tmdb = %q, quero \"999999\"", got)
	}

	// A entity importada, sem esse id ainda, não vê o evento no seu histórico.
	semMatch := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico", nil, http.StatusOK)
	if eventos, _ := semMatch["eventos"].([]any); len(eventos) != 0 {
		t.Fatalf("historico da entity antes do backfill = %d, quero 0", len(eventos))
	}

	// (3) O dono corrige a identidade da entity (cenário real: o tmdb estava
	// errado/incompleto) para o id que o evento órfão trouxe -- PUT é
	// substituição total do mapa, então reenvia o tmdb correto (999999, o
	// mesmo do webhook) mais um imdb novo, simulando uma correção real de
	// identidade.
	c.obj(http.MethodPut, "/api/v1/entities/"+id+"/external-ids",
		map[string]any{"external_ids": map[string]string{"tmdb": "999999", "imdb": "tt9999999"}}, http.StatusOK)

	// (4) O evento órfão foi vinculado automaticamente: some da lista de
	// órfãos...
	orfaos2 := c.obj(http.MethodGet, "/api/v1/historico/orfaos", nil, http.StatusOK)
	if got := orfaos2["total"]; got != float64(0) {
		t.Fatalf("historico/orfaos total apos backfill = %v, quero 0", got)
	}

	// ...e passa a aparecer no histórico da entity.
	comMatch := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico", nil, http.StatusOK)
	eventos, _ := comMatch["eventos"].([]any)
	if len(eventos) != 1 {
		t.Fatalf("historico da entity apos backfill = %d, quero 1", len(eventos))
	}
	vinculado, _ := eventos[0].(map[string]any)
	if got := str(t, vinculado, "source"); got != "jellyfin-webhook" {
		t.Errorf("evento vinculado por backfill: source = %q, quero \"jellyfin-webhook\"", got)
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
