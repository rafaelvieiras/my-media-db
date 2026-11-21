package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/execqueue"
)

// registroGravado records the snapshots handed to the queue, so a test can assert
// what a step NARRATES (issue #98: the screen showed a step with no bar because the
// sweep reported a counter without a denominator).
type registroGravado struct {
	progressos []execqueue.Progresso
	eventos    []execqueue.Evento
}

func (r *registroGravado) Progresso(p execqueue.Progresso) { r.progressos = append(r.progressos, p) }
func (r *registroGravado) Evento(e execqueue.Evento)       { r.eventos = append(r.eventos, e) }

// TestRecalcularCompletudeNarraPosicaoETotal covers the denominator of the bar: the
// sweep walks EVERY entity (the draft is counted in the position and skipped), and
// every snapshot carries the total, so the screen can draw a moving bar.
func TestRecalcularCompletudeNarraPosicaoETotal(t *testing.T) {
	h, d := setupEntitiesServer(t)
	corpo := catalogCompletenessSettings{"movie": {Description: true}}
	if rec := do(t, h, http.MethodPut, "/api/v1/settings/catalog-completeness", corpo); rec.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	semearEntity(t, d, "m1", "movie", "Sem sinopse")
	semearEntity(t, d, "d1", "movie", "Draft")
	definirStatus(t, d, "d1", "draft")

	reg := &registroGravado{}
	avaliados, _, err := New("v-teste", d).RecalcularCompletudeCatalogacao(context.Background(), reg)
	if err != nil {
		t.Fatalf("RecalcularCompletudeCatalogacao: %v", err)
	}
	if avaliados != 1 {
		t.Fatalf("avaliados = %d, quero 1 (o draft é pulado)", avaliados)
	}
	if len(reg.progressos) != 2 {
		t.Fatalf("progressos = %d, quero 2 (um por entity andada, draft incluído)", len(reg.progressos))
	}
	for i, p := range reg.progressos {
		if p.Total != 2 {
			t.Errorf("progresso[%d].Total = %d, quero 2 (o denominador da barra)", i, p.Total)
		}
		if p.Item == "" {
			t.Errorf("progresso[%d].Item vazio, quero o título da entity", i)
		}
	}
	if reg.progressos[0].Atual != 1 || reg.progressos[1].Atual != 2 {
		t.Errorf("Atual = %d,%d, quero 1,2 (a posição anda mesmo quando o item é pulado)",
			reg.progressos[0].Atual, reg.progressos[1].Atual)
	}
}
