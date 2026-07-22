package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Testes do EDITOR DE OVERRIDE por idioma (briefing 0016). Sem rede: setup só com
// banco migrado (sem provider) — a base externa fica vazia, então o override compõe
// sobre a base ARQUIVO. As settings caem no default seguro (pt-BR principal, en
// fallback, [pt-BR, en] permitidos), ativando o modo idioma no resolver.

// patchOv envia um PATCH de override com corpo JSON cru (para controlar null/aninhados).
func patchOv(t *testing.T, h http.Handler, id, lang, corpo string) *httptest.ResponseRecorder {
	t.Helper()
	alvo := "/api/v1/entities/" + id + "/override"
	if lang != "" {
		alvo += "?lang=" + lang
	}
	return do(t, h, http.MethodPatch, alvo, []byte(corpo))
}

// getBlob lê o blob CRU de override como mapa campo→raw.
func getBlob(t *testing.T, h http.Handler, id string) map[string]json.RawMessage {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/entities/"+id+"/override", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET override status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decodificar blob: %v (corpo=%s)", err, rec.Body.String())
	}
	return m
}

// Critério §5: "Editar o título em pt-BR grava override.title['pt-BR'] e o resolver
// passa a servi-lo só em pt-BR; ler em en não é afetado."
func TestPatchTituloPorIdiomaIndependente(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base Title")

	rec := patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	ptResp := decodeEntidade(t, rec)
	eq(t, "resposta.title", ptResp.Title, "Duna")
	eq(t, "resposta.lang", ptResp.Lang, "pt-BR")

	// Blob gravado é o mapa por idioma.
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title", string(blob["title"]), `{"pt-BR":"Duna"}`)

	// pt-BR serve o override; en cai na base ARQUIVO (não afetado).
	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title", pt.Title, "Duna")
	eq(t, "pt.resolved.title", pt.ResolvedLanguages["title"], "pt-BR")

	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	eq(t, "en.title", en.Title, "Base Title")
	eq(t, "en.resolved.title", en.ResolvedLanguages["title"], "")
}

// Critério §5: "Merge de 2 níveis preserva os outros idiomas do mesmo campo."
func TestPatchMergeDoisNiveisPreservaIdiomas(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	if rec := patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH pt status = %d", rec.Code)
	}
	if rec := patchOv(t, h, "e1", "en", `{"title":"Dune"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH en status = %d", rec.Code)
	}

	blob := getBlob(t, h, "e1")
	// json.Marshal ordena as chaves; en antes de pt-BR.
	eq(t, "blob.title", string(blob["title"]), `{"en":"Dune","pt-BR":"Duna"}`)

	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title", pt.Title, "Duna")
	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	eq(t, "en.title", en.Title, "Dune")
}

// Critério §5: "Limpar override (DELETE …/{campo}?lang=) ≠ definir vazio (null no
// idioma)." Limpar volta a herdar a base; null apaga a base e interrompe o fallback.
func TestLimparDiferenteDeDefinirVazio(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base Title")

	// DEFINIR VAZIO: null no idioma. title pt-BR fica vazio e interrompe o fallback.
	if rec := patchOv(t, h, "e1", "pt-BR", `{"title":null}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH null status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title(null)", string(blob["title"]), `{"pt-BR":null}`)
	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title(vazio)", pt.Title, "") // apagado, NÃO caiu na base
	eq(t, "pt.resolved.title(vazio)", pt.ResolvedLanguages["title"], "pt-BR")

	// LIMPAR: DELETE do campo naquele idioma. Volta a HERDAR a base ARQUIVO.
	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE campo status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	// O mapa do campo esvaziou → a chave campo saiu; blob inteiro esvaziou → linha removida.
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob após limpar = %v, quero {}", b)
	}
	pt2 := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title(herdado)", pt2.Title, "Base Title")
	eq(t, "pt.resolved.title(herdado)", pt2.ResolvedLanguages["title"], "")
}

// Critério §5: "tags editam por {add, remove} dentro do idioma."
func TestPatchTagsPorIdioma(t *testing.T) {
	h, d := setupEntitiesServer(t)
	// Base ARQUIVO com uma tag ("Guerra") para o remove morder.
	if _, err := escrita(t, d).CriarEntity(context.Background(), sqlcgen.CriarEntityParams{
		ID: "e1", Type: "movie", Title: "Base", TagsJson: nsOf(`["Guerra","Drama"]`),
	}); err != nil {
		t.Fatalf("CriarEntity: %v", err)
	}

	if rec := patchOv(t, h, "e1", "pt-BR", `{"tags":{"add":["Classico"],"remove":["Guerra"]}}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH tags status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.tags", string(blob["tags"]), `{"pt-BR":{"add":["Classico"],"remove":["Guerra"]}}`)

	// pt-BR: base ARQUIVO [Guerra, Drama] - Guerra + Classico = [Drama, Classico].
	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	if len(pt.Tags) != 2 || pt.Tags[0] != "Drama" || pt.Tags[1] != "Classico" {
		t.Errorf("pt.tags = %v, quero [Drama Classico]", pt.Tags)
	}
	// en: sem override, a base ARQUIVO segue viva.
	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	if len(en.Tags) != 2 || en.Tags[0] != "Guerra" || en.Tags[1] != "Drama" {
		t.Errorf("en.tags = %v, quero [Guerra Drama]", en.Tags)
	}
}

// Critério §5: "Campo neutro grava escalar único (sem idioma) e vale em qualquer lang."
func TestPatchCampoNeutroEscalar(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	// O lang é IGNORADO para o campo neutro.
	if rec := patchOv(t, h, "e1", "pt-BR", `{"original_title":"Dune"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH neutro status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.original_title", string(blob["original_title"]), `"Dune"`)

	// Vale em qualquer idioma.
	for _, lang := range []string{"pt-BR", "en"} {
		e := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang="+lang, nil))
		if e.OriginalTitle == nil || *e.OriginalTitle != "Dune" {
			t.Errorf("original_title(%s) = %v, quero Dune", lang, e.OriginalTitle)
		}
	}

	// Um segundo PATCH em outro idioma sobrescreve o escalar (não vira mapa).
	if rec := patchOv(t, h, "e1", "en", `{"original_title":"Dune2"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH neutro #2 status = %d", rec.Code)
	}
	eq(t, "blob.original_title#2", string(getBlob(t, h, "e1")["original_title"]), `"Dune2"`)
}

// Critério §5: "Migração de override escalar legado → {principal:…}." Ao fundir um
// novo idioma sobre um override legado (sem chave de idioma), migra para o principal.
func TestPatchMigraLegadoParaPrincipal(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")
	// Semeia um override LEGADO direto (title como escalar, sem chave de idioma).
	if _, err := escrita(t, d).UpsertMediaOverride(context.Background(), sqlcgen.UpsertMediaOverrideParams{
		EntityID: "e1", OverrideJson: nsOf(`{"title":"Legado"}`),
	}); err != nil {
		t.Fatalf("seed legado: %v", err)
	}

	// PATCH em en deve migrar o legado para o principal (pt-BR) e adicionar en.
	if rec := patchOv(t, h, "e1", "en", `{"title":"English"}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH migra status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title(migrado)", string(blob["title"]), `{"en":"English","pt-BR":"Legado"}`)

	pt := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	eq(t, "pt.title(migrado)", pt.Title, "Legado")
	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	eq(t, "en.title(migrado)", en.Title, "English")
}

// Critério §5: "Patch malformado / chave não-editorial / lang não permitido são
// rejeitados (422/400)."
func TestPatchValidacoes(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	casos := []struct {
		nome   string
		lang   string
		corpo  string
		status int
		code   string
	}{
		{"chave nao editorial", "pt-BR", `{"status":"cataloged"}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"escalar numero", "pt-BR", `{"title":123}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"tags forma invalida", "pt-BR", `{"tags":{"foo":["x"]}}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"extension_data array", "pt-BR", `{"extension_data":[1,2]}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"nota local string", "pt-BR", `{"local_rating":"9"}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"nota local fora da faixa", "pt-BR", `{"local_rating":11}`, http.StatusUnprocessableEntity, "invalid_body"},
		{"corpo malformado", "pt-BR", `{nao-json`, http.StatusUnprocessableEntity, "invalid_body"},
		{"lang nao permitido", "xx", `{"title":"y"}`, http.StatusBadRequest, "invalid_query"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rec := patchOv(t, h, "e1", c.lang, c.corpo)
			if rec.Code != c.status {
				t.Fatalf("status = %d, quero %d (corpo=%s)", rec.Code, c.status, rec.Body.String())
			}
			checkErrorShape(t, rec, c.code)
		})
	}
	// Uma chave inválida no meio de válidas NÃO grava nada (validação antes do merge).
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob após rejeições = %v, quero {} (nada gravado)", b)
	}
}

// TestPatchCreditsValido cobre um PATCH de credits (briefing 0029 Fase C) com as
// operações válidas do exemplo do openapi.yaml (OverridePatch): reorder+edit no
// cast, remove+add no crew. 200, grava a chave `credits` no blob, e o `add`
// aparece no resultado resolvido — o campo é NEUTRO (o lang é ignorado).
func TestPatchCreditsValido(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	corpo := `{"credits":{
		"cast": {"reorder": ["6193"], "edit": {"6193": {"character": "Paul Atreides"}}},
		"crew": {"remove": ["1892|Producer"], "add": [
			{"source_id": "custom-1", "name": "Fulano de Tal", "department": "Direcao", "job": "Assistente de Direcao"}
		]}
	}}`
	rec := patchOv(t, h, "e1", "pt-BR", corpo)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH credits status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if _, ok := getBlob(t, h, "e1")["credits"]; !ok {
		t.Fatalf("blob deveria ter a chave credits: %v", getBlob(t, h, "e1"))
	}

	// Sem provider/base externa neste teste, so o `add` de crew aparece; o campo é
	// NEUTRO (vale em qualquer lang, sem consultar a cadeia de idioma).
	en := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=en", nil))
	if len(en.Crew) != 1 || en.Crew[0].SourceID != "custom-1" || en.Crew[0].Job != "Assistente de Direcao" {
		t.Fatalf("crew = %+v, quero 1 credito custom-1 (o add, ja que a base externa esta vazia)", en.Crew)
	}
}

// TestPatchCreditsFormaInvalida cobre as formas REJEITADAS (422) do patch de
// credits: operação com tipo errado (reorder/edit), add sem os campos
// obrigatórios, kind de sub-bloco desconhecido, e credits/sub-bloco que não é
// objeto.
func TestPatchCreditsFormaInvalida(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	casos := []struct {
		nome  string
		corpo string
	}{
		{"reorder nao-array", `{"credits":{"cast":{"reorder":"6193"}}}`},
		{"edit nao-objeto", `{"credits":{"cast":{"edit":["x"]}}}`},
		{"add sem source_id", `{"credits":{"crew":{"add":[{"name":"Fulano"}]}}}`},
		{"add sem name", `{"credits":{"crew":{"add":[{"source_id":"c1"}]}}}`},
		{"kind de sub-bloco desconhecido", `{"credits":{"director":{"remove":["1"]}}}`},
		{"credits nao-objeto", `{"credits":"foo"}`},
		{"sub-bloco nao-objeto", `{"credits":{"cast":"foo"}}`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rec := patchOv(t, h, "e1", "pt-BR", c.corpo)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, quero 422 (corpo=%s)", rec.Code, rec.Body.String())
			}
			checkErrorShape(t, rec, "invalid_body")
		})
	}
	// Nenhuma das rejeições grava nada (validação antes do merge).
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob após rejeições = %v, quero {} (nada gravado)", b)
	}
}

// TestPatchNotaLocalDefinirELimpar cobre a NOTA LOCAL do dono (ADR-0012): definir a
// nota grava o número neutro (sem idioma) e o resolvido a expõe em local_rating;
// LIMPAR (DELETE sem lang) remove a chave e a nota volta a null (não-destrutivo).
func TestPatchNotaLocalDefinirELimpar(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	// Definir a nota (aceita 1 casa; o lang é IGNORADO — campo neutro).
	rec := patchOv(t, h, "e1", "pt-BR", `{"local_rating":8.5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH nota status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeEntidade(t, rec)
	if got.LocalRating == nil || *got.LocalRating != 8.5 {
		t.Fatalf("local_rating = %v, quero 8.5", got.LocalRating)
	}
	// Guardada como escalar numérico direto (sem mapa por idioma).
	eq(t, "blob.local_rating", string(getBlob(t, h, "e1")["local_rating"]), `8.5`)

	// LIMPAR (DELETE sem lang): remove a chave; a nota volta a null.
	del := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/local_rating", nil)
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE nota status = %d, quero 200 (corpo=%s)", del.Code, del.Body.String())
	}
	limpa := decodeEntidade(t, do(t, h, http.MethodGet, "/api/v1/entities/e1?lang=pt-BR", nil))
	if limpa.LocalRating != nil {
		t.Errorf("local_rating apos limpar = %v, quero null", limpa.LocalRating)
	}
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob apos limpar = %v, quero {}", b)
	}
}

// Critério §5: "GET devolve o blob cru." E {} quando não há override.
func TestGetBlobCru(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	// Sem override → {}.
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob inicial = %v, quero {}", b)
	}
	// Após patches em campos distintos → blob cru com ambas as chaves.
	_ = patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	_ = patchOv(t, h, "e1", "pt-BR", `{"original_title":"Dune"}`)
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title", string(blob["title"]), `{"pt-BR":"Duna"}`)
	eq(t, "blob.original_title", string(blob["original_title"]), `"Dune"`)
}

// Critério §5 / §4: "DELETE reset apaga o override inteiro."
func TestDeleteReset(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base Title")
	_ = patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	_ = patchOv(t, h, "e1", "en", `{"title":"Dune"}`)

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE reset status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob após reset = %v, quero {}", b)
	}
	// Resolve de volta na base.
	e := decodeEntidade(t, rec)
	eq(t, "reset.title", e.Title, "Base Title")
}

// DELETE de campo traduzível SEM lang remove a chave campo inteira (todos idiomas).
func TestDeleteCampoSemLangRemoveTudo(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base Title")
	_ = patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	_ = patchOv(t, h, "e1", "en", `{"title":"Dune"}`)

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE sem lang status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	if b := getBlob(t, h, "e1"); len(b) != 0 {
		t.Errorf("blob = %v, quero {} (campo inteiro removido)", b)
	}
}

// DELETE de um idioma específico preserva os outros idiomas do campo.
func TestDeleteCampoPorIdiomaPreservaOutros(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")
	_ = patchOv(t, h, "e1", "pt-BR", `{"title":"Duna"}`)
	_ = patchOv(t, h, "e1", "en", `{"title":"Dune"}`)

	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title?lang=en", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE en status = %d (corpo=%s)", rec.Code, rec.Body.String())
	}
	blob := getBlob(t, h, "e1")
	eq(t, "blob.title(sem en)", string(blob["title"]), `{"pt-BR":"Duna"}`)
}

// DELETE de chave ausente é idempotente (200), e {campo} não editorial → 422.
func TestDeleteCampoIdempotenteEValidacao(t *testing.T) {
	h, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Base")

	// Ausente: idempotente.
	rec := do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title?lang=pt-BR", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE ausente status = %d, quero 200", rec.Code)
	}
	// Campo não editorial → 422.
	rec = do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/status", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("DELETE status(nao editorial) = %d, quero 422", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_body")
	// lang presente mas fora dos permitidos → 400.
	rec = do(t, h, http.MethodDelete, "/api/v1/entities/e1/override/title?lang=xx", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE lang invalido = %d, quero 400", rec.Code)
	}
	checkErrorShape(t, rec, "invalid_query")
}

// Entity inexistente → 404 em PATCH e GET override.
func TestOverrideEntityInexistente404(t *testing.T) {
	h, _ := setupEntitiesServer(t)

	rec := patchOv(t, h, "naoexiste", "pt-BR", `{"title":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH inexistente = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")

	rec = do(t, h, http.MethodGet, "/api/v1/entities/naoexiste/override", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET inexistente = %d, quero 404", rec.Code)
	}
	checkErrorShape(t, rec, "not_found")
}
