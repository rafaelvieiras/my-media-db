package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// decodeHistoricoEvento decodifica o corpo de POST/itens de GET .../historico.
func decodeHistoricoEvento(t *testing.T, body []byte) historicoEventoDTO {
	t.Helper()
	var out historicoEventoDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar historicoEventoDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// decodeHistoricoList decodifica o corpo de GET .../historico.
func decodeHistoricoList(t *testing.T, body []byte) historicoEventoListDTO {
	t.Helper()
	var out historicoEventoListDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar historicoEventoListDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// decodeHistoricoEstado decodifica o corpo de GET .../historico/estado.
func decodeHistoricoEstado(t *testing.T, body []byte) historicoEstadoDTO {
	t.Helper()
	var out historicoEstadoDTO
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decodificar historicoEstadoDTO: %v (corpo=%s)", err, body)
	}
	return out
}

// TestCreateHistoricoEventoDefaults cobre §5: source ausente vira "manual",
// occurred_at ausente vira o timestamp do servidor, 201 com o shape completo.
func TestCreateHistoricoEventoDefaults(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	corpo := map[string]any{"kind": "assistir", "status": "started"}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/historico", corpo)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeHistoricoEvento(t, rec.Body.Bytes())
	eq(t, "kind", got.Kind, "assistir")
	eq(t, "status", got.Status, "started")
	eq(t, "source", got.Source, "manual")
	if got.OccurredAt == "" {
		t.Error("occurred_at vazio, quero default = agora")
	}
	if got.ID == "" {
		t.Error("id vazio")
	}
	if got.CreatedAt == "" {
		t.Error("created_at vazio")
	}
}

// TestCreateHistoricoEventoCamposExplicitos cobre §5: source/occurred_at/
// progress_percent/position_seconds/payload explícitos são persistidos.
func TestCreateHistoricoEventoCamposExplicitos(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	corpo := map[string]any{
		"kind":             "assistir",
		"status":           "stopped",
		"progress_percent": 42.5,
		"position_seconds": 120.0,
		"occurred_at":      "2026-01-01T10:00:00Z",
		"source":           "jellyfin-webhook",
		"payload":          map[string]any{"raw": "x"},
	}
	rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/historico", corpo)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, quero 201 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeHistoricoEvento(t, rec.Body.Bytes())
	eq(t, "source", got.Source, "jellyfin-webhook")
	eq(t, "occurred_at", got.OccurredAt, "2026-01-01T10:00:00Z")
	if got.ProgressPercent == nil || *got.ProgressPercent != 42.5 {
		t.Errorf("progress_percent = %v, quero 42.5", got.ProgressPercent)
	}
	if got.PositionSeconds == nil || *got.PositionSeconds != 120.0 {
		t.Errorf("position_seconds = %v, quero 120.0", got.PositionSeconds)
	}
	if got.Payload == nil {
		t.Error("payload nil, quero o objeto gravado")
	}
}

// TestCreateHistoricoEventoProgressForaDoIntervalo cobre §5: progress_percent
// fora de [0,100] -> 422, sem criar linha.
func TestCreateHistoricoEventoProgressForaDoIntervalo(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	for _, v := range []float64{-1, 100.1, 200} {
		corpo := map[string]any{"kind": "assistir", "status": "started", "progress_percent": v}
		rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/historico", corpo)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("progress_percent=%v: status = %d, quero 422 (corpo=%s)", v, rec.Code, rec.Body.String())
		}
		checkErrorShape(t, rec, "invalid_body")
	}

	recList := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/historico", nil)
	list := decodeHistoricoList(t, recList.Body.Bytes())
	if len(list.Eventos) != 0 {
		t.Errorf("eventos criados apesar do 422 = %d, quero 0", len(list.Eventos))
	}
}

// TestCreateHistoricoEventoKindStatusVazios cobre §5: kind/status vazios -> 422.
func TestCreateHistoricoEventoKindStatusVazios(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	casos := []map[string]any{
		{"kind": "", "status": "started"},
		{"kind": "assistir", "status": ""},
	}
	for i, c := range casos {
		rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/historico", c)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("caso %d: status = %d, quero 422 (corpo=%s)", i, rec.Code, rec.Body.String())
		}
		checkErrorShape(t, rec, "invalid_body")
	}
}

// TestCreateHistoricoEvento404EntityInexistente cobre §5: 404 para todos os
// 4 endpoints quando a entity não existe.
func TestHistorico404EntityInexistente(t *testing.T) {
	h, _ := setupEntitiesServer(t)
	inexistente := "nao-existe"

	rec := do(t, h, http.MethodPost, "/api/v1/entities/"+inexistente+"/historico",
		map[string]any{"kind": "assistir", "status": "started"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST: status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")

	rec = do(t, h, http.MethodGet, "/api/v1/entities/"+inexistente+"/historico", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET lista: status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")

	rec = do(t, h, http.MethodGet, "/api/v1/entities/"+inexistente+"/historico/estado", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET estado: status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")

	rec = do(t, h, http.MethodDelete, "/api/v1/entities/"+inexistente+"/historico/qualquer", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE: status = %d, quero 404 (corpo=%s)", rec.Code, rec.Body.String())
	}
	checkErrorShape(t, rec, "not_found")
}

// TestListHistoricoEventosOrdemEFiltro cobre §5: lista em occurred_at DESC;
// ?kind= filtra corretamente.
func TestListHistoricoEventosOrdemEFiltro(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	criar := func(kind, status, occurredAt string) {
		corpo := map[string]any{"kind": kind, "status": status, "occurred_at": occurredAt}
		rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/historico", corpo)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed POST: status = %d (corpo=%s)", rec.Code, rec.Body.String())
		}
	}
	criar("assistir", "started", "2026-01-01T10:00:00Z")
	criar("ouvir", "started", "2026-01-01T11:00:00Z")
	criar("assistir", "stopped", "2026-01-01T12:00:00Z")

	// Sem filtro: 3 eventos, DESC (mais recente primeiro).
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/historico", nil)
	list := decodeHistoricoList(t, rec.Body.Bytes())
	if len(list.Eventos) != 3 {
		t.Fatalf("len(eventos) = %d, quero 3", len(list.Eventos))
	}
	if list.Eventos[0].OccurredAt != "2026-01-01T12:00:00Z" || list.Eventos[2].OccurredAt != "2026-01-01T10:00:00Z" {
		t.Errorf("ordem inesperada: %v", list.Eventos)
	}

	// Filtro por kind=assistir: só 2.
	rec = do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/historico?kind=assistir", nil)
	list = decodeHistoricoList(t, rec.Body.Bytes())
	if len(list.Eventos) != 2 {
		t.Fatalf("len(eventos) kind=assistir = %d, quero 2", len(list.Eventos))
	}
	for _, e := range list.Eventos {
		if e.Kind != "assistir" {
			t.Errorf("evento com kind %q no filtro assistir", e.Kind)
		}
	}
}

// TestListHistoricoEventosVazio cobre §3: {"eventos":[]} quando não há
// nenhum evento (não 404, entity existe).
func TestListHistoricoEventosVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/historico", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	list := decodeHistoricoList(t, rec.Body.Bytes())
	if len(list.Eventos) != 0 {
		t.Errorf("len(eventos) = %d, quero 0", len(list.Eventos))
	}
}

// TestGetHistoricoEstadoDerivaUltimoPorKind cobre §5: para cada kind, o
// evento mais recente (occurred_at) vence -- inclusive com eventos de kinds
// diferentes intercalados FORA da ordem de inserção.
func TestGetHistoricoEstadoDerivaUltimoPorKind(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	criar := func(kind, status, occurredAt string) {
		corpo := map[string]any{"kind": kind, "status": status, "occurred_at": occurredAt}
		rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/historico", corpo)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed POST: status = %d (corpo=%s)", rec.Code, rec.Body.String())
		}
	}
	// Insercao INTERCALADA e fora de ordem cronologica: o mais recente de
	// "assistir" (14h) e inserido ANTES do mais antigo (10h); "ouvir" so
	// tem um evento.
	criar("assistir", "completed", "2026-01-01T14:00:00Z")
	criar("ouvir", "started", "2026-01-01T09:00:00Z")
	criar("assistir", "started", "2026-01-01T10:00:00Z")
	criar("assistir", "paused", "2026-01-01T12:00:00Z")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/historico/estado", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeHistoricoEstado(t, rec.Body.Bytes())
	if len(got.Estado) != 2 {
		t.Fatalf("len(estado) = %d, quero 2 (assistir, ouvir): %v", len(got.Estado), got.Estado)
	}
	assistir, ok := got.Estado["assistir"]
	if !ok {
		t.Fatal("estado sem chave 'assistir'")
	}
	if assistir.Status != "completed" || assistir.OccurredAt != "2026-01-01T14:00:00Z" {
		t.Errorf("assistir = %+v, quero status=completed occurred_at=2026-01-01T14:00:00Z", assistir)
	}
	ouvir, ok := got.Estado["ouvir"]
	if !ok {
		t.Fatal("estado sem chave 'ouvir'")
	}
	if ouvir.Status != "started" {
		t.Errorf("ouvir.status = %q, quero started", ouvir.Status)
	}
}

// TestGetHistoricoEstadoVazio cobre §5: objeto vazio quando não há histórico.
func TestGetHistoricoEstadoVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/historico/estado", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeHistoricoEstado(t, rec.Body.Bytes())
	if len(got.Estado) != 0 {
		t.Errorf("len(estado) = %d, quero 0", len(got.Estado))
	}
}

// TestDeleteHistoricoEvento cobre §5: DELETE remove o evento; um segundo
// DELETE do mesmo id retorna 404.
func TestDeleteHistoricoEvento(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/historico",
		map[string]any{"kind": "assistir", "status": "started"})
	criado := decodeHistoricoEvento(t, rec.Body.Bytes())

	recDel := do(t, h, http.MethodDelete, "/api/v1/entities/"+id+"/historico/"+criado.ID, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, quero 204 (corpo=%s)", recDel.Code, recDel.Body.String())
	}

	// Segundo DELETE do mesmo id -> 404.
	recDel2 := do(t, h, http.MethodDelete, "/api/v1/entities/"+id+"/historico/"+criado.ID, nil)
	if recDel2.Code != http.StatusNotFound {
		t.Errorf("segundo DELETE: status = %d, quero 404 (corpo=%s)", recDel2.Code, recDel2.Body.String())
	}
	checkErrorShape(t, recDel2, "not_found")

	// Confirma que sumiu da listagem.
	recList := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/historico", nil)
	list := decodeHistoricoList(t, recList.Body.Bytes())
	if len(list.Eventos) != 0 {
		t.Errorf("len(eventos) apos delete = %d, quero 0", len(list.Eventos))
	}
}

// TestDeleteHistoricoEventoDeOutraEntity cobre §4: DELETE de um evento que
// pertence a OUTRA entity -> 404 (não vaza cross-entity).
func TestDeleteHistoricoEventoDeOutraEntity(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id1 := semearEntity(t, d, "e1", "movie", "Filme 1")
	id2 := semearEntity(t, d, "e2", "movie", "Filme 2")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id1+"/historico",
		map[string]any{"kind": "assistir", "status": "started"})
	criado := decodeHistoricoEvento(t, rec.Body.Bytes())

	recDel := do(t, h, http.MethodDelete, "/api/v1/entities/"+id2+"/historico/"+criado.ID, nil)
	if recDel.Code != http.StatusNotFound {
		t.Errorf("status = %d, quero 404 (corpo=%s)", recDel.Code, recDel.Body.String())
	}
	checkErrorShape(t, recDel, "not_found")
}

// TestHistoricoEventoCascadeAoApagarEntity cobre §5: apagar a entity remove
// em cascata todo o historico_evento dela (ON DELETE CASCADE).
func TestHistoricoEventoCascadeAoApagarEntity(t *testing.T) {
	h, d := setupEntitiesServer(t)
	id := semearEntity(t, d, "e1", "movie", "Filme 1")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/"+id+"/historico",
		map[string]any{"kind": "assistir", "status": "started"})
	criado := decodeHistoricoEvento(t, rec.Body.Bytes())

	if err := sqlcgen.New(d.Write).ApagarEntity(context.Background(), id); err != nil {
		t.Fatalf("ApagarEntity: %v", err)
	}

	_, err := sqlcgen.New(d.Read).BuscarHistoricoEventoPorID(context.Background(), criado.ID)
	if err == nil {
		t.Error("evento ainda existe apos apagar a entity, quero CASCADE")
	}
}
