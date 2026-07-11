package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// Testes do descritor de formulário editorial (briefing 0030). Amarram os DOIS
// registros declarativos: a fonte única de campos (resolver.CamposEditaveis) e o
// registro de apresentação (registroApresentacao). A CATRACA DE COBERTURA garante
// que nunca haja campo editável (kind != extensao) sem descritor.

// TestDescritorCobreTodosEditaveis é a catraca de extensibilidade: TODO campo de
// resolver.CamposEditaveis() com kind != extensao DEVE ter descritor. Se um campo
// editável novo entrar no resolver sem apresentação, este teste FALHA — forçando o
// par "campo novo ⇒ descritor". O extension_data (kind=extensao) é excluído de
// propósito (não é campo de formulário simples).
func TestDescritorCobreTodosEditaveis(t *testing.T) {
	descritores := descritoresDeCampo()
	temDescritor := make(map[string]bool, len(descritores))
	for _, d := range descritores {
		temDescritor[d.Name] = true
	}

	for campo, info := range resolver.CamposEditaveis() {
		if info.Kind == resolver.KindExtensao {
			// extension_data: editável, mas NÃO é campo de formulário simples;
			// excluído dos descritores por regra (briefing 0030).
			if temDescritor[campo] {
				t.Errorf("campo %q (kind=extensao) NÃO deveria ter descritor", campo)
			}
			continue
		}
		if !temDescritor[campo] {
			t.Errorf("campo editável %q (kind=%s) SEM descritor de apresentação — "+
				"adicione-o em registroApresentacao", campo, info.Kind)
		}
	}
}

// TestDescritorOrdemCrescente confere que a lista sai ORDENADA por `order`
// crescente e estável (contrato da resposta do endpoint).
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

// TestDescritorWidgetCoerente confere que o widget de cada descritor é coerente
// com o Kind público do resolver: lista → tags; numero → number; escalar →
// text|textarea|date (formas de escalar). Impede um widget incompatível com a
// classe de composição do campo.
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

// buscarDescritoresHTTP sobe o handler e faz o GET do endpoint, devolvendo a
// resposta já decodificada. Compartilhado pelos testes de HTTP abaixo (não precisa
// de banco: o metadado é estático).
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

// exigeFloat confere que um *float64 opcional está presente e vale o esperado.
func exigeFloat(t *testing.T, got *float64, want float64, rotulo string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s ausente, quero %v", rotulo, want)
	}
	if *got != want {
		t.Errorf("%s = %v, quero %v", rotulo, *got, want)
	}
}

// TestGetEntityFieldsSchemaHTTP cobre o handler: 200 + JSON com os 6 campos na
// ordem certa.
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

// TestGetEntityFieldsSchemaFaixaNumerica confere que o local_rating (number)
// carrega a faixa min/max/step espelhada do backend, e que um escalar (title) NÃO
// carrega faixa (omitida).
func TestGetEntityFieldsSchemaFaixaNumerica(t *testing.T) {
	resp := buscarDescritoresHTTP(t)

	lr := resp.Fields[len(resp.Fields)-1]
	if lr.Name != "local_rating" || lr.Widget != "number" {
		t.Fatalf("último campo = %+v, quero local_rating/number", lr)
	}
	exigeFloat(t, lr.Min, 0, "local_rating.min")
	exigeFloat(t, lr.Max, 10, "local_rating.max")
	exigeFloat(t, lr.Step, 0.1, "local_rating.step")

	// Um campo escalar (title) NÃO carrega min/max/step (omitidos).
	title := resp.Fields[0]
	if title.Min != nil || title.Max != nil || title.Step != nil {
		t.Errorf("title não deveria ter min/max/step: %+v", title)
	}
}
