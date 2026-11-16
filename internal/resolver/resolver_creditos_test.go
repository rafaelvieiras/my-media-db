package resolver

import (
	"encoding/json"
	"testing"
)

// Tests of the CREDITS OVERRIDE (cast/crew/production companies, ADR-0013): the
// SAME non-destructive guarantee as the other editable fields
// (title/tags/local_rating) — override ⟵ externo, never loses the provider's base,
// `null` erases without destroying the record (it is reversible: clearing the
// override is enough). It uses entRica() (entity with a TMDB id) and an external
// layer of its own with several credits, to exercise
// reorder/remove/edit/add.

// camadaCreditosMulti returns a canonical external layer with 3 cast credits,
// 3 crew credits (two of the SAME source_id "10" with different jobs —
// Director+Writer, the case that justifies the composite key) and 1 production
// company.
func camadaCreditosMulti() []CamadaExterna {
	return []CamadaExterna{{Fonte: "tmdb", Campos: map[string]any{
		"cast": []any{
			map[string]any{"source_id": "1", "name": "Ana", "character": "Heroina", "order": 0},
			map[string]any{"source_id": "2", "name": "Bento", "character": "Vilao", "order": 1},
			map[string]any{"source_id": "3", "name": "Clara", "character": "Coadjuvante", "order": 2},
		},
		"crew": []any{
			map[string]any{"source_id": "10", "name": "Dirigente", "department": "Directing", "job": "Director"},
			map[string]any{"source_id": "10", "name": "Dirigente", "department": "Writing", "job": "Writer"},
			map[string]any{"source_id": "20", "name": "Produtor", "department": "Production", "job": "Producer"},
		},
		"companies": []any{
			map[string]any{"source_id": "100", "name": "StudioX"},
		},
	}}}
}

// resolverComCreditos builds the resolved Entidade with the multi-credit layer and
// the given override (nil = no override).
func resolverComCreditos(t *testing.T, override map[string]json.RawMessage) Entidade {
	t.Helper()
	return Resolver(Camadas{
		Entity:          entRica(),
		Cadeia:          []string{"pt-BR", "en"},
		LangPedido:      "pt-BR",
		IdiomaPrincipal: "pt-BR",
		Externo:         camadaCreditosMulti(),
		Override:        override,
	})
}

// TestCreditosSemOverridePreservaBaseExterna: with no override, cast/crew/companies
// come ENTIRELY from the external base — parity with the read-only behavior of
// Phase B (no regression).
func TestCreditosSemOverridePreservaBaseExterna(t *testing.T) {
	got := resolverComCreditos(t, nil)

	if len(got.Cast) != 3 || got.Cast[0].SourceID != "1" || got.Cast[2].Character != "Coadjuvante" {
		t.Fatalf("cast = %+v, quero 3 creditos da base", got.Cast)
	}
	if len(got.Crew) != 3 || got.Crew[0].Job != "Director" || got.Crew[1].Job != "Writer" {
		t.Fatalf("crew = %+v, quero 3 creditos (Director/Writer/Producer)", got.Crew)
	}
	if len(got.Companies) != 1 || got.Companies[0].Name != "StudioX" {
		t.Fatalf("companies = %+v, quero 1 produtora (StudioX)", got.Companies)
	}
}

// TestCreditosOverrideReorderCastReescreveOrder: a cast reorder is reflected in the
// final order of the list AND in the Order field (0-based) of each item.
func TestCreditosOverrideReorderCastReescreveOrder(t *testing.T) {
	got := resolverComCreditos(t, ov(map[string]string{
		"credits": `{"cast":{"reorder":["3","1"]}}`,
	}))

	if len(got.Cast) != 3 {
		t.Fatalf("cast = %+v, quero 3 creditos", got.Cast)
	}
	// Reorder: 3, 1 first (in the given order); 2 (not mentioned) comes afterwards.
	quero := []struct {
		sourceID string
		order    int
	}{{"3", 0}, {"1", 1}, {"2", 2}}
	for i, w := range quero {
		if got.Cast[i].SourceID != w.sourceID {
			t.Errorf("cast[%d].SourceID = %q, quero %q", i, got.Cast[i].SourceID, w.sourceID)
		}
		if got.Cast[i].Order != w.order {
			t.Errorf("cast[%d].Order = %d, quero %d", i, got.Cast[i].Order, w.order)
		}
	}
	// crew/companies untouched: they stay intact (a sub-key absent from the patch
	// inherits the base).
	if len(got.Crew) != 3 || len(got.Companies) != 1 {
		t.Errorf("crew/companies deveriam seguir intactos: crew=%+v companies=%+v", got.Crew, got.Companies)
	}
}

// TestCreditosOverrideRemoveCrewChaveComposta: a crew remove uses the composite key
// source_id|job — it removes only that credit, preserving the others of the
// SAME source_id with a different job (the Director+Writer case).
func TestCreditosOverrideRemoveCrewChaveComposta(t *testing.T) {
	got := resolverComCreditos(t, ov(map[string]string{
		"credits": `{"crew":{"remove":["10|Director"]}}`,
	}))

	if len(got.Crew) != 2 {
		t.Fatalf("crew = %+v, quero 2 creditos (removeu so o Director)", got.Crew)
	}
	achouWriter := false
	for _, c := range got.Crew {
		if c.SourceID == "10" && c.Job == "Director" {
			t.Errorf("crew ainda tem 10|Director, deveria ter sido removido: %+v", c)
		}
		if c.SourceID == "10" && c.Job == "Writer" {
			achouWriter = true
		}
	}
	if !achouWriter {
		t.Error("crew perdeu 10|Writer (deveria sobreviver, e um credito distinto de 10|Director)")
	}
}

// TestCreditosOverrideEditPreservaDemaisCampos: edit fixes one field of the
// credit (character) preserving the others (name/order) intact.
func TestCreditosOverrideEditPreservaDemaisCampos(t *testing.T) {
	got := resolverComCreditos(t, ov(map[string]string{
		"credits": `{"cast":{"edit":{"2":{"character":"Antagonista Revisado"}}}}`,
	}))

	var alvo *struct {
		Name      string
		Character string
		Order     int
	}
	for _, c := range got.Cast {
		if c.SourceID == "2" {
			alvo = &struct {
				Name      string
				Character string
				Order     int
			}{c.Name, c.Character, c.Order}
		}
	}
	if alvo == nil {
		t.Fatal("nao achei o credito 2 no cast editado")
	}
	eq(t, "character editado", alvo.Character, "Antagonista Revisado")
	eq(t, "name preservado", alvo.Name, "Bento")
	eq(t, "order preservado", alvo.Order, 1)
}

// TestCreditosOverrideAddCreditoCustom: add appends a CUSTOM credit (no provider, an
// id chosen by the owner) to the result.
func TestCreditosOverrideAddCreditoCustom(t *testing.T) {
	got := resolverComCreditos(t, ov(map[string]string{
		"credits": `{"crew":{"add":[{"source_id":"custom-1","name":"Fulano de Tal","department":"Direcao","job":"Assistente de Direcao"}]}}`,
	}))

	if len(got.Crew) != 4 {
		t.Fatalf("crew = %+v, quero 4 creditos (3 base + 1 custom)", got.Crew)
	}
	achou := false
	for _, c := range got.Crew {
		if c.SourceID == "custom-1" {
			achou = true
			eq(t, "custom.Name", c.Name, "Fulano de Tal")
			eq(t, "custom.Job", c.Job, "Assistente de Direcao")
			eq(t, "custom.Department", c.Department, "Direcao")
		}
	}
	if !achou {
		t.Error("credito custom-1 nao apareceu no crew")
	}
}

// TestCreditosOverrideNullApagaTudo: `credits: null` erases ALL credits
// (cast/crew/companies come out as empty []), but does NOT destroy the external
// base permanently — it is reversible (clearing the override returns to the
// inheritance, tested through a Resolver with no override in the same test).
func TestCreditosOverrideNullApagaTudo(t *testing.T) {
	got := resolverComCreditos(t, ov(map[string]string{
		"credits": `null`,
	}))
	if len(got.Cast) != 0 || len(got.Crew) != 0 || len(got.Companies) != 0 {
		t.Fatalf("com credits=null, cast/crew/companies deveriam ser [], veio cast=%+v crew=%+v companies=%+v",
			got.Cast, got.Crew, got.Companies)
	}
	if got.Cast == nil || got.Crew == nil || got.Companies == nil {
		t.Error("cast/crew/companies devem ser [] (nao nil) mesmo com credits=null")
	}

	// Non-destructive: with no override, the external base remains alive (the owner
	// did not lose the provider credits just for having zeroed the override before).
	semOverride := resolverComCreditos(t, nil)
	if len(semOverride.Cast) != 3 || len(semOverride.Crew) != 3 || len(semOverride.Companies) != 1 {
		t.Fatalf("base externa deveria seguir intacta para outra resolucao sem override, veio cast=%d crew=%d companies=%d",
			len(semOverride.Cast), len(semOverride.Crew), len(semOverride.Companies))
	}
}

// TestCreditosSubChaveAusenteHerdaBase: a patch with only `crew` leaves
// cast/companies UNTOUCHED (they inherit the external base entirely).
func TestCreditosSubChaveAusenteHerdaBase(t *testing.T) {
	got := resolverComCreditos(t, ov(map[string]string{
		"credits": `{"crew":{"remove":["20|Producer"]}}`,
	}))

	if len(got.Cast) != 3 {
		t.Errorf("cast deveria seguir intacto (sub-chave ausente), veio %+v", got.Cast)
	}
	if len(got.Companies) != 1 || got.Companies[0].Name != "StudioX" {
		t.Errorf("companies deveria seguir intacto (sub-chave ausente), veio %+v", got.Companies)
	}
	if len(got.Crew) != 2 {
		t.Errorf("crew deveria ter perdido o Producer, veio %+v", got.Crew)
	}
}

// TestCreditosEditCrewMudaJobNaoQuebraReorder: editing the `job` of a crew credit
// (a field that PARTICIPATES in the composite key source_id|job itself) must not
// "lose" the item from the reorder just for having changed key in the middle of the
// patch -- the reorder must match the key BEFORE the edit (the same one the front
// end computed when building the patch), otherwise the edited item would be pushed
// to the end of the list just for having been edited (bug fixed: reordenarCreditos
// used to receive the POST-edit key, which did not match the front end's reorder).
func TestCreditosEditCrewMudaJobNaoQuebraReorder(t *testing.T) {
	got := resolverComCreditos(t, ov(map[string]string{
		"credits": `{"crew":{
			"edit":{"10|Director":{"job":"Diretor Geral"}},
			"reorder":["20|Producer","10|Director","10|Writer"]
		}}`,
	}))

	if len(got.Crew) != 3 {
		t.Fatalf("crew = %+v, quero 3 creditos (so uma edicao, nada removido/adicionado)", got.Crew)
	}
	quero := []struct {
		sourceID string
		job      string
	}{{"20", "Producer"}, {"10", "Diretor Geral"}, {"10", "Writer"}}
	for i, w := range quero {
		if got.Crew[i].SourceID != w.sourceID || got.Crew[i].Job != w.job {
			t.Errorf("crew[%d] = {SourceID:%q Job:%q}, quero {SourceID:%q Job:%q}",
				i, got.Crew[i].SourceID, got.Crew[i].Job, w.sourceID, w.job)
		}
	}
}

// TestCamposEditaveisIncluiCredits: credits IS editable (it enters
// CamposEditaveis, PATCH/DELETE accept the key), NEUTRAL (not translatable — editing
// character/job/department does not compose per language in this phase) and of the
// KindCreditos class.
func TestCamposEditaveisIncluiCredits(t *testing.T) {
	info, ok := CamposEditaveis()["credits"]
	if !ok {
		t.Fatal("credits deveria ser editavel (override de creditos, Fase C)")
	}
	if info.Traduzivel {
		t.Error("credits e NEUTRO (character/job/department nao compoem por idioma nesta fase)")
	}
	if info.Kind != KindCreditos {
		t.Errorf("credits kind = %q, quero %q", info.Kind, KindCreditos)
	}
}
