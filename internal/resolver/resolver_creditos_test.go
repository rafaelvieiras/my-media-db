package resolver

import (
	"encoding/json"
	"testing"
)

// Testes do OVERRIDE DE CRÉDITOS (elenco/equipe/produtoras, briefing 0029 Fase
// C): a MESMA garantia não-destrutiva dos demais campos editáveis
// (title/tags/local_rating) — override ⟵ externo, nunca perde a base do
// provider, `null` apaga sem destruir o registro (é reversível: basta limpar o
// override). Usa entRica() (entity com id TMDB) e uma camada externa própria com
// vários créditos, para exercitar reorder/remove/edit/add.

// camadaCreditosMulti devolve uma camada externa canônica com 3 créditos de
// elenco, 3 de equipe (dois do MESMO source_id "10" com jobs diferentes —
// Diretor+Roteirista, o caso que justifica a chave composta) e 1 produtora.
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

// resolverComCreditos monta a Entidade resolvida com a camada multi-crédito e o
// override informado (nil = sem override).
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

// TestCreditosSemOverridePreservaBaseExterna: sem override, cast/crew/companies
// vêm INTEGRALMENTE da base externa — paridade com o comportamento read-only da
// Fase B (não regride).
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

// TestCreditosOverrideReorderCastReescreveOrder: reorder no cast reflete na
// ordem final da lista E no campo Order (0-based) de cada item.
func TestCreditosOverrideReorderCastReescreveOrder(t *testing.T) {
	got := resolverComCreditos(t, ov(map[string]string{
		"credits": `{"cast":{"reorder":["3","1"]}}`,
	}))

	if len(got.Cast) != 3 {
		t.Fatalf("cast = %+v, quero 3 creditos", got.Cast)
	}
	// Reorder: 3, 1 primeiro (na ordem dada); 2 (nao mencionado) vem depois.
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
	// crew/companies nao tocados: seguem intactos (sub-chave ausente herda base).
	if len(got.Crew) != 3 || len(got.Companies) != 1 {
		t.Errorf("crew/companies deveriam seguir intactos: crew=%+v companies=%+v", got.Crew, got.Companies)
	}
}

// TestCreditosOverrideRemoveCrewChaveComposta: remove no crew usa a chave
// composta source_id|job — remove só aquele credito, preservando os demais do
// MESMO source_id com job diferente (o caso Diretor+Roteirista).
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

// TestCreditosOverrideEditPreservaDemaisCampos: edit corrige um campo do
// credito (character) preservando os demais (name/order) intactos.
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

// TestCreditosOverrideAddCreditoCustom: add acrescenta um credito CUSTOM (sem
// provider, id escolhido pelo dono) ao resultado.
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

// TestCreditosOverrideNullApagaTudo: `credits: null` apaga TODOS os creditos
// (cast/crew/companies saem vazios []), mas NAO destroi a base externa
// permanentemente — e reversivel (limpar o override volta a heranca, testado
// via Resolver sem override no mesmo teste).
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

	// Nao-destrutivo: sem override, a base externa segue viva (o dono nao perdeu
	// os creditos do provider so por ter zerado o override antes).
	semOverride := resolverComCreditos(t, nil)
	if len(semOverride.Cast) != 3 || len(semOverride.Crew) != 3 || len(semOverride.Companies) != 1 {
		t.Fatalf("base externa deveria seguir intacta para outra resolucao sem override, veio cast=%d crew=%d companies=%d",
			len(semOverride.Cast), len(semOverride.Crew), len(semOverride.Companies))
	}
}

// TestCreditosSubChaveAusenteHerdaBase: um patch so com `crew` deixa
// cast/companies INTOCADOS (herdam a base externa integralmente).
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

// TestCreditosEditCrewMudaJobNaoQuebraReorder: editar o `job` de um credito de
// equipe (campo que PARTICIPA da propria chave composta source_id|job) nao pode
// "perder" o item do reorder so por ter mudado de chave no meio do patch -- o
// reorder deve casar com a chave ANTES do edit (a mesma que o front calculou ao
// montar o patch), senao o item editado seria empurrado para o fim da lista so
// por ter sido editado (bug corrigido: reordenarCreditos recebia a chave
// POS-edit, que nao batia com o reorder do front).
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

// TestCamposEditaveisIncluiCredits: credits E editavel (entra em
// CamposEditaveis, PATCH/DELETE aceitam a chave), NEUTRO (nao traduzivel — a
// edicao de character/job/department nao compoe por idioma nesta fase) e da
// classe KindCreditos.
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
