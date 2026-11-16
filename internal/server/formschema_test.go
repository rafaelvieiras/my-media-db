package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// Tests of the editorial form descriptor. They tie the TWO declarative
// registries together: the single source of fields (resolver.CamposEditaveis) and the
// presentation registry (registroApresentacao). The COVERAGE RATCHET guarantees
// that there is never an editable field (kind != extensao) without a descriptor.

// TestDescritorCobreTodosEditaveis is the extensibility ratchet: EVERY field of
// resolver.CamposEditaveis() with kind != extensao/creditos MUST have a descriptor. If
// a new editable field enters the resolver without presentation, this test FAILS —
// forcing the pair "new field ⇒ descriptor". extension_data (kind=extensao) and
// credits (kind=creditos) are excluded on purpose (they are not
// simple form fields — they get a dedicated editor in the front end).
func TestDescritorCobreTodosEditaveis(t *testing.T) {
	descritores := descritoresDeCampo()
	temDescritor := make(map[string]bool, len(descritores))
	for _, d := range descritores {
		temDescritor[d.Name] = true
	}

	for campo, info := range resolver.CamposEditaveis() {
		if info.Kind == resolver.KindExtensao || info.Kind == resolver.KindCreditos {
			// extension_data/credits: editable, but they are NOT simple form
			// fields; excluded from the descriptors on purpose.
			if temDescritor[campo] {
				t.Errorf("campo %q (kind=%s) NÃO deveria ter descritor", campo, info.Kind)
			}
			continue
		}
		if !temDescritor[campo] {
			t.Errorf("campo editável %q (kind=%s) SEM descritor de apresentação — "+
				"adicione-o em registroApresentacao", campo, info.Kind)
		}
	}
}

// TestDescritorOrdemCrescente checks that the list comes out SORTED by `order`
// ascending and stable (the response contract of the endpoint).
func TestDescritorOrdemCrescente(t *testing.T) {
	descritores := descritoresDeCampo()
	if len(descritores) < 2 {
		t.Fatalf("esperava vários descritores, obtive %d", len(descritores))
	}
	for i := 1; i < len(descritores); i++ {
		if descritores[i].Order < descritores[i-1].Order {
			t.Errorf("ordem não crescente: %q (order=%d) antes de %q (order=%d)",
				descritores[i-1].Name, descritores[i-1].Order,
				descritores[i].Name, descritores[i].Order)
		}
	}
}

// TestDescritorWidgetCoerente checks that the widget of each descriptor is coherent
// with the public Kind of the resolver: list → tags; numero → number; escalar →
// text|textarea|date (scalar forms). It prevents a widget incompatible with the
// composition class of the field.
func TestDescritorWidgetCoerente(t *testing.T) {
	escalares := map[string]bool{"text": true, "textarea": true, "date": true}
	for _, d := range descritoresDeCampo() {
		switch resolver.Kind(d.Kind) {
		case resolver.KindLista:
			if d.Widget != "tags" {
				t.Errorf("campo %q kind=lista tem widget %q, quero tags", d.Name, d.Widget)
			}
		case resolver.KindNumero:
			if d.Widget != "number" {
				t.Errorf("campo %q kind=numero tem widget %q, quero number", d.Name, d.Widget)
			}
		case resolver.KindEscalar:
			if !escalares[d.Widget] {
				t.Errorf("campo %q kind=escalar tem widget %q, quero text|textarea|date", d.Name, d.Widget)
			}
		default:
			t.Errorf("campo %q com kind inesperado %q no descritor", d.Name, d.Kind)
		}
	}
}

// buscarDescritoresHTTP starts the handler and performs the GET of the endpoint, returning
// the already decoded response. Shared by the HTTP tests below (it does not need
// a database: the metadata is static).
func buscarDescritoresHTTP(t *testing.T) fieldSchemaResponse {
	t.Helper()
	h, err := New("v-teste", nil).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := do(t, h, http.MethodGet, "/api/v1/schema/entity-fields", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var resp fieldSchemaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decodificar resposta: %v (corpo=%s)", err, rec.Body.String())
	}
	return resp
}

// exigeFloat checks that an optional *float64 is present and has the expected
// value.
func exigeFloat(t *testing.T, got *float64, want float64, rotulo string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s ausente, quero %v", rotulo, want)
	}
	if *got != want {
		t.Errorf("%s = %v, quero %v", rotulo, *got, want)
	}
}

// TestGetEntityFieldsSchemaHTTP covers the handler: 200 + JSON with the 6 fields in
// the right order.
func TestGetEntityFieldsSchemaHTTP(t *testing.T) {
	resp := buscarDescritoresHTTP(t)
	quero := []string{"title", "description", "tags", "original_title", "release_date", "local_rating"}
	if len(resp.Fields) != len(quero) {
		t.Fatalf("quero %d campos, obtive %d (%+v)", len(quero), len(resp.Fields), resp.Fields)
	}
	for i, nome := range quero {
		if resp.Fields[i].Name != nome {
			t.Errorf("posição %d: name = %q, quero %q", i, resp.Fields[i].Name, nome)
		}
	}
}

// TestGetEntityFieldsSchemaFaixaNumerica checks that local_rating (number)
// carries the min/max/step range mirrored from the backend, and that a scalar (title) does
// NOT carry a range (omitted).
func TestGetEntityFieldsSchemaFaixaNumerica(t *testing.T) {
	resp := buscarDescritoresHTTP(t)

	lr := resp.Fields[len(resp.Fields)-1]
	if lr.Name != "local_rating" || lr.Widget != "number" {
		t.Fatalf("último campo = %+v, quero local_rating/number", lr)
	}
	exigeFloat(t, lr.Min, 0, "local_rating.min")
	exigeFloat(t, lr.Max, 10, "local_rating.max")
	exigeFloat(t, lr.Step, 0.1, "local_rating.step")

	// A scalar field (title) does NOT carry min/max/step (omitted).
	title := resp.Fields[0]
	if title.Min != nil || title.Max != nil || title.Step != nil {
		t.Errorf("title não deveria ter min/max/step: %+v", title)
	}
}
