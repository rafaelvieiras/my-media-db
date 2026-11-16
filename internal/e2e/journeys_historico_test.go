package e2e

// TestJornadaHistoricoConsumo covers the end-to-end journey of the consumption
// history epic (ADR-0022): register a manual event, generate the Jellyfin webhook
// token and simulate a playback notification arriving through it, check that the two
// paths converge to the SAME `historico_evento` (same entity, `kind="assistir"`)
// and that the derived state (GET .../historico/estado) always reflects the most
// recent event — and finally delete a single event (the owner's correction).
import (
	"net/http"
	"testing"
)

func TestJornadaHistoricoConsumo(t *testing.T) {
	c := novoHarness(t)

	// (0) Imports the movie (entity with external_ids.tmdb=603) — the same target as
	// the other journeys of this package.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	// (1) MANUAL event (its own API): without `source`, it falls back to the
	// default "manual"; `occurred_at` pinned in the past.
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

	// GET .../historico lists the newly created event.
	lista1 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico", nil, http.StatusOK)
	eventos1, _ := lista1["eventos"].([]any)
	if len(eventos1) != 1 {
		t.Fatalf("eventos apos o POST manual = %d, quero 1 (%v)", len(eventos1), eventos1)
	}

	// GET .../historico/estado derives "completed" (the only event so far).
	estado1 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico/estado", nil, http.StatusOK)
	if got := estadoStatus(t, estado1, "assistir"); got != "completed" {
		t.Errorf("estado.assistir.status apos evento manual = %q, quero \"completed\"", got)
	}

	// (2) Jellyfin webhook: generates the token (upsert into app_settings) and
	// confirms that the GET returns the SAME value.
	tok := c.obj(http.MethodPost, "/api/v1/webhooks/jellyfin/token", nil, http.StatusOK)
	token := str(t, tok, "token")
	if token == "" {
		t.Fatal("token gerado veio vazio")
	}
	tokLido := c.obj(http.MethodGet, "/api/v1/webhooks/jellyfin/token", nil, http.StatusOK)
	if str(t, tokLido, "token") != token {
		t.Errorf("GET .../token = %q, quero o mesmo token gerado (%q)", str(t, tokLido, "token"), token)
	}

	// Wrong token -> 401, no event is written at all.
	if status, _ := c.bruto(http.MethodPost, "/api/v1/webhooks/jellyfin?token=errado",
		map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Movie", "Provider_tmdb": "603"}); status != http.StatusUnauthorized {
		t.Errorf("webhook com token errado: status = %d, quero 401", status)
	}

	// Unsupported ItemType (Audio): 204 but WITHOUT writing historico_evento
	// (silent discard, ADR-0022).
	if status, _ := c.bruto(http.MethodPost, "/api/v1/webhooks/jellyfin?token="+token,
		map[string]any{"NotificationType": "PlaybackStart", "ItemType": "Audio", "Provider_tmdb": "603"}); status != http.StatusNoContent {
		t.Errorf("webhook com ItemType=Audio: status = %d, quero 204 (descartado)", status)
	}

	// PlaybackStart of an already cataloged movie (matched through Provider_tmdb)
	// writes a 2nd `assistir`/started event, with `source="jellyfin-webhook"`.
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

	// The derived state now reflects the MOST RECENT event (the webhook one,
	// started) — it does not stay pinned to the first event written.
	estado2 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico/estado", nil, http.StatusOK)
	if got := estadoStatus(t, estado2, "assistir"); got != "started" {
		t.Errorf("estado.assistir.status apos o webhook = %q, quero \"started\"", got)
	}

	// (3) Delete the manual event (a one-off correction by the owner, it is not a
	// bulk purge) — the derived state now reflects only the webhook event.
	c.obj(http.MethodDelete, "/api/v1/entities/"+id+"/historico/"+manualID, nil, http.StatusNoContent)
	lista3 := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico", nil, http.StatusOK)
	eventos3, _ := lista3["eventos"].([]any)
	if len(eventos3) != 1 {
		t.Errorf("eventos apos apagar o manual = %d, quero 1 (só o do webhook restante)", len(eventos3))
	}

	// Deleting the same id again -> 404 (it no longer exists).
	if status, _ := c.bruto(http.MethodDelete, "/api/v1/entities/"+id+"/historico/"+manualID, nil); status != http.StatusNotFound {
		t.Errorf("2a DELETE do mesmo evento: status = %d, quero 404", status)
	}
}

// TestJornadaHistoricoOrfaosBackfill covers the end-to-end journey of orphan
// events + retroactive backfill: a Jellyfin webhook whose external id matches no
// local entity becomes an ORPHAN event (instead of being discarded), shows up in
// GET /historico/orfaos, and when the owner corrects the `external_ids` of the
// right entity (PUT .../external-ids) to include that same id, the automatic
// backfill links the event -- it disappears from the orphan list and starts
// showing up in GET .../entities/{id}/historico.
func TestJornadaHistoricoOrfaosBackfill(t *testing.T) {
	c := novoHarness(t)

	// Imports the movie (the same target as the other journeys of this package), but
	// WITHOUT the imdb the webhook is going to report -- only tmdb=603 comes from the
	// fake fixture.
	ent := c.obj(http.MethodPost, "/api/v1/import",
		map[string]string{"source": "tmdb", "source_id": "603", "type": "movie"}, http.StatusCreated)
	id := str(t, ent, "id")

	tok := c.obj(http.MethodPost, "/api/v1/webhooks/jellyfin/token", nil, http.StatusOK)
	token := str(t, tok, "token")

	// (1) Webhook of an item whose tmdb does NOT match any local entity: before
	// this behavior existed it would be discarded (204 without writing anything);
	// now it becomes an ORPHAN event (still 204 -- the HTTP contract of the webhook
	// does not change).
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

	// (2) GET /historico/orfaos lists the newly written event.
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

	// The imported entity, still without that id, does not see the event in its history.
	semMatch := c.obj(http.MethodGet, "/api/v1/entities/"+id+"/historico", nil, http.StatusOK)
	if eventos, _ := semMatch["eventos"].([]any); len(eventos) != 0 {
		t.Fatalf("historico da entity antes do backfill = %d, quero 0", len(eventos))
	}

	// (3) The owner corrects the entity identity (a real scenario: the tmdb was
	// wrong/incomplete) to the id the orphan event brought -- PUT is a total
	// replacement of the map, so it resends the correct tmdb (999999, the same as
	// the webhook) plus a new imdb, simulating a real identity correction.
	c.obj(http.MethodPut, "/api/v1/entities/"+id+"/external-ids",
		map[string]any{"external_ids": map[string]string{"tmdb": "999999", "imdb": "tt9999999"}}, http.StatusOK)

	// (4) The orphan event was linked automatically: it disappears from the orphan
	// list...
	orfaos2 := c.obj(http.MethodGet, "/api/v1/historico/orfaos", nil, http.StatusOK)
	if got := orfaos2["total"]; got != float64(0) {
		t.Fatalf("historico/orfaos total apos backfill = %v, quero 0", got)
	}

	// ...and starts showing up in the entity history.
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

// estadoStatus reads estado[kind].status from a GET .../historico/estado response.
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
